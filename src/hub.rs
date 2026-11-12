//! Frontend-WS-Hub — 1:1 Port von internal/ws.

use crate::model::WsEvent;
use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use futures_util::{SinkExt, StreamExt};
use std::sync::Mutex;
use tokio::sync::mpsc;

pub struct Hub {
    secret: String,
    clients: Mutex<Vec<mpsc::UnboundedSender<String>>>,
}

impl Hub {
    pub fn new(secret: String) -> Hub {
        Hub { secret, clients: Mutex::new(Vec::new()) }
    }

    pub fn broadcast(&self, event: &WsEvent) {
        let data = match serde_json::to_string(event) {
            Ok(s) => s,
            Err(_) => return,
        };
        let mut g = self.clients.lock().unwrap();
        g.retain(|tx| tx.send(data.clone()).is_ok());
    }

    pub async fn handle(self: &std::sync::Arc<Self>, req: axum::extract::Request, ws: WebSocketUpgrade) -> Response {
        let (parts, _) = req.into_parts();
        let query = parts.uri.query().unwrap_or("");
        let mut token = String::new();
        for pair in query.split('&') {
            if let Some(v) = pair.strip_prefix("token=") {
                token = crate::middleware::percent_decode(v);
            }
        }
        match crate::auth::parse_access_token(&self.secret, &token) {
            Some(c) if !c.two_fa_pending => {}
            _ => return crate::middleware::err_resp(StatusCode::UNAUTHORIZED, "unauthorized"),
        }
        if let Some(origin) = parts.headers.get("origin").and_then(|h| h.to_str().ok()) {
            if !origin.is_empty() {
                let host = parts.headers.get("host").and_then(|h| h.to_str().ok()).unwrap_or("");
                let parsed_host = origin.split("://").nth(1).unwrap_or("").split('/').next().unwrap_or("");
                if parsed_host != host {
                    return crate::middleware::err_resp(StatusCode::FORBIDDEN, "forbidden origin");
                }
            }
        }
        let me = self.clone();
        ws.on_upgrade(move |socket| async move { me.run(socket).await }).into_response()
    }

    async fn run(&self, socket: WebSocket) {
        let (mut sink, mut stream) = socket.split();
        let (tx, mut rx) = mpsc::unbounded_channel::<String>();
        self.clients.lock().unwrap().push(tx);
        let send_task = tokio::spawn(async move {
            while let Some(msg) = rx.recv().await {
                if sink.send(Message::Text(msg.into())).await.is_err() {
                    break;
                }
            }
        });
        while let Some(Ok(_msg)) = stream.next().await {}
        send_task.abort();
    }
}
