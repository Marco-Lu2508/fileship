package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/db"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// --- Search ---

func (h *Handler) searchFiles(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)

	// Index-Suche bevorzugen
	results, err := h.db.IndexSearch(claims.UserID, query, 200)
	if err != nil || len(results) == 0 {
		// Fallback: direktes Filesystem-Walk
		results, err = fsvc.Search(h.userRoot(r), query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	var perms []model.PathPermission
	if !claims.IsAdmin {
		var permErr error
		perms, permErr = h.db.ListPathPermissions(claims.UserID)
		if permErr != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
	}
	visible := make([]model.FileInfo, 0, len(results))
	for _, file := range results {
		if !claims.IsAdmin && !db.PathPermissionAllowed(perms, file.Path, "read") {
			continue
		}
		visible = append(visible, file)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(visible)
}

// reindex startet einen manuellen Neu-Index
func (h *Handler) reindex(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.claimsUser(r)
	root := h.userRoot(r)
	h.indexer.TriggerReindex(claims.UserID, root)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "indexing"})
}

// --- ZIP Multi-Download ---

func (h *Handler) zipMulti(w http.ResponseWriter, r *http.Request) {
	h.downloadArchive(w, r)
}

// --- Move ---

func (h *Handler) moveFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.checkPerm(w, r, body.Src, "write") {
		return
	}
	if !h.checkPerm(w, r, body.Dst, "write") {
		return
	}
	root := h.userRoot(r)
	if !h.checkTransfer(w, r, body.Src, body.Dst, true) {
		return
	}
	if err := fsvc.Move(root, body.Src, body.Dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, username := h.claimsUser(r)
	h.db.IndexRename(claims.UserID, body.Src, body.Dst)
	h.db.LogAction(claims.UserID, username, "move", body.Src+"->"+body.Dst, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "move", Path: body.Dst})
	w.WriteHeader(http.StatusNoContent)
}

// --- Copy ---

func (h *Handler) copyFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.checkPerm(w, r, body.Src, "read") {
		return
	}
	if !h.checkPerm(w, r, body.Dst, "write") {
		return
	}
	root := h.userRoot(r)
	if !h.checkTransfer(w, r, body.Src, body.Dst, false) {
		return
	}
	if err := fsvc.Copy(root, body.Src, body.Dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, username := h.claimsUser(r)
	h.indexer.TriggerReindex(claims.UserID, root)
	h.db.LogAction(claims.UserID, username, "copy", body.Src+"->"+body.Dst, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "copy", Path: body.Dst})
	w.WriteHeader(http.StatusCreated)
}

// --- Share Links ---

func (h *Handler) createShare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path          string `json:"path"`
		IsDir         bool   `json:"is_dir"`
		ExpiresIn     *int   `json:"expires_in_hours,omitempty"`
		Password      string `json:"password,omitempty"`
		DownloadLimit int    `json:"download_limit,omitempty"`
		AllowUpload   bool   `json:"allow_upload,omitempty"`
		AllowEdit     bool   `json:"allow_edit,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cleanPath, err := fsvc.CleanRelativePath(body.Path)
	if err != nil {
		http.Error(w, "invalid share path", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	root := h.userRoot(r)
	abs, err := fsvc.Resolve(root, cleanPath)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		http.Error(w, "invalid share target", http.StatusBadRequest)
		return
	}
	if !claims.IsAdmin {
		perms, err := h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		if !db.PathPermissionAllowed(perms, cleanPath, "read") || !db.PathPermissionAllowed(perms, cleanPath, "share") {
			http.Error(w, "permission denied for path: "+cleanPath, http.StatusForbidden)
			return
		}
		if info.IsDir() {
			entries, err := fsvc.PrepareArchive(root, []string{cleanPath})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for _, entry := range entries {
				if !db.PathPermissionAllowed(perms, entry.Name, "read") || !db.PathPermissionAllowed(perms, entry.Name, "share") {
					http.Error(w, "permission denied for path: "+entry.Name, http.StatusForbidden)
					return
				}
			}
		}
	}
	var expiresAt *time.Time
	if body.ExpiresIn != nil {
		t := time.Now().Add(time.Duration(*body.ExpiresIn) * time.Hour)
		expiresAt = &t
	}

	// Passwort hashen wenn angegeben
	passwordHash := ""
	if body.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		passwordHash = string(hash)
	}

	token, err := h.db.CreateShareLink(cleanPath, info.IsDir(), claims.UserID, expiresAt, passwordHash, body.DownloadLimit, body.AllowUpload, body.AllowEdit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (h *Handler) listShares(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	links, err := h.db.ListShareLinks(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// HasPassword befüllen
	for i := range links {
		links[i].HasPassword = links[i].PasswordHash != ""
	}
	if links == nil {
		links = []model.ShareLink{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

func (h *Handler) deleteShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	if err := h.db.DeleteShareLink(token, claims.UserID); err != nil {
		http.Error(w, "could not revoke share", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Public share download & interactive folder portal — kein Auth nötig
func (h *Handler) PublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := h.db.GetShareLink(token)
	if err != nil || share == nil {
		http.Error(w, "share not found or expired", http.StatusNotFound)
		return
	}

	pw := r.URL.Query().Get("pw")
	if share.PasswordHash != "" {
		if pw == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(sharePasswordPage(token)))
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(pw)) != nil {
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
	}

	user, err := h.db.GetUserByID(share.CreatedBy)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	root := h.resolveRoot(user.RootPath)
	var perms []model.PathPermission
	if !user.IsAdmin {
		perms, err = h.db.ListPathPermissions(user.ID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		if !db.PathPermissionAllowed(perms, share.Path, "read") || !db.PathPermissionAllowed(perms, share.Path, "share") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	if share.IsDir {
		// Einzelne Datei aus dem geteilten Ordner herunterladen / anzeigen
		if subFile := r.URL.Query().Get("file"); subFile != "" {
			cleanSub, cleanErr := fsvc.CleanRelativePath(subFile)
			if cleanErr != nil {
				http.Error(w, "invalid file path", http.StatusBadRequest)
				return
			}
			targetRel := filepath.ToSlash(filepath.Join(share.Path, cleanSub))
			if !user.IsAdmin && (!db.PathPermissionAllowed(perms, targetRel, "read") || !db.PathPermissionAllowed(perms, targetRel, "share")) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			abs, resolveErr := fsvc.Resolve(root, targetRel)
			if resolveErr != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			info, statErr := os.Stat(abs)
			if statErr != nil || !info.Mode().IsRegular() {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if !h.db.ConsumeShareDownload(token) {
				http.Error(w, "share not found or expired", http.StatusNotFound)
				return
			}
			mime, _ := fsvc.DetectMime(root, targetRel)
			w.Header().Set("Content-Type", mime)
			if r.URL.Query().Get("inline") == "1" {
				w.Header().Set("Content-Disposition", "inline")
			} else {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Name()))
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'unsafe-inline'; sandbox")
			http.ServeFile(w, r, abs)
			return
		}

		// Interaktive Ordner-Ansicht im Browser (sofern nicht ?zip=1 angefordert wurde)
		wantsHTML := r.URL.Query().Get("view") == "1" || (r.URL.Query().Get("zip") == "" && strings.Contains(r.Header.Get("Accept"), "text/html"))
		if wantsHTML {
			list, listErr := fsvc.List(root, share.Path)
			if listErr != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			visible := make([]model.FileInfo, 0, len(list))
			for _, f := range list {
				if f.IsDir {
					continue
				}
				if !user.IsAdmin && (!db.PathPermissionAllowed(perms, f.Path, "read") || !db.PathPermissionAllowed(perms, f.Path, "share")) {
					continue
				}
				relToShare, relErr := filepath.Rel(share.Path, f.Path)
				if relErr == nil {
					f.Path = filepath.ToSlash(relToShare)
				}
				visible = append(visible, f)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(shareFolderPage(token, filepath.Base(share.Path), pw, share.AllowUpload, r.URL.Query().Get("uploaded") == "1", visible)))
			return
		}

		entries, err := fsvc.PrepareArchive(root, []string{share.Path})
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if !user.IsAdmin {
			for _, entry := range entries {
				if !db.PathPermissionAllowed(perms, entry.Name, "read") || !db.PathPermissionAllowed(perms, entry.Name, "share") {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
		}
		if !h.db.ConsumeShareDownload(token) {
			http.Error(w, "share not found or expired", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="share.zip"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		fsvc.WriteArchive(entries, "zip", w)
		return
	}

	abs, err := fsvc.Resolve(root, share.Path)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "not a regular file", http.StatusBadRequest)
		return
	}
	if !h.db.ConsumeShareDownload(token) {
		http.Error(w, "share not found or expired", http.StatusNotFound)
		return
	}
	mime, _ := fsvc.DetectMime(root, share.Path)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeFile(w, r, abs)
}

// PublicShareUpload ermöglicht Gästen das Hochladen von Dateien in einen geteilten Ordner ("File-Drop"),
// sofern AllowUpload für den Share aktiviert wurde.
func (h *Handler) PublicShareUpload(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := h.db.GetShareLink(token)
	if err != nil || share == nil || !share.IsDir || !share.AllowUpload {
		http.Error(w, "upload not allowed for this share", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "file too large or invalid form", http.StatusBadRequest)
		return
	}
	pw := r.URL.Query().Get("pw")
	if pw == "" {
		pw = r.FormValue("pw")
	}
	if share.PasswordHash != "" {
		if bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(pw)) != nil {
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
	}

	user, err := h.db.GetUserByID(share.CreatedBy)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	root := h.resolveRoot(user.RootPath)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(user.ID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return
	}
	effectiveTypes := allowedTypes
	if effectiveTypes == "" {
		effectiveTypes = h.cfg.AllowedTypes
	}
	var perms []model.PathPermission
	if !user.IsAdmin {
		perms, err = h.db.ListPathPermissions(user.ID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files provided", http.StatusBadRequest)
		return
	}

	targetRels := make([]string, len(files))
	var incoming int64
	for i, fh := range files {
		cleanName, cleanErr := fsvc.CleanRelativePath(filepath.Base(fh.Filename))
		if cleanErr != nil {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		rel := filepath.ToSlash(filepath.Join(share.Path, cleanName))
		abs, resolveErr := fsvc.Resolve(root, rel)
		if resolveErr != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !user.IsAdmin && !db.PathPermissionAllowed(perms, rel, "write") {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		if !fsvc.TypeAllowed(rel, effectiveTypes) {
			http.Error(w, "file type not allowed", http.StatusForbidden)
			return
		}
		delta := fh.Size
		if info, statErr := os.Stat(abs); statErr == nil && info.Mode().IsRegular() {
			delta -= info.Size()
		}
		incoming += delta
		targetRels[i] = rel
	}

	if quota > 0 {
		usage, sizeErr := fsvc.DirSize(root)
		if sizeErr != nil || usage > quota || incoming > quota-usage {
			http.Error(w, "storage quota exceeded", http.StatusForbidden)
			return
		}
	}

	for i, fh := range files {
		f, openErr := fh.Open()
		if openErr != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}
		saveErr := fsvc.SaveUpload(root, targetRels[i], f)
		f.Close()
		if saveErr != nil {
			http.Error(w, "could not save file", http.StatusInternalServerError)
			return
		}
		h.db.LogAction(user.ID, user.Username, "share_upload", targetRels[i], r.RemoteAddr)
	}
	h.hub.Broadcast(model.WSEvent{Type: "upload", Path: share.Path})
	h.indexer.TriggerReindex(user.ID, root)

	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		redirectURL := "/s/" + url.PathEscape(token) + "?uploaded=1"
		if pw != "" {
			redirectURL += "&pw=" + url.QueryEscape(pw)
		}
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func shareFolderPage(token, folderName, pw string, allowUpload, uploaded bool, files []model.FileInfo) string {
	pwQuery := ""
	if pw != "" {
		pwQuery = "&pw=" + url.QueryEscape(pw)
	}
	var rows strings.Builder
	if len(files) == 0 {
		rows.WriteString(`<div class="empty">This shared folder is currently empty.</div>`)
	} else {
		for _, f := range files {
			fileEsc := html.EscapeString(f.Name)
			fileParam := url.QueryEscape(f.Path)
			dlHref := fmt.Sprintf("/s/%s?file=%s%s", url.PathEscape(token), fileParam, pwQuery)
			prevHref := dlHref + "&inline=1"
			rows.WriteString(fmt.Sprintf(`
				<div class="row">
					<div class="meta">
						<strong>%s</strong>
						<span>%d B · %s</span>
					</div>
					<div class="actions">
						<a class="btn ghost" href="%s" target="_blank" rel="noopener">Preview</a>
						<a class="btn" href="%s">Download</a>
					</div>
				</div>`, fileEsc, f.Size, f.ModTime.Format("2006-01-02 15:04"), prevHref, dlHref))
		}
	}

	uploadBox := ""
	if allowUpload {
		banner := ""
		if uploaded {
			banner = `<div class="banner">✅ Files uploaded successfully!</div>`
		}
		uploadBox = fmt.Sprintf(`
		<div class="card upload-card">
			<h3>📤 File-Drop: Upload files to this folder</h3>
			%s
			<form method="POST" action="/s/%s/upload" enctype="multipart/form-data">
				<input type="hidden" name="pw" value="%s">
				<input type="file" name="files" multiple required>
				<button type="submit" class="btn primary">Upload files</button>
			</form>
		</div>`, banner, html.EscapeString(token), html.EscapeString(pw))
	}

	zipHref := fmt.Sprintf("/s/%s?zip=1%s", url.PathEscape(token), pwQuery)
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Shared Folder · ` + html.EscapeString(folderName) + `</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0f172a;color:#f1f5f9;font-family:system-ui,-apple-system,sans-serif;padding:2rem 1rem}
.wrap{max-width:780px;margin:0 auto;display:flex;flex-direction:column;gap:1.25rem}
.card{background:#1e293b;border:1px solid #334155;border-radius:12px;padding:1.5rem}
.head{display:flex;align-items:center;justify-content:space-between;gap:1rem;flex-wrap:wrap;margin-bottom:1rem;padding-bottom:1rem;border-bottom:1px solid #334155}
h2{font-size:1.2rem;color:#60a5fa}
h3{font-size:1rem;margin-bottom:.75rem;color:#93c5fd}
.list{display:flex;flex-direction:column;gap:.5rem}
.row{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:.75rem 1rem;background:#0f172a;border:1px solid #1e293b;border-radius:8px}
.meta{display:flex;flex-direction:column;gap:.2rem}
.meta span{font-size:.8rem;color:#94a3b8}
.actions{display:flex;gap:.5rem}
.btn{background:#3b82f6;color:#fff;text-decoration:none;padding:.5rem .9rem;border-radius:6px;font-size:.85rem;font-weight:600;border:none;cursor:pointer;display:inline-flex;align-items:center}
.btn.ghost{background:#334155;color:#e2e8f0}
.btn:hover{opacity:.92}
.upload-card form{display:flex;gap:.75rem;align-items:center;flex-wrap:wrap}
input[type=file]{background:#0f172a;border:1px dashed #475569;color:#e2e8f0;padding:.6rem;border-radius:8px;flex:1}
.banner{background:#065f46;color:#d1fae5;padding:.6rem .9rem;border-radius:6px;margin-bottom:.75rem;font-size:.85rem}
.empty{color:#94a3b8;font-size:.9rem;padding:1rem 0}
</style></head><body>
<div class="wrap">
	<div class="card">
		<div class="head">
			<div>
				<h2>📁 ` + html.EscapeString(folderName) + `</h2>
				<small style="color:#94a3b8">Shared via Fileship</small>
			</div>
			<a class="btn" href="` + zipHref + `">⬇ Download All (.ZIP)</a>
		</div>
		<div class="list">` + rows.String() + `</div>
	</div>
	` + uploadBox + `
</div>
</body></html>`
}

// sharePasswordPage liefert ein minimales HTML-Formular für Passwort-geschützte Shares
func sharePasswordPage(token string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Protected Share</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#111827;color:#edf2f7;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh}
.card{background:#1d2a3d;border:1px solid #304158;border-radius:10px;padding:2rem;width:100%;max-width:360px;display:flex;flex-direction:column;gap:1rem}
h2{font-size:1.1rem;color:#5ca7ff}
p{color:#9aabc0;font-size:.85rem}
input{background:#152238;border:1px solid #304158;color:#edf2f7;padding:.6rem .8rem;border-radius:6px;font-size:.9rem;width:100%}
input:focus{outline:none;border-color:#5ca7ff}
button{background:#5ca7ff;border:none;color:#fff;padding:.65rem;border-radius:6px;font-size:.9rem;cursor:pointer;font-weight:600}
button:hover{background:#388be8}
</style>
</head><body>
<div class="card">
  <h2>🔒 Protected Share</h2>
  <p>This link is password-protected. Enter the password to access it.</p>
  <form method="GET">
    <input type="hidden" name="token" value="` + html.EscapeString(token) + `">
    <input type="password" name="pw" placeholder="Password" autofocus required>
    <br><br>
    <button type="submit">Access file</button>
  </form>
</div>
</body></html>`
}

// --- Me / Password ---

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.OldPassword)) != nil {
		http.Error(w, "wrong password", http.StatusUnauthorized)
		return
	}
	if len(body.NewPassword) < 8 {
		http.Error(w, "password too short (min 8 chars)", http.StatusBadRequest)
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	h.db.UpdateUserPassword(claims.UserID, string(hash))
	w.WriteHeader(http.StatusNoContent)
}

// --- Admin: Update User ---

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var body struct {
		Password string `json:"password,omitempty"`
		RootPath string `json:"root_path,omitempty"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Password != "" {
		hash, _ := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		h.db.UpdateUserPassword(id, string(hash))
	}
	if body.RootPath != "" {
		h.db.UpdateUserRootPath(id, body.RootPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Admin: Audit Log ---

func (h *Handler) auditLog(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	entries, err := h.db.GetAuditLog(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// --- Admin: Stats ---

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	users, _ := h.db.ListUsersWithQuota()
	diskUsage, _ := fsvc.DirSize(h.cfg.RootPath)
	// Disk-Usage pro User berechnen
	for i, u := range users {
		root := h.cfg.RootPath
		if u.RootPath != "/" && u.RootPath != "" {
			root = u.RootPath
		}
		users[i].DiskUsage, _ = fsvc.DirSize(root)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"user_count": len(users),
		"disk_usage": diskUsage,
		"root_path":  h.cfg.RootPath,
		"users":      users,
	})
}

// --- Admin: Quota ---

func (h *Handler) setQuota(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var body struct {
		QuotaBytes   int64  `json:"quota_bytes"`
		AllowedTypes string `json:"allowed_types"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	h.db.SetUserQuota(id, body.QuotaBytes)
	h.db.SetUserAllowedTypes(id, body.AllowedTypes)
	w.WriteHeader(http.StatusNoContent)
}
