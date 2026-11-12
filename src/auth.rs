//! Auth: JWT (HS256, hand-rolled — Go-kompatibel), Refresh-Tokens, TOTP-2FA, bcrypt.
//!
//! Go nutzt golang-jwt HS256 mit Claims {uid, adm, 2fa_pending, exp, iat}.
//! Wir signieren/validieren exakt dasselbe Format, damit Tokens austauschbar sind.

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use rand::RngCore;
use sha2::{Digest, Sha256};
use std::sync::OnceLock;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

pub const ACCESS_DUR_SECS: i64 = 15 * 60;
pub const PENDING_DUR_SECS: i64 = 5 * 60;
pub const REFRESH_DUR_SECS: i64 = 7 * 24 * 3600;

#[derive(Debug, Clone)]
pub struct Claims {
    pub user_id: i64,
    pub is_admin: bool,
    pub two_fa_pending: bool,
}

fn b64e(data: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(data)
}

fn b64d(s: &str) -> Option<Vec<u8>> {
    URL_SAFE_NO_PAD.decode(s).ok()
}

fn hmac_sha256(key: &[u8], msg: &[u8]) -> Vec<u8> {
    // RFC 2104
    let mut k: Vec<u8> = if key.len() > 64 {
        let mut h = Sha256::new();
        h.update(key);
        h.finalize().to_vec()
    } else {
        key.to_vec()
    };
    k.resize(64, 0);
    let ipad: Vec<u8> = k.iter().map(|b| b ^ 0x36).collect();
    let opad: Vec<u8> = k.iter().map(|b| b ^ 0x5c).collect();
    let mut inner = Sha256::new();
    inner.update(&ipad);
    inner.update(msg);
    let inner_hash = inner.finalize();
    let mut outer = Sha256::new();
    outer.update(&opad);
    outer.update(&inner_hash);
    outer.finalize().to_vec()
}

fn now_secs() -> i64 {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs() as i64).unwrap_or(0)
}

pub fn generate_access_token(secret: &str, user_id: i64, is_admin: bool) -> String {
    generate_token(secret, user_id, is_admin, false)
}

pub fn generate_pending_token(secret: &str, user_id: i64, is_admin: bool) -> String {
    generate_token(secret, user_id, is_admin, true)
}

fn generate_token(secret: &str, user_id: i64, is_admin: bool, pending: bool) -> String {
    let dur = if pending { PENDING_DUR_SECS } else { ACCESS_DUR_SECS };
    let now = now_secs();
    let header = r#"{"typ":"JWT","alg":"HS256"}"#;
    let payload = if pending {
        format!(
            r#"{{"uid":{},"adm":{},"2fa_pending":true,"exp":{},"iat":{}}}"#,
            user_id, is_admin, now + dur, now
        )
    } else {
        format!(r#"{{"uid":{},"adm":{},"exp":{},"iat":{}}}"#, user_id, is_admin, now + dur, now)
    };
    let signing = format!("{}.{}", b64e(header.as_bytes()), b64e(payload.as_bytes()));
    let sig = hmac_sha256(secret.as_bytes(), signing.as_bytes());
    format!("{}.{}", signing, b64e(&sig))
}

pub fn parse_access_token(secret: &str, token: &str) -> Option<Claims> {
    let mut parts = token.splitn(3, '.');
    let header_b64 = parts.next()?;
    let payload_b64 = parts.next()?;
    let sig_b64 = parts.next()?;
    // Alg prüfen (kein alg=none / kein RS256-Confusion)
    let header = b64d(header_b64)?;
    let hv: serde_json::Value = serde_json::from_slice(&header).ok()?;
    if hv.get("alg").and_then(|a| a.as_str()) != Some("HS256") {
        return None;
    }
    let expected = hmac_sha256(secret.as_bytes(), format!("{}.{}", header_b64, payload_b64).as_bytes());
    let given = b64d(sig_b64)?;
    if given.len() != expected.len() || subtle_eq(&given, &expected) != 1 {
        return None;
    }
    let payload = b64d(payload_b64)?;
    let pv: serde_json::Value = serde_json::from_slice(&payload).ok()?;
    let exp = pv.get("exp").and_then(|e| e.as_i64())?;
    if now_secs() > exp {
        return None;
    }
    Some(Claims {
        user_id: pv.get("uid").and_then(|v| v.as_i64())?,
        is_admin: pv.get("adm").and_then(|v| v.as_bool()).unwrap_or(false),
        two_fa_pending: pv.get("2fa_pending").and_then(|v| v.as_bool()).unwrap_or(false),
    })
}

fn subtle_eq(a: &[u8], b: &[u8]) -> u8 {
    // Constant-Time-Vergleich
    let mut diff = 0u8;
    for i in 0..a.len() {
        diff |= a[i] ^ b[i];
    }
    (diff == 0) as u8
}

pub fn generate_refresh_token() -> (String, chrono::DateTime<chrono::Local>) {
    let mut b = [0u8; 32];
    rand::rng().fill_bytes(&mut b);
    let expires = chrono::Local::now() + chrono::Duration::seconds(REFRESH_DUR_SECS);
    (hex::encode(b), expires)
}

// ── bcrypt ──────────────────────────────────────────────

/// Go bcrypt.DefaultCost = 10. Rust-Crate nutzt Default 12 — wir setzen 10 für Parität/Speed.
pub const BCRYPT_COST: u32 = 10;

pub fn hash_password(pw: &str) -> Result<String, bcrypt::BcryptError> {
    bcrypt::hash(pw, BCRYPT_COST)
}

pub fn verify_password(hash: &str, pw: &str) -> bool {
    bcrypt::verify(pw, hash).unwrap_or(false)
}

static DUMMY_HASH: OnceLock<String> = OnceLock::new();

pub fn dummy_hash() -> &'static str {
    DUMMY_HASH.get_or_init(|| hash_password("dummy-timing-protection").unwrap_or_default())
}

// ── TOTP ────────────────────────────────────────────────

const TOTP_DIGITS: u8 = 6;
const TOTP_PERIOD: u64 = 30;
const TOTP_WINDOW: i64 = 1;

pub fn generate_totp_secret() -> String {
    let mut raw = [0u8; 20];
    rand::rng().fill_bytes(&mut raw);
    // Base32 ohne Padding (Go: base32.StdEncoding.WithPadding(NoPadding))
    base32_encode(&raw)
}

const B32: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

fn base32_encode(data: &[u8]) -> String {
    let mut out = String::new();
    let mut buf: u32 = 0;
    let mut bits = 0;
    for &b in data {
        buf = (buf << 8) | b as u32;
        bits += 8;
        while bits >= 5 {
            bits -= 5;
            out.push(B32[((buf >> bits) & 31) as usize] as char);
        }
    }
    if bits > 0 {
        out.push(B32[((buf << (5 - bits)) & 31) as usize] as char);
    }
    out
}

fn base32_decode(s: &str) -> Option<Vec<u8>> {
    let s = s.trim().to_uppercase();
    let mut buf: u32 = 0;
    let mut bits = 0;
    let mut out = Vec::new();
    for c in s.bytes() {
        if c == b'=' {
            break;
        }
        let v = B32.iter().position(|&x| x == c)? as u32;
        buf = (buf << 5) | v;
        bits += 5;
        if bits >= 8 {
            bits -= 8;
            out.push((buf >> bits) as u8);
        }
    }
    Some(out)
}

fn hmac_sha1(key: &[u8], msg: &[u8]) -> Vec<u8> {
    use sha1::{Digest as _, Sha1};
    let mut k: Vec<u8> = if key.len() > 64 {
        let mut h = Sha1::new();
        h.update(key);
        h.finalize().to_vec()
    } else {
        key.to_vec()
    };
    k.resize(64, 0);
    let ipad: Vec<u8> = k.iter().map(|b| b ^ 0x36).collect();
    let opad: Vec<u8> = k.iter().map(|b| b ^ 0x5c).collect();
    let mut inner = Sha1::new();
    inner.update(&ipad);
    inner.update(msg);
    let ih = inner.finalize();
    let mut outer = Sha1::new();
    outer.update(&opad);
    outer.update(&ih);
    outer.finalize().to_vec()
}

fn totp_at(secret: &str, t: SystemTime) -> Option<String> {
    let key = base32_decode(secret)?;
    let secs = t.duration_since(UNIX_EPOCH).ok()?.as_secs();
    let counter = secs / TOTP_PERIOD;
    let h = hmac_sha1(&key, &counter.to_be_bytes());
    let off = (h[h.len() - 1] & 0x0f) as usize;
    let code = ((h[off] as u32 & 0x7f) << 24)
        | ((h[off + 1] as u32) << 16)
        | ((h[off + 2] as u32) << 8)
        | (h[off + 3] as u32);
    Some(format!("{:06}", code % 1_000_000))
}

pub fn verify_totp(secret: &str, code: &str) -> bool {
    let code = code.trim();
    if code.len() != TOTP_DIGITS as usize || !code.bytes().all(|b| b.is_ascii_digit()) {
        return false;
    }
    let now = SystemTime::now();
    for delta in -TOTP_WINDOW..=TOTP_WINDOW {
        let t = if delta < 0 {
            now - Duration::from_secs((-delta as u64) * TOTP_PERIOD)
        } else if delta > 0 {
            now + Duration::from_secs((delta as u64) * TOTP_PERIOD)
        } else {
            now
        };
        if let Some(c) = totp_at(secret, t) {
            if c == code {
                return true;
            }
        }
    }
    false
}

pub fn totp_provisioning_uri(secret: &str, account: &str, issuer: &str) -> String {
    format!(
        "otpauth://totp/{}:{}?secret={}&issuer={}&algorithm=SHA1&digits={}&period={}",
        issuer, account, secret, issuer, TOTP_DIGITS, TOTP_PERIOD
    )
}

pub fn generate_backup_codes() -> Vec<String> {
    (0..8)
        .map(|_| {
            let mut b = [0u8; 5];
            rand::rng().fill_bytes(&mut b);
            hex::encode(b)
        })
        .collect()
}
