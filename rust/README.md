# Fileship (Rust)

Vollständiger Port des [Fileship](https://github.com/Marco-Lu2508/fileship)-Backends von
Go nach Rust. Gleiche API, gleiches SQLite-Schema, gleiche Frontend-Assets —
drops in dieselbe Deployment-Umgebung, DBs sind austauschbar mit dem Original.

## Stack

- **Runtime:** Tokio (Multi-Thread) + Axum 0.8 (HTTP/1.1 + WebSocket)
- **DB:** SQLite via rusqlite (WAL, synchron, 64-MB-Cache)
- **Auth:** Hand-rolled JWT (HS256, Go-kompatibel), bcrypt, RFC-6238-TOTP
- **Sonstiges:** WebDAV, Agent-WebSocket (JSON-RPC), Chunk-Upload, Transcoding
  via ffmpeg-Subprozess, Embed-Frontend via rust-embed

## Speed-Profil

```toml
[profile.release]
opt-level   = 3
lto         = "fat"
codegen-units = 1
panic       = "abort"
strip       = true
```

Single binary, kein Runtime-Overhead. Die Hot-Pfade (Listen, Suche, Downloads)
 vermeiden Allokationen wo sinnvoll; Thumbnails via Lanczos-Resampling.

## Bauen & Laufen

```bash
cargo build --release
./target/release/fileship
```

## Konfiguration (Env)

| Variable        | Default       | Beschreibung                          |
|-----------------|---------------|---------------------------------------|
| `PORT`          | `8080`        | Listen-Port                           |
| `DB_PATH`       | `./fileship.db` | SQLite-DB (schema-kompatibel zum Go-Original) |
| `ROOT_PATH`     | `./data`      | Dateiroot                             |
| `JWT_SECRET`    | (ephemeral)   | Unbedingt setzen, sonst Sessions weg bei Restart |
| `ADMIN_PASSWORD`| —             | Setzt das Admin-Passwort beim Erststart |
| `MAX_UPLOAD_MB` | `1024`        | Upload-Limit                          |
| `TLS_CERT/TLS_KEY` | —          | Optional, TLS-Terminierung            |

Agent-Modus: `./fileship -agent --server ws://… --token …` (oder `FILESHIP_AGENT=1`).

## Docker

```bash
docker build -t fileship-rust .
docker run -p 8080:8080 -v ./data:/data -e JWT_SECRET=… -e ADMIN_PASSWORD=… fileship-rust
```

## Module

| Datei | Inhalt |
|-------|--------|
| `src/main.rs` | Bootstrap, Server, TLS, Agent-Modus |
| `src/handler.rs` | Alle 69 HTTP-Handler (1:1-Port von `internal/handler`) |
| `src/webdav.rs` | WebDAV (PROPFIND/LOCK/MOVE/…) |
| `src/agent.rs` | Remote-Device-Agent, JSON-RPC über WS |
| `src/db.rs` | SQLite-Layer (schema-identisch) |
| `src/fsops.rs` | Filesystem-Ops (Go-`filepath`-Semantik) |
| `src/auth.rs` | JWT/bcrypt/TOTP |
| `src/middleware.rs` | Auth, CSRF, Rate-Limit, Security-Header |
| `src/hub.rs` | Frontend-WebSocket-Hub |
| `src/static_fs.rs` | Embedded SPA (`frontend-dist/`) |

## Status

- Routen-Parität: **69/69** (Go ↔ Rust, per Script verifiziert)
- Smoke-Tests: Login, Browse, Upload, Download, Share, WebDAV ✓
- Release-Binary: ~11 MB, LTO/fat
