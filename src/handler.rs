//! Handler — 1:1-Port von internal/handler (Routen, Files, Shares, Trash, Agents).
//! Reine HTTP-Logik; FS-Ops in fsops, DB in db.

use crate::fsops;
use crate::middleware::{err_resp, json_resp, peer_addr, query_map, read_json_only, Auth, RateLimiter};
use crate::model::{FileInfo, ListOptions, PathPermission, WsEvent};
use axum::body::Body;
use axum::extract::{Path as AxPath, State};
use axum::http::{header, Request, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::Router;
use std::collections::HashMap;
use std::io::Write as _;
use std::path::Path;
use std::sync::Arc;
use sha2::Digest as _;

pub use crate::oidc::{oidc_callback, oidc_config, oidc_login};

#[derive(Clone)]
pub struct AppState {
    pub cfg: crate::config::Config,
    pub db: Arc<crate::db::Db>,
    pub hub: Arc<crate::hub::Hub>,
    pub indexer: fsops::Indexer,
    pub agent_hub: Arc<crate::agent::Hub>,
    pub login_limiter: Arc<RateLimiter>,
    pub global_limiter: Arc<RateLimiter>,
    pub proxies: Arc<crate::middleware::ProxyCfg>,
}

pub fn err(status: StatusCode, msg: &str) -> Response {
    err_resp(status, msg)
}

pub fn json<T: serde::Serialize>(status: StatusCode, v: &T) -> Response {
    json_resp(status, v)
}

fn empty(status: StatusCode) -> Response {
    crate::middleware::empty_resp(status)
}

// ── Router ──────────────────────────────────────────────



pub fn router(_state: AppState) -> Router<AppState> {
    use axum::routing::{get, post, put, delete};
    let public = Router::new()
        .route("/healthz", get(healthz))
        .route("/api/auth/login", post(login))
        .route("/api/auth/oidc/config", get(oidc_config))
        .route("/api/auth/oidc/login", get(oidc_login))
        .route("/api/auth/oidc/callback", get(oidc_callback))
        .route("/api/auth/refresh", post(refresh))
        .route("/api/auth/logout", post(logout))
        .route("/api/auth/2fa/verify", post(two_fa_verify))
        .route("/s/{token}", get(public_share).post(public_share))
        .route("/s/{token}/upload", post(public_share_upload))
        .route("/ws", get(ws_endpoint))
        .route("/api/agent/ws", get(agent_ws))
        ;

    let private = Router::new()
        .route("/api/files", get(list_files).delete(delete_file))
        .route("/api/files/info", get(file_info))
        .route("/api/files/upload", post(upload_file))
        .route("/api/files/upload/chunk", get(upload_chunk_status).post(upload_chunk))
        .route("/api/files/mkdir", post(mkdir))
        .route("/api/files/touch", post(touch))
        .route("/api/files/rename", post(rename_file))
        .route("/api/files/copy", post(copy_file))
        .route("/api/files/download", get(download_file))
        .route("/api/files/preview", get(preview_file))
        .route("/api/files/zip-preview", get(zip_preview))
        .route("/api/files/zip-entry", get(zip_entry))
        .route("/api/files/zip-multi", post(zip_multi))
        .route("/api/files/archive", post(download_archive))
        .route("/api/files/zip", get(zip_dir))
        .route("/api/files/move", post(move_file))
        .route("/api/files/search", get(search_files))
        .route("/api/files/unzip", post(unzip_file))
        .route("/api/files/text", get(read_text).put(write_text))
        .route("/api/files/thumb", get(thumbnail))
        .route("/api/files/reindex", post(reindex))
        .route("/api/i18n", get(i18n))
        .route("/api/me", get(me))
        .route("/api/me/password", post(change_password))
        .route("/api/me/settings", get(get_settings))
        .route("/api/me/tokens", get(list_api_tokens).post(create_api_token))
        .route("/api/me/tokens/{id}", delete(delete_api_token))
        .route("/api/me/favorites", get(list_favorites).post(add_favorite).delete(remove_favorite))
        .route("/api/shares", get(list_shares).post(create_share))
        .route("/api/shares/{token}", delete(delete_share))
        .route("/api/trash", get(trash_list).delete(trash_empty))
        .route("/api/trash/{name}", delete(trash_delete_permanent))
        .route("/api/trash/{name}/restore", post(trash_restore))
        .route("/api/me/sources", get(list_sources).post(create_source))
        .route("/api/me/sources/{id}", put(update_source).delete(delete_source))
        .route("/api/me/agents", get(list_agents).post(create_agent))
        .route("/api/me/agents/{id}", delete(delete_agent))
        .route("/api/me/2fa/setup", post(two_fa_setup))
        .route("/api/me/2fa/enable", post(two_fa_enable))
        .route("/api/me/2fa", delete(two_fa_disable))
        .route("/api/me/2fa/status", get(two_fa_status));

    // Admin routes need auth first (inserts AuthCtx), then the admin check.
    let admin = Router::new()
        .route("/api/users", get(list_users).post(create_user))
        .route("/api/users/{id}", delete(delete_user).patch(update_user))
        .route("/api/users/{id}/quota", put(set_quota))
        .route("/api/audit", get(audit_log))
        .route("/api/stats", get(stats))
        .route("/api/users/{id}/permissions", get(list_permissions).post(set_permission))
        .route("/api/permissions/{pid}", delete(delete_permission))
        .route("/api/admin/backup/db", get(db_backup))
        .route("/api/transfer", post(cross_transfer))
        .route_layer(axum::middleware::from_fn(crate::middleware::admin_mw))
        .route_layer(axum::middleware::from_fn(crate::middleware::auth_mw));

    private
        .route_layer(axum::middleware::from_fn(crate::middleware::agent_route_mw))
        .route_layer(axum::middleware::from_fn(crate::middleware::auth_mw))
        .merge(admin)
        .merge(public)
        .layer(axum::middleware::from_fn(crate::middleware::rate_limit_mw))
}

/// Download a consistent SQLite copy of the database (admin only).
async fn db_backup(State(state): State<AppState>, _auth: Auth) -> Response {
    let fname = format!(
        "fileship-backup-{}.db",
        chrono::Local::now().format("%Y%m%d-%H%M%S")
    );
    let tmp = std::env::temp_dir().join(fname.clone());
    let tmp_str = tmp.to_string_lossy().to_string();
    let state2 = state.clone();
    let result = tokio::task::spawn_blocking(move || {
        state2.db.vacuum_into(&tmp_str).map(|()| {
            let bytes = std::fs::read(&tmp_str).unwrap_or_default();
            let _ = std::fs::remove_file(&tmp_str);
            bytes
        })
    })
    .await;
    match result {
        Ok(Ok(bytes)) if !bytes.is_empty() => {
            let mut res = Response::new(Body::from(bytes));
            *res.status_mut() = StatusCode::OK;
            res.headers_mut().insert(
                axum::http::header::CONTENT_TYPE,
                axum::http::HeaderValue::from_static("application/octet-stream"),
            );
            res.headers_mut().insert(
                axum::http::header::CONTENT_DISPOSITION,
                axum::http::HeaderValue::from_str(&format!("attachment; filename=\"{}\"", fname))
                    .unwrap_or_else(|_| axum::http::HeaderValue::from_static("attachment")),
            );
            res
        }
        _ => err(StatusCode::INTERNAL_SERVER_ERROR, "database backup failed"),
    }
}

// ── Helpers ─────────────────────────────────────────────

/// disk_usage results cached per root for a short TTL — computing them
/// walks the whole tree and the sidebar asks on every page load.
fn cached_dir_size(root: &str) -> i64 {
    use std::collections::HashMap;
    use std::sync::{LazyLock, Mutex};
    use std::time::{Duration, Instant};
    static CACHE: LazyLock<Mutex<HashMap<String, (Instant, i64)>>> =
        LazyLock::new(|| Mutex::new(HashMap::new()));
    if let Ok(c) = CACHE.lock() {
        if let Some((at, v)) = c.get(root) {
            if at.elapsed() < Duration::from_secs(60) {
                return *v;
            }
        }
    }
    let v = fsops::dir_size(root).unwrap_or(0);
    if let Ok(mut c) = CACHE.lock() {
        c.insert(root.to_string(), (Instant::now(), v));
    }
    v
}

fn user_root(state: &AppState, req: &Request<Body>) -> String {
    let ctx = req.extensions().get::<crate::middleware::AuthCtx>().copied();
    let (user_id, is_admin) = match ctx {
        Some(c) => (c.user_id, c.is_admin),
        None => return state.cfg.root_path.clone(),
    };
    let user = match state.db.get_user_by_id(user_id) {
        Some(u) => u,
        None => return state.cfg.root_path.clone(),
    };
    let base = resolve_root(state, &user.root_path);
    let q = query_map(req);
    let raw = req
        .headers()
        .get("x-source-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    let raw = if !raw.is_empty() { raw } else { q.get("source_id").cloned().unwrap_or_default() };
    if !raw.is_empty() {
        if let Ok(src_id) = raw.parse::<i64>() {
            if src_id > 0 {
                if let Some(src) = state.db.get_source_by_id(user_id, src_id) {
                    if is_admin && fsops::is_abs(&src.root_path) {
                        return fsops::go_clean(&src.root_path);
                    }
                    if let Ok(resolved) = fsops::resolve(&base, &src.root_path) {
                        return resolved;
                    }
                }
            }
        }
    }
    base
}

pub fn resolve_root(state: &AppState, user_root_path: &str) -> String {
    if user_root_path == "/" || user_root_path.is_empty() {
        state.cfg.root_path.clone()
    } else {
        user_root_path.to_string()
    }
}

fn source_rel_path(state: &AppState, req: &Request<Body>) -> String {
    let ctx = match req.extensions().get::<crate::middleware::AuthCtx>().copied() {
        Some(c) if !c.is_admin => c,
        _ => return String::new(),
    };
    let q = query_map(req);
    let raw = req
        .headers()
        .get("x-source-id")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    let raw = if !raw.is_empty() { raw } else { q.get("source_id").cloned().unwrap_or_default() };
    if raw.is_empty() {
        return String::new();
    }
    let src_id = match raw.parse::<i64>() {
        Ok(v) if v > 0 => v,
        _ => return String::new(),
    };
    let src = match state.db.get_source_by_id(ctx.user_id, src_id) {
        Some(s) => s,
        None => return String::new(),
    };
    match fsops::clean_relative_path(&src.root_path) {
        Ok(r) => r,
        Err(_) => String::new(),
    }
}

fn perm_path(state: &AppState, req: &Request<Body>, rel: &str) -> String {
    let prefix = source_rel_path(state, req);
    if prefix.is_empty() {
        return rel.to_string();
    }
    let clean = match fsops::clean_relative_path(rel) {
        Ok(c) if !c.is_empty() => c,
        _ => return prefix,
    };
    format!("{}/{}", prefix, clean)
}

fn username_of(state: &AppState, user_id: i64) -> String {
    state.db.get_user_by_id(user_id).map(|u| u.username).unwrap_or_default()
}

fn auth_ctx(req: &Request<Body>) -> Option<crate::middleware::AuthCtx> {
    req.extensions().get::<crate::middleware::AuthCtx>().copied()
}

/// checkPerm — 403 + falsche wenn verweigert.
fn check_perm(state: &AppState, req: &Request<Body>, path: &str, action: &str) -> Result<(), Response> {
    let ctx = match auth_ctx(req) {
        Some(c) => c,
        None => return Err(err(StatusCode::UNAUTHORIZED, "unauthorized")),
    };
    if ctx.is_admin {
        return Ok(());
    }
    let eff = perm_path(state, req, path);
    if !state.db.check_permission(ctx.user_id, &eff, action) {
        return Err(err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", eff)));
    }
    Ok(())
}

/// Is pathp covered by write-perms all the way up (Go-Loop `p != "."`).
fn check_write_chain(state: &AppState, req: &Request<Body>, rel: &str) -> Result<(), Response> {
    let ctx = auth_ctx(req).unwrap();
    if ctx.is_admin {
        return Ok(());
    }
    let perms = state.db.list_path_permissions(ctx.user_id);
    let mut p = perm_path(state, req, rel);
    loop {
        if !crate::db::path_permission_allowed(&perms, &p, "write") {
            return Err(err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", p)));
        }
        if p == "." || p.is_empty() {
            break;
        }
        let parent = fsops::go_dir(&p);
        if parent == "." || parent == p {
            break;
        }
        p = parent;
    }
    Ok(())
}

fn check_transfer(state: &AppState, req: &Request<Body>, src: &str, dst: &str, moving: bool) -> Result<(), Response> {
    let root = user_root(state, req);
    if let Err(e) = fsops::resolve(&root, src) {
        return Err(err(StatusCode::BAD_REQUEST, &e.to_string()));
    }
    if let Err(e) = fsops::resolve(&root, dst) {
        return Err(err(StatusCode::BAD_REQUEST, &e.to_string()));
    }
    let ctx = auth_ctx(req).unwrap();
    let (quota, allowed_types) = state.db.get_user_quota_and_types(ctx.user_id);
    let perms = if ctx.is_admin { Vec::new() } else { state.db.list_path_permissions(ctx.user_id) };
    let source_abs = fsops::resolve(&root, src).unwrap();
    let mut size: i64 = 0;
    let mut walk_err: Option<String> = None;
    for entry in walk_all(&source_abs) {
        let entry = match entry {
            Ok(e) => e,
            Err(e) => {
                walk_err = Some(e.to_string());
                break;
            }
        };
        let (abs, md) = entry;
        if !md.is_dir() && !md.file_type().is_file() {
            walk_err = Some("cannot transfer non-regular file".to_string());
            break;
        }
        let suffix = abs.strip_prefix(&source_abs).unwrap_or("").trim_start_matches('/').to_string();
        let original = if suffix.is_empty() { src.to_string() } else { format!("{}/{}", src, suffix) };
        let target = if suffix.is_empty() { dst.to_string() } else { format!("{}/{}", dst, suffix) };
        let orig_perm = perm_path(state, req, &original);
        let target_perm = perm_path(state, req, &target);
        if !ctx.is_admin {
            if !crate::db::path_permission_allowed(&perms, &orig_perm, "read")
                || !crate::db::path_permission_allowed(&perms, &target_perm, "write")
            {
                walk_err = Some(format!("permission denied for path: {}", orig_perm));
                break;
            }
            if moving && !crate::db::path_permission_allowed(&perms, &orig_perm, "write") {
                walk_err = Some(format!("permission denied for path: {}", orig_perm));
                break;
            }
        }
        if md.file_type().is_file() {
            size += md.len() as i64;
            if !allowed_types.is_empty() && !fsops::type_allowed(&target, &allowed_types) {
                walk_err = Some(format!("file type not allowed: {}", target));
                break;
            }
        }
    }
    if let Some(e) = walk_err {
        return Err(err(StatusCode::FORBIDDEN, &e));
    }
    if !moving && quota > 0 {
        let usage = fsops::dir_size(&root).map_err(|e| err(StatusCode::BAD_REQUEST, &e.to_string()))?;
        if usage > quota || size > quota - usage {
            return Err(err(StatusCode::FORBIDDEN, "storage quota exceeded"));
        }
    }
    Ok(())
}

fn walk_all(root: &str) -> Vec<Result<(String, std::fs::Metadata), std::io::Error>> {
    let mut out = Vec::new();
    let mut stack = vec![root.to_string()];
    while let Some(cur) = stack.pop() {
        match std::fs::metadata(&cur) {
            Ok(md) => {
                if md.is_dir() {
                    match std::fs::read_dir(&cur) {
                        Ok(rd) => {
                            for e in rd {
                                match e {
                                    Ok(e) => stack.push(e.path().to_string_lossy().to_string()),
                                    Err(err) => out.push(Err(err)),
                                }
                            }
                        }
                        Err(err) => out.push(Err(err)),
                    }
                }
                out.push(Ok((cur, md)));
            }
            Err(err) => out.push(Err(err)),
        }
    }
    out
}

fn broadcast(state: &AppState, typ: &str, path: &str) {
    state.hub.broadcast(&WsEvent { typ: typ.to_string(), path: path.to_string(), payload: None });
}

fn get_dir_size_cached(state: &AppState, root: &str, rel: &str) -> i64 {
    let key = format!("{}|{}", root, rel);
    if let Some(size) = state.db.dir_size_cache_get(&key, chrono::Duration::minutes(5)) {
        return size;
    }
    let db = state.db.clone();
    let root = root.to_string();
    let rel = rel.to_string();
    std::thread::spawn(move || {
        if let Ok(abs) = fsops::resolve(&root, &rel) {
            if let Ok(size) = fsops::dir_size(&abs) {
                db.dir_size_cache_set(&key, size);
            }
        }
    });
    0
}

fn thumb_dir(state: &AppState) -> String {
    let dir = fsops::go_dir(&state.cfg.db_path);
    format!("{}/.thumbs", dir)
}

fn valid_upload_id(s: &str) -> bool {
    (4..=64).contains(&s.len()) && s.chars().all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '-')
}

fn chunk_dir(state: &AppState) -> String {
    let dir = fsops::go_dir(&state.cfg.db_path);
    format!("{}/.fileship_chunks", dir)
}

fn trash_dir(state: &AppState) -> std::path::PathBuf {
    crate::db::trash_dir(&state.cfg.db_path)
}

fn log_action(state: &AppState, req: &Request<Body>, action: &str, path: &str) {
    if let Some(ctx) = auth_ctx(req) {
        state.db.log_action(ctx.user_id, &username_of(state, ctx.user_id), action, path, &peer_addr(req));
    }
}

fn header_str(req: &Request<Body>, name: &str) -> String {
    req.headers().get(name).and_then(|h| h.to_str().ok()).unwrap_or("").to_string()
}

// ── Auth ──────────────────────────────────────────

#[derive(serde::Deserialize)]
struct LoginReq {
    username: String,
    password: String,
}

async fn login(State(state): State<AppState>, req: Request<Body>) -> Response {
    let body: LoginReq = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let user = match state.db.get_user_by_username(&body.username) {
        Some(u) => u,
        None => {
            let _ = crate::auth::verify_password(crate::auth::dummy_hash(), &body.password);
            return err(StatusCode::UNAUTHORIZED, "invalid credentials");
        }
    };
    if !crate::auth::verify_password(&user.password_hash, &body.password) {
        return err(StatusCode::UNAUTHORIZED, "invalid credentials");
    }
    if state.db.totp_is_enabled(user.id) {
        let pending = crate::auth::generate_pending_token(&state.cfg.jwt_secret, user.id, user.is_admin);
        return json(StatusCode::OK, &serde_json::json!({"requires_2fa": true, "pending_token": pending}));
    }
    let access = crate::auth::generate_access_token(&state.cfg.jwt_secret, user.id, user.is_admin);
    let (refresh, expires) = crate::auth::generate_refresh_token();
    state.db.save_refresh_token(&refresh, user.id, expires);
    json(
        StatusCode::OK,
        &serde_json::json!({"access_token": access, "refresh_token": refresh}),
    )
}

#[derive(serde::Deserialize)]
struct TwoFAVerifyReq {
    pending_token: String,
    code: String,
}

async fn two_fa_verify(State(state): State<AppState>, req: Request<Body>) -> Response {
    let body: TwoFAVerifyReq = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let claims = match crate::auth::parse_access_token(&state.cfg.jwt_secret, &body.pending_token) {
        Some(c) if c.two_fa_pending => c,
        _ => return err(StatusCode::UNAUTHORIZED, "invalid pending token"),
    };
    let (secret, enabled) = match state.db.totp_get_secret(claims.user_id) {
        Some(v) => v,
        None => (String::new(), false),
    };
    if !enabled {
        return err(StatusCode::BAD_REQUEST, "2FA not enabled");
    }
    // Per-attempt limiter: codes are 6 digits, a pending token lives 5 min.
    if !state.login_limiter.allow(&format!("2fa:{}", claims.user_id)) {
        return err(StatusCode::TOO_MANY_REQUESTS, "too many code attempts, please try again later");
    }
    let ok = crate::auth::verify_totp(&secret, &body.code) || state.db.totp_use_backup_code(claims.user_id, &body.code);
    if !ok {
        return err(StatusCode::UNAUTHORIZED, "invalid 2FA code");
    }
    let access = crate::auth::generate_access_token(&state.cfg.jwt_secret, claims.user_id, claims.is_admin);
    let (refresh, expires) = crate::auth::generate_refresh_token();
    state.db.save_refresh_token(&refresh, claims.user_id, expires);
    json(
        StatusCode::OK,
        &serde_json::json!({"access_token": access, "refresh_token": refresh}),
    )
}

#[derive(serde::Deserialize)]
struct RefreshReq {
    refresh_token: String,
}

async fn refresh(State(state): State<AppState>, req: Request<Body>) -> Response {
    let body: RefreshReq = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let user_id = match state.db.get_refresh_token(&body.refresh_token) {
        Some(u) => u,
        None => return err(StatusCode::UNAUTHORIZED, "invalid token"),
    };
    let user = match state.db.get_user_by_id(user_id) {
        Some(u) => u,
        None => return err(StatusCode::UNAUTHORIZED, "user not found"),
    };
    state.db.delete_refresh_token(&body.refresh_token);
    let access = crate::auth::generate_access_token(&state.cfg.jwt_secret, user.id, user.is_admin);
    let (new_refresh, expires) = crate::auth::generate_refresh_token();
    state.db.save_refresh_token(&new_refresh, user.id, expires);
    json(
        StatusCode::OK,
        &serde_json::json!({"access_token": access, "refresh_token": new_refresh}),
    )
}

#[derive(serde::Deserialize)]
struct LogoutReq {
    refresh_token: String,
}

async fn logout(State(state): State<AppState>, req: Request<Body>) -> Response {
    let body: LogoutReq = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    state.db.delete_refresh_token(&body.refresh_token);
    empty(StatusCode::NO_CONTENT)
}

// ── Files ──────────────────────────────────────────

async fn list_files(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    if let Err(r) = check_perm(&state, &req, q.get("path").map(|s| s.as_str()).unwrap_or(""), "read") {
        return r;
    }
    let page: i64 = q.get("page").and_then(|v| v.parse().ok()).unwrap_or(0);
    let per_page: i64 = q.get("per_page").and_then(|v| v.parse().ok()).unwrap_or(0);
    let opts = ListOptions {
        path: q.get("path").cloned().unwrap_or_default(),
        search: q.get("search").cloned().unwrap_or_default(),
        sort_by: q.get("sort_by").cloned().unwrap_or_default(),
        sort_asc: q.get("sort_asc").map(|v| v != "false").unwrap_or(true),
        page,
        per_page,
    };
    let perms = if auth.is_admin() { Vec::new() } else { state.db.list_path_permissions(auth.user_id()) };
    let root = user_root(&state, &req);
    let starred = state.db.favorite_path_set(auth.user_id());
    // Extract perm prefix BEFORE spawn_blocking (request can't be moved)
    let prefix = source_rel_path(&state, &req);
    let is_admin = auth.is_admin();
    let root_owned = root.clone();
    let opts_owned = opts.clone();
    let mut result = match tokio::task::spawn_blocking(move || {
        fsops::list_paged_filtered(&root_owned, &opts_owned, Some(&|f: &FileInfo| {
            if is_admin {
                return true;
            }
            let pp = if prefix.is_empty() {
                let clean = match fsops::clean_relative_path(&f.path) {
                    Ok(c) if !c.is_empty() => c,
                    _ => return false,
                };
                clean
            } else {
                let clean = match fsops::clean_relative_path(&f.path) {
                    Ok(c) if !c.is_empty() => c,
                    _ => prefix.clone(),
                };
                format!("{}/{}", prefix, clean)
            };
            crate::db::path_permission_allowed(&perms, &pp, "read")
        }))
    })
    .await
    {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
        Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    };
    for f in result.files.iter_mut() {
        if f.is_dir {
            f.dir_size = get_dir_size_cached(&state, &root, &f.path);
        }
        if starred.contains(&f.path) {
            f.starred = true;
        }
    }
    // Hide pseudo-filesystem roots (/proc, /sys, /dev, cgroup...) from the
    // listing unless explicitly requested — their virtual entries clutter
    // the UI when the root path is a bind-mounted host root ("/").
    let show_pseudo = q.get("show_pseudo").map(|v| v == "1" || v == "true").unwrap_or(false);
    if !show_pseudo {
        let root_abs = root.clone();
        result.files.retain(|f| {
            if !f.is_dir {
                return true;
            }
            // Bind-mounted host roots (/data -> /) expose system mounts
            // whose fstype statfs cannot see in the container, so also
            // hide the four classic FHS system locations at depth 1 by
            // name — they are virtually never data (toggle to show).
            let rel = f.path.trim_start_matches('/');
            if !rel.contains('/') && matches!(rel, "proc" | "sys" | "dev" | "run") {
                return false;
            }
            match fsops::resolve(&root_abs, &f.path) {
                Ok(abs) => !fsops::is_pseudo_fs(std::path::Path::new(&abs)),
                Err(_) => true,
            }
        });
        result.total = result.total.min(result.files.len() as i64);
    }
    // Fill folder sizes for this page (the UI shows "—" when dir_size is
    // empty). Parallel walk via cached_dir_size (60 s TTL); capped so a
    // page with hundreds of folders cannot stall the response.
    {
        use rayon::iter::{IntoParallelIterator, ParallelIterator};
        let root_abs = root.clone();
        let dirs: Vec<(usize, String)> = result
            .files
            .iter()
            .enumerate()
            .filter(|(_, f)| f.is_dir)
            .take(300)
            .map(|(i, f)| (i, f.path.clone()))
            .collect();
        let sizes: Vec<(usize, i64)> = dirs
            .into_par_iter()
            .map(|(i, rel)| {
                let v = fsops::resolve(&root_abs, &rel)
                    .map(|abs| cached_dir_size(&abs))
                    .unwrap_or(0);
                (i, v)
            })
            .collect();
        for (i, v) in sizes {
            result.files[i].dir_size = v;
        }
    }
    if state.db.index_count_for_user(auth.user_id()) == 0 {
        state.indexer.trigger_reindex(auth.user_id(), root);
    }
    json(StatusCode::OK, &result)
}

/// Read image EXIF and return only the tags worth showing in the Details
/// view (camera, time, exposure, GPS, dimensions). Unknown/binary tags are
/// dropped; strings come back as plain text.
fn read_exif(abs: &str) -> Option<serde_json::Value> {
    use little_exif::filetype::FileExtension;
    use little_exif::metadata::Metadata;
    let bytes = std::fs::read(abs).ok()?;
    if bytes.len() > (16 << 20) {
        return None;
    }
    let metadata = Metadata::new_from_vec(&bytes, FileExtension::JPEG).ok()?;
    let mut map = serde_json::Map::new();
    for tag in &metadata {
        let debug = format!("{:?}", tag);
        let Some(rest) = debug.strip_prefix("ExifTag::") else {
            continue;
        };
        let Some(open) = rest.find('(') else {
            continue;
        };
        let name = &rest[..open];
        // Skip binary/unknown tags and anything huge.
        if matches!(name, "MakerNote" | "ThumbnailOffset" | "ThumbnailLength" | "StripOffsets" | "StripByteCounts" | "UserComment")
            || name.starts_with("Unknown")
        {
            continue;
        }
        let raw = rest[open + 1..rest.len() - 1].trim();
        let value = if let Some(s) = raw.strip_prefix('"').and_then(|r| r.strip_suffix('"')) {
            s.to_string()
        } else if raw.starts_with('[') {
            // Numeric vector like [42] or [1, 2, 3]
            raw.trim_matches(['[', ']']).split(',').map(|x| x.trim().to_string()).collect::<Vec<_>>().join(", ")
        } else {
            raw.to_string()
        };
        if !value.is_empty() && map.len() < 48 {
            map.insert(name.to_string(), serde_json::Value::String(value));
        }
    }
    if map.is_empty() {
        None
    } else {
        Some(serde_json::Value::Object(map))
    }
}

async fn file_info(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let path = q.get("path").cloned().unwrap_or_default();
    let root = user_root(&state, &req);
    if let Err(r) = check_perm(&state, &req, &path, "read") {
        return r;
    }
    let abs = match fsops::resolve(&root, &path) {
        Ok(a) => a,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(_) => return err(StatusCode::NOT_FOUND, "file not found"),
    };
    let name = Path::new(&abs).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
    let mod_time = md.modified().ok().map(|t| chrono::DateTime::<chrono::Local>::from(t)).unwrap_or_else(chrono::Local::now);
    let mut out = serde_json::json!({
        "name": name,
        "path": path,
        "size": md.len() as i64,
        "is_dir": md.is_dir(),
        "mod_time": crate::model::fmt_rfc3339(&mod_time),
        "mode": format_mode(&md),
        "mime_type": "",
        "files": 0,
        "folders": 0,
    });
    if !md.is_dir() {
        if let Some(exif) = read_exif(&abs) {
            out["exif"] = exif;
        }
    }
    if md.is_dir() {
        out["size"] = serde_json::json!(0);
        let perms = if auth.is_admin() { Vec::new() } else { state.db.list_path_permissions(auth.user_id()) };
        let root_abs = fsops::resolve(&root, "").unwrap_or(root.clone());
        let mut files = 0i64;
        let mut folders = 0i64;
        let mut size = 0i64;
        for entry in walk_all(&abs) {
            let (eabs, emd) = match entry {
                Ok(e) => e,
                Err(_) => continue,
            };
            if eabs == abs {
                continue;
            }
            if emd.file_type().is_symlink() {
                continue;
            }
            let rel = eabs.strip_prefix(&root_abs).unwrap_or("").trim_start_matches('/').to_string();
            if !auth.is_admin() {
                let pp = perm_path(&state, &req, &rel);
                if !crate::db::path_permission_allowed(&perms, &pp, "read") {
                    continue;
                }
            }
            if emd.is_dir() {
                folders += 1;
            } else if emd.file_type().is_file() {
                files += 1;
                size += emd.len() as i64;
            }
        }
        out["files"] = serde_json::json!(files);
        out["folders"] = serde_json::json!(folders);
        out["size"] = serde_json::json!(size);
    } else {
        if !md.file_type().is_file() {
            return err(StatusCode::BAD_REQUEST, "not a regular file");
        }
        out["mime_type"] = serde_json::json!(fsops::mime_type(&path));
        if let Ok((w, h)) = image_dimensions(&abs) {
            out["width"] = serde_json::json!(w);
            out["height"] = serde_json::json!(h);
        }
        if let Some(algo) = q.get("checksum") {
            let checksum = match checksum_file(&abs, algo) {
                Ok(c) => c,
                Err(_) => return err(StatusCode::BAD_REQUEST, "unsupported checksum"),
            };
            out["checksum"] = serde_json::json!(checksum);
        }
    }
    json(StatusCode::OK, &out)
}

fn format_mode(md: &std::fs::Metadata) -> String {
    use std::os::unix::fs::{MetadataExt, PermissionsExt};
    let ft = md.file_type();
    let mut s = String::new();
    if ft.is_dir() {
        s.push('d');
    } else if ft.is_symlink() {
        s.push('L');
    } else if ft.is_file() {
        s.push('-');
    } else {
        s.push('?');
    }
    let m = md.permissions().mode() & 0o777;
    for &(r, w, x) in &[(0o400, 0o200, 0o100), (0o040, 0o020, 0o010), (0o004, 0o002, 0o001)] {
        s.push(if m & r != 0 { 'r' } else { '-' });
        s.push(if m & w != 0 { 'w' } else { '-' });
        s.push(if m & x != 0 { 'x' } else { '-' });
    }
    let _ = md.size();
    s
}

fn image_dimensions(abs: &str) -> Result<(u32, u32), ()> {
    image::ImageReader::open(abs)
        .map_err(|_| ())?
        .into_dimensions()
        .map_err(|_| ())
}

fn checksum_file(abs: &str, algo: &str) -> Result<String, ()> {
    // Streaming hash — no full file in RAM (large files!)
    use std::io::Read as _;
    let mut file = std::fs::File::open(abs).map_err(|_| ())?;
    let mut buf = vec![0u8; 1 << 20];
    match algo {
        "md5" => {
            let mut h = md5::Context::new();
            loop {
                let n = file.read(&mut buf).map_err(|_| ())?;
                if n == 0 { break; }
                h.consume(&buf[..n]);
            }
            Ok(hex::encode(h.compute().0))
        }
        "sha1" => {
            use sha1::Digest as _;
            let mut h = sha1::Sha1::new();
            loop {
                let n = file.read(&mut buf).map_err(|_| ())?;
                if n == 0 { break; }
                h.update(&buf[..n]);
            }
            Ok(hex::encode(h.finalize()))
        }
        "sha256" => {
            use sha2::Digest as _;
            let mut h = sha2::Sha256::new();
            loop {
                let n = file.read(&mut buf).map_err(|_| ())?;
                if n == 0 { break; }
                h.update(&buf[..n]);
            }
            Ok(hex::encode(h.finalize()))
        }
        "sha512" => {
            use sha2::Digest as _;
            let mut h = sha2::Sha512::new();
            loop {
                let n = file.read(&mut buf).map_err(|_| ())?;
                if n == 0 { break; }
                h.update(&buf[..n]);
            }
            Ok(hex::encode(h.finalize()))
        }
        _ => Err(()),
    }
}

// ── Upload (multipart) ────────────────────────────────

#[derive(serde::Deserialize)]
struct PathBody {
    #[serde(default)]
    path: String,
}

#[derive(serde::Deserialize)]
struct RenameBody {
    #[serde(default)]
    old: String,
    #[serde(default)]
    new: String,
    #[serde(default)]
    src: String,
    #[serde(default)]
    dst: String,
}

struct SpooledFile {
    filename: String,
    path: std::path::PathBuf,
    size: i64,
}

async fn read_multipart(req: Request<Body>, max_total: i64, max_files: usize) -> Result<(HashMap<String, String>, Vec<SpooledFile>), Response> {
    use std::io::Write as _;
    let boundary = req
        .headers()
        .get("content-type")
        .and_then(|h| h.to_str().ok())
        .and_then(|ct| ct.split("boundary=").nth(1).map(|b| b.trim().trim_matches('"').to_string()));
    let boundary = match boundary {
        Some(b) if !b.is_empty() => b,
        _ => return Err(err(StatusCode::BAD_REQUEST, "bad request")),
    };
    let body = axum::body::to_bytes(req.into_body(), (max_total + (1 << 20)) as usize)
        .await
        .map_err(|_| err(StatusCode::PAYLOAD_TOO_LARGE, "file too large"))?;
    let mut fields: HashMap<String, String> = HashMap::new();
    let mut files: Vec<SpooledFile> = Vec::new();
    let mut total: i64 = 0;
    // hand-parse as chunks (axum's Multipart cannot stream to disk):
    // (axum multipart doesn't allow streaming to file — we parse from the buffer,
    //  in chunks, but spool each file part to a temp file.)
    let mut mp = MultipartParser::new(&body, &boundary);
    loop {
        let part = match mp.next_part() {
            Ok(Some(p)) => p,
            Ok(None) => break,
            Err(_) => return Err(err(StatusCode::BAD_REQUEST, "bad request")),
        };
        let data = part.data;
        if let Some(fname) = part.filename {
            if files.len() >= max_files {
                cleanup_spools(&mut files);
                return Err(err(StatusCode::BAD_REQUEST, "too many files (max 100)"));
            }
            let (path, mut f) = fsops::spool_file().map_err(|_| err(StatusCode::INTERNAL_SERVER_ERROR, "server error"))?;
            if f.write_all(&data).is_err() {
                cleanup_spools(&mut files);
                return Err(err(StatusCode::INTERNAL_SERVER_ERROR, "server error"));
            }
            total += data.len() as i64;
            if total > max_total {
                cleanup_spools(&mut files);
                let _ = std::fs::remove_file(&path);
                return Err(err(StatusCode::PAYLOAD_TOO_LARGE, "file too large"));
            }
            files.push(SpooledFile { filename: fname, path, size: data.len() as i64 });
        } else if let Some(name) = part.name {
            fields.insert(name, String::from_utf8_lossy(&data).to_string());
        }
    }
    Ok((fields, files))
}

fn cleanup_spools(files: &mut Vec<SpooledFile>) {
    for f in files.drain(..) {
        let _ = std::fs::remove_file(f.path);
    }
}

struct MpPart {
    name: Option<String>,
    filename: Option<String>,
    data: Vec<u8>,
}

struct MultipartParser<'a> {
    body: &'a [u8],
    boundary: Vec<u8>,
    pos: usize,
}

impl<'a> MultipartParser<'a> {
    fn new(body: &'a [u8], boundary: &str) -> Self {
        MultipartParser { body, boundary: boundary.as_bytes().to_vec(), pos: 0 }
    }

    fn next_part(&mut self) -> Result<Option<MpPart>, ()> {
        // suche --boundary
        let mut delim = Vec::new();
        delim.extend_from_slice(b"--");
        delim.extend_from_slice(&self.boundary);
        // Part-Anfang finden
        let start = find_subslice(&self.body[self.pos..], &delim).ok_or(())? + self.pos;
        let mut p = start + delim.len();
        // Check whether the terminator is "--"
        if p + 1 < self.body.len() && &self.body[p..p + 2] == b"--" {
            return Ok(None);
        }
        // Skip CRLF
        if p + 1 < self.body.len() && &self.body[p..p + 2] == b"\r\n" {
            p += 2;
        }
        // Header bis Leerzeile
        let header_end = find_subslice(&self.body[p..], b"\r\n\r\n").ok_or(())? + p;
        let headers = String::from_utf8_lossy(&self.body[p..header_end]).to_string();
        let mut name = None;
        let mut filename = None;
        for line in headers.lines() {
            let line = line.trim();
            if let Some(rest) = line.strip_prefix("Content-Disposition:") {
                for seg in rest.split(';') {
                    let seg = seg.trim();
                    if let Some(v) = seg.strip_prefix("name=") {
                        name = Some(v.trim_matches('"').to_string());
                    }
                    if let Some(v) = seg.strip_prefix("filename=") {
                        filename = Some(v.trim_matches('"').to_string());
                    }
                }
            }
        }
        let data_start = header_end + 4;
        // Datenende: \r\n--boundary
        let mut end_delim = Vec::new();
        end_delim.extend_from_slice(b"\r\n--");
        end_delim.extend_from_slice(&self.boundary);
        let data_end = find_subslice(&self.body[data_start..], &end_delim).ok_or(())? + data_start;
        let data = self.body[data_start..data_end].to_vec();
        self.pos = data_end + 2; // points at --boundary
        Ok(Some(MpPart { name, filename, data }))
    }
}

fn find_subslice(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    if needle.is_empty() || haystack.len() < needle.len() {
        return None;
    }
    haystack.windows(needle.len()).position(|w| w == needle)
}

async fn upload_file(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (_, is_admin) = (auth.user_id(), auth.is_admin());
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let root = user_root(&state, &req_view);
    let max_upload = state.cfg.max_upload_size;
    let full_req = Request::from_parts(req_view.into_parts().0, req_body);
    let (fields, mut files) = match read_multipart(full_req, max_upload, 100).await {
        Ok(v) => v,
        Err(r) => return r,
    };
    let dir = fields.get("path").cloned().unwrap_or_default();
    if dir.is_empty() {
        cleanup_spools(&mut files);
        return err(StatusCode::BAD_REQUEST, "path required");
    }
    if !is_admin {
        let perms = state.db.list_path_permissions(auth.user_id());
        let mut p = perm_path_rooted(&state, &dir);
        loop {
            if !crate::db::path_permission_allowed(&perms, &p, "write") {
                cleanup_spools(&mut files);
                return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", p));
            }
            if p == "." || p.is_empty() {
                break;
            }
            let parent = fsops::go_dir(&p);
            if parent == "." || parent == p {
                break;
            }
            p = parent;
        }
    }
    if files.is_empty() {
        return err(StatusCode::BAD_REQUEST, "too many files (max 100)");
    }
    let relative_path = fields.get("relative_path").cloned().unwrap_or_default();
    if !relative_path.is_empty() && files.len() != 1 {
        cleanup_spools(&mut files);
        return err(StatusCode::BAD_REQUEST, "relative_path requires one file");
    }
    let (quota, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    // Preflight
    let mut target_rels: Vec<String> = Vec::with_capacity(files.len());
    let mut incoming: i64 = 0;
    for f in files.iter() {
        let name = if !relative_path.is_empty() { relative_path.clone() } else { f.filename.clone() };
        let safe = match fsops::clean_relative_path(&name) {
            Ok(s) => s,
            Err(_) => {
                cleanup_spools(&mut files);
                return err(StatusCode::BAD_REQUEST, "invalid upload path");
            }
        };
        let rel = format!("{}/{}", dir.trim_end_matches('/'), safe);
        let rel = fsops::go_clean(&rel);
        let abs = match fsops::resolve(&root, &rel) {
            Ok(a) => a,
            Err(_) => {
                cleanup_spools(&mut files);
                return err(StatusCode::BAD_REQUEST, "invalid upload path");
            }
        };
        if !allowed_types.is_empty() && !fsops::type_allowed(&safe, &allowed_types) {
            cleanup_spools(&mut files);
            return err(StatusCode::FORBIDDEN, &format!("file type not allowed: {}", safe));
        }
        incoming += f.size;
        if let Ok(info) = std::fs::metadata(&abs) {
            if info.file_type().is_file() {
                incoming -= info.len() as i64;
            }
        }
        target_rels.push(rel);
    }
    if quota > 0 {
        let usage = match fsops::dir_size(&root) {
            Ok(u) => u,
            Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot calculate storage usage"),
        };
        if usage > quota || incoming > quota - usage {
            cleanup_spools(&mut files);
            return err(StatusCode::FORBIDDEN, "storage quota exceeded");
        }
    }
    for (i, f) in files.iter().enumerate() {
        let rel = &target_rels[i];
        let mut file = match std::fs::File::open(&f.path) {
            Ok(fl) => fl,
            Err(_) => {
                cleanup_spools(&mut files);
                return err(StatusCode::BAD_REQUEST, "could not read upload");
            }
        };
        if let Err(e) = fsops::save_upload(&root, rel, &mut file) {
            cleanup_spools(&mut files);
            return err(StatusCode::INTERNAL_SERVER_ERROR, &format!("could not save file: {}", e));
        }
        log_action(&state, &Request::new(Body::empty()), "upload", rel);
        state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "upload", rel, "");
    }
    cleanup_spools(&mut files);
    broadcast(&state, "upload", &dir);
    state.indexer.trigger_reindex(auth.user_id(), root);
    empty(StatusCode::CREATED)
}

/// perm_path without the request (for multipart handlers that already consumed it).
/// X-Source-ID wasn't passed through headers — Go uses r.Header; here best effort without source
/// (Uploads mit aktiver Source sind im Go ebenfalls durch den Header erreichbar; wir haben den
/// original request once we are in multipart. Accepted simplification.)
fn perm_path_rooted(_state: &AppState, rel: &str) -> String {
    match fsops::clean_relative_path(rel) {
        Ok(c) => c,
        Err(_) => rel.to_string(),
    }
}

// ── Chunk-Upload ───────────────────────────────────────

async fn upload_chunk_status(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let upload_id = q.get("upload_id").cloned().unwrap_or_default();
    if !valid_upload_id(&upload_id) {
        return err(StatusCode::BAD_REQUEST, "invalid upload_id");
    }
    let part_path = format!("{}/{}_{}.part", chunk_dir(&state), auth.user_id(), upload_id);
    let received = std::fs::metadata(&part_path).map(|m| m.len() as i64).unwrap_or(0);
    json(StatusCode::OK, &serde_json::json!({"upload_id": upload_id, "received_bytes": received}))
}

async fn upload_chunk(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let upload_id = q.get("upload_id").cloned().unwrap_or_default();
    if !valid_upload_id(&upload_id) {
        return err(StatusCode::BAD_REQUEST, "invalid upload_id");
    }
    let dir = q.get("path").cloned().unwrap_or_default();
    let filename = q.get("filename").cloned().unwrap_or_default();
    let offset: i64 = q.get("offset").and_then(|v| v.parse().ok()).unwrap_or(0);
    let total_size: i64 = q.get("total_size").and_then(|v| v.parse().ok()).unwrap_or(0);
    let is_final = q.get("done").map(|v| v == "1" || v == "true").unwrap_or(false);
    if filename.is_empty() || offset < 0 || total_size < 0 {
        return err(StatusCode::BAD_REQUEST, "invalid chunk parameters");
    }
    let target_rel = match fsops::clean_relative_path(&fsops::go_clean(&format!("{}/{}", dir, filename))) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "invalid target path"),
    };
    let root = user_root(&state, &req);
    let target_abs = match fsops::resolve(&root, &target_rel) {
        Ok(a) => a,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
    };
    let (quota, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    let effective_types = if allowed_types.is_empty() { state.cfg.allowed_types.clone() } else { allowed_types };
    if !fsops::type_allowed(&target_rel, &effective_types) {
        return err(StatusCode::FORBIDDEN, &format!("file type not allowed: {}", Path::new(&target_rel).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default()));
    }
    if !auth.is_admin() {
        let perms = state.db.list_path_permissions(auth.user_id());
        let mut cur = target_rel.clone();
        loop {
            if !crate::db::path_permission_allowed(&perms, &cur, "write") {
                return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", cur));
            }
            if cur.is_empty() {
                break;
            }
            let parent = fsops::go_dir(&cur);
            if parent == "." || parent == cur {
                break;
            }
            cur = parent;
        }
    }
    if quota > 0 {
        let mut usage = match fsops::dir_size(&root) {
            Ok(u) => u,
            Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot calculate storage usage"),
        };
        if let Ok(info) = std::fs::metadata(&target_abs) {
            if info.file_type().is_file() {
                usage -= info.len() as i64;
            }
        }
        if usage > quota || total_size > quota - usage {
            return err(StatusCode::FORBIDDEN, "storage quota exceeded");
        }
    }
    let cdir = chunk_dir(&state);
    if let Err(_) = std::fs::create_dir_all(&cdir) {
        return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot create chunk directory");
    }
    let part_path = format!("{}/{}_{}.part", cdir, auth.user_id(), upload_id);
    let mut opts = std::fs::OpenOptions::new();
    opts.write(true).create(true);
    if offset == 0 {
        opts.truncate(true);
    }
    let mut part = match opts.open(&part_path) {
        Ok(f) => f,
        Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot open chunk file"),
    };
    let cur_size = part.metadata().map(|m| m.len()).unwrap_or(0);
    if offset as u64 > cur_size {
        return err(StatusCode::CONFLICT, "chunk offset beyond current size");
    }
    use std::io::Seek as _;
        if part.seek(std::io::SeekFrom::Start(offset as u64)).is_err() {
        return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot seek chunk file");
    }
    let body = match axum::body::to_bytes(req.into_body(), (32 << 20) + 1 + (1 << 20)).await {
        Ok(b) => b,
        Err(_) => return err(StatusCode::BAD_REQUEST, "failed to write chunk"),
    };
    if body.len() as u64 > (32 << 20) {
        return err(StatusCode::BAD_REQUEST, "failed to write chunk");
    }
    let written = body.len() as i64;
    if part.write_all(&body).is_err() {
        return err(StatusCode::BAD_REQUEST, "failed to write chunk");
    }
    let new_size = offset + written;
    if part.set_len(new_size as u64).is_err() {
        return err(StatusCode::INTERNAL_SERVER_ERROR, "failed to finalize chunk");
    }
    drop(part);
    if is_final || (total_size > 0 && new_size >= total_size) {
        if total_size > 0 && new_size != total_size {
            let _ = std::fs::remove_file(&part_path);
            return err(StatusCode::BAD_REQUEST, "uploaded size mismatch");
        }
        if let Some(parent) = Path::new(&target_abs).parent() {
            if std::fs::create_dir_all(parent).is_err() {
                return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot create target directory");
            }
        }
        if let Err(e) = fsops::transfer(&part_path, &target_abs) {
            return err(StatusCode::INTERNAL_SERVER_ERROR, &format!("cannot move completed file: {}", e));
        }
        state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "upload", &target_rel, "");
        broadcast(&state, "upload", &dir);
        state.indexer.trigger_reindex(auth.user_id(), root);
        return json(StatusCode::CREATED, &serde_json::json!({"completed": true, "received_bytes": new_size, "path": target_rel}));
    }
    json(StatusCode::OK, &serde_json::json!({"completed": false, "received_bytes": new_size}))
}

// ── Einfache File-Ops ──────────────────────────────────

async fn delete_file(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let path = q.get("path").cloned().unwrap_or_default();
    let clean = match fsops::clean_relative_path(&path) {
        Ok(c) => c,
        Err(_) => return err(StatusCode::BAD_REQUEST, "cannot delete workspace root"),
    };
    let root = user_root(&state, &req);
    let abs = match fsops::resolve(&root, &clean) {
        Ok(a) => a,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
    };
    if !auth.is_admin() {
        let perms = state.db.list_path_permissions(auth.user_id());
        let target_perm = perm_path(&state, &req, &clean);
        if !crate::db::path_permission_allowed(&perms, &target_perm, "delete") {
            return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", target_perm));
        }
        // Check all children for dirs
        if std::fs::metadata(&abs).map(|m| m.is_dir()).unwrap_or(false) {
            let root_abs = fsops::resolve(&root, "").unwrap_or_default();
            for entry in walk_all(&abs) {
                if let Ok((eabs, _)) = entry {
                    let rel = eabs.strip_prefix(&root_abs).unwrap_or("").trim_start_matches('/').to_string();
                    let pp = perm_path(&state, &req, &rel);
                    if !crate::db::path_permission_allowed(&perms, &pp, "delete") {
                        return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", pp));
                    }
                }
            }
        }
    }
    let permanent = q.get("permanent").map(|v| v == "true").unwrap_or(false);
    if permanent {
        if let Err(e) = fsops::delete(&root, &clean) {
            return err(StatusCode::BAD_REQUEST, &e.to_string());
        }
    } else if let Err(e) = soft_delete(&state, auth.user_id(), &root, &clean) {
        return err(StatusCode::INTERNAL_SERVER_ERROR, &format!("could not move to trash: {}", e));
    }
    state.db.index_delete(auth.user_id(), &clean);
    broadcast(&state, "delete", &clean);
    empty(StatusCode::NO_CONTENT)
}

pub fn soft_delete(state: &AppState, user_id: i64, root: &str, rel_path: &str) -> Result<(), String> {
    let _clean = fsops::clean_relative_path(rel_path).map_err(|e| e.to_string())?;
    let abs = fsops::resolve(root, rel_path).map_err(|e| e.to_string())?;
    let info = std::fs::metadata(&abs).map_err(|e| e.to_string())?;
    let tdir = trash_dir(state);
    std::fs::create_dir_all(&tdir).map_err(|e| e.to_string())?;
    let nanos = chrono::Local::now().timestamp_nanos_opt().unwrap_or_default();
    let name = Path::new(&abs).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
    let trash_name = format!("{}_{}", nanos, name);
    let trash_path = tdir.join(&trash_name);
    let size = if info.is_dir() { fsops::dir_size(&abs).unwrap_or(0) } else { info.len() as i64 };
    let retention_days: i64 = std::env::var("TRASH_RETENTION_DAYS").ok().and_then(|v| v.parse().ok()).unwrap_or(30);
    state.db.trash_put(user_id, rel_path, &trash_name, &name, info.is_dir(), size, retention_days);
    if let Err(e) = fsops::transfer(&abs, &trash_path.to_string_lossy()) {
        if !trash_path.exists() {
            state.db.trash_delete(user_id, &trash_name);
        }
        return Err(e.to_string());
    }
    Ok(())
}

async fn mkdir(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: PathBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let clean = match fsops::clean_relative_path(&body.path) {
        Ok(c) => c,
        Err(_) => return err(StatusCode::BAD_REQUEST, "folder path required"),
    };
    if !auth.is_admin() {
        let perms = state.db.list_path_permissions(auth.user_id());
        let mut p = perm_path(&state, &req_view, &clean);
        loop {
            if !crate::db::path_permission_allowed(&perms, &p, "write") {
                return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", p));
            }
            if p == "." || p.is_empty() {
                break;
            }
            let parent = fsops::go_dir(&p);
            if parent == "." || parent == p {
                break;
            }
            p = parent;
        }
    }
    let root = user_root(&state, &req_view);
    if let Err(e) = fsops::mkdir(&root, &clean) {
        return err(StatusCode::BAD_REQUEST, &e.to_string());
    }
    state.indexer.trigger_reindex(auth.user_id(), root);
    broadcast(&state, "mkdir", &clean);
    empty(StatusCode::CREATED)
}

async fn touch(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: PathBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let clean = match fsops::clean_relative_path(&body.path) {
        Ok(c) => c,
        Err(_) => return err(StatusCode::BAD_REQUEST, "file path required"),
    };
    if !auth.is_admin() {
        let perms = state.db.list_path_permissions(auth.user_id());
        let mut p = clean.clone();
        loop {
            if !crate::db::path_permission_allowed(&perms, &p, "write") {
                return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", p));
            }
            if p == "." || p.is_empty() {
                break;
            }
            let parent = fsops::go_dir(&p);
            if parent == "." || parent == p {
                break;
            }
            p = parent;
        }
    }
    let (_, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    if !allowed_types.is_empty() && !fsops::type_allowed(&clean, &allowed_types) {
        return err(StatusCode::FORBIDDEN, "file type not allowed");
    }
    let root = user_root(&state, &req_view);
    if let Err(e) = fsops::create_file(&root, &clean) {
        return err(StatusCode::BAD_REQUEST, &e.to_string());
    }
    state.indexer.trigger_reindex(auth.user_id(), root);
    broadcast(&state, "create", &clean);
    empty(StatusCode::CREATED)
}

async fn rename_file(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: RenameBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    run_transfer(&state, &req_view, auth.user_id(), &body.old, &body.new, true, "rename")
}

async fn move_file(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: RenameBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if let Err(r) = check_perm(&state, &req_view, &body.src, "write") {
        return r;
    }
    if let Err(r) = check_perm(&state, &req_view, &body.dst, "write") {
        return r;
    }
    run_transfer(&state, &req_view, auth.user_id(), &body.src, &body.dst, true, "move")
}

async fn copy_file(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: RenameBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if let Err(r) = check_perm(&state, &req_view, &body.src, "read") {
        return r;
    }
    if let Err(r) = check_perm(&state, &req_view, &body.dst, "write") {
        return r;
    }
    run_transfer(&state, &req_view, auth.user_id(), &body.src, &body.dst, false, "copy")
}

fn run_transfer(state: &AppState, req: &Request<Body>, user_id: i64, src: &str, dst: &str, moving: bool, event: &str) -> Response {
    if moving {
        if let Err(r) = check_perm(state, req, src, "write") {
            return r;
        }
        if let Err(r) = check_perm(state, req, dst, "write") {
            return r;
        }
    }
    if let Err(r) = check_transfer(state, req, src, dst, moving) {
        return r;
    }
    let root = user_root(state, req);
    let res = if moving { fsops::rename(&root, src, dst) } else { fsops::copy(&root, src, dst) };
    if let Err(e) = res {
        return err(StatusCode::BAD_REQUEST, &e.to_string());
    }
    if moving {
        state.db.index_rename(user_id, src, dst);
    } else {
        state.indexer.trigger_reindex(user_id, root);
    }
    state.db.log_action(user_id, &username_of(state, user_id), event, &format!("{}->{}", src, dst), "");
    broadcast(state, event, dst);
    if moving { empty(StatusCode::NO_CONTENT) } else { empty(StatusCode::CREATED) }
}

// ── Download / Preview ─────────────────────────────────

pub fn serve_file_response(abs: &str, _name: Option<&str>, extra_headers: &[(&str, &str)], req: &Request<Body>) -> Response {
    let md = match std::fs::metadata(abs) {
        Ok(m) => m,
        Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
    };
    let size = md.len();
    // Range-Parsing (bytes=)
    let range = req
        .headers()
        .get(header::RANGE)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    let mut status = StatusCode::OK;
    let mut start: u64 = 0;
    let mut end: u64 = size.saturating_sub(1);
    let mut partial = false;
    if range.starts_with("bytes=") && size > 0 {
        let spec = &range[6..];
        if let Some((a, b)) = spec.split_once('-') {
            if let Ok(av) = a.parse::<u64>() {
                start = av;
            }
            if let Ok(bv) = b.parse::<u64>() {
                end = bv.min(size - 1);
            } else if a.is_empty() {
                if let Ok(suffix) = b.parse::<u64>() {
                    start = size.saturating_sub(suffix);
                }
            }
            if start <= end {
                partial = true;
                status = StatusCode::PARTIAL_CONTENT;
            }
        }
    }
    let len = if size == 0 { 0 } else { end - start + 1 };
    let file = match std::fs::File::open(abs) {
        Ok(f) => tokio::fs::File::from_std(f),
        Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    };
    use tokio::io::{AsyncReadExt, AsyncSeekExt};
    let stream = futures_util::stream::unfold((file, start, len), |(mut f, pos, remaining)| async move {
        if remaining == 0 {
            return None;
        }
        let _ = f.seek(std::io::SeekFrom::Start(pos)).await;
        let to_read = remaining.min(1 << 20) as usize; // 1 MiB chunks: fewer syscalls, less userspace copying
        let mut buf = vec![0u8; to_read];
        match f.read(&mut buf).await {
            Ok(0) => None,
            Ok(n) => {
                buf.truncate(n);
                Some((Ok::<_, std::io::Error>(axum::body::Bytes::from(buf)), (f, pos + n as u64, remaining - n as u64)))
            }
            Err(e) => Some((Err(e), (f, pos, 0))),
        }
    });
    let mut res = Response::new(Body::from_stream(stream));
    *res.status_mut() = status;
    res.headers_mut().insert(header::CONTENT_LENGTH, len.to_string().parse().unwrap_or(header::HeaderValue::from_static("0")));
    res.headers_mut().insert(header::ACCEPT_RANGES, header::HeaderValue::from_static("bytes"));
    if partial {
        res.headers_mut().insert(
            header::CONTENT_RANGE,
            format!("bytes {}-{}/{}", start, end, size).parse().unwrap_or(header::HeaderValue::from_static("bytes */0")),
        );
    }
    for (k, v) in extra_headers {
        if let (Ok(kn), Ok(vv)) = (header::HeaderName::from_bytes(k.as_bytes()), header::HeaderValue::from_str(v)) {
            res.headers_mut().insert(kn, vv);
        }
    }
    res
}

async fn download_file(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let rel = match fsops::clean_relative_path(q.get("path").map(|s| s.as_str()).unwrap_or("")) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "file path required"),
    };
    if let Err(r) = check_perm(&state, &req, &rel, "read") {
        return r;
    }
    let root = user_root(&state, &req);
    let abs = match fsops::resolve(&root, &rel) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
    };
    if !md.file_type().is_file() {
        return err(StatusCode::BAD_REQUEST, "not a regular file");
    }
    let mime = fsops::mime_type(&rel);
    serve_file_response(
        &abs,
        None,
        &[
            ("content-type", &mime),
            ("content-disposition", "attachment"),
            ("x-content-type-options", "nosniff"),
            ("content-security-policy", "default-src 'none'; sandbox"),
        ],
        &req,
    )
}

async fn preview_file(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let rel = match fsops::clean_relative_path(q.get("path").map(|s| s.as_str()).unwrap_or("")) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "file path required"),
    };
    if let Err(r) = check_perm(&state, &req, &rel, "read") {
        return r;
    }
    let root = user_root(&state, &req);
    let abs = match fsops::resolve(&root, &rel) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
    };
    if !md.file_type().is_file() {
        return err(StatusCode::BAD_REQUEST, "not a regular file");
    }
    if fsops::is_heic(&rel) {
        match fsops::generate_thumbnail(&root, &rel, &thumb_dir(&state)) {
            Ok(name) => {
                let abs = format!("{}/{}", thumb_dir(&state), name);
                return serve_file_response(
                    &abs,
                    None,
                    &[
                        ("content-type", "image/jpeg"),
                        ("content-disposition", "inline"),
                        ("x-content-type-options", "nosniff"),
                    ],
                    &req,
                );
            }
            Err(e) => return err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
        }
    }
    if q.get("transcode").map(|v| v == "1").unwrap_or(false) && fsops::is_video(&rel) {
        if !fsops::ffmpeg_available() {
            return err(StatusCode::NOT_IMPLEMENTED, "ffmpeg not available for transcoding");
        }
        return stream_transcoded(&abs, &root, &rel).await;
    }
    let mime = fsops::mime_type(&rel);
    serve_file_response(
        &abs,
        None,
        &[
            ("content-type", &mime),
            ("content-disposition", "inline"),
            ("x-content-type-options", "nosniff"),
            ("content-security-policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox"),
        ],
        &req,
    )
}

/// List the entries of a ZIP archive without extracting it.
async fn zip_preview(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let rel = match fsops::clean_relative_path(q.get("path").map(|s| s.as_str()).unwrap_or("")) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "file path required"),
    };
    if let Err(r) = check_perm(&state, &req, &rel, "read") {
        return r;
    }
    let root = user_root(&state, &req);
    let abs = match fsops::resolve(&root, &rel) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
    };
    let file = match std::fs::File::open(&abs) {
        Ok(f) => f,
        Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
    };
    let mut archive = match zip::ZipArchive::new(file) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::BAD_REQUEST, "not a valid zip archive"),
    };
    let mut entries = Vec::new();
    for i in 0..archive.len() {
        let f = match archive.by_index(i) {
            Ok(f) => f,
            Err(_) => continue,
        };
        entries.push(serde_json::json!({
            "name": f.name().unwrap_or_default(),
            "size": f.size(),
            "compressed_size": f.compressed_size(),
            "is_dir": f.is_dir(),
        }));
    }
    json(StatusCode::OK, &entries)
}

/// Stream a single entry out of a ZIP archive.
async fn zip_entry(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let rel = match fsops::clean_relative_path(q.get("path").map(|s| s.as_str()).unwrap_or("")) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "file path required"),
    };
    let want = match q.get("entry") {
        Some(e) if !e.is_empty() => e.clone(),
        _ => return err(StatusCode::BAD_REQUEST, "entry required"),
    };
    if let Err(r) = check_perm(&state, &req, &rel, "read") {
        return r;
    }
    let root = user_root(&state, &req);
    let abs = match fsops::resolve(&root, &rel) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
    };
    let file = match std::fs::File::open(&abs) {
        Ok(f) => f,
        Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
    };
    let mut archive = match zip::ZipArchive::new(file) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::BAD_REQUEST, "not a valid zip archive"),
    };
    let data = match archive.by_name(&want) {
        Ok(mut f) => {
            use std::io::Read as _;
            let mut buf = Vec::new();
            match f.read_to_end(&mut buf) {
                Ok(_) => buf,
                Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "read failed"),
            }
        }
        Err(_) => return err(StatusCode::NOT_FOUND, "entry not found"),
    };
    let base = want.rsplit('/').next().unwrap_or(&want);
    let mut res = Response::new(Body::from(data));
    let disp = format!("attachment; filename=\"{}\"", base.replace('"', ""));
    res.headers_mut().insert(
        axum::http::header::CONTENT_DISPOSITION,
        match axum::http::HeaderValue::from_str(&disp) {
            Ok(v) => v,
            Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "bad filename"),
        },
    );
    res.headers_mut().insert(
        axum::http::header::CONTENT_TYPE,
        axum::http::HeaderValue::from_static("application/octet-stream"),
    );
    res
}

async fn stream_transcoded(abs: &str, _root: &str, _rel: &str) -> Response {
    let ffmpeg = fsops::ffmpeg_binary().unwrap();
    let mut child = match tokio::process::Command::new(&ffmpeg)
        .args([
            "-hide_banner", "-loglevel", "error", "-re", "-i", abs,
            "-vf", "scale=1280:-2", "-c:v", "libx264", "-preset", "veryfast",
            "-movflags", "frag_keyframe+empty_moov+default_base_moof+faststart",
            "-f", "mp4", "pipe:1",
        ])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .spawn()
    {
        Ok(c) => c,
        Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "server error"),
    };
    let stdout = child.stdout.take().unwrap();
    use tokio::io::AsyncReadExt;
    let stream = futures_util::stream::unfold(stdout, |mut s| async move {
        let mut buf = vec![0u8; 64 * 1024];
        match s.read(&mut buf).await {
            Ok(0) => None,
            Ok(n) => {
                buf.truncate(n);
                Some((Ok::<_, std::io::Error>(axum::body::Bytes::from(buf)), s))
            }
            Err(_) => None,
        }
    });
    tokio::spawn(async move {
        let _ = child.wait().await;
    });
    let mut res = Response::new(Body::from_stream(stream));
    res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("video/mp4"));
    res.headers_mut().insert(header::CONTENT_DISPOSITION, header::HeaderValue::from_static("inline"));
    res.headers_mut().insert(header::HeaderName::from_static("x-content-type-options"), header::HeaderValue::from_static("nosniff"));
    res
}

// ── Archive ────────────────────────────────────────────

#[derive(serde::Deserialize)]
struct ArchiveBody {
    #[serde(default)]
    paths: Vec<String>,
    #[serde(default)]
    format: String,
}

async fn download_archive(State(state): State<AppState>, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: ArchiveBody = match read_json_only(req_body, 1 << 20).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.paths.is_empty() || body.paths.len() > 500 {
        return err(StatusCode::BAD_REQUEST, "invalid archive selection");
    }
    serve_archive(&state, &req_view, &body.paths, &body.format)
}

async fn zip_dir(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let path = q.get("path").cloned().unwrap_or_default();
    serve_archive(&state, &req, &[path], "zip")
}

async fn zip_multi(State(state): State<AppState>, req: Request<Body>) -> Response {
    download_archive(State(state), req).await
}

fn serve_archive(state: &AppState, req: &Request<Body>, paths: &[String], format: &str) -> Response {
    let format = if format.is_empty() { "zip" } else { format };
    let (extension, mime) = match format {
        "zip" => ("zip", "application/zip"),
        "tar" => ("tar", "application/x-tar"),
        "targz" => ("tar.gz", "application/gzip"),
        _ => return err(StatusCode::BAD_REQUEST, "unsupported archive format"),
    };
    let root = user_root(state, req);
    let entries = match fsops::prepare_archive(&root, paths) {
        Ok(e) => e,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
    };
    let ctx = auth_ctx(req).unwrap();
    if !ctx.is_admin {
        let perms = state.db.list_path_permissions(ctx.user_id);
        for entry in &entries {
            let perm_name = perm_path(state, req, &entry.name);
            if !crate::db::path_permission_allowed(&perms, &perm_name, "read") {
                return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", perm_name));
            }
        }
    }
    // Stream the archive: entries are written on a blocking thread into a
    // channel, the HTTP body pulls chunks as they come. No temp file, no
    // full archive in RAM — works for multi-GB folders.
    let format_owned = format.to_string();
    let (tx, rx) = tokio::sync::mpsc::channel::<Result<bytes::Bytes, std::io::Error>>(32);
    tokio::task::spawn_blocking(move || {
        struct ChanWriter {
            tx: tokio::sync::mpsc::Sender<Result<bytes::Bytes, std::io::Error>>,
            buf: Vec<u8>,
        }
        impl std::io::Write for ChanWriter {
            fn write(&mut self, data: &[u8]) -> std::io::Result<usize> {
                self.buf.extend_from_slice(data);
                if self.buf.len() >= (8 << 20) {
                    self.flush()?;
                }
                Ok(data.len())
            }
            fn flush(&mut self) -> std::io::Result<()> {
                if self.buf.is_empty() {
                    return Ok(());
                }
                let chunk = bytes::Bytes::from(std::mem::take(&mut self.buf));
                match self.tx.blocking_send(Ok(chunk)) {
                    Ok(()) => Ok(()),
                    Err(_) => Err(std::io::Error::new(std::io::ErrorKind::BrokenPipe, "client disconnected")),
                }
            }
        }
        impl Drop for ChanWriter {
            fn drop(&mut self) {
                let _ = self.flush();
            }
        }
        let mut w = ChanWriter { tx, buf: Vec::with_capacity(1 << 20) };
        if let Err(e) = fsops::write_archive(&entries, &format_owned, &mut w) {
            eprintln!("archive stream failed: {}", e);
        }
    });
    let stream = futures_util::stream::unfold(rx, |mut rx| async move {
        let item = rx.recv().await?;
        Some((item, rx))
    });
    let mut res = Response::new(Body::from_stream(stream));
    res.headers_mut().insert(header::CONTENT_TYPE, mime.parse().unwrap_or(header::HeaderValue::from_static("application/zip")));
    res.headers_mut().insert(
        header::CONTENT_DISPOSITION,
        format!("attachment; filename=\"fileship-download.{}\"", extension).parse().unwrap_or(header::HeaderValue::from_static("attachment")),
    );
    res
}

// ── Unzip / Text / Thumb / Search / Reindex ────────────

async fn healthz() -> Response {
    json(StatusCode::OK, &serde_json::json!({"ok": true}))
}

#[derive(serde::Deserialize)]
struct UnzipBody {
    #[serde(default, rename = "zip_path")]
    zip_path: String,
    #[serde(default, rename = "dest_dir")]
    dest_dir: String,
}

async fn unzip_file(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: UnzipBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if let Err(r) = check_perm(&state, &req_view, &body.zip_path, "read") {
        return r;
    }
    if let Err(r) = check_perm(&state, &req_view, &body.dest_dir, "write") {
        return r;
    }
    let root = user_root(&state, &req_view);
    let plan = match fsops::prepare_unzip(&root, &body.zip_path, &body.dest_dir) {
        Ok(p) => p,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
    };
    let (quota, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    let perms = if auth.is_admin() { Vec::new() } else { state.db.list_path_permissions(auth.user_id()) };
    let mut incoming: i64 = 0;
    for entry in &plan.entries {
        if !auth.is_admin() {
            let mut p = perm_path(&state, &req_view, &entry.path);
            loop {
                if !crate::db::path_permission_allowed(&perms, &p, "write") {
                    return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", p));
                }
                if p == "." || p.is_empty() {
                    break;
                }
                let parent = fsops::go_dir(&p);
                if parent == "." || parent == p {
                    break;
                }
                p = parent;
            }
        }
        if !entry.is_dir {
            if !fsops::type_allowed(&entry.path, &allowed_types) {
                return err(StatusCode::FORBIDDEN, &format!("file type not allowed: {}", entry.path));
            }
            incoming += entry.size;
        }
    }
    if quota > 0 {
        let usage = match fsops::dir_size(&root) {
            Ok(u) => u,
            Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot calculate storage usage"),
        };
        if usage > quota || incoming > quota - usage {
            return err(StatusCode::FORBIDDEN, "storage quota exceeded");
        }
    }
    if let Err(e) = fsops::extract_unzip(&root, &plan) {
        return err(StatusCode::BAD_REQUEST, &e.to_string());
    }
    state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "unzip", &format!("{}->{}", body.zip_path, body.dest_dir), "");
    broadcast(&state, "unzip", &body.dest_dir);
    state.indexer.trigger_reindex(auth.user_id(), root);
    empty(StatusCode::CREATED)
}

async fn read_text(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let path = q.get("path").cloned().unwrap_or_default();
    if let Err(r) = check_perm(&state, &req, &path, "read") {
        return r;
    }
    let root = user_root(&state, &req);
    match fsops::read_text_file(&root, &path) {
        Ok(content) => json(StatusCode::OK, &serde_json::json!({"content": content})),
        Err(e) => err(StatusCode::BAD_REQUEST, &e.to_string()),
    }
}

#[derive(serde::Deserialize)]
struct TextBody {
    #[serde(default)]
    path: String,
    #[serde(default)]
    content: String,
}

async fn write_text(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: TextBody = match read_json_only(req_body, 7 << 20).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if let Err(r) = check_perm(&state, &req_view, &body.path, "write") {
        return r;
    }
    if body.content.len() > (1 << 20) {
        return err(StatusCode::PAYLOAD_TOO_LARGE, "file too large for editor (max 1MB)");
    }
    let (quota, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    if !fsops::type_allowed(&body.path, &allowed_types) {
        return err(StatusCode::FORBIDDEN, "file type not allowed");
    }
    let root = user_root(&state, &req_view);
    let abs = match fsops::resolve(&root, &body.path) {
        Ok(a) => a,
        Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
    };
    if quota > 0 {
        let mut usage = match fsops::dir_size(&root) {
            Ok(u) => u,
            Err(_) => return err(StatusCode::INTERNAL_SERVER_ERROR, "cannot calculate storage usage"),
        };
        match std::fs::metadata(&abs) {
            Ok(m) if m.file_type().is_file() => usage -= m.len() as i64,
            Err(_) => {}
            _ => {}
        }
        if usage > quota || body.content.len() as i64 > quota - usage {
            return err(StatusCode::FORBIDDEN, "storage quota exceeded");
        }
    }
    if let Err(e) = fsops::write_text_file(&root, &body.path, &body.content) {
        return err(StatusCode::BAD_REQUEST, &e.to_string());
    }
    state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "edit", &body.path, "");
    broadcast(&state, "edit", &body.path);
    state.indexer.trigger_reindex(auth.user_id(), root);
    empty(StatusCode::NO_CONTENT)
}

async fn thumbnail(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let rel = q.get("path").cloned().unwrap_or_default();
    if let Err(r) = check_perm(&state, &req, &rel, "read") {
        return r;
    }
    let root = user_root(&state, &req);
    let tdir = thumb_dir(&state);
    if let Some(name) = fsops::thumbnail_exists(&root, &tdir, &rel) {
        let abs = format!("{}/{}", tdir, name);
        let mut res = serve_file_response(&abs, None, &[("cache-control", "private, max-age=3600")], &req);
        return res;
    }
    if fsops::is_image(&rel) || fsops::is_pdf(&rel) {
        match fsops::generate_thumbnail(&root, &rel, &tdir) {
            Ok(name) => {
                let abs = format!("{}/{}", tdir, name);
                return serve_file_response(&abs, None, &[("cache-control", "private, max-age=3600")], &req);
            }
            Err(e) => return err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
        }
    }
    if fsops::is_video(&rel) {
        match fsops::generate_video_thumbnail(&root, &rel, &tdir) {
            Ok(name) => {
                let abs = format!("{}/{}", tdir, name);
                return serve_file_response(&abs, None, &[("cache-control", "private, max-age=3600")], &req);
            }
            Err(_) => return err(StatusCode::NOT_FOUND, "thumbnail not available"),
        }
    }
    err(StatusCode::BAD_REQUEST, "not an image or video")
}

async fn search_files(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let query = q.get("q").cloned().unwrap_or_default();
    if query.is_empty() {
        return err(StatusCode::BAD_REQUEST, "missing query");
    }
    let has_source = !header_str(&req, "x-source-id").is_empty() || q.contains_key("source_id");
    let root = user_root(&state, &req);
    let mut results: Vec<FileInfo> = Vec::new();
    if !has_source {
        results = state.db.index_search(auth.user_id(), &query, 200);
    }
    if has_source || results.is_empty() {
        match fsops::search(&root, &query) {
            Ok(r) => results = r,
            Err(e) => return err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
        }
    }
    let perms = if auth.is_admin() { Vec::new() } else { state.db.list_path_permissions(auth.user_id()) };
    let visible: Vec<FileInfo> = results
        .into_iter()
        .filter(|f| {
            if auth.is_admin() {
                return true;
            }
            let pp = perm_path(&state, &req, &f.path);
            crate::db::path_permission_allowed(&perms, &pp, "read")
        })
        .collect();
    json(StatusCode::OK, &visible)
}

async fn reindex(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let root = user_root(&state, &req);
    state.indexer.trigger_reindex(auth.user_id(), root);
    json(StatusCode::OK, &serde_json::json!({"status": "indexing"}))
}

async fn i18n(State(_state): State<AppState>, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let mut lang = q.get("lang").cloned().unwrap_or_default();
    if lang.is_empty() {
        let accept = header_str(&req, "accept-language");
        let bytes = accept.as_bytes();
        if bytes.len() >= 2 {
            lang = String::from_utf8_lossy(&bytes[..2]).to_string();
        }
    }
    let valid = lang.len() == 2 && lang.chars().all(|c| c.is_ascii_lowercase());
    if !valid {
        lang = "en".to_string();
    }
    let mut content = crate::static_fs::locale(&lang);
    if content.is_none() {
        content = crate::static_fs::locale("en");
    }
    match content {
        Some(c) => {
            let mut res = Response::new(Body::from(c));
            res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("application/json"));
            res
        }
        None => err(StatusCode::INTERNAL_SERVER_ERROR, "translation unavailable"),
    }
}

// ── Me / User ──────────────────────────────────────────

async fn me(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    match state.db.get_user_by_id(auth.user_id()) {
        Some(u) => json(StatusCode::OK, &u),
        None => err(StatusCode::NOT_FOUND, "not found"),
    }
}

#[derive(serde::Deserialize)]
struct PasswordBody {
    #[serde(default, rename = "old_password")]
    old_password: String,
    #[serde(default, rename = "new_password")]
    new_password: String,
}

async fn change_password(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: PasswordBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let user = match state.db.get_user_by_id(auth.user_id()) {
        Some(u) => u,
        None => return err(StatusCode::NOT_FOUND, "not found"),
    };
    if !crate::auth::verify_password(&user.password_hash, &body.old_password) {
        return err(StatusCode::UNAUTHORIZED, "wrong password");
    }
    if body.new_password.len() < 8 {
        return err(StatusCode::BAD_REQUEST, "password too short (min 8 chars)");
    }
    let hash = crate::auth::hash_password(&body.new_password).unwrap_or_default();
    state.db.update_user_password(auth.user_id(), &hash);
    empty(StatusCode::NO_CONTENT)
}

async fn get_settings(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (quota, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    let root = user_root(&state, &req);
    let disk_usage = cached_dir_size(&root);
    json(
        StatusCode::OK,
        &serde_json::json!({
            "quota_bytes": quota,
            "allowed_types": allowed_types,
            "disk_usage": disk_usage,
            "root_path": root,
        }),
    )
}

async fn list_users(State(state): State<AppState>, _auth: Auth, _req: Request<Body>) -> Response {
    json(StatusCode::OK, &state.db.list_users_with_quota())
}

#[derive(serde::Deserialize)]
struct CreateUserBody {
    #[serde(default)]
    username: String,
    #[serde(default)]
    password: String,
    #[serde(default, rename = "is_admin")]
    is_admin: bool,
    #[serde(default, rename = "root_path")]
    root_path: String,
}

async fn create_user(State(state): State<AppState>, _auth: Auth, req: Request<Body>) -> Response {
    let mut body: CreateUserBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.root_path.is_empty() {
        body.root_path = "/".to_string();
    }
    match state.db.create_user(&body.username, &body.password, body.is_admin, &body.root_path) {
        Ok(()) => empty(StatusCode::CREATED),
        Err(e) => err(StatusCode::CONFLICT, &e.to_string()),
    }
}

async fn delete_user(State(state): State<AppState>, auth: Auth, AxPath(id): AxPath<i64>, _req: Request<Body>) -> Response {
    if auth.user_id() == id {
        return err(StatusCode::BAD_REQUEST, "cannot delete your own account");
    }
    let admin_count = state.db.admin_count();
    if admin_count <= 1 {
        if let Some(u) = state.db.get_user_by_id(id) {
            if u.is_admin {
                return err(StatusCode::BAD_REQUEST, "cannot delete the last admin");
            }
        }
    }
    state.db.delete_user(id);
    empty(StatusCode::NO_CONTENT)
}

#[derive(serde::Deserialize)]
struct UpdateUserBody {
    #[serde(default)]
    password: String,
    #[serde(default, rename = "root_path")]
    root_path: String,
}

async fn update_user(State(state): State<AppState>, _auth: Auth, AxPath(id): AxPath<i64>, req: Request<Body>) -> Response {
    let body: UpdateUserBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if !body.password.is_empty() {
        if body.password.len() < 8 {
            return err(StatusCode::BAD_REQUEST, "password too short (min 8 chars)");
        }
        let hash = crate::auth::hash_password(&body.password).unwrap_or_default();
        state.db.update_user_password(id, &hash);
    }
    if !body.root_path.is_empty() {
        state.db.update_user_root_path(id, &body.root_path);
    }
    empty(StatusCode::NO_CONTENT)
}

async fn audit_log(State(state): State<AppState>, _auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let limit: i64 = q.get("limit").and_then(|v| v.parse().ok()).unwrap_or(0);
    let limit = if limit <= 0 { 100 } else { limit };
    json(StatusCode::OK, &state.db.get_audit_log(limit))
}

async fn stats(State(state): State<AppState>, _auth: Auth, _req: Request<Body>) -> Response {
    let mut users = state.db.list_users_with_quota();
    let disk_usage = cached_dir_size(&state.cfg.root_path);
    for u in users.iter_mut() {
        let root = resolve_root(&state, &u.root_path);
        u.disk_usage = cached_dir_size(&root);
    }
    let user_count = users.len();
    json(
        StatusCode::OK,
        &serde_json::json!({
            "user_count": user_count,
            "disk_usage": disk_usage,
            "root_path": state.cfg.root_path,
            "users": users,
        }),
    )
}

#[derive(serde::Deserialize)]
struct QuotaBody {
    #[serde(default, rename = "quota_bytes")]
    quota_bytes: i64,
    #[serde(default, rename = "allowed_types")]
    allowed_types: String,
}

async fn set_quota(State(state): State<AppState>, _auth: Auth, AxPath(id): AxPath<i64>, req: Request<Body>) -> Response {
    let body: QuotaBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    state.db.set_user_quota(id, body.quota_bytes);
    state.db.set_user_allowed_types(id, &body.allowed_types);
    empty(StatusCode::NO_CONTENT)
}

// ── API Tokens ─────────────────────────────────────────

async fn list_api_tokens(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    json(StatusCode::OK, &state.db.list_api_tokens(auth.user_id()))
}

#[derive(serde::Deserialize)]
struct ApiTokenBody {
    #[serde(default)]
    name: String,
    #[serde(default, rename = "expires_in_days")]
    expires_in_days: Option<i64>,
}

async fn create_api_token(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: ApiTokenBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(_) => return err(StatusCode::BAD_REQUEST, "name required"),
    };
    if body.name.is_empty() {
        return err(StatusCode::BAD_REQUEST, "name required");
    }
    let expires_at = match body.expires_in_days {
        Some(d) if d > 0 => Some(chrono::Local::now() + chrono::Duration::days(d)),
        _ => None,
    };
    match state.db.create_api_token(auth.user_id(), &body.name, &expires_at) {
        Ok((_plain, mut tok)) => {
            tok.token = _plain;
            json(StatusCode::CREATED, &tok)
        }
        Err(e) => err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
    }
}

async fn delete_api_token(State(state): State<AppState>, auth: Auth, AxPath(id): AxPath<i64>, _req: Request<Body>) -> Response {
    state.db.delete_api_token(id, auth.user_id());
    empty(StatusCode::NO_CONTENT)
}

// ── Favorites ──────────────────────────────────────────

async fn list_favorites(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    json(StatusCode::OK, &state.db.list_favorites(auth.user_id()))
}

#[derive(serde::Deserialize)]
struct FavoriteBody {
    #[serde(default)]
    path: String,
    #[serde(default)]
    name: String,
    #[serde(default, rename = "is_dir")]
    is_dir: bool,
}

async fn add_favorite(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: FavoriteBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.path.is_empty() {
        return err(StatusCode::BAD_REQUEST, "path required");
    }
    state.db.add_favorite(auth.user_id(), &body.path, &body.name, body.is_dir);
    empty(StatusCode::CREATED)
}

async fn remove_favorite(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let q = query_map(&req);
    let path = q.get("path").cloned().unwrap_or_default();
    if path.is_empty() {
        return err(StatusCode::BAD_REQUEST, "path required");
    }
    state.db.remove_favorite(auth.user_id(), &path);
    empty(StatusCode::NO_CONTENT)
}

// ── Permissions (Admin) ────────────────────────────────

async fn list_permissions(State(state): State<AppState>, _auth: Auth, AxPath(id): AxPath<i64>, _req: Request<Body>) -> Response {
    json(StatusCode::OK, &state.db.list_path_permissions(id))
}

async fn set_permission(State(state): State<AppState>, _auth: Auth, AxPath(id): AxPath<i64>, req: Request<Body>) -> Response {
    let mut p: PathPermission = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if p.path.is_empty() {
        return err(StatusCode::BAD_REQUEST, "path required");
    }
    p.user_id = id;
    state.db.set_path_permission(&p);
    empty(StatusCode::CREATED)
}

async fn delete_permission(State(state): State<AppState>, _auth: Auth, AxPath(pid): AxPath<i64>, _req: Request<Body>) -> Response {
    state.db.delete_path_permission(pid);
    empty(StatusCode::NO_CONTENT)
}

// ── Shares ─────────────────────────────────────────────

#[derive(serde::Deserialize)]
struct CreateShareBody {
    #[serde(default)]
    path: String,
    #[serde(default, rename = "is_dir")]
    is_dir: bool,
    #[serde(default, rename = "expires_in_hours")]
    expires_in_hours: Option<i64>,
    #[serde(default)]
    password: String,
    #[serde(default, rename = "download_limit")]
    download_limit: i64,
    #[serde(default, rename = "allow_upload")]
    allow_upload: bool,
    #[serde(default, rename = "allow_edit")]
    allow_edit: bool,
}

async fn create_share(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, req_body) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: CreateShareBody = match read_json_only(req_body, 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let clean = match fsops::clean_relative_path(&body.path) {
        Ok(c) => c,
        Err(_) => return err(StatusCode::BAD_REQUEST, "invalid share path"),
    };
    let root = user_root(&state, &req_view);
    let abs = match fsops::resolve(&root, &clean) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return err(StatusCode::NOT_FOUND, "not found"),
        Err(_) => return err(StatusCode::BAD_REQUEST, "invalid share target"),
    };
    if !md.is_dir() && !md.file_type().is_file() {
        return err(StatusCode::BAD_REQUEST, "invalid share target");
    }
    let eff = perm_path(&state, &req_view, &clean);
    if !auth.is_admin() {
        let perms = state.db.list_path_permissions(auth.user_id());
        if !crate::db::path_permission_allowed(&perms, &eff, "read") || !crate::db::path_permission_allowed(&perms, &eff, "share") {
            return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", eff));
        }
        if md.is_dir() {
            match fsops::prepare_archive(&root, &[clean.clone()]) {
                Ok(entries) => {
                    for entry in &entries {
                        let pe = perm_path(&state, &req_view, &entry.name);
                        if !crate::db::path_permission_allowed(&perms, &pe, "read") || !crate::db::path_permission_allowed(&perms, &pe, "share") {
                            return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", pe));
                        }
                    }
                }
                Err(e) => return err(StatusCode::BAD_REQUEST, &e.to_string()),
            }
        }
    }
    let expires_at = body.expires_in_hours.map(|h| chrono::Local::now() + chrono::Duration::hours(h));
    let password_hash = if body.password.is_empty() {
        String::new()
    } else {
        crate::auth::hash_password(&body.password).unwrap_or_default()
    };
    match state.db.create_share_link(&eff, body.is_dir, auth.user_id(), &expires_at, &password_hash, body.download_limit, body.allow_upload, body.allow_edit) {
        Ok(token) => json(StatusCode::OK, &serde_json::json!({"token": token})),
        Err(e) => err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
    }
}

async fn list_shares(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    let mut links = state.db.list_share_links(auth.user_id());
    for l in links.iter_mut() {
        l.has_password = !l.password_hash.is_empty();
    }
    json(StatusCode::OK, &links)
}

async fn delete_share(State(state): State<AppState>, auth: Auth, AxPath(token): AxPath<String>, _req: Request<Body>) -> Response {
    state.db.delete_share_link(&token, auth.user_id());
    empty(StatusCode::NO_CONTENT)
}

// ── Trash ──────────────────────────────────────────────

async fn trash_list(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    let expired = state.db.trash_purge_expired(auth.user_id());
    let tdir = trash_dir(&state);
    for name in expired {
        let _ = std::fs::remove_dir_all(tdir.join(&name));
        let _ = std::fs::remove_file(tdir.join(&name));
    }
    json(StatusCode::OK, &state.db.trash_list(auth.user_id()))
}

async fn trash_delete_permanent(State(state): State<AppState>, auth: Auth, AxPath(name): AxPath<String>, _req: Request<Body>) -> Response {
    let item = match state.db.trash_get(auth.user_id(), &name) {
        Some(i) => i,
        None => return err(StatusCode::NOT_FOUND, "not found"),
    };
    let tdir = trash_dir(&state);
    let _ = std::fs::remove_dir_all(tdir.join(&item.trash_name));
    let _ = std::fs::remove_file(tdir.join(&item.trash_name));
    state.db.trash_delete(auth.user_id(), &name);
    empty(StatusCode::NO_CONTENT)
}

async fn trash_restore(State(state): State<AppState>, auth: Auth, AxPath(name): AxPath<String>, req: Request<Body>) -> Response {
    let item = match state.db.trash_get(auth.user_id(), &name) {
        Some(i) => i,
        None => return err(StatusCode::NOT_FOUND, "not found"),
    };
    let tdir = trash_dir(&state);
    let trash_path = tdir.join(&item.trash_name);
    let root = user_root(&state, &req);
    let mut restore_rel = fsops::clean_relative_path(&item.orig_path).unwrap_or_default();
    if restore_rel.is_empty() {
        restore_rel = fsops::clean_relative_path(&item.name).unwrap_or_default();
    }
    if restore_rel.is_empty() {
        return err(StatusCode::BAD_REQUEST, "invalid restore path");
    }
    let mut restore_path = match fsops::resolve(&root, &restore_rel) {
        Ok(p) => p,
        Err(_) => {
            restore_rel = Path::new(&item.name).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or(item.name.clone());
            match fsops::resolve(&root, &restore_rel) {
                Ok(p) => p,
                Err(_) => return err(StatusCode::BAD_REQUEST, "invalid restore path"),
            }
        }
    };
    if Path::new(&restore_path).exists() {
        let ext = Path::new(&restore_path).extension().map(|e| format!(".{}", e.to_string_lossy())).unwrap_or_default();
        let base = restore_path.trim_end_matches(&ext).to_string();
        restore_path = format!("{}_restored_{}{}", base, chrono::Local::now().timestamp(), ext);
        if let Ok(rel) = Path::new(&restore_path).strip_prefix(&root) {
            restore_rel = rel.to_string_lossy().to_string();
        }
    }
    let (quota, allowed_types) = state.db.get_user_quota_and_types(auth.user_id());
    if !auth.is_admin() {
        let perms = state.db.list_path_permissions(auth.user_id());
        let mut cur = restore_rel.clone();
        loop {
            if !crate::db::path_permission_allowed(&perms, &cur, "write") {
                return err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", cur));
            }
            if cur.is_empty() {
                break;
            }
            let parent = fsops::go_dir(&cur);
            if parent == "." || parent == cur {
                break;
            }
            cur = parent;
        }
    }
    if !item.is_dir && !allowed_types.is_empty() && !fsops::type_allowed(&fsops::go_dir(&restore_rel), &allowed_types) {
        // Go checks TypeAllowed on base — simplified: check name
        if !fsops::type_allowed(&item.name, &allowed_types) {
            return err(StatusCode::FORBIDDEN, "file type not allowed");
        }
    }
    if quota > 0 {
        let used = fsops::dir_size(&root).unwrap_or(0);
        if used > quota || item.size > quota - used {
            return err(StatusCode::FORBIDDEN, "storage quota exceeded");
        }
    }
    if let Some(parent) = Path::new(&restore_path).parent() {
        if std::fs::create_dir_all(parent).is_err() {
            return err(StatusCode::INTERNAL_SERVER_ERROR, "could not restore");
        }
    }
    if let Err(e) = fsops::transfer(&trash_path.to_string_lossy(), &restore_path) {
        return err(StatusCode::INTERNAL_SERVER_ERROR, &format!("could not restore: {}", e));
    }
    state.db.trash_delete(auth.user_id(), &name);
    state.db.dir_size_cache_set(&root, 0);
    state.indexer.trigger_reindex(auth.user_id(), root);
    state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "restore", &restore_rel, "");
    broadcast(&state, "restore", &restore_rel);
    empty(StatusCode::NO_CONTENT)
}

async fn trash_empty(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    let tdir = trash_dir(&state);
    for item in state.db.trash_list(auth.user_id()) {
        let _ = std::fs::remove_dir_all(tdir.join(&item.trash_name));
        let _ = std::fs::remove_file(tdir.join(&item.trash_name));
        state.db.trash_delete(auth.user_id(), &item.trash_name);
    }
    empty(StatusCode::NO_CONTENT)
}

// ── Sources ────────────────────────────────────────────

fn validate_source_root(state: &AppState, auth: &Auth, raw: &str) -> Result<String, Response> {
    let user = match state.db.get_user_by_id(auth.user_id()) {
        Some(u) => u,
        None => return Err(err(StatusCode::UNAUTHORIZED, "unauthorized")),
    };
    let base = resolve_root(state, &user.root_path);
    if user.is_admin && fsops::is_abs(raw) {
        let clean = fsops::go_clean(raw);
        match std::fs::metadata(&clean) {
            Ok(m) if m.is_dir() => return Ok(clean),
            _ => return Err(err(StatusCode::BAD_REQUEST, "source root directory not found")),
        }
    }
    let clean = match fsops::clean_relative_path(raw) {
        Ok(c) => c,
        Err(_) => return Err(err(StatusCode::BAD_REQUEST, "invalid source root_path")),
    };
    let abs = match fsops::resolve(&base, &clean) {
        Ok(a) => a,
        Err(_) => return Err(err(StatusCode::FORBIDDEN, "forbidden source root_path")),
    };
    match std::fs::metadata(&abs) {
        Ok(m) if m.is_dir() => {}
        _ => return Err(err(StatusCode::BAD_REQUEST, "source root directory not found")),
    }
    if !user.is_admin && !state.db.check_permission(user.id, &clean, "read") {
        return Err(err(StatusCode::FORBIDDEN, &format!("permission denied for path: {}", clean)));
    }
    Ok(clean)
}

#[derive(serde::Deserialize)]
struct SourceBody {
    #[serde(default)]
    name: String,
    #[serde(default, rename = "root_path")]
    root_path: String,
    #[serde(default, rename = "is_default")]
    is_default: bool,
}

async fn list_sources(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    json(StatusCode::OK, &state.db.list_sources(auth.user_id()))
}

async fn create_source(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: SourceBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.name.is_empty() || body.root_path.is_empty() {
        return err(StatusCode::BAD_REQUEST, "name and root_path required");
    }
    let valid = match validate_source_root(&state, &auth, &body.root_path) {
        Ok(v) => v,
        Err(r) => return r,
    };
    match state.db.create_source(auth.user_id(), &body.name, &valid, body.is_default) {
        Ok(src) => json(StatusCode::CREATED, &src),
        Err(e) => err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
    }
}

async fn update_source(State(state): State<AppState>, auth: Auth, AxPath(id): AxPath<i64>, req: Request<Body>) -> Response {
    let body: SourceBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.name.is_empty() || body.root_path.is_empty() {
        return err(StatusCode::BAD_REQUEST, "bad request");
    }
    let valid = match validate_source_root(&state, &auth, &body.root_path) {
        Ok(v) => v,
        Err(r) => return r,
    };
    state.db.update_source(auth.user_id(), id, &body.name, &valid, body.is_default);
    empty(StatusCode::NO_CONTENT)
}

async fn delete_source(State(state): State<AppState>, auth: Auth, AxPath(id): AxPath<i64>, _req: Request<Body>) -> Response {
    state.db.delete_source(auth.user_id(), id);
    empty(StatusCode::NO_CONTENT)
}

// ── Cross-source transfer (server <-> agent, agent <-> agent) ──

#[derive(serde::Deserialize)]
struct CrossTransferBody {
    #[serde(default)]
    src_agent_id: i64,
    #[serde(default)]
    src_path: String,
    #[serde(default)]
    dst_agent_id: i64,
    #[serde(default)]
    dst_path: String,
    #[serde(default)]
    mode: String,
}

enum Side {
    Local(String),          // absolute root dir
    Agent(i64),
}

const TRANSFER_CHUNK: u64 = 4 << 20;

fn xfer_join(base: &str, rel: &str) -> String {
    if rel.is_empty() {
        base.to_string()
    } else if base == "/" {
        format!("/{}", rel)
    } else {
        format!("{}/{}", base.trim_end_matches('/'), rel)
    }
}

async fn agent_call(
    hub: &std::sync::Arc<crate::agent::Hub>,
    agent_id: i64,
    action: &str,
    path: &str,
    offset: u64,
) -> Result<crate::agent::Message, String> {
    let mut p = HashMap::new();
    p.insert("path".to_string(), path.to_string());
    p.insert("offset".to_string(), offset.to_string());
    hub.request(agent_id, action, p, Vec::new()).await
}

async fn xfer_ensure_dst(state: &AppState, dst: &Side, dst_rel: &str) -> Result<(), String> {
    match dst {
        Side::Local(root) => {
            let abs = fsops::resolve(root, dst_rel).map_err(|e| e.to_string())?;
            if let Some(parent) = std::path::Path::new(&abs).parent() {
                tokio::fs::create_dir_all(parent).await.map_err(|e| e.to_string())?;
            }
            tokio::fs::OpenOptions::new()
                .create(true)
                .write(true)
                .truncate(true)
                .open(&abs)
                .await
                .map_err(|e| e.to_string())?;
        }
        Side::Agent(id) => {
            let mut p = HashMap::new();
            p.insert("path".to_string(), dst_rel.to_string());
            p.insert("offset".to_string(), "0".to_string());
            let res = state.agent_hub.request(*id, "write_chunk", p, Vec::new()).await?;
            if res.status >= 400 {
                return Err(res.error.clone().unwrap_or_else(|| "write failed".to_string()));
            }
        }
    }
    Ok(())
}

async fn xfer_copy_one(
    state: &AppState,
    src: &Side,
    src_rel: &str,
    dst: &Side,
    dst_rel: &str,
) -> Result<u64, String> {
    let mut offset: u64 = 0;
    let mut total: u64 = 0;
    let mut wrote_any = false;
    loop {
        // read chunk
        let data: Vec<u8> = match src {
            Side::Local(root) => {
                use tokio::io::{AsyncReadExt, AsyncSeekExt};
                let abs = fsops::resolve(root, src_rel).map_err(|e| e.to_string())?;
                let mut f = tokio::fs::File::open(&abs).await.map_err(|e| e.to_string())?;
                f.seek(std::io::SeekFrom::Start(offset)).await.map_err(|e| e.to_string())?;
                let mut buf = vec![0u8; TRANSFER_CHUNK as usize];
                let n = f.read(&mut buf).await.map_err(|e| e.to_string())?;
                buf.truncate(n);
                total = std::fs::metadata(&abs).map(|m| m.len()).unwrap_or(total);
                buf
            }
            Side::Agent(id) => {
                let res = agent_call(&state.agent_hub, *id, "read_chunk", src_rel, offset).await?;
                if res.status >= 400 {
                    return Err(res.error.clone().unwrap_or_else(|| "read failed".to_string()));
                }
                if let Some(p) = res.params.as_ref() {
                    if let Some(s) = p.get("size") {
                        total = s.parse().unwrap_or(total);
                    }
                }
                res.data
            }
        };
        let n = data.len() as u64;
        if n == 0 {
            if !wrote_any {
                xfer_ensure_dst(state, dst, dst_rel).await?;
            }
            break;
        }
        // write chunk
        match dst {
            Side::Local(root) => {
                use tokio::io::{AsyncSeekExt, AsyncWriteExt};
                let abs = fsops::resolve(root, dst_rel).map_err(|e| e.to_string())?;
                if let Some(parent) = std::path::Path::new(&abs).parent() {
                    tokio::fs::create_dir_all(parent).await.map_err(|e| e.to_string())?;
                }
                let mut f = tokio::fs::OpenOptions::new()
                    .create(true)
                    .write(true)
                    .truncate(offset == 0)
                    .open(&abs)
                    .await
                    .map_err(|e| e.to_string())?;
                if offset > 0 {
                    f.seek(std::io::SeekFrom::Start(offset)).await.map_err(|e| e.to_string())?;
                }
                f.write_all(&data).await.map_err(|e| e.to_string())?;
            }
            Side::Agent(id) => {
                let mut p = HashMap::new();
                p.insert("path".to_string(), dst_rel.to_string());
                p.insert("offset".to_string(), offset.to_string());
                let res = state.agent_hub.request(*id, "write_chunk", p, data).await?;
                if res.status >= 400 {
                    return Err(res.error.clone().unwrap_or_else(|| "write failed".to_string()));
                }
            }
        }
        offset += n;
        wrote_any = true;
        if total > 0 && offset >= total {
            break;
        }
        if total == 0 && n < TRANSFER_CHUNK {
            break;
        }
    }
    Ok(total.max(offset))
}

async fn xfer_delete(state: &AppState, side: &Side, rel: &str) -> Result<(), String> {
    match side {
        Side::Local(root) => fsops::delete(root, rel).map_err(|e| e.to_string()),
        Side::Agent(id) => {
            let mut p = HashMap::new();
            p.insert("path".to_string(), rel.to_string());
            let res = state.agent_hub.request(*id, "delete", p, Vec::new()).await?;
            if res.status >= 400 {
                return Err(res.error.clone().unwrap_or_else(|| "delete failed".to_string()));
            }
            Ok(())
        }
    }
}

// POST /api/transfer — copy/move between this server and remote agents
// (agent_id 0 = this server). Files are streamed through the server in
// 4 MiB chunks, so arbitrarily large files work without buffering in RAM.
async fn cross_transfer(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let (parts, body_req) = req.into_parts();
    let req_view = Request::from_parts(parts, Body::empty());
    let body: CrossTransferBody = match read_json_only(body_req, 256 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    let mode = if body.mode == "move" { "move" } else { "copy" };
    let src_rel = match fsops::clean_relative_path(&body.src_path) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "invalid source path"),
    };
    let dst_rel = match fsops::clean_relative_path(&body.dst_path) {
        Ok(r) => r,
        Err(_) => return err(StatusCode::BAD_REQUEST, "invalid destination path"),
    };
    if src_rel.is_empty() || dst_rel.is_empty() {
        return err(StatusCode::BAD_REQUEST, "paths required");
    }
    if body.src_agent_id == body.dst_agent_id && body.src_path == body.dst_path {
        return err(StatusCode::BAD_REQUEST, "source and destination are identical");
    }
    let user_id = auth.user_id();
    // Verify agents belong to this user
    for id in [body.src_agent_id, body.dst_agent_id] {
        if id > 0 {
            if state.db.get_agent_device_by_id(user_id, id).is_none() {
                return err(StatusCode::NOT_FOUND, "agent device not found");
            }
            if !state.agent_hub.is_online(id) {
                return err(StatusCode::SERVICE_UNAVAILABLE, "remote device is offline");
            }
        }
    }
    let root = user_root(&state, &req_view);
    let src_side = if body.src_agent_id > 0 {
        Side::Agent(body.src_agent_id)
    } else {
        if let Err(r) = check_perm(&state, &req_view, &src_rel, "read") {
            return r;
        }
        Side::Local(root.clone())
    };
    let dst_side = if body.dst_agent_id > 0 {
        Side::Agent(body.dst_agent_id)
    } else {
        if let Err(r) = check_perm(&state, &req_view, &dst_rel, "write") {
            return r;
        }
        Side::Local(root.clone())
    };
    if body.src_agent_id == body.dst_agent_id {
        // Same side: use the fast in-place copy/rename.
        let res = if mode == "move" {
            fsops::rename(&root, &src_rel, &dst_rel)
        } else {
            fsops::copy(&root, &src_rel, &dst_rel)
        };
        return match res {
            Ok(()) => {
                state.indexer.trigger_reindex(user_id, root);
                state.db.log_action(user_id, &username_of(&state, user_id), mode, &format!("{}->{}", src_rel, dst_rel), "");
                broadcast(&state, mode, &dst_rel);
                empty(StatusCode::CREATED)
            }
            Err(e) => err(StatusCode::BAD_REQUEST, &e.to_string()),
        };
    }

    // Collect the file list on the source side.
    struct XferItem {
        rel: String,
        is_dir: bool,
    }
    let (mut items, src_is_dir): (Vec<XferItem>, bool) = match &src_side {
        Side::Local(r) => {
            let abs = match fsops::resolve(r, &src_rel) {
                Ok(a) => a,
                Err(e) => return err(StatusCode::FORBIDDEN, &e.to_string()),
            };
            let md = match std::fs::metadata(&abs) {
                Ok(m) => m,
                Err(e) => return err(StatusCode::NOT_FOUND, &e.to_string()),
            };
            if !md.is_dir() {
                (vec![XferItem { rel: src_rel.clone(), is_dir: false }], false)
            } else {
                let mut out = Vec::new();
                let mut stack = vec![abs.clone()];
                while let Some(dir) = stack.pop() {
                    let entries = match std::fs::read_dir(&dir) {
                        Ok(e) => e,
                        Err(_) => continue,
                    };
                    for e in entries.flatten() {
                        let p = e.path();
                        let m = match std::fs::symlink_metadata(&p) {
                            Ok(m) => m,
                            Err(_) => continue,
                        };
                        let rel = p
                            .strip_prefix(&abs)
                            .map(|r| r.to_string_lossy().replace('\\', "/"))
                            .unwrap_or_default();
                        out.push(XferItem { rel: xfer_join(&src_rel, &rel), is_dir: m.is_dir() });
                        if m.is_dir() {
                            stack.push(p.to_string_lossy().to_string());
                        }
                    }
                }
                (out, true)
            }
        }
        Side::Agent(id) => {
            let mut p = HashMap::new();
            p.insert("path".to_string(), src_rel.clone());
            p.insert("offset".to_string(), "0".to_string());
            match state.agent_hub.request(*id, "walk", p, Vec::new()).await {
                Ok(res) if res.status < 400 => {
                    let mut out = Vec::new();
                    let mut any = false;
                    if let Some(meta) = res.meta.clone() {
                        if let Some(arr) = meta.get("items").and_then(|v| v.as_array()) {
                            for it in arr {
                                any = true;
                                out.push(XferItem {
                                    rel: xfer_join(
                                        &src_rel,
                                        it.get("path").and_then(|v| v.as_str()).unwrap_or(""),
                                    ),
                                    is_dir: it.get("is_dir").and_then(|v| v.as_bool()).unwrap_or(false),
                                });
                            }
                        }
                    }
                    if !any {
                        (vec![XferItem { rel: src_rel.clone(), is_dir: false }], false)
                    } else {
                        (out, true)
                    }
                }
                _ => (vec![XferItem { rel: src_rel.clone(), is_dir: false }], false),
            }
        }
    };

    // Ensure the destination directory exists.
    let dst_parent = std::path::Path::new(&dst_rel)
        .parent()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_default();
    if src_is_dir {
        match &dst_side {
            Side::Local(r) => {
                let abs = match fsops::resolve(r, &dst_rel) {
                    Ok(a) => a,
                    Err(e) => return err(StatusCode::FORBIDDEN, &e.to_string()),
                };
                if let Err(e) = std::fs::create_dir_all(&abs) {
                    return err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string());
                }
            }
            Side::Agent(id) => {
                let mut p = HashMap::new();
                p.insert("path".to_string(), dst_rel.clone());
                let res = match state.agent_hub.request(*id, "mkdir", p, Vec::new()).await {
                    Ok(r) => r,
                    Err(e) => return err(StatusCode::BAD_GATEWAY, &e),
                };
                if res.status >= 400 {
                    return err(StatusCode::BAD_GATEWAY, res.error.as_deref().unwrap_or("mkdir failed"));
                }
            }
        }
    } else if !dst_parent.is_empty() {
        match &dst_side {
            Side::Local(r) => {
                if let Ok(abs) = fsops::resolve(r, &dst_parent) {
                    let _ = std::fs::create_dir_all(abs);
                }
            }
            Side::Agent(id) => {
                let mut p = HashMap::new();
                p.insert("path".to_string(), dst_parent.clone());
                let res = state.agent_hub.request(*id, "mkdir", p, Vec::new()).await;
                if let Ok(res) = res {
                    if res.status >= 400 {
                        return err(StatusCode::BAD_GATEWAY, res.error.as_deref().unwrap_or("mkdir failed"));
                    }
                }
            }
        }
    }

    let mut bytes_total: u64 = 0;
    let mut files_done: u64 = 0;
    for item in items.drain(..) {
        if item.is_dir {
            continue;
        }
        let rel_in_src = item.rel;
        let rel_in_dst = if src_is_dir {
            let sub = rel_in_src.strip_prefix(&src_rel).unwrap_or(&rel_in_src);
            let sub = sub.trim_start_matches('/');
            xfer_join(&dst_rel, sub)
        } else {
            dst_rel.clone()
        };
        match xfer_copy_one(&state, &src_side, &rel_in_src, &dst_side, &rel_in_dst).await {
            Ok(n) => {
                bytes_total += n;
                files_done += 1;
            }
            Err(e) => {
                return err(StatusCode::BAD_GATEWAY, &format!("transferring {}: {}", rel_in_src, e));
            }
        }
    }

    if mode == "move" {
        if let Err(e) = xfer_delete(&state, &src_side, &src_rel).await {
            return err(StatusCode::BAD_GATEWAY, &format!("copied, but deleting source failed: {}", e));
        }
    }
    if matches!(src_side, Side::Local(_)) {
        state.indexer.trigger_reindex(user_id, root.clone());
    }
    if matches!(dst_side, Side::Local(_)) {
        state.indexer.trigger_reindex(user_id, root);
    }
    state.db.log_action(user_id, &username_of(&state, user_id), mode, &format!("{}->{} ({} files)", src_rel, dst_rel, files_done), "");
    broadcast(&state, mode, &dst_rel);
    json(StatusCode::CREATED, &serde_json::json!({"files": files_done, "bytes": bytes_total}))
}

// ── Agents ─────────────────────────────────────────────

#[derive(serde::Deserialize)]
struct AgentBody {
    #[serde(default)]
    name: String,
    #[serde(default, rename = "root_path")]
    root_path: String,
}

async fn list_agents(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    let mut list = state.db.list_agent_devices(auth.user_id());
    for d in list.iter_mut() {
        d.online = state.agent_hub.is_online(d.id);
    }
    json(StatusCode::OK, &list)
}

async fn create_agent(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: AgentBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.name.trim().is_empty() {
        return err(StatusCode::BAD_REQUEST, "device name required");
    }
    match state.db.create_agent_device(auth.user_id(), body.name.trim(), body.root_path.trim()) {
        Ok(dev) => {
            state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "agent_create", &dev.name, "");
            json(StatusCode::CREATED, &dev)
        }
        Err(e) => err(StatusCode::INTERNAL_SERVER_ERROR, &e.to_string()),
    }
}

async fn delete_agent(State(state): State<AppState>, auth: Auth, AxPath(id): AxPath<i64>, _req: Request<Body>) -> Response {
    state.agent_hub.disconnect(id);
    state.db.delete_agent_device(auth.user_id(), id);
    state.db.log_action(auth.user_id(), &username_of(&state, auth.user_id()), "agent_delete", &id.to_string(), "");
    empty(StatusCode::NO_CONTENT)
}

async fn agent_ws(State(state): State<AppState>, ws: axum::extract::ws::WebSocketUpgrade, req: Request<Body>) -> Response {
    let header_v = header_str(&req, "authorization");
    let mut token = header_v.strip_prefix("Bearer ").unwrap_or("").trim().to_string();
    if token.is_empty() {
        let q = query_map(&req);
        token = q.get("token").cloned().unwrap_or_default();
    }
    if !token.starts_with("fsa_") {
        return err(StatusCode::UNAUTHORIZED, "unauthorized agent token");
    }
    let dev = match state.db.validate_agent_token(&token) {
        Some(d) => d,
        None => return err(StatusCode::UNAUTHORIZED, "invalid agent token"),
    };
    let hub = state.agent_hub.clone();
    let hub2 = state.hub.clone();
    hub2.broadcast(&WsEvent { typ: "agent_status".to_string(), path: dev.id.to_string(), payload: None });
    ws.on_upgrade(move |socket| async move {
        hub.serve_session(dev.id, socket).await;
        hub2.broadcast(&WsEvent { typ: "agent_status".to_string(), path: dev.id.to_string(), payload: None });
    })
    .into_response()
}

pub async fn serve_agent_file_request(state: &AppState, req: Request<Body>, agent_id: i64) -> Response {
    use std::collections::HashMap as Map;
    let path = req.uri().path().to_string();
    let method = req.method().clone();
    let q = query_map(&req);
    let agent_req = |action: &str, params: Map<String, String>, data: Vec<u8>| {
        let hub = state.agent_hub.clone();
        let action = action.to_string();
        async move { hub.request(agent_id, &action, params, data).await }
    };

    match (path.as_str(), method) {
        ("/api/files", axum::http::Method::GET) => {
            let mut p = Map::new();
            for k in ["path", "search", "sort_by", "sort_asc", "page", "per_page"] {
                p.insert(k.to_string(), q.get(k).cloned().unwrap_or_default());
            }
            match agent_req("list", p, Vec::new()).await {
                Ok(res) => write_agent_json(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files", axum::http::Method::DELETE) => {
            let mut p = Map::new();
            p.insert("path".to_string(), q.get("path").cloned().unwrap_or_default());
            match agent_req("delete", p, Vec::new()).await {
                Ok(res) => write_agent_empty(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/info", axum::http::Method::GET) => {
            let mut p = Map::new();
            p.insert("path".to_string(), q.get("path").cloned().unwrap_or_default());
            match agent_req("info", p, Vec::new()).await {
                Ok(res) => write_agent_json(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/search", axum::http::Method::GET) => {
            let mut p = Map::new();
            p.insert("q".to_string(), q.get("q").cloned().unwrap_or_default());
            match agent_req("search", p, Vec::new()).await {
                Ok(res) => write_agent_json(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/mkdir", axum::http::Method::POST) => {
            let body: PathBody = read_json_only(req.into_body(), 64 << 10).await.unwrap_or(PathBody { path: String::new() });
            let mut p = Map::new();
            p.insert("path".to_string(), body.path);
            match agent_req("mkdir", p, Vec::new()).await {
                Ok(res) => write_agent_empty(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/touch", axum::http::Method::POST) => {
            let body: PathBody = read_json_only(req.into_body(), 64 << 10).await.unwrap_or(PathBody { path: String::new() });
            let mut p = Map::new();
            p.insert("path".to_string(), body.path);
            match agent_req("touch", p, Vec::new()).await {
                Ok(res) => write_agent_empty(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/rename", axum::http::Method::POST) => {
            let body: RenameBody = read_json_only(req.into_body(), 64 << 10).await.unwrap_or(RenameBody { old: String::new(), new: String::new(), src: String::new(), dst: String::new() });
            let mut p = Map::new();
            p.insert("src".to_string(), body.old);
            p.insert("dst".to_string(), body.new);
            match agent_req("rename", p, Vec::new()).await {
                Ok(res) => write_agent_empty(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/move", axum::http::Method::POST) | ("/api/files/copy", axum::http::Method::POST) => {
            let act = if path.ends_with("/copy") { "copy" } else { "move" };
            let body: RenameBody = read_json_only(req.into_body(), 64 << 10).await.unwrap_or(RenameBody { old: String::new(), new: String::new(), src: String::new(), dst: String::new() });
            let mut p = Map::new();
            p.insert("src".to_string(), body.src);
            p.insert("dst".to_string(), body.dst);
            match agent_req(act, p, Vec::new()).await {
                Ok(res) => write_agent_empty(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/text", axum::http::Method::GET) => {
            let mut p = Map::new();
            p.insert("path".to_string(), q.get("path").cloned().unwrap_or_default());
            match agent_req("read_text", p, Vec::new()).await {
                Ok(res) => write_agent_json(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/text", axum::http::Method::PUT) => {
            let body: TextBody = read_json_only(req.into_body(), 7 << 20).await.unwrap_or(TextBody { path: String::new(), content: String::new() });
            let mut p = Map::new();
            p.insert("path".to_string(), body.path);
            p.insert("content".to_string(), body.content);
            match agent_req("write_text", p, Vec::new()).await {
                Ok(res) => write_agent_empty(&res),
                Err(e) => err(StatusCode::BAD_GATEWAY, &e),
            }
        }
        ("/api/files/download", axum::http::Method::GET) | ("/api/files/preview", axum::http::Method::GET) => {
            let mut p = Map::new();
            p.insert("path".to_string(), q.get("path").cloned().unwrap_or_default());
            let res = match agent_req("read_file", p, Vec::new()).await {
                Ok(r) => r,
                Err(e) => return err(StatusCode::BAD_GATEWAY, &e),
            };
            if res.status >= 400 {
                return err(StatusCode::from_u16(res.status as u16).unwrap_or(StatusCode::BAD_REQUEST), res.error.as_deref().unwrap_or("error"));
            }
            let mime = res.params.as_ref().and_then(|m| m.get("mime_type")).cloned().unwrap_or_else(|| "application/octet-stream".to_string());
            let name = res.params.as_ref().and_then(|m| m.get("name")).cloned().unwrap_or_default();
            let disp = if path.ends_with("/download") { "attachment" } else { "inline" };
            let mut r = Response::new(Body::from(res.data));
            *r.status_mut() = StatusCode::OK;
            r.headers_mut().insert(header::CONTENT_TYPE, mime.parse().unwrap_or(header::HeaderValue::from_static("application/octet-stream")));
            r.headers_mut().insert(header::HeaderName::from_static("x-content-type-options"), header::HeaderValue::from_static("nosniff"));
            r.headers_mut().insert(header::CONTENT_SECURITY_POLICY, header::HeaderValue::from_static("default-src 'none'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'unsafe-inline'; sandbox"));
            if !name.is_empty() {
                r.headers_mut().insert(header::CONTENT_DISPOSITION, disp.parse().unwrap_or(header::HeaderValue::from_static("inline")));
            } else {
                r.headers_mut().insert(header::CONTENT_DISPOSITION, disp.parse().unwrap_or(header::HeaderValue::from_static("inline")));
            }
            r
        }
        ("/api/files/thumb", axum::http::Method::GET) => {
            let mut p = Map::new();
            p.insert("path".to_string(), q.get("path").cloned().unwrap_or_default());
            match agent_req("thumb", p, Vec::new()).await {
                Ok(res) if res.status < 400 => {
                    let mut r = Response::new(Body::from(res.data));
                    r.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("image/jpeg"));
                    r.headers_mut().insert(header::CACHE_CONTROL, header::HeaderValue::from_static("private, max-age=3600"));
                    r
                }
                _ => err(StatusCode::NOT_FOUND, "thumbnail not available"),
            }
        }
        ("/api/files/upload", axum::http::Method::POST) => {
            let (fields, mut files) = match read_multipart(req, state.cfg.max_upload_size, 100).await {
                Ok(v) => v,
                Err(r) => return r,
            };
            let dir = fields.get("path").cloned().unwrap_or_default();
            let rel_override = fields.get("relative_path").cloned().unwrap_or_default();
            for f in files.iter() {
                let data = std::fs::read(&f.path).unwrap_or_default();
                let name = if files.len() == 1 && !rel_override.is_empty() {
                    rel_override.clone()
                } else {
                    Path::new(&f.filename).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_else(|| f.filename.clone())
                };
                let rel = fsops::go_clean(&format!("{}/{}", dir, name));
                let mut p = Map::new();
                p.insert("path".to_string(), rel);
                let res = agent_req("write_file", p, data).await;
                match res {
                    Ok(r) if r.status < 400 => {}
                    Ok(r) => {
                        cleanup_spools(&mut files);
                        return err(StatusCode::from_u16(r.status as u16).unwrap_or(StatusCode::BAD_REQUEST), r.error.as_deref().unwrap_or("error"));
                    }
                    Err(e) => {
                        cleanup_spools(&mut files);
                        return err(StatusCode::BAD_GATEWAY, &e);
                    }
                }
            }
            cleanup_spools(&mut files);
            empty(StatusCode::CREATED)
        }
        _ => err(StatusCode::NOT_IMPLEMENTED, "operation not supported on remote agent"),
    }
}

fn write_agent_json(res: &crate::agent::Message) -> Response {
    if res.status >= 400 {
        return err(StatusCode::from_u16(res.status as u16).unwrap_or(StatusCode::BAD_REQUEST), res.error.as_deref().unwrap_or("error"));
    }
    let body = res.meta.as_ref().map(|m| serde_json::to_vec(m).unwrap_or_default()).unwrap_or_default();
    let mut r = Response::new(Body::from(body));
    *r.status_mut() = StatusCode::from_u16(res.status as u16).unwrap_or(StatusCode::OK);
    r.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("application/json"));
    r
}

fn write_agent_empty(res: &crate::agent::Message) -> Response {
    if res.status >= 400 {
        return err(StatusCode::from_u16(res.status as u16).unwrap_or(StatusCode::BAD_REQUEST), res.error.as_deref().unwrap_or("error"));
    }
    empty(StatusCode::from_u16(res.status as u16).unwrap_or(StatusCode::OK))
}

// ── 2FA ────────────────────────────────────────────────

async fn two_fa_status(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    json(StatusCode::OK, &serde_json::json!({"enabled": state.db.totp_is_enabled(auth.user_id())}))
}

async fn two_fa_setup(State(state): State<AppState>, auth: Auth, _req: Request<Body>) -> Response {
    let user = state.db.get_user_by_id(auth.user_id()).unwrap_or(crate::model::User {
        id: 0,
        username: String::new(),
        password_hash: String::new(),
        is_admin: false,
        root_path: String::new(),
        created_at: chrono::Local::now(),
    });
    let secret = crate::auth::generate_totp_secret();
    state.db.totp_set_secret(auth.user_id(), &secret);
    let uri = crate::auth::totp_provisioning_uri(&secret, &user.username, "Fileship");
    json(StatusCode::OK, &serde_json::json!({"secret": secret, "provisioning_uri": uri}))
}

#[derive(serde::Deserialize)]
struct CodeBody {
    #[serde(default)]
    code: String,
}

async fn two_fa_enable(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: CodeBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.code.is_empty() {
        return err(StatusCode::BAD_REQUEST, "code required");
    }
    let (secret, _) = match state.db.totp_get_secret(auth.user_id()) {
        Some(v) if !v.0.is_empty() => v,
        _ => return err(StatusCode::BAD_REQUEST, "run /api/me/2fa/setup first"),
    };
    if !crate::auth::verify_totp(&secret, &body.code) {
        return err(StatusCode::UNAUTHORIZED, "invalid code");
    }
    let codes = crate::auth::generate_backup_codes();
    state.db.totp_enable(auth.user_id(), &codes);
    json(StatusCode::OK, &serde_json::json!({"enabled": true, "backup_codes": codes}))
}

#[derive(serde::Deserialize)]
struct DisableBody {
    #[serde(default)]
    password: String,
}

async fn two_fa_disable(State(state): State<AppState>, auth: Auth, req: Request<Body>) -> Response {
    let body: DisableBody = match read_json_only(req.into_body(), 64 << 10).await {
        Ok(b) => b,
        Err(r) => return r,
    };
    if body.password.is_empty() {
        return err(StatusCode::BAD_REQUEST, "password required");
    }
    let user = match state.db.get_user_by_id(auth.user_id()) {
        Some(u) => u,
        None => return err(StatusCode::NOT_FOUND, "not found"),
    };
    if !crate::auth::verify_password(&user.password_hash, &body.password) {
        return err(StatusCode::UNAUTHORIZED, "wrong password");
    }
    state.db.totp_disable(auth.user_id());
    empty(StatusCode::NO_CONTENT)
}

// ── Public Shares ──────────────────────────────────────

fn share_cookie_name(token: &str) -> String {
    format!("fileship_share_{}", token)
}

fn share_cookie_mac(state: &AppState, token: &str, password_hash: &str) -> String {
    let mut h = sha2::Sha256::new();
    h.update(state.cfg.jwt_secret.as_bytes());
    h.update(b":");
    h.update(token.as_bytes());
    h.update(b":");
    h.update(password_hash.as_bytes());
    hex::encode(h.finalize())
}

fn parse_cookies(req: &Request<Body>) -> HashMap<String, String> {
    let mut m = HashMap::new();
    if let Some(h) = req.headers().get(header::COOKIE).and_then(|v| v.to_str().ok()) {
        for part in h.split(';') {
            let part = part.trim();
            if let Some((k, v)) = part.split_once('=') {
                m.insert(k.to_string(), v.to_string());
            }
        }
    }
    m
}

fn has_valid_share_cookie(state: &AppState, req: &Request<Body>, token: &str, password_hash: &str) -> bool {
    let cookies = parse_cookies(req);
    match cookies.get(&share_cookie_name(token)) {
        Some(v) if !v.is_empty() => {
            let expected = share_cookie_mac(state, token, password_hash);
            v == &expected
        }
        _ => false,
    }
}

fn set_share_cookie(res: &mut Response, state: &AppState, token: &str, password_hash: &str) {
    let mac = share_cookie_mac(state, token, password_hash);
    let mut cookie = format!("{}={}; Path=/s/{}; HttpOnly; SameSite=Lax; Max-Age=86400", share_cookie_name(token), mac, token);
    if state.cfg.tls_enabled() {
        cookie.push_str("; Secure");
    }
    if let Ok(v) = header::HeaderValue::from_str(&cookie) {
        res.headers_mut().append(header::SET_COOKIE, v);
    }
}

async fn read_form_pw(req: &Request<Body>) -> String {
    // urlencoded body "pw=..."
    if let Some(ct) = req.headers().get(header::CONTENT_TYPE).and_then(|h| h.to_str().ok()) {
        if ct.contains("application/x-www-form-urlencoded") {
            return String::new(); // body bereits konsumiert anderswo — Form-Parser im Handler via Query
        }
    }
    String::new()
}

async fn public_share(State(state): State<AppState>, AxPath(token): AxPath<String>, req: Request<Body>) -> Response {
    let share = match state.db.get_share_link(&token) {
        Some(s) => s,
        None => return err(StatusCode::NOT_FOUND, "share not found or expired"),
    };
    let q = query_map(&req);
    let is_post = req.method() == axum::http::Method::POST;
    let mut pw = q.get("pw").cloned().unwrap_or_default();
    // Request split: keep req_view for headers/query/extensions after body consumption
    let (req_view, req_body) = if is_post {
        let (parts, b) = req.into_parts();
        (Request::from_parts(parts, Body::empty()), b)
    } else {
        let (parts, b) = req.into_parts();
        (Request::from_parts(parts, Body::empty()), b)
    };
    if is_post {
        // Form-Body lesen (urlencoded oder multipart minimal)
        if let Ok(bytes) = axum::body::to_bytes(req_body, 1 << 20).await {
            let text = String::from_utf8_lossy(&bytes).to_string();
            if let Some(idx) = text.find("pw=") {
                let rest = &text[idx + 3..];
                let val = rest.split('&').next().unwrap_or("");
                if !val.is_empty() {
                    pw = crate::middleware::percent_decode(val);
                }
            }
        }
    }
    if !share.password_hash.is_empty() {
        if !has_valid_share_cookie(&state, &req_view, &token, &share.password_hash) {
            if pw.is_empty() {
                let mut res = Response::new(Body::from(share_password_page(&token, "")));
                *res.status_mut() = StatusCode::UNAUTHORIZED;
                res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("text/html; charset=utf-8"));
                return res;
            }
            // Rate limit for password attempts
            let key = crate::middleware::request_client(&req_view, &state.proxies);
            if !state.login_limiter.allow(&key) {
                return err(StatusCode::TOO_MANY_REQUESTS, "too many password attempts, please try again later");
            }
            if !crate::auth::verify_password(&share.password_hash, &pw) {
                if is_post || header_str(&req_view, "accept").contains("text/html") {
                    let mut res = Response::new(Body::from(share_password_page(&token, "Invalid password. Please try again.")));
                    *res.status_mut() = StatusCode::UNAUTHORIZED;
                    res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("text/html; charset=utf-8"));
                    return res;
                }
                return err(StatusCode::UNAUTHORIZED, "wrong password");
            }
            let mut res = Response::new(Body::empty());
            set_share_cookie(&mut res, &state, &token, &share.password_hash);
            if is_post {
                *res.status_mut() = StatusCode::SEE_OTHER;
                res.headers_mut().insert(header::LOCATION, format!("/s/{}", token).parse().unwrap_or(header::HeaderValue::from_static("/")));
                return res;
            }
            // GET with correct pw -> continue directly
        } else if is_post {
            let mut res = Response::new(Body::empty());
            *res.status_mut() = StatusCode::SEE_OTHER;
            res.headers_mut().insert(header::LOCATION, format!("/s/{}", token).parse().unwrap_or(header::HeaderValue::from_static("/")));
            return res;
        }
    }

    let user = match state.db.get_user_by_id(share.created_by) {
        Some(u) => u,
        None => return err(StatusCode::NOT_FOUND, "not found"),
    };
    let root = resolve_root(&state, &user.root_path);
    let perms = if user.is_admin { Vec::new() } else { state.db.list_path_permissions(user.id) };
    if !user.is_admin {
        if !crate::db::path_permission_allowed(&perms, &share.path, "read") || !crate::db::path_permission_allowed(&perms, &share.path, "share") {
            return err(StatusCode::FORBIDDEN, "forbidden");
        }
    }

    if share.is_dir {
        if let Some(sub) = q.get("file") {
            let clean_sub = match fsops::clean_relative_path(sub) {
                Ok(c) => c,
                Err(_) => return err(StatusCode::BAD_REQUEST, "invalid file path"),
            };
            let target_rel = format!("{}/{}", share.path.trim_end_matches('/'), clean_sub);
            if !user.is_admin && (!crate::db::path_permission_allowed(&perms, &target_rel, "read") || !crate::db::path_permission_allowed(&perms, &target_rel, "share")) {
                return err(StatusCode::FORBIDDEN, "forbidden");
            }
            let abs = match fsops::resolve(&root, &target_rel) {
                Ok(a) => a,
                Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
            };
            let _md = match std::fs::metadata(&abs) {
                Ok(m) if m.file_type().is_file() => m,
                _ => return err(StatusCode::NOT_FOUND, "not found"),
            };
            if !state.db.consume_share_download(&token) {
                return err(StatusCode::NOT_FOUND, "share not found or expired");
            }
            let mime = fsops::mime_type(&target_rel);
            let disp = if q.get("inline").map(|v| v == "1").unwrap_or(false) {
                "inline"
            } else {
                "attachment"
            };
            let name = Path::new(&abs).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
            let cdispo = if disp == "attachment" { format!("attachment; filename=\"{}\"", name) } else { "inline".to_string() };
            return serve_file_response(
                &abs,
                None,
                &[
                    ("content-type", &mime),
                    ("content-disposition", &cdispo),
                    ("x-content-type-options", "nosniff"),
                    ("content-security-policy", "default-src 'none'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'unsafe-inline'; sandbox"),
                ],
                &req_view,
            );
        }
        let wants_html = q.get("view").map(|v| v == "1").unwrap_or(false)
            || (q.get("zip").is_none() && header_str(&req_view, "accept").contains("text/html"));
        if wants_html {
            let mut sub_dir = String::new();
            let mut browse_path = share.path.clone();
            if let Some(raw) = q.get("dir") {
                let clean = match fsops::clean_relative_path(raw) {
                    Ok(c) => c,
                    Err(_) => return err(StatusCode::BAD_REQUEST, "invalid directory path"),
                };
                sub_dir = clean.clone();
                browse_path = format!("{}/{}", share.path.trim_end_matches('/'), clean);
                if !user.is_admin && (!crate::db::path_permission_allowed(&perms, &browse_path, "read") || !crate::db::path_permission_allowed(&perms, &browse_path, "share")) {
                    return err(StatusCode::FORBIDDEN, "forbidden");
                }
                let abs = match fsops::resolve(&root, &browse_path) {
                    Ok(a) => a,
                    Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
                };
                match std::fs::metadata(&abs) {
                    Ok(m) if m.is_dir() => {}
                    _ => return err(StatusCode::NOT_FOUND, "not found"),
                }
            }
            let list = match fsops::list(&root, &browse_path) {
                Ok(l) => l,
                Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
            };
            let visible: Vec<FileInfo> = list
                .into_iter()
                .filter(|f| {
                    if user.is_admin {
                        return true;
                    }
                    crate::db::path_permission_allowed(&perms, &f.path, "read") && crate::db::path_permission_allowed(&perms, &f.path, "share")
                })
                .map(|mut f| {
                    if let Some(rel) = Path::new(&f.path).strip_prefix(&share.path).ok() {
                        f.path = rel.to_string_lossy().to_string();
                    }
                    f
                })
                .collect();
            let folder_name = Path::new(&share.path).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_else(|| share.path.clone());
            let uploaded = q.get("uploaded").map(|v| v == "1").unwrap_or(false);
            let body = share_folder_page(&token, &folder_name, &sub_dir, &pw, share.allow_upload, uploaded, &visible);
            let mut res = Response::new(Body::from(body));
            res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("text/html; charset=utf-8"));
            return res;
        }
        let entries = match fsops::prepare_archive(&root, &[share.path.clone()]) {
            Ok(e) => e,
            Err(_) => return err(StatusCode::NOT_FOUND, "not found"),
        };
        if !user.is_admin {
            for entry in &entries {
                if !crate::db::path_permission_allowed(&perms, &entry.name, "read") || !crate::db::path_permission_allowed(&perms, &entry.name, "share") {
                    return err(StatusCode::FORBIDDEN, "forbidden");
                }
            }
        }
        if !state.db.consume_share_download(&token) {
            return err(StatusCode::NOT_FOUND, "share not found or expired");
        }
        let mut buf = Vec::new();
        if fsops::write_archive(&entries, "zip", &mut buf).is_err() {
            return err(StatusCode::INTERNAL_SERVER_ERROR, "server error");
        }
        let mut res = Response::new(Body::from(buf));
        res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("application/zip"));
        res.headers_mut().insert(header::CONTENT_DISPOSITION, header::HeaderValue::from_static("attachment; filename=\"share.zip\""));
        res.headers_mut().insert(header::HeaderName::from_static("x-content-type-options"), header::HeaderValue::from_static("nosniff"));
        res.headers_mut().insert(header::CONTENT_SECURITY_POLICY, header::HeaderValue::from_static("default-src 'none'; sandbox"));
        return res;
    }

    let abs = match fsops::resolve(&root, &share.path) {
        Ok(a) => a,
        Err(_) => return err(StatusCode::FORBIDDEN, "forbidden"),
    };
    let md = match std::fs::metadata(&abs) {
        Ok(m) => m,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return err(StatusCode::NOT_FOUND, "not found"),
        Err(_) => return err(StatusCode::BAD_REQUEST, "not a regular file"),
    };
    if !md.file_type().is_file() {
        return err(StatusCode::BAD_REQUEST, "not a regular file");
    }
    if !state.db.consume_share_download(&token) {
        return err(StatusCode::NOT_FOUND, "share not found or expired");
    }
    let mime = fsops::mime_type(&share.path);
    serve_file_response(
        &abs,
        None,
        &[
            ("content-type", &mime),
            ("content-disposition", "attachment"),
            ("x-content-type-options", "nosniff"),
            ("content-security-policy", "default-src 'none'; sandbox"),
        ],
        &req_view,
    )
}

async fn public_share_upload(State(state): State<AppState>, AxPath(token): AxPath<String>, req: Request<Body>) -> Response {
    let share = match state.db.get_share_link(&token) {
        Some(s) => s,
        None => return err(StatusCode::FORBIDDEN, "upload not allowed for this share"),
    };
    if !share.is_dir || !share.allow_upload {
        return err(StatusCode::FORBIDDEN, "upload not allowed for this share");
    }
    let q = query_map(&req);
    let (fields, mut files) = match read_multipart(req, 512 << 20, 100).await {
        Ok(v) => v,
        Err(r) => return r,
    };
    let mut pw = q.get("pw").cloned().unwrap_or_default();
    if pw.is_empty() {
        pw = fields.get("pw").cloned().unwrap_or_default();
    }
    if !share.password_hash.is_empty() {
        if !has_valid_share_cookie(&state, &Request::new(Body::empty()), &token, &share.password_hash) {
            let key = crate::middleware::request_client(&Request::new(Body::empty()), &state.proxies);
            if !state.login_limiter.allow(&key) {
                cleanup_spools(&mut files);
                return err(StatusCode::TOO_MANY_REQUESTS, "too many password attempts, please try again later");
            }
            if !crate::auth::verify_password(&share.password_hash, &pw) {
                cleanup_spools(&mut files);
                return err(StatusCode::UNAUTHORIZED, "wrong password");
            }
        } else {
            pw = String::new();
        }
    }
    let user = match state.db.get_user_by_id(share.created_by) {
        Some(u) => u,
        None => {
            cleanup_spools(&mut files);
            return err(StatusCode::NOT_FOUND, "not found");
        }
    };
    let root = resolve_root(&state, &user.root_path);
    let (quota, allowed_types) = state.db.get_user_quota_and_types(user.id);
    let effective_types = if allowed_types.is_empty() { state.cfg.allowed_types.clone() } else { allowed_types };
    let perms = if user.is_admin { Vec::new() } else { state.db.list_path_permissions(user.id) };
    let mut sub_dir = String::new();
    let mut target_base = share.path.clone();
    if let Some(raw) = fields.get("dir") {
        if !raw.is_empty() {
            match fsops::clean_relative_path(raw) {
                Ok(c) => {
                    sub_dir = c.clone();
                    target_base = format!("{}/{}", share.path.trim_end_matches('/'), c);
                }
                Err(_) => {
                    cleanup_spools(&mut files);
                    return err(StatusCode::BAD_REQUEST, "invalid target directory");
                }
            }
        }
    }
    if files.is_empty() {
        return err(StatusCode::BAD_REQUEST, "no files provided");
    }
    let mut target_rels: Vec<String> = Vec::new();
    let mut incoming: i64 = 0;
    for f in files.iter() {
        let clean_name = match fsops::clean_relative_path(&Path::new(&f.filename).file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default()) {
            Ok(c) => c,
            Err(_) => {
                cleanup_spools(&mut files);
                return err(StatusCode::BAD_REQUEST, "invalid file name");
            }
        };
        let rel = format!("{}/{}", target_base.trim_end_matches('/'), clean_name);
        let abs = match fsops::resolve(&root, &rel) {
            Ok(a) => a,
            Err(_) => {
                cleanup_spools(&mut files);
                return err(StatusCode::FORBIDDEN, "forbidden");
            }
        };
        if !user.is_admin {
            let mut p = rel.clone();
            loop {
                if !crate::db::path_permission_allowed(&perms, &p, "write") {
                    cleanup_spools(&mut files);
                    return err(StatusCode::FORBIDDEN, "permission denied");
                }
                if p == "." || p.is_empty() {
                    break;
                }
                let parent = fsops::go_dir(&p);
                if parent == "." || parent == p {
                    break;
                }
                p = parent;
            }
        }
        if !fsops::type_allowed(&rel, &effective_types) {
            cleanup_spools(&mut files);
            return err(StatusCode::FORBIDDEN, "file type not allowed");
        }
        let mut delta = f.size;
        if let Ok(info) = std::fs::metadata(&abs) {
            if info.file_type().is_file() {
                delta -= info.len() as i64;
            }
        }
        incoming += delta;
        target_rels.push(rel);
    }
    if quota > 0 {
        let usage = fsops::dir_size(&root).unwrap_or(0);
        if usage > quota || incoming > quota - usage {
            cleanup_spools(&mut files);
            return err(StatusCode::FORBIDDEN, "storage quota exceeded");
        }
    }
    for (i, f) in files.iter().enumerate() {
        let rel = &target_rels[i];
        let mut file = match std::fs::File::open(&f.path) {
            Ok(fl) => fl,
            Err(_) => {
                cleanup_spools(&mut files);
                return err(StatusCode::INTERNAL_SERVER_ERROR, "could not read upload");
            }
        };
        if let Err(e) = fsops::save_upload(&root, rel, &mut file) {
            cleanup_spools(&mut files);
            return err(StatusCode::INTERNAL_SERVER_ERROR, &format!("could not save file: {}", e));
        }
        state.db.log_action(user.id, &user.username, "share_upload", rel, "");
    }
    cleanup_spools(&mut files);
    broadcast(&state, "upload", &target_base);
    state.indexer.trigger_reindex(user.id, root);
    let accept = header_str(&Request::new(Body::empty()), "accept");
    let _ = accept;
    // Redirect for browsers (Accept text/html) — headers were consumed; query acts as proxy
    let mut res = Response::new(Body::empty());
    *res.status_mut() = StatusCode::SEE_OTHER;
    let mut loc = format!("/s/{}?uploaded=1", token);
    if !sub_dir.is_empty() {
        loc.push_str(&format!("&dir={}", crate::middleware::percent_decode(&sub_dir)));
    }
    if !pw.is_empty() {
        loc.push_str(&format!("&pw={}", pw));
    } else {
        // Set cookie for HTML clients
        set_share_cookie(&mut res, &state, &token, &share.password_hash);
    }
    res.headers_mut().insert(header::LOCATION, loc.parse().unwrap_or(header::HeaderValue::from_static("/")));
    res
}

// ── Share-HTML-Seiten ─────────────────────────────────

fn html_escape(s: &str) -> String {
    s.replace('&', "&amp;").replace('<', "&lt;").replace('>', "&gt;").replace('"', "&quot;").replace('\'', "&#39;")
}

fn share_password_page(token: &str, err_msg: &str) -> String {
    let err_banner = if err_msg.is_empty() {
        String::new()
    } else {
        format!("<div class=\"err\">{}</div>", html_escape(err_msg))
    };
    format!(
        "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Protected Share</title>\n<style>\n*{{box-sizing:border-box;margin:0;padding:0}}\nbody{{background:#111827;color:#edf2f7;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;padding:1rem}}\n.card{{background:#1d2a3d;border:1px solid #304158;border-radius:10px;padding:2rem;width:100%;max-width:360px;display:flex;flex-direction:column;gap:1rem}}\nh2{{font-size:1.1rem;color:#5ca7ff}}\np{{color:#9aabc0;font-size:.85rem}}\n.err{{background:#7f1d1d;color:#fecaca;padding:.55rem .75rem;border-radius:6px;font-size:.82rem}}\ninput{{background:#152238;border:1px solid #304158;color:#edf2f7;padding:.6rem .8rem;border-radius:6px;font-size:.9rem;width:100%}}\ninput:focus{{outline:none;border-color:#5ca7ff}}\nbutton{{background:#5ca7ff;border:none;color:#fff;padding:.65rem;border-radius:6px;font-size:.9rem;cursor:pointer;font-weight:600;width:100%}}\nbutton:hover{{background:#388be8}}\n</style>\n</head><body>\n<div class=\"card\">\n  <h2>🔒 Protected Share</h2>\n  <p>This link is password-protected. Enter the password to access it.</p>\n  {banner}\n  <form method=\"POST\" action=\"/s/{token}\">\n    <input type=\"password\" name=\"pw\" placeholder=\"Password\" autofocus required>\n    <br><br>\n    <button type=\"submit\">Access share</button>\n  </form>\n</div>\n</body></html>",
        token = token,
        banner = err_banner
    )
}

fn share_folder_page(token: &str, folder_name: &str, sub_dir: &str, pw: &str, allow_upload: bool, uploaded: bool, files: &[FileInfo]) -> String {
    let pw_query = if pw.is_empty() { String::new() } else { format!("&pw={}", crate::middleware::percent_decode(pw)) };
    let mut rows = String::new();
    if !sub_dir.is_empty() {
        let parent_dir = fsops::go_dir(sub_dir);
        let mut parent_href = format!("/s/{}?view=1{}", token, pw_query);
        if parent_dir != "." && !parent_dir.is_empty() {
            parent_href.push_str(&format!("&dir={}", crate::middleware::percent_decode(&parent_dir)));
        }
        rows.push_str(&format!(
            "\n\t\t\t<div class=\"row\">\n\t\t\t\t<div class=\"meta\">\n\t\t\t\t\t<strong>⬅ ..</strong>\n\t\t\t\t\t<span>Parent folder</span>\n\t\t\t\t</div>\n\t\t\t\t<div class=\"actions\">\n\t\t\t\t\t<a class=\"btn ghost\" href=\"{}\">Back</a>\n\t\t\t\t</div>\n\t\t\t</div>",
            parent_href
        ));
    }
    if files.is_empty() {
        rows.push_str("<div class=\"empty\">This shared folder is currently empty.</div>");
    } else {
        for f in files {
            let file_esc = html_escape(&f.name);
            let file_param = crate::middleware::percent_decode(&f.path);
            let mod_str = f.mod_time.format("%Y-%m-%d %H:%M").to_string();
            if f.is_dir {
                let dir_href = format!("/s/{}?dir={}{}", token, file_param, pw_query);
                rows.push_str(&format!(
                    "\n\t\t\t\t<div class=\"row\">\n\t\t\t\t\t<div class=\"meta\">\n\t\t\t\t\t\t<strong>📁 {}</strong>\n\t\t\t\t\t\t<span>Folder · {}</span>\n\t\t\t\t\t</div>\n\t\t\t\t\t<div class=\"actions\">\n\t\t\t\t\t\t<a class=\"btn ghost\" href=\"{}\">Open</a>\n\t\t\t\t\t</div>\n\t\t\t\t</div>",
                    file_esc, mod_str, dir_href
                ));
            } else {
                let dl = format!("/s/{}?file={}{}", token, file_param, pw_query);
                let prev = format!("{}&inline=1", dl);
                rows.push_str(&format!(
                    "\n\t\t\t\t<div class=\"row\">\n\t\t\t\t\t<div class=\"meta\">\n\t\t\t\t\t\t<strong>{}</strong>\n\t\t\t\t\t\t<span>{} B · {}</span>\n\t\t\t\t\t</div>\n\t\t\t\t\t<div class=\"actions\">\n\t\t\t\t\t\t<a class=\"btn ghost\" href=\"{}\" target=\"_blank\" rel=\"noopener\">Preview</a>\n\t\t\t\t\t\t<a class=\"btn\" href=\"{}\">Download</a>\n\t\t\t\t\t</div>\n\t\t\t\t</div>",
                    file_esc, f.size, mod_str, prev, dl
                ));
            }
        }
    }
    let upload_box = if allow_upload {
        let banner = if uploaded { "<div class=\"banner\">✅ Files uploaded successfully!</div>" } else { "" };
        format!(
            "\n\t\t<div class=\"card upload-card\">\n\t\t\t<h3>📤 File-Drop: Upload files to this folder</h3>\n\t\t\t{}\n\t\t\t<form method=\"POST\" action=\"/s/{}/upload\" enctype=\"multipart/form-data\">\n\t\t\t\t<input type=\"hidden\" name=\"pw\" value=\"{}\">\n\t\t\t\t<input type=\"hidden\" name=\"dir\" value=\"{}\">\n\t\t\t\t<input type=\"file\" name=\"files\" multiple required>\n\t\t\t\t<button type=\"submit\" class=\"btn primary\">Upload files</button>\n\t\t\t</form>\n\t\t</div>",
            banner,
            token,
            html_escape(pw),
            html_escape(sub_dir)
        )
    } else {
        String::new()
    };
    let display_title = if sub_dir.is_empty() { folder_name.to_string() } else { format!("{} / {}", folder_name, sub_dir) };
    let zip_href = format!("/s/{}?zip=1{}", token, pw_query);
    format!(
        "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Shared Folder · {}</title>\n<style>\n*{{box-sizing:border-box;margin:0;padding:0}}\nbody{{background:#0f172a;color:#f1f5f9;font-family:system-ui,-apple-system,sans-serif;padding:2rem 1rem}}\n.wrap{{max-width:780px;margin:0 auto;display:flex;flex-direction:column;gap:1.25rem}}\n.card{{background:#1e293b;border:1px solid #334155;border-radius:12px;padding:1.5rem}}\n.head{{display:flex;align-items:center;justify-content:space-between;gap:1rem;flex-wrap:wrap;margin-bottom:1rem;padding-bottom:1rem;border-bottom:1px solid #334155}}\nh2{{font-size:1.2rem;color:#60a5fa}}\nh3{{font-size:1rem;margin-bottom:.75rem;color:#93c5fd}}\n.list{{display:flex;flex-direction:column;gap:.5rem}}\n.row{{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:.75rem 1rem;background:#0f172a;border:1px solid #1e293b;border-radius:8px}}\n.meta{{display:flex;flex-direction:column;gap:.2rem}}\n.meta span{{font-size:.8rem;color:#94a3b8}}\n.actions{{display:flex;gap:.5rem}}\n.btn{{background:#3b82f6;color:#fff;text-decoration:none;padding:.5rem .9rem;border-radius:6px;font-size:.85rem;font-weight:600;border:none;cursor:pointer;display:inline-flex;align-items:center}}\n.btn.ghost{{background:#334155;color:#e2e8f0}}\n.btn:hover{{opacity:.92}}\n.upload-card form{{display:flex;gap:.75rem;align-items:center;flex-wrap:wrap}}\ninput[type=file]{{background:#0f172a;border:1px dashed #475569;color:#e2e8f0;padding:.6rem;border-radius:8px;flex:1}}\n.banner{{background:#065f46;color:#d1fae5;padding:.6rem .9rem;border-radius:6px;margin-bottom:.75rem;font-size:.85rem}}\n.empty{{color:#94a3b8;font-size:.9rem;padding:1rem 0}}\n</style></head><body>\n<div class=\"wrap\">\n\t<div class=\"card\">\n\t\t<div class=\"head\">\n\t\t\t<div>\n\t\t\t\t<h2>📁 {}</h2>\n\t\t\t\t<small style=\"color:#94a3b8\">Shared via Fileship</small>\n\t\t\t</div>\n\t\t\t<a class=\"btn\" href=\"{}\">⬇ Download All (.ZIP)</a>\n\t\t</div>\n\t\t<div class=\"list\">{}</div>\n\t</div>\n\t{}\n</div>\n</body></html>",
        html_escape(&display_title),
        html_escape(&display_title),
        zip_href,
        rows,
        upload_box
    )
}

// ── WS ─────────────────────────────────────────────────

async fn ws_endpoint(State(state): State<AppState>, ws: axum::extract::ws::WebSocketUpgrade, req: Request<Body>) -> Response {
    state.hub.handle(req, ws).await
}

// ── Static ─────────────────────────────────────────────

pub async fn static_handler(req: Request<Body>) -> Response {
    crate::static_fs::serve(req.uri().path())
}
