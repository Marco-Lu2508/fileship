# 🚀 Fileship

A fast, modern, self-hosted file browser — built from scratch.

> Spiritual successor to [filebrowser](https://github.com/filebrowser/filebrowser), rewritten with a clean architecture.

## Features

- 📁 Browse, upload, download, rename, delete files
- 🗜️ Download folders as ZIP
- 👥 Multi-user with per-user root paths
- 🔐 JWT authentication with auto-refreshing tokens
- ⚡ Live updates via WebSocket
- 🌙 Dark UI built with Svelte
- 🐳 Single Docker image, no external dependencies
- 💾 SQLite — no database server needed

## Quick Start

### Docker (recommended)

```bash
cp .env.example .env
# Edit .env — set JWT_SECRET and ADMIN_PASSWORD!
docker compose up -d
```

Open http://localhost:8080 — login with `admin` / your password.

### Local Development

```bash
# Backend
make dev-backend

# Frontend (separate terminal)
make dev-frontend
```

### Build

```bash
make build
./fileship
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `JWT_SECRET` | `change-me` | Secret for JWT signing |
| `DB_PATH` | `./fileship.db` | SQLite database path |
| `ROOT_PATH` | `./data` | Root directory for files |
| `ADMIN_PASSWORD` | `admin` | Initial admin password |
| `MAX_UPLOAD_MB` | `1024` | Max upload size in MB |

## API

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/auth/login` | Login |
| `POST` | `/api/auth/refresh` | Refresh tokens |
| `POST` | `/api/auth/logout` | Logout |
| `GET` | `/api/files?path=` | List files |
| `POST` | `/api/files/upload` | Upload files |
| `DELETE` | `/api/files?path=` | Delete file/folder |
| `POST` | `/api/files/mkdir` | Create folder |
| `POST` | `/api/files/rename` | Rename |
| `GET` | `/api/files/download?path=` | Download file |
| `GET` | `/api/files/zip?path=` | Download folder as ZIP |
| `GET` | `/api/users` | List users (admin) |
| `POST` | `/api/users` | Create user (admin) |
| `DELETE` | `/api/users/{id}` | Delete user (admin) |
| `GET` | `/ws` | WebSocket live updates |

## License

MIT
