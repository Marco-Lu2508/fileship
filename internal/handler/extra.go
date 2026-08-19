package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/auth"
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
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
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(body.Paths) > 500 {
		http.Error(w, "too many paths (max 500)", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="fileship-download.zip"`)
	if err := fsvc.ZipMulti(h.userRoot(r), body.Paths, w); err != nil {
		return
	}
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
	root := h.userRoot(r)
	if err := fsvc.Move(root, body.Src, body.Dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, username := h.claimsUser(r)
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
	root := h.userRoot(r)
	if err := fsvc.Copy(root, body.Src, body.Dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, username := h.claimsUser(r)
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
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	// Pfad gegen User-Root validieren
	if _, err := fsvc.Resolve(h.userRoot(r), body.Path); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !h.checkPerm(w, r, body.Path, "share") {
		return
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

	token, err := h.db.CreateShareLink(body.Path, body.IsDir, claims.UserID, expiresAt, passwordHash, body.DownloadLimit, body.AllowUpload, body.AllowEdit)
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
	h.db.DeleteShareLink(token, claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// Public share download — kein Auth nötig
func (h *Handler) PublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := h.db.GetShareLink(token)
	if err != nil || share == nil {
		http.Error(w, "share not found or expired", http.StatusNotFound)
		return
	}

	// Passwort-Check
	if share.PasswordHash != "" {
		pw := r.URL.Query().Get("pw")
		if pw == "" {
			// Passwort-Eingabe-Seite rendern
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
	root := h.cfg.RootPath
	if user.RootPath != "/" && user.RootPath != "" {
		root = user.RootPath
	}

	// Download-Counter erhöhen
	h.db.IncrementShareDownload(token)

	if share.IsDir {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="share.zip"`)
		fsvc.ZipDir(root, share.Path, w)
		return
	}
	abs, err := fsvc.Resolve(root, share.Path)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	mime, _ := fsvc.DetectMime(root, share.Path)
	w.Header().Set("Content-Type", mime)
	http.ServeFile(w, r, abs)
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
    <input type="hidden" name="token" value="` + token + `">
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
