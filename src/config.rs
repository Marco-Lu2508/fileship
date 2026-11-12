//! Config — identische Env-Variablen & Defaults wie Go.

use rand_core::Rng;

#[derive(Debug, Clone)]
pub struct Config {
    pub port: String,
    pub jwt_secret: String,
    pub db_path: String,
    pub root_path: String,
    pub max_upload_size: i64,
    pub tls_cert: String,
    pub tls_key: String,
    pub allowed_types: String,
}

fn get_env(key: &str, fallback: &str) -> String {
    match std::env::var(key) {
        Ok(v) if !v.is_empty() => v,
        _ => fallback.to_string(),
    }
}

fn gen_secret() -> String {
    let mut b = [0u8; 32];
    rand::rng().fill_bytes(&mut b);
    hex::encode(b)
}

impl Config {
    pub fn load() -> Self {
        let max_upload_mb: i64 = get_env("MAX_UPLOAD_MB", "1024").parse().unwrap_or(1024);
        let jwt_secret = get_env("JWT_SECRET", "");
        let jwt_secret = if jwt_secret.is_empty() {
            eprintln!(
                "⚠️  JWT_SECRET not set — generated ephemeral secret. Set JWT_SECRET env var for persistent sessions!"
            );
            gen_secret()
        } else {
            jwt_secret
        };
        Config {
            port: get_env("PORT", "8080"),
            jwt_secret,
            db_path: get_env("DB_PATH", "./fileship.db"),
            root_path: get_env("ROOT_PATH", "./data"),
            max_upload_size: max_upload_mb * 1024 * 1024,
            tls_cert: get_env("TLS_CERT", ""),
            tls_key: get_env("TLS_KEY", ""),
            allowed_types: get_env("ALLOWED_TYPES", ""),
        }
    }

    pub fn tls_enabled(&self) -> bool {
        !self.tls_cert.is_empty() && !self.tls_key.is_empty()
    }
}
