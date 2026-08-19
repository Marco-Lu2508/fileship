# Fileship

A fast, self-hosted file browser. Spiritual successor to [filebrowser](https://github.com/filebrowser/filebrowser).

![Go](https://img.shields.io/badge/Go-1.23-blue) ![Svelte](https://img.shields.io/badge/Svelte-5-orange) ![License](https://img.shields.io/badge/license-MIT-green) ![Docker](https://img.shields.io/badge/docker-ready-blue)

---

## Quick Start

No repository checkout or database setup is required. Docker keeps the files and database in named volumes.

### Docker Compose (recommended)

```bash
mkdir fileship && cd fileship
curl -fsSL https://raw.githubusercontent.com/Marco-Lu2508/fileship/main/docker-compose.yml -o docker-compose.yml
ADMIN_PASSWORD='choose-a-password' docker compose up -d
```

Open **http://localhost:8080** and log in as `admin` with the password from `ADMIN_PASSWORD`.

For a persistent JWT secret, create a `.env` file next to the Compose file:

```env
ADMIN_PASSWORD=choose-a-password
JWT_SECRET=generate-a-long-random-secret
```

Stop or update it with:

```bash
docker compose pull
docker compose up -d --force-recreate
```

The data and database stay in named Docker volumes across updates.

```bash
docker compose logs fileship
```

The application serves its own web UI. A reverse proxy is only needed when adding a custom domain or HTTPS.

### Docker Run

Compose is recommended. For a single-container install, use:

```bash
docker run -d --name fileship --restart unless-stopped -p 8080:8080 \
  -v fileship-data:/data -v fileship-db:/app \
  -e ADMIN_PASSWORD=choose-a-password \
  ghcr.io/marco-lu2508/fileship:latest
```

---

## Features

- 📁 Browse, upload, download, rename, delete files
- 📦 Download folders as ZIP
- 👥 Multi-user with per-user home directories and storage quotas
- 🔗 Public share links (no login required)
- 🖼️ Image, video and audio preview
- ✏️ Built-in text editor
- 🌐 WebDAV support
- 🎨 5 themes: Dark, Light, Nord, Solarized, Gruvbox
- ⚡ Live updates via WebSocket
- 🔐 JWT auth with auto-refresh
- 📋 Audit log
- 💾 SQLite — no database server needed

---

## Docker Compose

```yaml
services:
  fileship:
    image: ghcr.io/marco-lu2508/fileship:latest
    ports:
      - "8080:8080"
    volumes:
      - fileship-data:/data
      - fileship-db:/app
    environment:
      - ADMIN_PASSWORD=your-secure-password
      - JWT_SECRET=your-random-secret-min-32-chars
    restart: unless-stopped

volumes:
  fileship-data:
  fileship-db:
```

---

## Configuration

Edit `.env` before starting:

```env
ADMIN_PASSWORD=your-secure-password
JWT_SECRET=your-random-secret-min-32-chars
MAX_UPLOAD_MB=1024
PORT=8080
```

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `JWT_SECRET` | auto-generated | Secret for JWT signing |
| `ADMIN_PASSWORD` | auto-generated | Initial admin password (printed to logs) |
| `ROOT_PATH` | `/data` | Root directory for files |
| `DB_PATH` | `/app/fileship.db` | SQLite database path |
| `MAX_UPLOAD_MB` | `1024` | Max upload size in MB |
| `TLS_CERT` | — | Path to TLS certificate (enables HTTPS) |
| `TLS_KEY` | — | Path to TLS private key |

---

## Development

```bash
# Backend
go run ./cmd/fileship

# Frontend
cd frontend && npm install && npm run dev
```

---

## License

MIT
