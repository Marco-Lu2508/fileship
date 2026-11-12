//! Fileship — Rust (axum + tokio). 1:1-Port des Go-Originals.

mod agent;
mod auth;
mod config;
mod db;
mod fsops;
mod handler;
mod hub;
mod middleware;
mod model;
mod static_fs;
mod webdav;

use rand_core::Rng;
use std::sync::Arc;

fn env_or(key: &str, fallback: &str) -> String {
    match std::env::var(key) {
        Ok(v) if !v.is_empty() => v,
        _ => fallback.to_string(),
    }
}

fn flag(args: &[String], names: &[&str]) -> Option<String> {
    // --name value | --name=value | -name value
    for (i, a) in args.iter().enumerate() {
        for n in names {
            if a == n {
                return args.get(i + 1).cloned();
            }
            if let Some(v) = a.strip_prefix(&format!("{}=", n)) {
                return Some(v.to_string());
            }
        }
    }
    None
}

fn has_flag(args: &[String], names: &[&str]) -> bool {
    args.iter().any(|a| names.contains(&a.as_str()))
}

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();

    // ── Agent-Modus ──
    let agent_mode = has_flag(&args, &["-agent", "--agent"]) || env_or("FILESHIP_AGENT", "") == "1";
    let agent_server = flag(&args, &["-server", "--server"]).unwrap_or_else(|| env_or("FILESHIP_SERVER", ""));
    let agent_token = flag(&args, &["-token", "--token"]).unwrap_or_else(|| env_or("FILESHIP_TOKEN", ""));
    let agent_root = flag(&args, &["-root", "--root"]).unwrap_or_else(|| env_or("ROOT_PATH", ""));
    if agent_mode || !agent_token.is_empty() {
        if agent_server.is_empty() || agent_token.is_empty() {
            eprintln!("Agent mode requires --server <url> and --token <fsa_...>");
            std::process::exit(1);
        }
        let root_dir = if agent_root.is_empty() { "./data".to_string() } else { agent_root };
        eprintln!("🛰️  Starting Fileship Remote Agent -> {} (Root: {})", agent_server, root_dir);
        agent::run_client(&agent_server, &agent_token, &root_dir).await;
        return;
    }

    // ── Server-Modus ──
    let cfg = config::Config::load();
    let proxies = middleware::ProxyCfg::from_env();
    if let Err(e) = std::fs::create_dir_all(&cfg.root_path) {
        eprintln!("cannot create root path: {}", e);
        std::process::exit(1);
    }
    let database = db::Db::open(&cfg.db_path).unwrap_or_else(|e| {
        eprintln!("db: {}", e);
        std::process::exit(1);
    });
    let database = Arc::new(database);

    let admin_pass = env_or("ADMIN_PASSWORD", "");
    let admin_pass = if admin_pass.is_empty() {
        let mut b = [0u8; 12];
        rand::rng().fill_bytes( &mut b);
        let generated = hex::encode(b);
        eprintln!("⚠️  ADMIN_PASSWORD not set — generated: {} (set ADMIN_PASSWORD env var!)", generated);
        generated
    } else {
        admin_pass
    };
    if let Err(e) = database.create_admin_if_not_exists("admin", &admin_pass, "/") {
        eprintln!("admin setup: {}", e);
        std::process::exit(1);
    }

    let hub = Arc::new(hub::Hub::new(cfg.jwt_secret.clone()));
    let db_for_hb = database.clone();
    let agent_hub = Arc::new(agent::Hub::new(Arc::new(move |id, hostname, os_info, root_path, usage| {
        db_for_hb.update_agent_heartbeat(id, hostname, os_info, root_path, usage);
    })));
    let indexer = fsops::Indexer::new(database.clone());
    let state = handler::AppState {
        cfg: cfg.clone(),
        db: database.clone(),
        hub: hub.clone(),
        indexer,
        agent_hub,
        login_limiter: middleware::RateLimiter::new(1.0 / 6.0, 10.0),
        global_limiter: middleware::RateLimiter::new(50.0, 100.0),
        proxies,
    };

    let tls = cfg.tls_enabled();
    let state_for_ext = state.clone();
    let dav_state = state.clone();
    // WICHTIG: .fallback() MUSS vor den .layer()-Aufrufen stehen —
    // axum wendet layer() nur auf bereits registrierte Routen (+ Fallback) an.
    //
    // Compression nur auf den Static-Fallback (SPA/WebDAV): API-JSON und
    // vor allem File-Downloads (potenziell GB-groß, evtl. schon komprimiert)
    // bleiben unkomprimiert.
    let static_app = axum::Router::<()>::new()
        .fallback(move |req: axum::http::Request<axum::body::Body>| {
            let dav_state = dav_state.clone();
            async move {
                if req.uri().path().starts_with("/webdav") {
                    use axum::extract::State;
                    webdav::webdav_handler(State(dav_state), req).await
                } else {
                    handler::static_handler(req).await
                }
            }
        })
        .layer(tower_http::compression::CompressionLayer::new());

    let app = handler::router(state.clone()).with_state(state)
        .merge(static_app)
        .layer(axum::middleware::from_fn(middleware::csrf_security_mw))
        .layer(axum::Extension(tls))
        .layer(axum::Extension(cfg.root_path.clone()))
        .layer(axum::Extension(state_for_ext));

    let addr = format!("0.0.0.0:{}", cfg.port);
    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap_or_else(|e| {
        eprintln!("bind {}: {}", addr, e);
        std::process::exit(1);
    });

    if tls {
        eprintln!("🚀 Fileship running on https://localhost:{} (TLS)", cfg.port);
        eprintln!("   Root: {} | DB: {}", cfg.root_path, cfg.db_path);
        let mut cert_reader = std::io::BufReader::new(std::fs::File::open(&cfg.tls_cert).unwrap());
        let certs: Vec<rustls::pki_types::CertificateDer<'static>> = rustls_pemfile::certs(&mut cert_reader)
            .map(|c| c.unwrap().into_owned())
            .collect();
        let mut key_reader = std::io::BufReader::new(std::fs::File::open(&cfg.tls_key).unwrap());
        let key = rustls_pemfile::pkcs8_private_keys(&mut key_reader).next().unwrap().unwrap();
        let server_config = rustls::ServerConfig::builder()
            .with_no_client_auth()
            .with_single_cert(certs, rustls::pki_types::PrivateKeyDer::Pkcs8(key))
            .unwrap();
        let tls_listener = TlsListener { inner: listener, acceptor: tokio_rustls::TlsAcceptor::from(Arc::new(server_config)) };
        axum::serve(tls_listener, app)
            .with_graceful_shutdown(async {
                let _ = tokio::signal::ctrl_c().await;
                eprintln!("shutting down...");
            })
            .await
            .unwrap_or_else(|e| {
                eprintln!("serve: {}", e);
            });
    } else {
        eprintln!("🚀 Fileship running on http://localhost:{}", cfg.port);
        eprintln!("   Root: {} | DB: {}", cfg.root_path, cfg.db_path);
        axum::serve(listener, app)
            .with_graceful_shutdown(async {
                let _ = tokio::signal::ctrl_c().await;
                eprintln!("shutting down...");
            })
            .await
            .unwrap_or_else(|e| {
                eprintln!("serve: {}", e);
            });
    }
}

/// axum::serve-kompatibler TLS-Listener (tokio-rustls 0.26: LazyConfigAcceptor nicht mehr direkt).
struct TlsListener {
    inner: tokio::net::TcpListener,
    acceptor: tokio_rustls::TlsAcceptor,
}

impl axum::serve::Listener for TlsListener {
    type Addr = std::net::SocketAddr;
    type Io = tokio_rustls::server::TlsStream<tokio::net::TcpStream>;

    fn local_addr(&self) -> std::io::Result<Self::Addr> {
        self.inner.local_addr()
    }

    async fn accept(&mut self) -> (Self::Io, Self::Addr) {
        let (stream, addr) = self.inner.accept().await.expect("accept");
        let tls = self.acceptor.clone().accept(stream).await.expect("tls handshake");
        (tls, addr)
    }
}
