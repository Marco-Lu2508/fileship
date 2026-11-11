//! Static — Embed des Frontend-Builds (rust-embed), 1:1 wie internal/static.

use axum::body::Body;
use axum::http::{header, StatusCode};
use axum::response::Response;
use rust_embed::RustEmbed;

#[derive(RustEmbed)]
#[folder = "frontend-dist"]
struct FrontendDist;

fn serve_embedded(path: &str) -> Option<(Vec<u8>, &'static str)> {
    let asset = FrontendDist::get(path)?;
    let mime: &'static str = match path.rsplit('.').next().unwrap_or("") {
        "html" => "text/html; charset=utf-8",
        "js" => "text/javascript; charset=utf-8",
        "css" => "text/css; charset=utf-8",
        "svg" => "image/svg+xml",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "ico" => "image/x-icon",
        "woff" => "font/woff",
        "woff2" => "font/woff2",
        "ttf" => "font/ttf",
        "json" => "application/json",
        "map" => "application/json",
        _ => "application/octet-stream",
    };
    Some((asset.data.into_owned(), mime))
}

pub fn serve(req_path: &str) -> Response {
    let path = req_path.trim_start_matches('/');
    if path.is_empty() || path == "index.html" {
        let (data, mime) = serve_embedded("index.html").unwrap_or_else(|| (b"<html></html>".to_vec(), "text/html; charset=utf-8"));
        let mut res = Response::new(Body::from(data));
        res.headers_mut().insert(header::CONTENT_TYPE, mime.parse().unwrap_or(header::HeaderValue::from_static("text/html")));
        res.headers_mut().insert(header::CACHE_CONTROL, header::HeaderValue::from_static("no-cache"));
        return res;
    }
    if let Some((data, mime)) = serve_embedded(path) {
        let mut res = Response::new(Body::from(data));
        res.headers_mut().insert(header::CONTENT_TYPE, mime.parse().unwrap_or(header::HeaderValue::from_static("application/octet-stream")));
        if path.starts_with("assets/") {
            res.headers_mut().insert(header::CACHE_CONTROL, header::HeaderValue::from_static("public, max-age=31536000, immutable"));
        } else if path.starts_with("locales/") || path.starts_with("fonts/") || path == "favicon.svg" {
            res.headers_mut().insert(header::CACHE_CONTROL, header::HeaderValue::from_static("public, max-age=86400"));
        }
        return res;
    }
    // Nicht gefunden: assets/locales/fonts/favicon → 404, sonst SPA-Fallback
    if path.starts_with("assets/") || path.starts_with("locales/") || path.starts_with("fonts/") || path == "favicon.svg" {
        let mut res = Response::new(Body::from("404 page not found\n"));
        *res.status_mut() = StatusCode::NOT_FOUND;
        res.headers_mut().insert(header::CONTENT_TYPE, header::HeaderValue::from_static("text/plain; charset=utf-8"));
        return res;
    }
    let (data, mime) = serve_embedded("index.html").unwrap_or_else(|| (b"<html></html>".to_vec(), "text/html; charset=utf-8"));
    let mut res = Response::new(Body::from(data));
    res.headers_mut().insert(header::CONTENT_TYPE, mime.parse().unwrap_or(header::HeaderValue::from_static("text/html")));
    res.headers_mut().insert(header::CACHE_CONTROL, header::HeaderValue::from_static("no-cache"));
    res
}

pub fn locale(lang: &str) -> Option<Vec<u8>> {
    let path = format!("locales/{}.json", lang);
    FrontendDist::get(&path).map(|a| a.data.into_owned())
}
