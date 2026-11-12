//! Agent — Server-Hub + Client (Remote Device Agent).
//! Protocol as in Go: JSON-RPC over WebSocket, Message{type,action,params,data(base64),meta,error,status}.

use crate::fsops;
use crate::model::{ListOptions};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use tokio::sync::oneshot;

pub const MAX_AGENT_MESSAGE_BYTES: usize = 64 << 20;

mod b64_bytes {
    use base64::{engine::general_purpose::STANDARD, Engine as _};
    use serde::{Deserialize, Deserializer, Serializer};

    pub fn serialize<S: Serializer>(v: &[u8], s: S) -> Result<S::Ok, S::Error> {
        if v.is_empty() {
            return s.serialize_none();
        }
        s.serialize_str(&STANDARD.encode(v))
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<Vec<u8>, D::Error> {
        let opt: Option<String> = Option::deserialize(d)?;
        match opt {
            None => Ok(Vec::new()),
            Some(s) => STANDARD.decode(s.as_bytes()).map_err(serde::de::Error::custom),
        }
    }

    pub fn is_empty(v: &[u8]) -> bool {
        v.is_empty()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Message {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(rename = "type")]
    pub typ: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub action: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub params: Option<HashMap<String, String>>,
    #[serde(
        default,
        skip_serializing_if = "b64_bytes::is_empty",
        with = "b64_bytes"
    )]
    pub data: Vec<u8>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub meta: Option<serde_json::Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub status: i64,
}

fn is_zero(v: &i64) -> bool {
    *v == 0
}

impl Message {
    pub fn err(status: i64, err: &str) -> Message {
        Message {
            id: None,
            typ: String::new(),
            action: None,
            params: None,
            data: Vec::new(),
            meta: None,
            error: Some(err.to_string()),
            status,
        }
    }
    pub fn ok(status: i64, meta: Option<serde_json::Value>) -> Message {
        Message {
            id: None,
            typ: String::new(),
            action: None,
            params: None,
            data: Vec::new(),
            meta,
            error: None,
            status,
        }
    }
}

// ── Server-Hub ──────────────────────────────────────────

pub type HeartbeatCb = Arc<dyn Fn(i64, &str, &str, &str, i64) + Send + Sync>;

struct Session {
    tx: tokio::sync::mpsc::UnboundedSender<String>,
    pending: Mutex<HashMap<String, oneshot::Sender<Message>>>,
    closed: std::sync::atomic::AtomicBool,
}

pub struct Hub {
    sessions: Mutex<HashMap<i64, Arc<Session>>>,
    req_seq: std::sync::atomic::AtomicU64,
    on_heartbeat: HeartbeatCb,
}

impl Hub {
    pub fn new(on_heartbeat: HeartbeatCb) -> Hub {
        Hub {
            sessions: Mutex::new(HashMap::new()),
            req_seq: std::sync::atomic::AtomicU64::new(0),
            on_heartbeat,
        }
    }

    pub fn is_online(&self, agent_id: i64) -> bool {
        self.sessions.lock().unwrap().contains_key(&agent_id)
    }

    pub fn disconnect(&self, agent_id: i64) {
        let sess = self.sessions.lock().unwrap().remove(&agent_id);
        if let Some(s) = sess {
            s.closed.store(true, std::sync::atomic::Ordering::SeqCst);
        }
    }

    pub async fn serve_session(self: &Arc<Hub>, agent_id: i64, ws: axum::extract::ws::WebSocket) {
        use futures_util::{SinkExt, StreamExt};
        let (mut sink, mut stream) = ws.split();
        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel::<String>();
        let sess = Arc::new(Session {
            tx: tx.clone(),
            pending: Mutex::new(HashMap::new()),
            closed: std::sync::atomic::AtomicBool::new(false),
        });
        {
            let mut g = self.sessions.lock().unwrap();
            if let Some(old) = g.insert(agent_id, sess.clone()) {
                old.closed.store(true, std::sync::atomic::Ordering::SeqCst);
            }
        }
        let send_task = tokio::spawn(async move {
            while let Some(msg) = rx.recv().await {
                if sink.send(axum::extract::ws::Message::Text(msg.into())).await.is_err() {
                    break;
                }
            }
        });
        while let Some(res) = stream.next().await {
            let msg = match res {
                Ok(axum::extract::ws::Message::Text(t)) => {
                    match serde_json::from_str::<Message>(&t) {
                        Ok(m) => m,
                        Err(_) => continue,
                    }
                }
                Ok(axum::extract::ws::Message::Binary(b)) => {
                    match serde_json::from_slice::<Message>(&b) {
                        Ok(m) => m,
                        Err(_) => continue,
                    }
                }
                _ => continue,
            };
            match msg.typ.as_str() {
                "hello" => {
                    if let Some(p) = &msg.params {
                        let usage: i64 = p.get("disk_usage").and_then(|s| s.parse().ok()).unwrap_or(0);
                        (self.on_heartbeat)(
                            agent_id,
                            p.get("hostname").map(|s| s.as_str()).unwrap_or(""),
                            p.get("os_info").map(|s| s.as_str()).unwrap_or(""),
                            p.get("root_path").map(|s| s.as_str()).unwrap_or(""),
                            usage,
                        );
                    }
                }
                "res" => {
                    if let Some(id) = &msg.id {
                        if let Some(ch) = sess.pending.lock().unwrap().remove(id) {
                            let _ = ch.send(msg);
                        }
                    }
                }
                _ => {}
            }
        }
        send_task.abort();
        sess.closed.store(true, std::sync::atomic::Ordering::SeqCst);
        let mut g = self.sessions.lock().unwrap();
        if let Some(cur) = g.get(&agent_id) {
            if Arc::ptr_eq(cur, &sess) {
                g.remove(&agent_id);
            }
        }
    }

    pub async fn request(
        &self,
        agent_id: i64,
        action: &str,
        params: HashMap<String, String>,
        data: Vec<u8>,
    ) -> Result<Message, String> {
        let sess = {
            let g = self.sessions.lock().unwrap();
            match g.get(&agent_id) {
                Some(s) => s.clone(),
                None => return Err("agent offline".to_string()),
            }
        };
        if sess.closed.load(std::sync::atomic::Ordering::SeqCst) {
            return Err("agent offline".to_string());
        }
        let req_id = (self.req_seq.fetch_add(1, std::sync::atomic::Ordering::SeqCst) + 1).to_string();
        let (tx, rx) = oneshot::channel::<Message>();
        sess.pending.lock().unwrap().insert(req_id.clone(), tx);
        let msg = Message {
            id: Some(req_id.clone()),
            typ: "req".to_string(),
            action: Some(action.to_string()),
            params: Some(params),
            data,
            meta: None,
            error: None,
            status: 0,
        };
        let json = serde_json::to_string(&msg).map_err(|e| e.to_string())?;
        if sess.tx.send(json).is_err() {
            sess.pending.lock().unwrap().remove(&req_id);
            return Err("agent offline".to_string());
        }
        match tokio::time::timeout(std::time::Duration::from_secs(120), rx).await {
            Ok(Ok(res)) => Ok(res),
            Ok(Err(_)) => Err("agent disconnected".to_string()),
            Err(_) => {
                sess.pending.lock().unwrap().remove(&req_id);
                Err("agent request timed out".to_string())
            }
        }
    }
}

// ── Client (runs on the remote devices) ───────────────

pub fn ws_url(server_url: &str) -> String {
    let u = server_url.trim().trim_end_matches('/');
    let mut u = if let Some(rest) = u.strip_prefix("https://") {
        format!("wss://{}", rest)
    } else if let Some(rest) = u.strip_prefix("http://") {
        format!("ws://{}", rest)
    } else if u.starts_with("ws://") || u.starts_with("wss://") {
        u.to_string()
    } else {
        format!("ws://{}", u)
    };
    if !u.ends_with("/api/agent/ws") {
        u.push_str("/api/agent/ws");
    }
    u
}

pub async fn run_client(server_url: &str, token: &str, root_path: &str) {
    std::fs::create_dir_all(root_path).unwrap_or_else(|e| {
        eprintln!("cannot create agent root {}: {}", root_path, e);
        std::process::exit(1);
    });
    let root_abs = std::path::Path::new(root_path)
        .canonicalize()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|_| root_path.to_string());
    let endpoint = ws_url(server_url);
    loop {
        match connect_and_serve(&endpoint, token, &root_abs).await {
            Ok(()) => {}
            Err(e) => eprintln!("⚠️  Agent connection lost ({}) — reconnecting in 3s...", e),
        }
        tokio::time::sleep(std::time::Duration::from_secs(3)).await;
    }
}

async fn connect_and_serve(endpoint: &str, token: &str, root_abs: &str) -> Result<(), String> {
    use futures_util::{SinkExt, StreamExt};
    use tokio_tungstenite::tungstenite::client::IntoClientRequest;
    let mut req = endpoint.into_client_request().map_err(|e| e.to_string())?;
    req.headers_mut().insert(
        "Authorization",
        format!("Bearer {}", token.trim()).parse().map_err(|e| format!("{}", e))?,
    );
    let (mut ws, _) = tokio_tungstenite::connect_async(req)
        .await
        .map_err(|e| e.to_string())?;
    let (mut sink, mut stream) = ws.split();
    let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel::<String>();

    let send_hello = |tx: &tokio::sync::mpsc::UnboundedSender<String>| {
        let hostname = std::fs::read_to_string("/etc/hostname")
            .map(|s| s.trim().to_string())
            .unwrap_or_default();
        let usage = fsops::dir_size(root_abs).unwrap_or(0);
        let mut params = HashMap::new();
        params.insert("hostname".to_string(), hostname);
        params.insert("os_info".to_string(), format!("{}/{}", std::env::consts::OS, std::env::consts::ARCH));
        params.insert("root_path".to_string(), root_abs.to_string());
        params.insert("disk_usage".to_string(), usage.to_string());
        let msg = Message {
            id: None,
            typ: "hello".to_string(),
            action: None,
            params: Some(params),
            data: Vec::new(),
            meta: None,
            error: None,
            status: 0,
        };
        let _ = tx.send(serde_json::to_string(&msg).unwrap_or_default());
    };
    send_hello(&tx);
    eprintln!("✅ Fileship Agent connected to {} (Root: {})", endpoint, root_abs);

    let heartbeat_tx = tx.clone();
    let root_hb = root_abs.to_string();
    let hb_task = tokio::spawn(async move {
        let mut tick = tokio::time::interval(std::time::Duration::from_secs(30));
        loop {
            tick.tick().await;
            let usage = fsops::dir_size(&root_hb).unwrap_or(0);
            let hostname = std::fs::read_to_string("/etc/hostname")
                .map(|s| s.trim().to_string())
                .unwrap_or_default();
            let mut params = HashMap::new();
            params.insert("hostname".to_string(), hostname);
            params.insert("os_info".to_string(), format!("{}/{}", std::env::consts::OS, std::env::consts::ARCH));
            params.insert("root_path".to_string(), root_hb.clone());
            params.insert("disk_usage".to_string(), usage.to_string());
            let msg = Message {
                id: None,
                typ: "hello".to_string(),
                action: None,
                params: Some(params),
                data: Vec::new(),
                meta: None,
                error: None,
                status: 0,
            };
            if heartbeat_tx.send(serde_json::to_string(&msg).unwrap_or_default()).is_err() {
                break;
            }
        }
    });

    let send_task = tokio::spawn(async move {
        while let Some(msg) = rx.recv().await {
            if sink
                .send(tokio_tungstenite::tungstenite::Message::Text(msg.into()))
                .await
                .is_err()
            {
                break;
            }
        }
    });

    // Read loop runs here; responses go via tx
    let tx2 = tx.clone();
    let root_owned = root_abs.to_string();
    while let Some(res) = stream.next().await {
        let msg = match res {
            Ok(tokio_tungstenite::tungstenite::Message::Text(t)) => {
                match serde_json::from_str::<Message>(&t) {
                    Ok(m) => m,
                    Err(_) => continue,
                }
            }
            Ok(tokio_tungstenite::tungstenite::Message::Binary(b)) => {
                match serde_json::from_slice::<Message>(&b) {
                    Ok(m) => m,
                    Err(_) => continue,
                }
            }
            _ => continue,
        };
        if msg.typ != "req" {
            continue;
        }
        let tx3 = tx2.clone();
        let root3 = root_owned.clone();
        tokio::spawn(async move {
            let mut res = execute_action(&root3, &msg);
            res.id = msg.id.clone();
            res.typ = "res".to_string();
            let _ = tx3.send(serde_json::to_string(&res).unwrap_or_default());
        });
    }
    hb_task.abort();
    send_task.abort();
    Err("connection closed".to_string())
}

pub fn execute_action(root_abs: &str, req: &Message) -> Message {
    let empty = HashMap::new();
    let p = req.params.as_ref().unwrap_or(&empty);
    let get = |k: &str| p.get(k).map(|s| s.as_str()).unwrap_or("");
    match req.action.as_deref().unwrap_or("") {
        "list" => {
            let page: i64 = get("page").parse().unwrap_or(0);
            let per_page: i64 = get("per_page").parse().unwrap_or(0);
            let opts = ListOptions {
                path: get("path").to_string(),
                search: get("search").to_string(),
                sort_by: get("sort_by").to_string(),
                sort_asc: get("sort_asc") != "false",
                page,
                per_page,
            };
            match fsops::list_paged_filtered(root_abs, &opts, None::<&fn(&crate::model::FileInfo) -> bool>) {
                Ok(res) => Message::ok(200, serde_json::to_value(&res).ok()),
                Err(e) => Message::err(400, &e.to_string()),
            }
        }
        "search" => match fsops::search(root_abs, get("q")) {
            Ok(res) => Message::ok(200, serde_json::to_value(&res).ok()),
            Err(e) => Message::err(500, &e.to_string()),
        },
        "info" => {
            let rel = get("path");
            let abs = match fsops::resolve(root_abs, rel) {
                Ok(a) => a,
                Err(e) => return Message::err(400, &e.to_string()),
            };
            let md = match std::fs::metadata(&abs) {
                Ok(m) => m,
                Err(e) => return Message::err(404, &e.to_string()),
            };
            let name = std::path::Path::new(&abs)
                .file_name()
                .map(|n| n.to_string_lossy().to_string())
                .unwrap_or_default();
            let mod_time = md
                .modified()
                .ok()
                .map(|t| chrono::DateTime::<chrono::Local>::from(t))
                .unwrap_or_else(chrono::Local::now);
            let mut out = serde_json::json!({
                "name": name,
                "path": rel,
                "size": md.len() as i64,
                "is_dir": md.is_dir(),
                "mod_time": crate::model::fmt_rfc3339(&mod_time),
            });
            if md.is_dir() {
                let count = std::fs::read_dir(&abs).map(|d| d.count()).unwrap_or(0);
                let size = fsops::dir_size(&abs).unwrap_or(0);
                out["item_count"] = serde_json::json!(count);
                out["size"] = serde_json::json!(size);
            } else {
                let mime = fsops::detect_mime(root_abs, rel).unwrap_or_default();
                out["mime_type"] = serde_json::json!(mime);
                if md.len() <= (50 << 20) {
                    if let Ok(data) = std::fs::read(&abs) {
                        use sha2::{Digest, Sha256};
                        let mut h = Sha256::new();
                        h.update(&data);
                        out["sha256"] = serde_json::json!(hex::encode(h.finalize()));
                    }
                }
            }
            Message::ok(200, Some(out))
        }
        "mkdir" => {
            if fsops::clean_relative_path(get("path")).is_err() {
                return Message::err(400, "path outside root");
            }
            match fsops::mkdir(root_abs, get("path")) {
                Ok(()) => Message::ok(201, None),
                Err(e) => Message::err(400, &e.to_string()),
            }
        }
        "touch" => {
            if fsops::clean_relative_path(get("path")).is_err() {
                return Message::err(400, "path outside root");
            }
            match fsops::create_file(root_abs, get("path")) {
                Ok(()) => Message::ok(201, None),
                Err(e) => Message::err(400, &e.to_string()),
            }
        }
        "delete" => match fsops::delete(root_abs, get("path")) {
            Ok(()) => Message::ok(204, None),
            Err(e) => Message::err(400, &e.to_string()),
        },
        "rename" | "move" => match fsops::rename(root_abs, get("src"), get("dst")) {
            Ok(()) => Message::ok(204, None),
            Err(e) => Message::err(400, &e.to_string()),
        },
        "copy" => match fsops::copy(root_abs, get("src"), get("dst")) {
            Ok(()) => Message::ok(201, None),
            Err(e) => Message::err(400, &e.to_string()),
        },
        "read_text" => match fsops::read_text_file(root_abs, get("path")) {
            Ok(c) => Message::ok(200, Some(serde_json::json!({"content": c}))),
            Err(e) => Message::err(400, &e.to_string()),
        },
        "write_text" => match fsops::write_text_file(root_abs, get("path"), get("content")) {
            Ok(()) => Message::ok(204, None),
            Err(e) => Message::err(400, &e.to_string()),
        },
        "read_file" => {
            let clean = match fsops::clean_relative_path(get("path")) {
                Ok(c) => c,
                Err(e) => return Message::err(400, &e.to_string()),
            };
            let abs = match fsops::resolve(root_abs, &clean) {
                Ok(a) => a,
                Err(e) => return Message::err(403, &e.to_string()),
            };
            let md = match std::fs::metadata(&abs) {
                Ok(m) => m,
                Err(e) => return Message::err(404, &e.to_string()),
            };
            if !md.file_type().is_file() {
                return Message::err(400, "not a regular file");
            }
            if md.len() > (MAX_AGENT_MESSAGE_BYTES as u64) - (1024 * 1024) {
                return Message::err(413, "file too large for agent stream");
            }
            let data = match std::fs::read(&abs) {
                Ok(d) => d,
                Err(e) => return Message::err(500, &e.to_string()),
            };
            let mime = fsops::detect_mime(root_abs, &clean).unwrap_or_default();
            let name = std::path::Path::new(&abs)
                .file_name()
                .map(|n| n.to_string_lossy().to_string())
                .unwrap_or_default();
            let mut params = HashMap::new();
            params.insert("mime_type".to_string(), mime);
            params.insert("name".to_string(), name);
            Message {
                id: None,
                typ: String::new(),
                action: None,
                params: Some(params),
                data,
                meta: None,
                error: None,
                status: 200,
            }
        }
        "write_file" => {
            let clean = match fsops::clean_relative_path(get("path")) {
                Ok(c) => c,
                Err(e) => return Message::err(400, &e.to_string()),
            };
            let mut data: &[u8] = &req.data;
            match fsops::save_upload(root_abs, &clean, &mut data) {
                Ok(()) => Message::ok(201, None),
                Err(e) => Message::err(500, &e.to_string()),
            }
        }
        "thumb" => {
            let clean = match fsops::clean_relative_path(get("path")) {
                Ok(c) => c,
                Err(e) => return Message::err(400, &e.to_string()),
            };
            let thumb_dir = format!("{}/.fileship_thumbs", root_abs);
            let name = if let Some(existing) = fsops::thumbnail_exists(root_abs, &thumb_dir, &clean) {
                existing
            } else if fsops::is_image(&clean) {
                match fsops::generate_thumbnail(root_abs, &clean, &thumb_dir) {
                    Ok(n) => n,
                    Err(e) => return Message::err(500, &e.to_string()),
                }
            } else {
                return Message::err(404, "not an image");
            };
            let data = match std::fs::read(format!("{}/{}", thumb_dir, name)) {
                Ok(d) => d,
                Err(e) => return Message::err(404, &e.to_string()),
            };
            let mut params = HashMap::new();
            params.insert("mime_type".to_string(), "image/jpeg".to_string());
            Message {
                id: None,
                typ: String::new(),
                action: None,
                params: Some(params),
                data,
                meta: None,
                error: None,
                status: 200,
            }
        }
        other => Message::err(400, &format!("unknown action: {}", other)),
    }
}
