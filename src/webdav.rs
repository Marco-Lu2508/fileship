//! WebDAV — Port von internal/handler/webdav.go (x/net/webdav-Verhalten).
//! Auth: Bearer-JWT oder Basic-Auth (2FA nur Bearer). Pfad-Validierung wie safeWebDAVFileSystem.

use crate::fsops;
use crate::handler::AppState;
use crate::middleware::err_resp;
use axum::body::Body;
use axum::extract::State;
use axum::http::{header, HeaderMap, Request, StatusCode};
use axum::response::Response;
use std::path::Path;

pub async fn webdav_handler(State(state): State<AppState>, req: Request<Body>) -> Response {
    // ── Auth ──
    let root: String;
    let user_id: i64;
    let is_admin: bool;
    let bearer = req
        .headers()
        .get(header::AUTHORIZATION)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    if let Some(tok) = bearer.strip_prefix("Bearer ") {
        let claims = match crate::auth::parse_access_token(&state.cfg.jwt_secret, tok) {
            Some(c) if !c.two_fa_pending => c,
            _ => return err_resp(StatusCode::UNAUTHORIZED, "unauthorized"),
        };
        let user = match state.db.get_user_by_id(claims.user_id) {
            Some(u) => u,
            None => return err_resp(StatusCode::UNAUTHORIZED, "unauthorized"),
        };
        user_id = user.id;
        is_admin = user.is_admin;
        root = resolve_root(&state, &user.root_path);
    } else {
        // Basic Auth
        let (username, password) = match basic_auth(&req) {
            Some(v) => v,
            None => {
                let mut res = err_resp(StatusCode::UNAUTHORIZED, "unauthorized");
                res.headers_mut().insert(
                    header::WWW_AUTHENTICATE,
                    header::HeaderValue::from_static("Basic realm=\"Fileship WebDAV\""),
                );
                return res;
            }
        };
        let user = match state.db.get_user_by_username(&username) {
            Some(u) => u,
            None => {
                let mut res = err_resp(StatusCode::UNAUTHORIZED, "unauthorized");
                res.headers_mut().insert(
                    header::WWW_AUTHENTICATE,
                    header::HeaderValue::from_static("Basic realm=\"Fileship WebDAV\""),
                );
                return res;
            }
        };
        if !crate::auth::verify_password(&user.password_hash, &password) {
            let mut res = err_resp(StatusCode::UNAUTHORIZED, "unauthorized");
            res.headers_mut().insert(
                header::WWW_AUTHENTICATE,
                header::HeaderValue::from_static("Basic realm=\"Fileship WebDAV\""),
            );
            return res;
        }
        if state.db.totp_is_enabled(user.id) {
            return err_resp(
                StatusCode::UNAUTHORIZED,
                "2fa_required: use a completed Bearer login for WebDAV",
            );
        }
        state
            .db
            .log_action(user.id, &user.username, &format!("webdav:{}", req.method()), req.uri().path(), &peer(&req));
        user_id = user.id;
        is_admin = user.is_admin;
        root = resolve_root(&state, &user.root_path);
    }

    // ── Restriktionen ──
    if !is_admin {
        let perms = state.db.list_path_permissions(user_id);
        let (quota, allowed_types) = state.db.get_user_quota_and_types(user_id);
        if !perms.is_empty() || quota > 0 || !allowed_types.is_empty() || !state.cfg.allowed_types.is_empty() {
            return err_resp(
                StatusCode::FORBIDDEN,
                "WebDAV is unavailable while path, storage, or file-type restrictions are configured",
            );
        }
    }

    let method = req.method().clone();
    let method_name = method.as_str();
    let raw_path = req.uri().path().to_string();
    let name = raw_path.strip_prefix("/webdav").unwrap_or("").to_string();

    match method_name {
        "OPTIONS" => {
            let mut res = Response::new(Body::empty());
            res.headers_mut().insert(header::HeaderName::from_static("dav"), header::HeaderValue::from_static("1, 2"));
            res.headers_mut().insert(
                header::ALLOW,
                header::HeaderValue::from_static("OPTIONS, GET, HEAD, POST, PUT, DELETE, TRACE, COPY, MOVE, PROPFIND, PROPPATCH, LOCK, UNLOCK, MKCOL"),
            );
            res
        }
        "PROPFIND" => propfind(&root, &name, &req),
        "GET" | "HEAD" => dav_get(&root, &name, req, method_name == "HEAD").await,
        "PUT" => dav_put(&root, &name, req).await,
        "DELETE" => dav_delete(&root, &name),
        "MKCOL" => dav_mkcol(&root, &name),
        "MOVE" => dav_move_copy(&root, &name, &req, true),
        "COPY" => dav_move_copy(&root, &name, &req, false),
        "LOCK" => dav_lock(),
        "UNLOCK" => {
            let mut res = Response::new(Body::empty());
            *res.status_mut() = StatusCode::NO_CONTENT;
            res
        }
        "PROPPATCH" => dav_proppatch(&root, &name),
        _ => err_resp(StatusCode::METHOD_NOT_ALLOWED, "method not allowed"),
    }
}

fn peer(req: &Request<Body>) -> String {
    crate::middleware::peer_addr(req)
}

fn resolve_root(state: &AppState, user_root_path: &str) -> String {
    if user_root_path == "/" || user_root_path.is_empty() {
        state.cfg.root_path.clone()
    } else {
        user_root_path.to_string()
    }
}

fn basic_auth(req: &Request<Body>) -> Option<(String, String)> {
    let h = req.headers().get(header::AUTHORIZATION)?.to_str().ok()?;
    let b64 = h.strip_prefix("Basic ")?;
    let decoded = base64_decode(b64)?;
    let s = String::from_utf8(decoded).ok()?;
    let (u, p) = s.split_once(':')?;
    Some((u.to_string(), p.to_string()))
}

fn base64_decode(s: &str) -> Option<Vec<u8>> {
    use base64::{engine::general_purpose::STANDARD, Engine as _};
    STANDARD.decode(s).ok()
}

/// safeWebDAVFileSystem::clean + validate aus Go.
fn validate(root: &str, name: &str) -> Result<String, ()> {
    if name.contains('\\') || name.contains('\0') {
        return Err(());
    }
    // path.Clean("/"+name), dann "/" trimmen
    let cleaned = go_path_clean(&format!("/{}", name));
    let trimmed = cleaned.trim_start_matches('/').to_string();
    let rel = if trimmed == "." || trimmed.is_empty() {
        String::new()
    } else {
        match fsops::clean_relative_path(&trimmed) {
            Ok(r) => r,
            Err(_) => return Err(()),
        }
    };
    if !rel.is_empty() {
        fsops::resolve(root, &rel).map_err(|_| ())?;
    }
    Ok(rel)
}

/// Go path.Clean (Slash-Pfad)
fn go_path_clean(p: &str) -> String {
    fsops::go_clean(p)
}

fn href_escape(name: &str) -> String {
    // URL-escapen wie x/net/webdav (path-escapes)
    let mut out = String::new();
    for seg in name.split('/') {
        if !out.is_empty() {
            out.push('/');
        }
        for b in seg.bytes() {
            match b {
                b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => out.push(b as char),
                _ => out.push_str(&format!("%{:02X}", b)),
            }
        }
    }
    out
}

fn http_date(t: &chrono::DateTime<chrono::Local>) -> String {
    // RFC 1123 GMT
    t.with_timezone(&chrono::Utc).format("%a, %d %b %Y %H:%M:%S GMT").to_string()
}

fn propfind(root: &str, name: &str, req: &Request<Body>) -> Response {
    let rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    let abs = if rel.is_empty() {
        root.to_string()
    } else {
        match fsops::resolve(root, &rel) {
            Ok(a) => a,
            Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
        }
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(_) => return err_resp(StatusCode::NOT_FOUND, "not found"),
    };
    let depth = req
        .headers()
        .get("depth")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("infinity")
        .trim()
        .to_string();
    let mut xml = String::from("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<multistatus xmlns=\"DAV:\">\n");
    xml.push_str(&prop_response(&abs, &rel, &md));
    let want_children = md.is_dir() && (depth == "1" || depth == "infinity");
    if want_children {
        if let Ok(entries) = std::fs::read_dir(&abs) {
            let mut names: Vec<String> = entries.flatten().map(|e| e.file_name().to_string_lossy().to_string()).collect();
            names.sort();
            for n in names {
                let child_abs = format!("{}/{}", abs.trim_end_matches('/'), n);
                if let Ok(cmd) = std::fs::metadata(&child_abs) {
                    let child_rel = if rel.is_empty() { n.clone() } else { format!("{}/{}", rel, n) };
                    xml.push_str(&prop_response(&child_abs, &child_rel, &cmd));
                }
            }
        }
    }
    xml.push_str("</multistatus>\n");
    let mut res = Response::new(Body::from(xml));
    *res.status_mut() = StatusCode::MULTI_STATUS;
    res.headers_mut().insert(
        header::CONTENT_TYPE,
        header::HeaderValue::from_static("application/xml; charset=utf-8"),
    );
    res
}

fn prop_response(abs: &str, rel: &str, md: &std::fs::Metadata) -> String {
    let is_dir = md.is_dir();
    let display = if rel.is_empty() {
        // Root: x/net/webdav nutzt Basis des Pfads
        Path::new(abs).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_else(|| abs.to_string())
    } else {
        rel.rsplit('/').next().unwrap_or(rel).to_string()
    };
    let href_path = if rel.is_empty() { "/webdav/".to_string() } else { format!("/webdav/{}{}", href_escape(rel), if is_dir { "/" } else { "" }) };
    let mtime = md
        .modified()
        .ok()
        .map(|t| chrono::DateTime::<chrono::Local>::from(t))
        .unwrap_or_else(chrono::Local::now);
    let mut s = String::new();
    s.push_str("<response>\n");
    s.push_str(&format!("<href>{}</href>\n", xml_escape(&href_path)));
    s.push_str("<propstat>\n<prop>\n");
    s.push_str(&format!("<displayname>{}</displayname>\n", xml_escape(&display)));
    if !is_dir {
        s.push_str(&format!("<getcontentlength>{}</getcontentlength>\n", md.len()));
    }
    s.push_str(&format!("<getlastmodified>{}</getlastmodified>\n", http_date(&mtime)));
    if is_dir {
        s.push_str("<resourcetype><collection/></resourcetype>\n");
    } else {
        s.push_str("<resourcetype/>\n");
    }
    s.push_str("</prop>\n<status>HTTP/1.1 200 OK</status>\n</propstat>\n</response>\n");
    s
}

fn xml_escape(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}

async fn dav_get(root: &str, name: &str, req: Request<Body>, head: bool) -> Response {
    let rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    let check_name = if rel.is_empty() { "/".to_string() } else { rel.clone() };
    let abs = match fsops::resolve(root, &check_name) {
        Ok(a) => a,
        Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(_) => return err_resp(StatusCode::NOT_FOUND, "not found"),
    };
    if md.is_dir() {
        return err_resp(StatusCode::METHOD_NOT_ALLOWED, "method not allowed");
    }
    if head {
        let mut res = Response::new(Body::empty());
        res.headers_mut().insert(header::CONTENT_LENGTH, md.len().to_string().parse().unwrap_or(header::HeaderValue::from_static("0")));
        res.headers_mut().insert(
            header::CONTENT_TYPE,
            header::HeaderValue::from_str(&fsops::mime_type(&rel)).unwrap_or(header::HeaderValue::from_static("application/octet-stream")),
        );
        return res;
    }
    // Range-Support wie http.ServeContent
    crate::handler::serve_file_response(&abs, None, &[], &req)
}

async fn dav_put(root: &str, name: &str, req: Request<Body>) -> Response {
    let rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    if rel.is_empty() {
        return err_resp(StatusCode::METHOD_NOT_ALLOWED, "method not allowed");
    }
    let abs = match fsops::resolve(root, &rel) {
        Ok(a) => a,
        Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    // Parent muss existieren (x/net/webdav: 409)
    if let Some(parent) = Path::new(&abs).parent() {
        let pstr = parent.to_string_lossy().to_string();
        if !Path::new(&pstr).exists() {
            return err_resp(StatusCode::CONFLICT, "parent does not exist");
        }
    }
    let existed = Path::new(&abs).exists();
    let body = match axum::body::to_bytes(req.into_body(), usize::MAX).await {
        Ok(b) => b,
        Err(_) => return err_resp(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    };
    let write_res = if existed {
        std::fs::write(&abs, &body)
    } else {
        std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&abs)
            .and_then(|mut f| {
                use std::io::Write;
                f.write_all(&body)
            })
    };
    match write_res {
        Ok(()) => {
            let mut res = Response::new(Body::empty());
            *res.status_mut() = if existed { StatusCode::NO_CONTENT } else { StatusCode::CREATED };
            res
        }
        Err(_) => err_resp(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    }
}

fn dav_delete(root: &str, name: &str) -> Response {
    let rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    if rel.is_empty() {
        return err_resp(StatusCode::FORBIDDEN, "forbidden");
    }
    let abs = match fsops::resolve(root, &rel) {
        Ok(a) => a,
        Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    if !Path::new(&abs).exists() {
        return err_resp(StatusCode::NOT_FOUND, "not found");
    }
    let res = if Path::new(&abs).is_dir() {
        std::fs::remove_dir_all(&abs)
    } else {
        std::fs::remove_file(&abs)
    };
    match res {
        Ok(()) => {
            let mut r = Response::new(Body::empty());
            *r.status_mut() = StatusCode::NO_CONTENT;
            r
        }
        Err(_) => err_resp(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    }
}

fn dav_mkcol(root: &str, name: &str) -> Response {
    let rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    if rel.is_empty() {
        return err_resp(StatusCode::METHOD_NOT_ALLOWED, "method not allowed");
    }
    let abs = match fsops::resolve(root, &rel) {
        Ok(a) => a,
        Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    match std::fs::create_dir(&abs) {
        Ok(()) => {
            let mut r = Response::new(Body::empty());
            *r.status_mut() = StatusCode::CREATED;
            r
        }
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => err_resp(StatusCode::METHOD_NOT_ALLOWED, "method not allowed"),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => err_resp(StatusCode::CONFLICT, "parent does not exist"),
        Err(_) => err_resp(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    }
}

fn dav_move_copy(root: &str, name: &str, req: &Request<Body>, is_move: bool) -> Response {
    let src_rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    if src_rel.is_empty() {
        return err_resp(StatusCode::FORBIDDEN, "forbidden");
    }
    let dest = req.headers().get(header::LOCATION).or_else(|| req.headers().get("destination"));
    let dest = match dest.and_then(|h| h.to_str().ok()) {
        Some(d) => d.to_string(),
        None => return err_resp(StatusCode::BAD_REQUEST, "missing destination"),
    };
    // Destination: volle URL oder Pfad — Pfadteil extrahieren
    let dest_path = if let Some(idx) = dest.find("/webdav") {
        dest[idx..].to_string()
    } else {
        dest.clone()
    };
    let dest_name = dest_path.strip_prefix("/webdav").unwrap_or("").to_string();
    let dst_rel = match validate(root, &dest_name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    if dst_rel.is_empty() {
        return err_resp(StatusCode::FORBIDDEN, "forbidden");
    }
    let overwrite = req
        .headers()
        .get("overwrite")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("T")
        .eq_ignore_ascii_case("T");
    let dst_abs = match fsops::resolve(root, &dst_rel) {
        Ok(a) => a,
        Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    let dst_exists = Path::new(&dst_abs).exists();
    if dst_exists && !overwrite {
        return err_resp(StatusCode::PRECONDITION_FAILED, "destination exists");
    }
    if dst_exists {
        let _ = if Path::new(&dst_abs).is_dir() {
            std::fs::remove_dir_all(&dst_abs)
        } else {
            std::fs::remove_file(&dst_abs)
        };
    }
    let src_abs = match fsops::resolve(root, &src_rel) {
        Ok(a) => a,
        Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    let res: Result<(), String> = if is_move {
        fsops::transfer(&src_abs, &dst_abs).map(|_| ()).map_err(|e| e.to_string())
    } else {
        fsops::copy(root, &src_rel, &dst_rel).map(|_| ()).map_err(|e| e.to_string())
    };
    match res {
        Ok(()) => {
            let mut r = Response::new(Body::empty());
            *r.status_mut() = if dst_exists { StatusCode::NO_CONTENT } else { StatusCode::CREATED };
            r
        }
        Err(e) => err_resp(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
    }
}

fn dav_lock() -> Response {
    let mut raw = [0u8; 16];
    rand::RngCore::fill_bytes(&mut rand::rng(), &mut raw);
    let token = hex::encode(raw);
    let body = format!(
        "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<prop xmlns=\"DAV:\">\n<lockdiscovery>\n<activelock>\n<locktype><write/></locktype>\n<lockscope><exclusive/></lockscope>\n<depth>infinity</depth>\n<owner>fileship</owner>\n<timeout>Second-600</timeout>\n<locktoken><href>opaquelocktoken:{}</href></locktoken>\n<lockroot><href>/webdav/</href></lockroot>\n</activelock>\n</lockdiscovery>\n</prop>\n",
        token
    );
    let mut res = Response::new(Body::from(body));
    *res.status_mut() = StatusCode::OK;
    res.headers_mut().insert(
        header::CONTENT_TYPE,
        header::HeaderValue::from_static("application/xml; charset=utf-8"),
    );
    res
}

fn dav_proppatch(root: &str, name: &str) -> Response {
    let rel = match validate(root, name) {
        Ok(r) => r,
        Err(()) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
    };
    let abs = if rel.is_empty() {
        root.to_string()
    } else {
        match fsops::resolve(root, &rel) {
            Ok(a) => a,
            Err(_) => return err_resp(StatusCode::FORBIDDEN, "forbidden"),
        }
    };
    if !Path::new(&abs).exists() {
        return err_resp(StatusCode::NOT_FOUND, "not found");
    }
    // x/net/webdav: PROPPATCH -> 207 mit 403 pro Property
    let href_path = if rel.is_empty() { "/webdav/".to_string() } else { format!("/webdav/{}/", href_escape(&rel)) };
    let body = format!(
        "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<multistatus xmlns=\"DAV:\">\n<response>\n<href>{}</href>\n<propstat>\n<prop/>\n<status>HTTP/1.1 403 Forbidden</status>\n</propstat>\n</response>\n</multistatus>\n",
        xml_escape(&href_path)
    );
    let mut res = Response::new(Body::from(body));
    *res.status_mut() = StatusCode::MULTI_STATUS;
    res.headers_mut().insert(
        header::CONTENT_TYPE,
        header::HeaderValue::from_static("application/xml; charset=utf-8"),
    );
    res
}

// HeaderMap-Import-Fix (HeaderMap wird nur für Signaturen genutzt)
#[allow(dead_code)]
fn _unused(_: HeaderMap) {}
