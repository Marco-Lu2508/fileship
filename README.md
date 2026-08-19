# Fileship

Self-hosted file management without a separate database server.

Fileship gives teams a fast web workspace for browsing, organizing, previewing, editing, sharing and serving files. It ships as one Docker image with an embedded Svelte frontend and a SQLite database.

![Go](https://img.shields.io/badge/Go-1.23-00ADD8?style=flat-square&logo=go&logoColor=white)
![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00?style=flat-square&logo=svelte&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-2ea44f?style=flat-square)

## What You Get

### File workspace

- Browse folders with breadcrumbs and fast folder navigation
- List view and persistent grid view
- Sort by name, size and modification date
- Instant filtering plus recursive server-side search
- Multi-select with keyboard shortcuts
- Drag and drop files into folders
- Upload individual files or complete folder trees
- Create empty files and folders from the UI
- Rename, copy, move and delete items
- Download files, folders as ZIP, or selected items as one archive
- Extract ZIP archives

### Preview and editing

- Stream images, video and audio directly in the browser
- Preview PDFs in the built-in viewer
- Read common text and source formats
- Edit text files with a line-numbered editor
- Save changes with keyboard-friendly controls
- Download any file, including formats without an inline preview

### Sharing and access

- Public share links with optional expiration
- Multiple users with separate home directories
- Per-user storage quotas
- Per-user allowed MIME type restrictions
- Admin-only user and storage management
- Audit log for file and account activity
- JWT access and refresh sessions
- Automatic live updates through WebSocket
- WebDAV access for Finder, Windows Explorer, Cyberduck and other clients

### Operations

- SQLite storage, no database service required
- Persistent Docker volumes for files and database
- Embedded frontend in the Go binary
- Five built-in themes: Dark, Light, Nord, Solarized and Gruvbox
- Configurable upload size and optional HTTPS
- Minimal Alpine runtime image

## Quick Start With Docker Compose

Docker Compose is the recommended installation. It pulls the published image and stores data in named volumes.

```bash
mkdir fileship
cd fileship
curl -fsSL https://raw.githubusercontent.com/Marco-Lu2508/fileship/main/docker-compose.yml -o docker-compose.yml
```

Create a `.env` file next to `docker-compose.yml`:

```env
ADMIN_PASSWORD=replace-with-a-long-password
JWT_SECRET=replace-with-a-long-random-secret
```

Generate a secret with:

```bash
openssl rand -hex 32
```

Start Fileship:

```bash
docker compose up -d
```

Open [http://localhost:8080](http://localhost:8080) and sign in as `admin` with the configured `ADMIN_PASSWORD`.

Check the service:

```bash
docker compose ps
docker compose logs -f fileship
```

If `ADMIN_PASSWORD` is empty, Fileship generates an initial password and prints it to the container logs. Set the variable before exposing the service.

## Updating

The Compose file uses `pull_policy: always`, so the current `latest` image is pulled automatically:

```bash
docker compose up -d --force-recreate
```

Confirm the new container:

```bash
docker compose ps
docker compose images
```

Your files and database remain in the named volumes during normal updates.

## Backup and Restore

List the volumes:

```bash
docker volume ls | grep fileship
```

Back up the file volume:

```bash
docker run --rm \
  -v fileship-data:/data:ro \
  -v "$PWD":/backup \
  alpine tar czf /backup/fileship-data.tgz -C /data .
```

Back up the SQLite volume:

```bash
docker run --rm \
  -v fileship-db:/app:ro \
  -v "$PWD":/backup \
  alpine tar czf /backup/fileship-db.tgz -C /app .
```

Stop Fileship before restoring a database volume. Restore archives with the same volume mounts and start the service again.

## Compose File

The published Compose setup is intentionally small:

```yaml
services:
  fileship:
    image: ghcr.io/marco-lu2508/fileship:latest
    pull_policy: always
    ports:
      - "8080:8080"
    volumes:
      - fileship-data:/data
      - fileship-db:/app
    environment:
      PORT: 8080
      ROOT_PATH: /data
      DB_PATH: /app/fileship.db
      ADMIN_PASSWORD: ${ADMIN_PASSWORD:-}
      JWT_SECRET: ${JWT_SECRET:-}
      MAX_UPLOAD_MB: ${MAX_UPLOAD_MB:-1024}
    restart: unless-stopped

volumes:
  fileship-data:
  fileship-db:
```

The binary is stored outside `/app`, so the database volume cannot hide a newly pulled application image.

## Configuration

All values are optional unless your deployment requires a specific policy.

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `ROOT_PATH` | `./data` locally, `/data` in Docker | File root served to users |
| `DB_PATH` | `./fileship.db` locally, `/app/fileship.db` in Docker | SQLite database path |
| `ADMIN_PASSWORD` | generated if empty | Initial password for the `admin` account |
| `JWT_SECRET` | generated if empty | Secret used to sign access tokens |
| `MAX_UPLOAD_MB` | `1024` | Maximum request upload size in megabytes |
| `ALLOWED_TYPES` | empty | Optional comma-separated global MIME restrictions |
| `TLS_CERT` | empty | Certificate path; enables HTTPS with `TLS_KEY` |
| `TLS_KEY` | empty | Private key path; used with `TLS_CERT` |

For Docker, set values in `.env` rather than editing the Compose file:

```env
PORT=8080
ROOT_PATH=/data
DB_PATH=/app/fileship.db
MAX_UPLOAD_MB=2048
ALLOWED_TYPES=image/,video/,audio/,application/pdf,text/
JWT_SECRET=your-generated-secret
ADMIN_PASSWORD=your-long-password
```

## WebDAV

The WebDAV endpoint is:

```text
https://your-domain.example/webdav
```

Use the Fileship username and password for authentication.

Examples:

- macOS Finder: **Go -> Connect to Server**
- Windows Explorer: **This PC -> Map network drive**
- Cyberduck: choose **WebDAV (HTTPS)**

Use HTTPS for WebDAV outside a trusted local network.

## Shares

Users can create public links from the file action menu. A share may target a file or folder and can have an expiration in hours. Anyone who receives the link can download the shared content until it expires or an administrator/user removes it.

Treat share links like passwords. Do not publish them in logs, tickets or public pages.

## Administration

Administrators can open `/admin` and manage:

- users and administrator status
- per-user root directories
- storage quotas in megabytes
- allowed MIME types
- total and per-user disk usage
- recent audit activity

The account used for the first login is `admin`. Change its password immediately in Settings.

## API

The API is available below the same host as the web interface. JSON endpoints use an access token returned by the login endpoint.

### Login and token use

```bash
BASE_URL=http://localhost:8080

ACCESS_TOKEN=$(curl -fsS "$BASE_URL/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

curl -fsS "$BASE_URL/api/me" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

Access tokens are short-lived. Store the `refresh_token` returned at login and send it to `/api/auth/refresh` when the access token expires:

```bash
curl -fsS "$BASE_URL/api/auth/refresh" \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"your-refresh-token"}'
```

### File operations

```bash
# List the current folder
curl -fsS "$BASE_URL/api/files?path=" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# List a folder with sorting and pagination
curl -fsS "$BASE_URL/api/files?path=documents&sort_by=name&sort_asc=true&page=1&per_page=100" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Search recursively
curl -fsS "$BASE_URL/api/files/search?q=report" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Download a file
curl -fL "$BASE_URL/api/files/download?path=documents/report.pdf" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -o report.pdf

# Create a folder
curl -fsS "$BASE_URL/api/files/mkdir" \
  -X POST -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' -d '{"path":"documents/2026"}'

# Upload a file
curl -fS "$BASE_URL/api/files/upload" \
  -X POST -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -F 'path=documents' -F 'files=@report.pdf'
```

Mutating browser requests also use the CSRF double-submit token. A script should first make a GET request, read the `csrf_token` cookie, and send the same value in `X-CSRF-Token`.

### Shares and account settings

```bash
# Create a share link; expires_in_hours may be null for no expiry
curl -fsS "$BASE_URL/api/shares" \
  -X POST -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "X-CSRF-Token: $CSRF_TOKEN" -H 'Content-Type: application/json' \
  -d '{"path":"documents/report.pdf","is_dir":false,"expires_in_hours":24}'

# Read storage and account settings
curl -fsS "$BASE_URL/api/me/settings" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

The public share URL returned by the API is `/s/{token}`. Keep tokens private and use short expiry times for sensitive files.

### Admin endpoints

Admin tokens can call:

- `GET /api/users` and `POST /api/users`
- `PATCH /api/users/{id}` and `DELETE /api/users/{id}`
- `PUT /api/users/{id}/quota`
- `GET /api/stats`
- `GET /api/audit?limit=200`

All admin endpoints require both a valid access token and administrator claims.

## Keyboard Shortcuts

In the file workspace:

| Shortcut | Action |
| --- | --- |
| `Ctrl`/`Cmd` + `A` | Select visible items |
| `Ctrl`/`Cmd` + `N` | Open new-folder action |
| `Delete` | Delete selected items |
| `Escape` | Clear selection and close transient actions |
| `R` | Reload the current folder |
| `Ctrl`/`Cmd` + `S` | Save the editor |

## Reverse Proxy and HTTPS

Fileship serves its own web interface. A reverse proxy is only needed for a custom domain, TLS termination, access control or multiple services.

Example Nginx location:

```nginx
server {
    listen 443 ssl;
    server_name files.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

WebSocket forwarding is required for live file updates. Put TLS at the proxy and keep port `8080` bound to localhost when the service must not be directly exposed.

## Security Checklist

Before exposing Fileship beyond a trusted LAN:

1. Set a strong `ADMIN_PASSWORD`.
2. Generate a long random `JWT_SECRET`.
3. Put Fileship behind HTTPS.
4. Restrict the published host port or bind it to localhost behind a proxy.
5. Back up both Docker volumes.
6. Keep share links private and short-lived where possible.
7. Restrict `ALLOWED_TYPES` if the workspace has a controlled file policy.
8. Run the container with only the intended data volume mounted.

## Development

Requirements:

- Go 1.23+
- Node.js 18+
- npm
- GCC/CGO for the SQLite driver

Backend:

```bash
go test ./...
go vet ./...
go run ./cmd/fileship
```

Frontend:

```bash
cd frontend
npm ci
npm run dev
npm run build
```

Build the production binary:

```bash
CGO_ENABLED=1 go build -o fileship ./cmd/fileship
```

Build the Docker image locally:

```bash
docker build -t fileship:local .
docker compose up -d --build
```

The Docker build compiles the Svelte frontend first, copies its `dist` output into `internal/static/dist`, and then embeds it into the Go binary.

## Troubleshooting

### The page still looks old after an update

```bash
docker compose up -d --force-recreate
docker compose images
```

Then hard-refresh the browser with `Ctrl+F5`.

### Login does not work after changing secrets

`ADMIN_PASSWORD` is used when the admin account is first created. Changing the variable later does not overwrite an existing password. Change it in Settings or update the account through the admin panel.

### WebDAV cannot connect

Check that the proxy forwards `/webdav`, that HTTPS certificates are valid, and that the username/password are correct. Do not remove the `/webdav` prefix.

### Video or PDF preview fails

Check the browser console, confirm that the file is readable by the container, and verify that the reverse proxy forwards range requests and authenticated requests. Downloading the file should still work even when a browser cannot render its format.

### Inspect logs

```bash
docker compose logs --tail=200 fileship
```

## Project Layout

```text
cmd/fileship/          Application entrypoint
internal/auth/         Access and refresh token handling
internal/config/       Environment configuration
internal/db/           SQLite database and migrations
internal/fs/           Safe file operations, archives and MIME detection
internal/handler/      HTTP API and WebDAV handlers
internal/middleware/   Authentication, CSRF and rate limiting
internal/static/       Embedded production frontend
internal/ws/           Live update hub
frontend/src/          Svelte application
docker-compose.yml     Recommended deployment
Dockerfile             Multi-stage production image
```

## License

Fileship is marked as MIT-licensed in the project metadata. Add the repository's complete license text before distributing a fork or a packaged derivative.
