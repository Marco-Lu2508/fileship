# 🚢 Fileship

[![Rust](https://img.shields.io/badge/Rust-1.90%2B-dea584)](https://rust-lang.org)
[![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00)](https://svelte.dev)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED)](https://www.docker.com)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-lightgrey)](https://github.com/Marco-Lu2508/fileship)

A self-hosted file browser + manager with WebDAV, file sharing, favorites, HTTP range downloads and a modern Svelte 5 UI. Powered by a Rust backend (Axum/Tokio) — built for maximum speed: LTO release profile, gzip + Brotli compression, and ~107 KB (gzip) initial frontend load.

## ✨ Key Features

- 📁 Browse, upload (chunked or multipart), download, rename, copy, move, delete
- 🎬 Video streaming with HTTP Range (MP4/MKV/WebM) + ffmpeg transcoding
- 🖼️ Image previews & thumbnails (Lanczos), incl. PDF first-page and HEIC/HEIF (via poppler-utils / libheif)
- 📦 Create/extract archives (ZIP, TAR, TAR.GZ)
- 🔗 Public share links with optional password and expiry
- ⭐ Favorites, 🗑️ Trash with automatic expiry (`TRASH_RETENTION_DAYS`, default 30)
- 📱 PWA: installable, offline app shell (service worker caches assets only, never the API)
- 🌐 HTTP/1.1 + HTTP/2 + HTTP/3 on one port (h3 when TLS is enabled)
- 🔌 WebDAV support at `/webdav`
- 🤖 Agent filesystem with WebSocket agent protocol
- 🔍 Full-text search (FTS5, prefix-ranked)
- 📦 ZIP archive preview (browse + download entries without extracting)
- 🔑 OIDC / SSO login (Keycloak, Authelia, Google, GitHub, … — PKCE, auto-provisioning, 2FA respected)
- 🗄️ One-click database backup (consistent SQLite export, admin menu)

## 🌐 Live Demo

> Deployed 24/7 on a homelab server — accessible via Tailscale/WireGuard VPN.

**No public demo instance** — this is a self-hosted application.

## 🐳 Docker

```bash
docker compose up -d
```

Open http://localhost:8080

## ⚙️ Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `ROOT_PATH` | `./data` | Filesystem root shown in the UI |
| `DB_PATH` | `./fileship.db` | SQLite database (auto-migrated) |
| `JWT_SECRET` | *(ephemeral)* | JWT signing key — **set this**, sessions are lost on restart otherwise |
| `MAX_UPLOAD_MB` | `1024` | Max upload size |
| `ADMIN_PASSWORD` | — | Initial admin password (first start only) |
| `TLS_CERT` / `TLS_KEY` | — | Enable HTTPS when set (rustls) |

## 📡 API

- `POST /api/auth/login` · `/api/auth/refresh` · `/api/auth/2fa/verify` — auth + TOTP 2FA
- `GET/POST/DELETE /api/files/*` — list, upload, download, mkdir, move, copy, delete, info, search
- `POST /api/files/upload/chunk` — chunked upload (16 MB chunks)
- `/api/shares/*`, `/api/trash/*`, `/api/favorites/*`, `/api/agents/*`, `/api/audit/*`, `/api/stats/*` — shares, trash, favorites, agents, audit, stats
- `PROPFIND /webdav/` — WebDAV
- `GET /ws` — live updates (JWT in the upgrade token)

Auth: Bearer JWT (`Authorization: Bearer <token>`), API tokens for WebDAV/scripts.

All 69 API/WebDAV routes of the previous Go backend are implemented in Rust (route parity verified against the original route table).

## 🤖 AI Agents

Fileship ships with an agent filesystem: external agents connect via WebSocket (`/api/agent/ws`) and expose virtual filesystems. JSON-RPC over WS, binary payloads base64-encoded inline.

```bash
# Agent mode
fileship --agent --server ws://host:8080/api/agent/ws --token <token>
# or: FILESHIP_AGENT=1
```

The agent binary is the same executable — it registers with the server and can e.g. provide transcoding/WebDAV on remote hosts.

## 🎨 Design & Themes

7 built-in themes: Light, Ocean, Violet, Dark, Nord, Solarized, Gruvbox — glass topbar, gradient buttons, row fade-in animations, blurred modal backdrops. i18n in 6 languages (DE/EN/FR/ES/IT/PT).

## ⚡ Performance

### Backend (Rust)
- **Release profile**: `opt-level=3`, `lto="fat"`, `codegen-units=1`, `panic="abort"`, stripped binary (~12 MB)
- **Compression**: gzip + Brotli on static assets (API/downloads stay uncompressed)
- **Async I/O**: `spawn_blocking` for listing/search/indexing; streaming SHA/MD5 hashing (1 MB buffers — no full-file RAM)
- **DB**: rusqlite WAL mode, `prepare_cached` statement cache, `synchronous=NORMAL`, 64 MB page cache
- **JWT**: hand-rolled HS256 (no crypto-stack overhead), 15-min access tokens

### Frontend (Svelte 5 + Vite 8)
- **Lazy components**: Admin, Preview, Settings, Editor load on demand
- **Code splitting**: separate vendor/shared chunks with immutable cache headers
- **~107 KB gzip** initial load (Brotli even less — down from ~130 KB of a single bundle)
- **es2022 target**, manual chunks, svelte-check clean

## 🔒 Security

- bcrypt password hashing
- JWT auth with 15-minute access tokens + 7-day refresh tokens
- TOTP two-factor auth with backup codes
- CSRF protection (double-submit cookie)
- Rate limiting (12/min login, 5/min 2FA)
- Path traversal blocked, symlink escapes rejected
- Security headers (nosniff, sameorigin, XSS filters)

## 🏗️ Building from Source

### Backend (Rust, MSRV 1.89)

```bash
cargo build --release
./target/release/fileship
```

The built binary is fully self-contained (frontend embedded via rust-embed).

### Frontend

```bash
cd frontend
npm install
npm run build    # output → frontend/dist (embedded into the Rust binary)
npm test         # 43 tests (node --test + jsdom)
```

### Rebuild everything

```bash
make build       # frontend build + cargo --release
```

### Run tests

```bash
cargo test --release
```

## 📁 Project Structure

```
src/               Rust backend (axum/tokio)
frontend/          Svelte 5 UI (sources)
frontend/dist/     Built frontend — embedded into the binary
Dockerfile         Multi-stage build (Rust 1.90 + Node 22)
docker-compose.yml Deployment
```

## 🛠️ Built With

- **Backend**: Axum 0.8, Tokio, rusqlite, rustls, serde, tower-http
- **Frontend**: Svelte 5, Vite 8
- **Auth**: JWT HS256, bcrypt, TOTP (RFC 6238)
- **Streaming**: ffmpeg/ffprobe for video transcoding

## 📚 Documentation

- [File Browser Workflows](docs/filebrowser-workflows.md)
- [Interface Design](docs/interface-design.md)

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Run `cargo test` (Rust) and `npm test` (frontend) before submitting

## 📄 License

MIT — see [LICENSE](LICENSE) for details.
