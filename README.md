# Fileship

Dateimanager fürs Homelab — als **eine einzige Rust-Binary**: Web-Backend, WebDAV, Agent-Server und das eingebettete Svelte-Frontend. Keine Go-Abhängigkeiten, keine externe Runtime außer `ffmpeg` (Transcoding).

## Features

- **Backend in Rust** — Axum 0.8 auf Tokio, SQLite (rusqlite, WAL), JWT/bcrypt/TOTP
- **69/69 Routen-Parität** zum alten Go-Backend (Auth, Dateien, Shares, Trash, Favoriten, Quellen/Agenten, Audit, Stats, Berechtigungen)
- **Svelte-5-Frontend**, eingebettet per `rust-embed` — ein Binary, keine Static-Server
- **WebDAV** (`/webdav`, Basic- oder Bearer-Auth), **Agent-Protocol** (WebSocket JSON-RPC), **Chunk-Upload**, **Live-Transcoding via ffmpeg**
- **2FA (TOTP)** mit Backup-Codes, API-Tokens, Rate-Limiting, CSRF-Double-Submit, Security-Headers
- **Themes**: Light, Ocean, Violet, Dark, Nord, Solarized, Gruvbox
- **i18n**: Deutsch, Englisch, Französisch, Spanisch, Italienisch, Portugiesisch

## Performance

- **Release-Profil**: `opt-level=3`, `lto="fat"`, `codegen-units=1`, `panic="abort"`, `strip=true`
- **Spawn-blocking** für Filesystem-Listen/Suchen/Index — der Async-Runtime bleibt frei
- **Statement-Caching** in SQLite (`prepare_cached`), WAL + `synchronous=NORMAL` + 64 MB Page-Cache
- **Streaming-Hash** (Checksummen) und Streaming-Downloads — keine Datei liegt komplett im RAM
- **Compression**: gzip + Brotli auf den Static-Assets (API/Uploads bleiben unkomprimiert)
- **Cache**: hashbasierte Assets mit `immutable`, `index.html` mit `no-cache`
- **Frontend**: 230 KB Initial-Bundle + Vendor-Chunk (separat cachebar), Admin/Preview/Settings/Editor werden lazy geladen (~67 KB nur auf Bedarf)

## Bauen

```bash
# Frontend bauen (generiert frontend/dist, wird ins Binary eingebettet)
cd frontend && npm ci && npm run build

# Rust-Backend bauen
cargo build --release
./target/release/fileship

# Tests
cargo test --release --all-targets
cd frontend && npm test && npx svelte-check
```

## Docker

```bash
docker build -t fileship .
docker run --rm -p 8080:8080 \
  -v fileship-data:/data \
  -e JWT_SECRET=dein-geheimnis \
  -e ADMIN_PASSWORD=dein-passwort \
  fileship
```

Oder mit Compose (zieht das Image aus `ghcr.io/marco-lu2508/fileship:latest`, das die CI bei jedem Push auf `main` baut):

```bash
docker compose up -d
```

## Konfiguration (Env-Variablen)

| Variable | Default | Beschreibung |
|---|---|---|
| `PORT` | `8080` | HTTP-Port |
| `ROOT_PATH` | `./data` | Dateien-Root |
| `DB_PATH` | `./fileship.db` | SQLite-Datei |
| `JWT_SECRET` | *ephemeral* | JWT-Signing-Key — **unbedingt setzen**, sonst sind Sessions nach jedem Neustart weg |
| `ADMIN_PASSWORD` | — | Initial-Admin (`admin`), wird nur beim allerersten Start verwendet |
| `MAX_UPLOAD_MB` | `1024` | Max. Upload-Größe |
| `TLS_CERT` / `TLS_KEY` | — | Optionales TLS (rustls) |

## Agent-Modus

```bash
./fileship --agent --server ws://host:8080/api/agent/ws --token <token>
# oder: FILESHIP_AGENT=1
```

Der Agent registriert sich am Server und kann z. B. Transcodierung/WebDAV auf Remote-Hosts bereitstellen.

## API (Auswahl)

- `POST /api/auth/login` · `/api/auth/refresh` · `/api/auth/2fa/verify`
- `GET/POST/DELETE /api/files/*` (List, Upload, Download, Mkdir, Move, Copy, Delete, Info, Search)
- `POST /api/files/upload/chunk` (Chunk-Upload, 16 MB-Chunks)
- `/api/shares/*`, `/api/trash/*`, `/api/favorites/*`, `/api/agents/*`, `/api/audit/*`, `/api/stats/*`
- `/s/{token}` — öffentliche Share-Seite
- `/webdav/*` — WebDAV
- `/ws` — Live-Updates (JWT im Upgrade-Token)

## Lizenz / Status

Homelab-Self-Hosting-Projekt. Backend komplett in Rust (Code-Coverage der Routen: 69/69 gegen das Go-Original verifiziert).
