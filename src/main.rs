//! Fileship — Rust (axum + tokio). 1:1-Port des Go-Originals.

mod agent;
mod auth;
mod config;
mod db;
mod fsops;
mod handler;
mod h3server;
mod hub;
mod middleware;
mod model;
mod static_fs;
mod webdav;

use rand_core::Rng;
use std::net::SocketAddr;
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
    let tls_cert = cfg.tls_cert.clone();
    let tls_key = cfg.tls_key.clone();
    let state_for_ext = state.clone();
    let dav_state = state.clone();
    // IMPORTANT: .fallback() MUST be registered before the .layer() calls —
    // axum applies layer() only to already-registered routes (+ fallback).
    //
    // Compression only on the static fallback (SPA/WebDAV): API JSON and
    // especially file downloads (potentially GB-sized, possibly already compressed)
    // stay uncompressed.
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

    // Cap the audit log at AUDIT_RETENTION_DAYS (default 90).
    let retention_days: i64 = env_or("AUDIT_RETENTION_DAYS", "90").parse().unwrap_or(90);
    let pruned = database.prune_audit_log(retention_days);
    if pruned > 0 {
        eprintln!("pruned {} audit entries older than {} days", pruned, retention_days);
    }

    // Purge expired trash entries (all users) at startup so the trash
    // directory does not grow forever for inactive users.
    let expired_trash = database.purge_all_expired_trash();
    if !expired_trash.is_empty() {
        let tdir = db::trash_dir(&cfg.db_path);
        for name in &expired_trash {
            let _ = std::fs::remove_dir_all(tdir.join(name));
            let _ = std::fs::remove_file(tdir.join(name));
        }
        eprintln!("purged {} expired trash entries", expired_trash.len());
    }

    let addr = format!("0.0.0.0:{}", cfg.port);
    let listener = tokio::net::TcpListener::bind(&addr).await.unwrap_or_else(|e| {
        eprintln!("bind {}: {}", addr, e);
        std::process::exit(1);
    });

    // HTTP/3 on UDP, same port (TLS deployments only).
    if tls {
        let h3_addr: SocketAddr = format!("0.0.0.0:{}", cfg.port).parse().unwrap();
        if let Err(e) = h3server::serve_http3(h3_addr, &tls_cert, &tls_key, app.clone()) {
            eprintln!("HTTP/3 disabled: {}", e);
        } else {
            eprintln!("   + HTTP/3 enabled (UDP/{})", cfg.port);
        }
    }

    // HTTP/1.1 + HTTP/2 on the same port (h2c prior-knowledge / upgrade, and
    // ALPN "h2"/"http/1.1" for TLS simultaneously) via hyper's auto builder.
    let acceptor: Option<tokio_rustls::TlsAcceptor> = if tls {
        eprintln!("🚀 Fileship running on https://localhost:{} (TLS, HTTP/1.1 + HTTP/2)", cfg.port);
        eprintln!("   Root: {} | DB: {}", cfg.root_path, cfg.db_path);
        let mut cert_reader = std::io::BufReader::new(std::fs::File::open(&cfg.tls_cert).unwrap());
        let certs: Vec<rustls::pki_types::CertificateDer<'static>> = rustls_pemfile::certs(&mut cert_reader)
            .map(|c| c.unwrap().into_owned())
            .collect();
        let mut key_reader = std::io::BufReader::new(std::fs::File::open(&cfg.tls_key).unwrap());
        let key = rustls_pemfile::pkcs8_private_keys(&mut key_reader).next().unwrap().unwrap();
        let mut server_config = rustls::ServerConfig::builder()
            .with_no_client_auth()
            .with_single_cert(certs, rustls::pki_types::PrivateKeyDer::Pkcs8(key))
            .unwrap();
        server_config.alpn_protocols = vec![b"h2".to_vec(), b"http/1.1".to_vec()];
        Some(tokio_rustls::TlsAcceptor::from(Arc::new(server_config)))
    } else {
        eprintln!("🚀 Fileship running on http://localhost:{} (HTTP/1.1 + HTTP/2)", cfg.port);
        eprintln!("   Root: {} | DB: {}", cfg.root_path, cfg.db_path);
        None
    };

    loop {
        tokio::select! {
            res = listener.accept() => {
                let (stream, _addr) = match res {
                    Ok(x) => x,
                    Err(e) => {
                        eprintln!("accept: {}", e);
                        continue;
                    }
                };
                let app = hyper_util::service::TowerToHyperService::new(app.clone());
                let acceptor = acceptor.clone();
                tokio::spawn(async move {
                    use hyper_util::rt::TokioIo;
                    let builder = hyper_util::server::conn::auto::Builder::new(
                        hyper_util::rt::TokioExecutor::new(),
                    );
                    let conn = match acceptor {
                        Some(acc) => match acc.accept(stream).await {
                            Ok(tls_stream) => builder
                                .serve_connection_with_upgrades(TokioIo::new(tls_stream), app)
                                .await,
                            Err(e) => {
                                eprintln!("tls handshake: {}", e);
                                return;
                            }
                        },
                        None => builder
                            .serve_connection_with_upgrades(TokioIo::new(stream), app)
                            .await,
                    };
                    if let Err(e) = conn {
                        eprintln!("connection: {}", e);
                    }
                });
            }
            _ = tokio::signal::ctrl_c() => {
                eprintln!("shutting down...");
                break;
            }
        }
    }
}
