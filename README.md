# Fileship

A fast, self-hosted file browser — spiritual successor to [filebrowser](https://github.com/filebrowser/filebrowser).

**Stack**: Go · Svelte 5 · SQLite · JWT · WebSocket · Docker

---

## Quick Start

```bash
docker compose up -d
```

Open **http://localhost:8080** and login with `admin` / `admin`.

> **Change the password immediately** in the admin panel after first login!

---

## Configuration

Create a `.env` file to customize:

```env
ADMIN_PASSWORD=your-secure-password
JWT_SECRET=your-random-secret-min-32-chars
MAX_UPLOAD_MB=1024
PORT=8080
```

Or set environment variables directly in `docker-compose.yml`.

---

## Features

- Browse, upload, download, rename, delete files
- Drag & drop upload with progress
- Download folders as ZIP
- Multi-user with per-user home directories and quotas
- JWT authentication with auto-refresh
- Live updates via WebSocket
- 5 themes: Dark, Light, Nord, Solarized, Gruvbox
- Text editor with syntax highlighting
- Image/video/audio preview
- Share links (public, no login required)
- WebDAV support
- Audit log
- SQLite — no database server needed

---

## Docker Compose

```yaml
services:
  fileship:
    image: ghcr.io/Marco-Lu2508/fileship:latest
    ports:
      - "8080:8080"
    volumes:
      - fileship-data:/data
      - fileship-db:/app
    environment:
      - ADMIN_PASSWORD=admin
      - JWT_SECRET=change-me-in-production
    restart: unless-stopped

volumes:
  fileship-data:
  fileship-db:
```

Or clone the repo and build yourself:

```bash
git clone https://github.com/Marco-Lu2508/fileship.git
cd fileship
docker compose up -d
```

---

## Configuration Reference

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `JWT_SECRET` | random | Secret for JWT signing (auto-generated if not set) |
| `DB_PATH` | `/app/fileship.db` | SQLite database path |
| `ROOT_PATH` | `/data` | Root directory for files |
| `ADMIN_PASSWORD` | random | Initial admin password (printed to logs if not set) |
| `MAX_UPLOAD_MB` | `1024` | Max upload size in MB |
| `TLS_CERT` | — | Path to TLS certificate (enables HTTPS) |
| `TLS_KEY` | — | Path to TLS private key |

---

## Development

```bash
# Backend (Go)
export PATH=$PATH:/usr/local/go/bin
go run ./cmd/fileship

# Frontend (Svelte)
cd frontend
npm install
npm run dev
```

---

## License

MIT
