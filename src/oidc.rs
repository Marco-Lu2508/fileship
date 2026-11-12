//! OIDC / SSO login (Authorization Code + PKCE).
//!
//! Configure via environment:
//!   OIDC_ISSUER          issuer URL (discovery document is fetched)
//!   OIDC_CLIENT_ID       OAuth client id
//!   OIDC_CLIENT_SECRET   OAuth client secret
//!   OIDC_REDIRECT_URI    redirect URI registered at the provider
//!                          (defaults to http://<Host>/api/auth/oidc/callback)
//!   OIDC_AUTO_REGISTER   "1" (default) to create accounts on first login
//!   OIDC_ADMIN_EMAILS    comma-separated emails that become admins
//!
//! Flow: GET /api/auth/oidc/login -> provider -> /api/auth/oidc/callback
//! -> redirect to /?access_token=..&refresh_token=.. (or ?sso_pending=..
//! for 2FA, ?sso_error=.. on failure). Tokens ride in the query (same-
//! origin redirect) and are consumed by the SPA immediately.

use std::borrow::Cow;
use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};
use std::time::{Duration, Instant};

use axum::body::Body;
use axum::extract::State;
use axum::http::{Request, StatusCode};
use axum::response::Response;
use openidconnect::core::{CoreClient, CoreProviderMetadata};
use openidconnect::{
    AuthenticationFlow, AuthorizationCode, ClientId, ClientSecret, CsrfToken, IssuerUrl, Nonce,
    PkceCodeChallenge, PkceCodeVerifier, RedirectUrl, Scope, TokenResponse,
};

use crate::handler::{err, json, AppState};

/// The client type returned by `from_provider_metadata` (auth endpoint set,
/// token endpoint maybe set, other endpoints unset).
type DiscoveredClient = openidconnect::Client<
    openidconnect::EmptyAdditionalClaims,
    openidconnect::core::CoreAuthDisplay,
    openidconnect::core::CoreGenderClaim,
    openidconnect::core::CoreJweContentEncryptionAlgorithm,
    openidconnect::core::CoreJsonWebKey,
    openidconnect::core::CoreAuthPrompt,
    openidconnect::StandardErrorResponse<openidconnect::core::CoreErrorResponseType>,
    openidconnect::core::CoreTokenResponse,
    openidconnect::core::CoreTokenIntrospectionResponse,
    openidconnect::core::CoreRevocableToken,
    openidconnect::core::CoreRevocationErrorResponse,
    openidconnect::EndpointSet,
    openidconnect::EndpointNotSet,
    openidconnect::EndpointNotSet,
    openidconnect::EndpointNotSet,
    openidconnect::EndpointMaybeSet,
    openidconnect::EndpointMaybeSet,
>;

struct Pending {
    pkce_verifier: String,
    nonce: String,
    expires: Instant,
}

fn pending_store() -> &'static Mutex<HashMap<String, Pending>> {
    static STORE: OnceLock<Mutex<HashMap<String, Pending>>> = OnceLock::new();
    STORE.get_or_init(|| Mutex::new(HashMap::new()))
}

pub struct OidcCfg {
    issuer: String,
    client_id: String,
    client_secret: String,
}

pub fn config_from_env() -> Option<OidcCfg> {
    let issuer = std::env::var("OIDC_ISSUER").ok().filter(|v| !v.is_empty())?;
    let client_id = std::env::var("OIDC_CLIENT_ID").ok().filter(|v| !v.is_empty())?;
    let client_secret = std::env::var("OIDC_CLIENT_SECRET").unwrap_or_default();
    Some(OidcCfg {
        issuer,
        client_id,
        client_secret,
    })
}

pub fn enabled() -> bool {
    config_from_env().is_some()
}

fn redirect_uri(req: &Request<Body>) -> RedirectUrl {
    let v = std::env::var("OIDC_REDIRECT_URI").unwrap_or_default();
    let v = if !v.is_empty() {
        v
    } else {
        let host = req
            .headers()
            .get("host")
            .and_then(|h| h.to_str().ok())
            .unwrap_or("localhost:8080");
        format!("http://{}/api/auth/oidc/callback", host)
    };
    RedirectUrl::new(v).expect("valid redirect URI")
}

fn redirect(location: String) -> Response {
    let mut res = Response::new(Body::empty());
    *res.status_mut() = StatusCode::SEE_OTHER;
    if let Ok(v) = axum::http::HeaderValue::from_str(&location) {
        res.headers_mut().insert(axum::http::header::LOCATION, v);
    }
    res
}

fn sso_error(msg: &str) -> Response {
    redirect(format!("/?sso_error={}", urlencoding(msg)))
}

fn urlencoding(s: &str) -> String {
    let mut out = String::new();
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{:02X}", b)),
        }
    }
    out
}

async fn client_with_metadata(cfg: &OidcCfg) -> Result<DiscoveredClient, String> {
    let http = openidconnect::reqwest::Client::new();
    let md = CoreProviderMetadata::discover_async(
        IssuerUrl::new(cfg.issuer.clone()).map_err(|e| e.to_string())?,
        &http,
    )
    .await
    .map_err(|e| e.to_string())?;
    let secret = if cfg.client_secret.is_empty() {
        None
    } else {
        Some(ClientSecret::new(cfg.client_secret.clone()))
    };
    Ok(CoreClient::from_provider_metadata(
        md,
        ClientId::new(cfg.client_id.clone()),
        secret,
    ))
}

pub async fn oidc_config() -> Response {
    json(StatusCode::OK, &serde_json::json!({"enabled": enabled()}))
}

pub async fn oidc_login(req: Request<Body>) -> Response {
    let cfg = match config_from_env() {
        Some(c) => c,
        None => return err(StatusCode::NOT_FOUND, "OIDC not configured"),
    };
    let client = match client_with_metadata(&cfg).await {
        Ok(c) => c,
        Err(e) => {
            eprintln!("oidc discovery failed: {}", e);
            return err(StatusCode::INTERNAL_SERVER_ERROR, "OIDC discovery failed");
        }
    };
    let (pkce_challenge, pkce_verifier) = PkceCodeChallenge::new_random_sha256();
    let (auth_url, csrf, nonce) = client
        .authorize_url(
            AuthenticationFlow::<openidconnect::core::CoreResponseType>::AuthorizationCode,
            || CsrfToken::new_random(),
            || Nonce::new_random(),
        )
        .add_scope(Scope::new("openid".to_string()))
        .add_scope(Scope::new("email".to_string()))
        .add_scope(Scope::new("profile".to_string()))
        .set_redirect_uri(Cow::Owned(redirect_uri(&req)))
        .set_pkce_challenge(pkce_challenge)
        .url();
    {
        let mut store = pending_store().lock().unwrap();
        store.retain(|_, p| p.expires > Instant::now());
        store.insert(
            csrf.secret().clone(),
            Pending {
                pkce_verifier: pkce_verifier.secret().clone(),
                nonce: nonce.secret().clone(),
                expires: Instant::now() + Duration::from_secs(600),
            },
        );
    }
    redirect(auth_url.to_string())
}

pub async fn oidc_callback(State(state): State<AppState>, req: Request<Body>) -> Response {
    let q = crate::middleware::query_map(&req);
    let code = match q.get("code") {
        Some(c) if !c.is_empty() => c.clone(),
        _ => return sso_error("missing code"),
    };
    let state_param = match q.get("state") {
        Some(s) if !s.is_empty() => s.clone(),
        _ => return sso_error("missing state"),
    };
    let pending = {
        let mut store = pending_store().lock().unwrap();
        store.remove(&state_param)
    };
    let pending = match pending {
        Some(p) if p.expires > Instant::now() => p,
        _ => return sso_error("login expired, please retry"),
    };
    let cfg = match config_from_env() {
        Some(c) => c,
        None => return sso_error("OIDC not configured"),
    };
    let client = match client_with_metadata(&cfg).await {
        Ok(c) => c,
        Err(_) => return sso_error("OIDC discovery failed"),
    };
    let http = openidconnect::reqwest::Client::new();
    let code_req = match client.exchange_code(AuthorizationCode::new(code)) {
        Ok(r) => r,
        Err(e) => {
            eprintln!("oidc exchange setup failed: {:?}", e);
            return sso_error("token exchange failed");
        }
    };
    let token_response = match code_req
        .set_redirect_uri(Cow::Owned(redirect_uri(&req)))
        .set_pkce_verifier(PkceCodeVerifier::new(pending.pkce_verifier))
        .request_async(&http)
        .await
    {
        Ok(r) => r,
        Err(e) => {
            eprintln!("oidc token exchange failed: {:?}", e);
            return sso_error("token exchange failed");
        }
    };
    let id_token = match token_response.id_token() {
        Some(t) => t,
        None => return sso_error("no id_token from provider"),
    };
    let verifier = client.id_token_verifier();
    let claims = match id_token.claims(&verifier, &Nonce::new(pending.nonce)) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("oidc claims verification failed: {:?}", e);
            return sso_error("invalid id_token");
        }
    };
    let sub = claims.subject().to_string();
    let email = claims.email().map(|e| e.to_string()).unwrap_or_default();
    // Only trust the email for linking/privileges when the IdP verified it —
    // otherwise anyone could claim someone else's address at a lax IdP.
    let email_verified = claims.email_verified().unwrap_or(false);

    let user = match resolve_user(&state, &sub, &email, email_verified) {
        Ok(u) => u,
        Err(msg) => return sso_error(&msg),
    };

    if state.db.totp_is_enabled(user.id) {
        let pending_token =
            crate::auth::generate_pending_token(&state.cfg.jwt_secret, user.id, user.is_admin);
        return redirect(format!("/?sso_pending={}", pending_token));
    }
    let access = crate::auth::generate_access_token(&state.cfg.jwt_secret, user.id, user.is_admin);
    let (refresh, expires) = crate::auth::generate_refresh_token();
    state.db.save_refresh_token(&refresh, user.id, expires);
    redirect(format!("/?access_token={}&refresh_token={}", access, refresh))
}

fn admin_emails() -> Vec<String> {
    std::env::var("OIDC_ADMIN_EMAILS")
        .unwrap_or_default()
        .split(',')
        .map(|s| s.trim().to_lowercase())
        .filter(|s| !s.is_empty())
        .collect()
}

fn resolve_user(state: &AppState, sub: &str, email: &str, email_verified: bool) -> Result<crate::model::User, String> {
    if let Some(u) = state.db.get_user_by_oidc_sub(sub) {
        return Ok(u);
    }
    // Link by email-as-username if that account exists (verified emails only).
    if !email.is_empty() && email_verified {
        if let Some(u) = state.db.get_user_by_username(email) {
            state.db.link_oidc(u.id, sub, email);
            return Ok(u);
        }
    }
    let auto = std::env::var("OIDC_AUTO_REGISTER").unwrap_or_else(|_| "1".into()) != "0";
    if !auto {
        return Err("account not registered (ask an admin to create your account first)".into());
    }
    // Create a new account. Username: email local part (unique-ified).
    let base = email
        .split('@')
        .next()
        .unwrap_or("user")
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || *c == '_' || *c == '-' || *c == '.')
        .collect::<String>();
    let base = if base.is_empty() {
        "user".to_string()
    } else {
        base
    };
    // Admin promotion only for verified addresses, too.
    let is_admin = !email.is_empty() && email_verified && admin_emails().contains(&email.to_lowercase());
    let mut username = base.clone();
    let mut n = 1;
    loop {
        match state.db.create_user(&username, &format!("sso:{}", sub), is_admin, "/") {
            Ok(()) => break,
            Err(_) => {
                n += 1;
                username = format!("{}-{}", base, n);
                if n > 100 {
                    return Err("could not create account".into());
                }
            }
        }
    }
    let user = state
        .db
        .get_user_by_username(&username)
        .ok_or_else(|| "account creation failed".to_string())?;
    state.db.link_oidc(user.id, sub, email);
    Ok(user)
}
