//! HTTP/3 (QUIC) server: translates h3 requests into the same axum Router
//! used for HTTP/1.1 + HTTP/2, so all routes/middleware work unchanged.
//! Only available with TLS (h3 requires QUIC + TLS by spec).

use axum::body::Body;
use bytes::Buf as _;
use axum::http::{Request, Response};
use futures_util::TryStreamExt;
use std::net::SocketAddr;
use std::sync::Arc;
use tower::ServiceExt;

pub type AppRouter = axum::Router;

pub fn serve_http3(
    addr: SocketAddr,
    tls_cert: &str,
    tls_key: &str,
    app: AppRouter,
) -> Result<(), String> {
    // quinn pulls in a second crypto provider; rustls can no longer pick
    // one automatically, so install the default explicitly.
    let _ = rustls::crypto::aws_lc_rs::default_provider().install_default();

    let certs = read_pem_certs(tls_cert)?;
    let key = read_pem_key(tls_key)?;

    let mut provider = rustls::ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(certs, key)
        .map_err(|e| format!("tls: {}", e))?;
    provider.alpn_protocols = vec![b"h3".to_vec()];

    let quic_cfg = quinn::crypto::rustls::QuicServerConfig::try_from(provider)
        .map_err(|e| format!("quic config: {}", e))?;
    let server_cfg = quinn::ServerConfig::with_crypto(Arc::new(quic_cfg));

    let endpoint = quinn::Endpoint::server(server_cfg, addr)
        .map_err(|e| format!("quic bind {}: {}", addr, e))?;

    tokio::spawn(async move {
        while let Some(incoming) = endpoint.accept().await {
            if let Ok(conn) = incoming.await {
                let app = app.clone();
                tokio::spawn(async move {
                    let h3_conn = match h3::server::Connection::new(h3_quinn::Connection::new(conn)).await
                    {
                        Ok(c) => c,
                        Err(e) => {
                            eprintln!("h3 handshake failed: {}", e);
                            return;
                        }
                    };
                    serve_h3_connection(h3_conn, app).await;
                });
            }
        }
    });
    Ok(())
}

async fn serve_h3_connection(
    mut conn: h3::server::Connection<h3_quinn::Connection, bytes::Bytes>,
    app: AppRouter,
) {
    loop {
        let resolver = match conn.accept().await {
            Ok(Some(r)) => r,
            Ok(None) => return,
            Err(_) => return,
        };
        let app = app.clone();
        tokio::spawn(async move {
            let (h3_req, mut req_stream) = match resolver.resolve_request().await {
                Ok(r) => r,
                Err(_) => return,
            };

            // Read the full request body (API requests are small; uploads go
            // through the same code path as HTTP/1.1, just without early
            // streaming — acceptable for the h3 convenience channel).
            let mut body = Vec::new();
            loop {
                match req_stream.recv_data().await {
                    Ok(Some(chunk)) => body.extend_from_slice(chunk.chunk()),
                    Ok(None) => break,
                    Err(_) => return,
                }
            }

            let mut builder = Request::builder()
                .method(h3_req.method().clone())
                .uri(h3_req.uri().clone());
            for (k, v) in h3_req.headers().iter() {
                builder = builder.header(k, v);
            }
            let http_req = match builder.body(Body::from(body)) {
                Ok(r) => r,
                Err(_) => return,
            };

            let http_resp: Response<Body> = match app.oneshot(http_req).await {
                Ok(r) => r,
                Err(_) => return,
            };
            let (parts, resp_body) = http_resp.into_parts();

            let mut head = Response::new(());
            *head.status_mut() = parts.status;
            *head.headers_mut() = parts.headers;
            if req_stream.send_response(head).await.is_err() {
                return;
            }

            let mut stream = resp_body.into_data_stream();
            while let Ok(Some(chunk)) = stream.try_next().await {
                if req_stream.send_data(chunk).await.is_err() {
                    return;
                }
            }
            let _ = req_stream.finish().await;
        });
    }
}

fn read_pem_certs(path: &str) -> Result<Vec<rustls::pki_types::CertificateDer<'static>>, String> {
    let mut reader = std::io::BufReader::new(
        std::fs::File::open(path).map_err(|e| format!("open {}: {}", path, e))?,
    );
    let certs: Vec<rustls::pki_types::CertificateDer<'static>> = rustls_pemfile::certs(&mut reader)
        .map(|c| c.map_err(|e| e.to_string()).map(|c| c.into_owned()))
        .collect::<Result<_, _>>()?;
    if certs.is_empty() {
        return Err(format!("no certificates in {}", path));
    }
    Ok(certs)
}

fn read_pem_key(path: &str) -> Result<rustls::pki_types::PrivateKeyDer<'static>, String> {
    use base64::Engine as _;
    let pem = std::fs::read_to_string(path).map_err(|e| format!("open {}: {}", path, e))?;
    let begin = pem
        .find("-----BEGIN PRIVATE KEY-----")
        .ok_or_else(|| format!("no PKCS#8 private key in {}", path))?;
    let rest = &pem[begin + "-----BEGIN PRIVATE KEY-----".len()..];
    let end = rest
        .find("-----END PRIVATE KEY-----")
        .ok_or_else(|| format!("malformed private key in {}", path))?;
    let b64: String = rest[..end].chars().filter(|c| !c.is_whitespace()).collect();
    let der = base64::engine::general_purpose::STANDARD
        .decode(&b64)
        .map_err(|e| format!("private key decode: {}", e))?;
    Ok(rustls::pki_types::PrivateKeyDer::Pkcs8(der.into()))
}
