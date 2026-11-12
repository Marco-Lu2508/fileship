//! Middleware: Auth (JWT/API-Token), Admin-Only, Agent-Routing,
//! Rate-Limit (Token-Bucket wie Go x/time/rate), CSRF (Double-Submit),
//! Security-Header, Trusted-Proxies / Client-IP.

use crate::auth;
use crate::handler::AppState;
use axum::body::Body;
use axum::extract::FromRequestParts;
use axum::http::{header, Method, Request, StatusCode};
use axum::middleware::Next;
use axum::response::Response;
use std::collections::HashMap;
use std::net::{IpAddr, SocketAddr};
use std::sync::{Arc, Mutex};
use std::time::Instant;

#[derive(Debug, Clone, Copy)]
pub struct AuthCtx {
    pub user_id: i64,
    pub is_admin: bool,
    pub two_fa_pending: bool,
}

pub fn err_resp(status: StatusCode, msg: &str) -> Response {
    let mut res = Response::new(Body::from(format!("{}\n", msg)));
    *res.status_mut() = status;
    res.headers_mut().insert(
        header::CONTENT_TYPE,
        header::HeaderValue::from_static("text/plain; charset=utf-8"),
    );
    res.headers_mut().insert(
        header::HeaderName::from_static("x-content-type-options"),
        header::HeaderValue::from_static("nosniff"),
    );
    res
}

pub fn extract_token(req: &Request<Body>) -> String {
    if let Some(h) = req.headers().get(header::AUTHORIZATION) {
        if let Ok(s) = h.to_str() {
            if let Some(rest) = s.strip_prefix("Bearer ") {
                return rest.to_string();
            }
        }
    }
    if req.method() == Method::GET {
        if let Some(q) = req.uri().query() {
            for pair in q.split('&') {
                if let Some(v) = pair.strip_prefix("access_token=") {
                    return percent_decode(v);
                }
            }
        }
    }
    String::new()
}

pub fn percent_decode(s: &str) -> String {
    let mut out = Vec::with_capacity(s.len());
    let bytes = s.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'%' if i + 2 < bytes.len() => {
                if let Ok(hex) = std::str::from_utf8(&bytes[i + 1..i + 3]) {
                    if let Ok(v) = u8::from_str_radix(hex, 16) {
                        out.push(v);
                        i += 3;
                        continue;
                    }
                }
                out.push(b'%');
                i += 1;
            }
            b'+' => {
                out.push(b' ');
                i += 1;
            }
            b => {
                out.push(b);
                i += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).to_string()
}

// ── Auth-Middleware ─────────────────────────────────────

pub async fn auth_mw(
    axum::Extension(state): axum::Extension<AppState>,
    req: Request<Body>,
    next: Next,
) -> Response {
    let token = extract_token(&req);
    if token.is_empty() {
        return err_resp(StatusCode::UNAUTHORIZED, "unauthorized");
    }
    let ctx = if token.starts_with("fsk_") {
        match state.db.validate_api_token(&token) {
            Ok((uid, admin)) => AuthCtx { user_id: uid, is_admin: admin, two_fa_pending: false },
            Err(_) => return err_resp(StatusCode::UNAUTHORIZED, "unauthorized"),
        }
    } else {
        match auth::parse_access_token(&state.cfg.jwt_secret, &token) {
            Some(c) => {
                if c.two_fa_pending {
                    return err_resp(StatusCode::UNAUTHORIZED, "2fa_required");
                }
                AuthCtx { user_id: c.user_id, is_admin: c.is_admin, two_fa_pending: false }
            }
            None => return err_resp(StatusCode::UNAUTHORIZED, "unauthorized"),
        }
    };
    let mut req = req;
    req.extensions_mut().insert(ctx);
    next.run(req).await
}

pub async fn admin_mw(req: Request<Body>, next: Next) -> Response {
    let ctx = req.extensions().get::<AuthCtx>().copied();
    match ctx {
        Some(c) if c.is_admin => next.run(req).await,
        _ => err_resp(StatusCode::FORBIDDEN, "forbidden"),
    }
}

/// AuthCtx-Extractor für Handler (liest aus Extensions, sonst parse-on-demand).
pub struct Auth(AuthCtx);

impl<S> FromRequestParts<S> for Auth
where
    S: Send + Sync,
{
    type Rejection = Response;

    async fn from_request_parts(parts: &mut axum::http::request::Parts, state: &S) -> Result<Self, Response> {
        if let Some(ctx) = parts.extensions.get::<AuthCtx>() {
            return Ok(Auth(*ctx));
        }
        // Fallback (sollte nicht passieren — Middleware läuft immer davor)
        let _ = state;
        Err(err_resp(StatusCode::UNAUTHORIZED, "unauthorized"))
    }
}

impl Auth {
    pub fn user_id(&self) -> i64 {
        self.0.user_id
    }
    pub fn is_admin(&self) -> bool {
        self.0.is_admin
    }
}

// ── Agent-Route-Middleware ──────────────────────────────

pub fn request_agent_id(req: &Request<Body>) -> i64 {
    if let Some(h) = req.headers().get("x-agent-id") {
        if let Ok(s) = h.to_str() {
            if let Ok(id) = s.parse::<i64>() {
                return id;
            }
        }
    }
    if let Some(q) = req.uri().query() {
        for pair in q.split('&') {
            if let Some(v) = pair.strip_prefix("agent_id=") {
                if let Ok(id) = percent_decode(v).parse::<i64>() {
                    return id;
                }
            }
        }
    }
    0
}

pub async fn agent_route_mw(
    axum::Extension(state): axum::Extension<AppState>,
    req: Request<Body>,
    next: Next,
) -> Response {
    let agent_id = request_agent_id(&req);
    let path = req.uri().path().to_string();
    if agent_id <= 0 || !path.starts_with("/api/files") {
        return next.run(req).await;
    }
    let claims = match req.extensions().get::<AuthCtx>() {
        Some(c) => *c,
        None => return err_resp(StatusCode::UNAUTHORIZED, "unauthorized"),
    };
    let dev = state.db.get_agent_device_by_id(claims.user_id, agent_id);
    let dev = match dev {
        Some(d) => d,
        None => return err_resp(StatusCode::NOT_FOUND, "agent device not found"),
    };
    let _ = dev;
    if !state.agent_hub.is_online(agent_id) {
        return err_resp(StatusCode::SERVICE_UNAVAILABLE, "remote device is offline");
    }
    crate::handler::serve_agent_file_request(&state, req, agent_id).await
}

// ── Rate Limiter (x/time/rate-Äquivalent) ───────────────

struct Bucket {
    tokens: f64,
    last: Instant,
}

pub struct RateLimiter {
    buckets: Mutex<HashMap<String, Bucket>>,
    refill_per_sec: f64,
    burst: f64,
}

impl RateLimiter {
    pub fn new(refill_per_sec: f64, burst: f64) -> Arc<RateLimiter> {
        let rl = Arc::new(RateLimiter {
            buckets: Mutex::new(HashMap::new()),
            refill_per_sec,
            burst,
        });
        let weak = Arc::clone(&rl);
        tokio::spawn(async move {
            let mut tick = tokio::time::interval(std::time::Duration::from_secs(300));
            loop {
                tick.tick().await;
                let mut g = weak.buckets.lock().unwrap();
                let now = Instant::now();
                g.retain(|_, b| now.duration_since(b.last) <= std::time::Duration::from_secs(600));
            }
        });
        rl
    }

    pub fn allow(&self, key: &str) -> bool {
        let now = Instant::now();
        let mut g = self.buckets.lock().unwrap();
        let b = g.entry(key.to_string()).or_insert(Bucket { tokens: self.burst, last: now });
        let elapsed = now.duration_since(b.last).as_secs_f64();
        b.tokens = (b.tokens + elapsed * self.refill_per_sec).min(self.burst);
        b.last = now;
        if b.tokens >= 1.0 {
            b.tokens -= 1.0;
            true
        } else {
            false
        }
    }
}

// ── Trusted Proxies / Client-IP ─────────────────────────

#[derive(Clone, Copy)]
struct IpNet {
    addr: IpAddr,
    prefix: u8,
    v6: bool,
}

pub struct ProxyCfg {
    nets: Vec<IpNet>,
}

impl ProxyCfg {
    pub fn from_env() -> Arc<ProxyCfg> {
        let list = std::env::var("TRUSTED_PROXIES").unwrap_or_default();
        let mut nets = Vec::new();
        for item in list.split(',') {
            let item = item.trim();
            if item.is_empty() {
                continue;
            }
            let (addr_s, prefix) = match item.split_once('/') {
                Some((a, p)) => match p.parse::<u8>() {
                    Ok(n) => (a, n),
                    Err(_) => continue,
                },
                None => match item.parse::<IpAddr>() {
                    Ok(IpAddr::V4(_)) => (item, 32),
                    Ok(_) => (item, 128),
                    Err(_) => continue,
                },
            };
            if let Ok(addr) = addr_s.parse::<IpAddr>() {
                let v6 = matches!(addr, IpAddr::V6(_));
                nets.push(IpNet { addr, prefix, v6 });
            }
        }
        Arc::new(ProxyCfg { nets })
    }

    fn contains(&self, ip: &IpAddr) -> bool {
        for n in &self.nets {
            if n.v6 != matches!(ip, IpAddr::V6(_)) {
                continue;
            }
            let mask: u128 = if n.prefix == 0 {
                0
            } else if n.v6 {
                u128::MAX << (128 - n.prefix)
            } else {
                (u32::MAX as u128) << (32 - n.prefix)
            };
            if ip_to_u128(ip) & mask == ip_to_u128(&n.addr) & mask {
                return true;
            }
        }
        false
    }
}

fn ip_to_u128(ip: &IpAddr) -> u128 {
    match ip {
        IpAddr::V4(v) => v.octets().iter().fold(0u128, |a, b| (a << 8) | *b as u128),
        IpAddr::V6(v) => u128::from(*v),
    }
}

pub fn client_key(ip: &IpAddr) -> String {
    match ip {
        IpAddr::V4(v) => v.to_string(),
        IpAddr::V6(v) => {
            // /64 gruppieren wie Go
            let masked = u128::from(*v) & (u128::MAX << 64);
            std::net::Ipv6Addr::from(masked).to_string()
        }
    }
}

pub fn request_client(req: &Request<Body>, proxies: &ProxyCfg) -> String {
    let peer: Option<IpAddr> = req.extensions().get::<axum::extract::ConnectInfo<SocketAddr>>().map(|c| c.0.ip());
    let peer = match peer {
        Some(p) => p,
        None => return String::from("unknown"),
    };
    if proxies.contains(&peer) {
        if let Some(xff) = req.headers().get("x-forwarded-for").and_then(|h| h.to_str().ok()) {
            let parts: Vec<&str> = xff.split(',').collect();
            for raw in parts.iter().rev() {
                let cand = raw.trim();
                if cand.is_empty() {
                    continue;
                }
                match cand.parse::<IpAddr>() {
                    Ok(ip) => {
                        if !proxies.contains(&ip) {
                            return client_key(&ip);
                        }
                    }
                    Err(_) => continue,
                }
            }
        }
    }
    client_key(&peer)
}

pub async fn rate_limit_mw(
    axum::Extension(state): axum::Extension<AppState>,
    req: Request<Body>,
    next: Next,
) -> Response {
    let path = req.uri().path();
    let key = request_client(&req, &state.proxies);
    if !state.global_limiter.allow(&key) {
        return err_resp(StatusCode::TOO_MANY_REQUESTS, "too many requests");
    }
    if path == "/api/auth/login" || path == "/api/auth/2fa/verify" {
        if !state.login_limiter.allow(&key) {
            return err_resp(StatusCode::TOO_MANY_REQUESTS, "too many requests");
        }
    }
    next.run(req).await
}

// ── CSRF + Security Headers ─────────────────────────────

const APP_CSP: &str = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self' data:; connect-src 'self' ws: wss:; frame-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'";

fn new_csrf_token() -> String {
    let mut b = [0u8; 16];
    rand::RngCore::fill_bytes(&mut rand::rng(), &mut b);
    hex::encode(b)
}

pub async fn csrf_security_mw(
    axum::Extension(tls): axum::Extension<bool>,
    req: Request<Body>,
    next: Next,
) -> Response {
    let method = req.method().clone();
    let path = req.uri().path().to_string();
    let safe = matches!(method, Method::GET | Method::HEAD | Method::OPTIONS);

    if safe {
        let has_cookie = req
            .headers()
            .get(header::COOKIE)
            .and_then(|h| h.to_str().ok())
            .map(|s| s.split(';').any(|c| c.trim().starts_with("csrf_token=")))
            .unwrap_or(false);
        let mut res = next.run(req).await;
        if !has_cookie {
            let token = new_csrf_token();
            let mut cookie = format!("csrf_token={}; Path=/; SameSite=Strict", token);
            if tls {
                cookie.push_str("; Secure");
            }
            if let Ok(v) = header::HeaderValue::from_str(&cookie) {
                res.headers_mut().append(header::SET_COOKIE, v);
            }
        }
        apply_security_headers(&mut res, tls);
        return res;
    }

    if path == "/api/auth/login" || path == "/api/auth/refresh" || path == "/api/auth/logout"
        || path.starts_with("/webdav") || path.starts_with("/s/")
    {
        let mut res = next.run(req).await;
        apply_security_headers(&mut res, tls);
        return res;
    }

    // Cookie + Header vergleichen (Double-Submit, constant-time)
    let cookie_val: Option<String> = req
        .headers()
        .get(header::COOKIE)
        .and_then(|h| h.to_str().ok())
        .and_then(|s| {
            s.split(';').find_map(|c| {
                let c = c.trim();
                c.strip_prefix("csrf_token=").map(|v| v.to_string())
            })
        });
    let cookie_val = match cookie_val {
        Some(v) if !v.is_empty() => v,
        _ => {
            let mut res = err_resp(StatusCode::FORBIDDEN, "missing csrf token");
            apply_security_headers(&mut res, tls);
            return res;
        }
    };
    let header_val = req
        .headers()
        .get("x-csrf-token")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    if header_val.is_empty() || !constant_time_eq(header_val.as_bytes(), cookie_val.as_bytes()) {
        let mut res = err_resp(StatusCode::FORBIDDEN, "invalid csrf token");
        apply_security_headers(&mut res, tls);
        return res;
    }
    let mut res = next.run(req).await;
    apply_security_headers(&mut res, tls);
    res
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

fn apply_security_headers(res: &mut Response, tls: bool) {
    let h = res.headers_mut();
    let set = |h: &mut header::HeaderMap, k: &'static str, v: &'static str| {
        if let Ok(val) = header::HeaderValue::from_str(v) {
            h.insert(header::HeaderName::from_static(k), val);
        }
    };
    set(h, "x-content-type-options", "nosniff");
    set(h, "x-frame-options", "SAMEORIGIN");
    set(h, "referrer-policy", "no-referrer");
    set(h, "permissions-policy", "camera=(), microphone=(), geolocation=(), payment=()");
    set(h, "cross-origin-opener-policy", "same-origin");
    // Handler-eigene CSP (Download/Preview) gewinnt — Go setzt sie nachträglich
    if !h.contains_key(header::CONTENT_SECURITY_POLICY) {
        set(h, "content-security-policy", APP_CSP);
    }
    if tls {
        set(h, "strict-transport-security", "max-age=31536000; includeSubDomains");
    }
}

// ── Helpers für Handler ─────────────────────────────────

pub fn json_resp<T: serde::Serialize>(status: StatusCode, v: &T) -> Response {
    let body = serde_json::to_vec(v).unwrap_or_else(|_| b"null".to_vec());
    let mut res = Response::new(Body::from(body));
    *res.status_mut() = status;
    res.headers_mut().insert(
        header::CONTENT_TYPE,
        header::HeaderValue::from_static("application/json"),
    );
    res
}

pub fn empty_resp(status: StatusCode) -> Response {
    let mut res = Response::new(Body::empty());
    *res.status_mut() = status;
    res
}

pub async fn read_json_only<T: for<'de> serde::Deserialize<'de>>(
    body: Body,
    max_bytes: usize,
) -> Result<T, Response> {
    let bytes = axum::body::to_bytes(body, max_bytes)
        .await
        .map_err(|_| err_resp(StatusCode::BAD_REQUEST, "bad request"))?;
    serde_json::from_slice(&bytes).map_err(|_| err_resp(StatusCode::BAD_REQUEST, "bad request"))
}

/// Query-Parameter als HashMap (axum Query-Äquivalent, ohne Extra-Dep).
pub fn query_map(req: &Request<Body>) -> HashMap<String, String> {
    let mut map = HashMap::new();
    if let Some(q) = req.uri().query() {
        for pair in q.split('&') {
            if pair.is_empty() {
                continue;
            }
            match pair.split_once('=') {
                Some((k, v)) => {
                    map.insert(percent_decode(k), percent_decode(v));
                }
                None => {
                    map.insert(percent_decode(pair), String::new());
                }
            }
        }
    }
    map
}

pub fn peer_addr(req: &Request<Body>) -> String {
    req.extensions()
        .get::<axum::extract::ConnectInfo<SocketAddr>>()
        .map(|c| c.0.to_string())
        .unwrap_or_else(|| "0.0.0.0:0".to_string())
}
