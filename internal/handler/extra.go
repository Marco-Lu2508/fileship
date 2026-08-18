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
	results, err := fsvc.Search(h.userRoot(r), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
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
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="fileship-download.zip"`)
	if err := fsvc.ZipMulti(h.userRoot(r), body.Paths, w); err != nil {
		// Header already sent, can't change status
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
	root := h.userRoot(r)
	if err := fsvc.Move(root, body.Src, body.Dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	h.db.LogAction(claims.UserID, "", "move", body.Src+"->"+body.Dst, r.RemoteAddr)
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
	root := h.userRoot(r)
	if err := fsvc.Copy(root, body.Src, body.Dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	h.db.LogAction(claims.UserID, "", "copy", body.Src+"->"+body.Dst, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "copy", Path: body.Dst})
	w.WriteHeader(http.StatusCreated)
}

// --- Share Links ---

func (h *Handler) createShare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path      string  `json:"path"`
		IsDir     bool    `json:"is_dir"`
		ExpiresIn *int    `json:"expires_in_hours,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	var expiresAt *time.Time
	if body.ExpiresIn != nil {
		t := time.Now().Add(time.Duration(*body.ExpiresIn) * time.Hour)
		expiresAt = &t
	}
	token, err := h.db.CreateShareLink(body.Path, body.IsDir, claims.UserID, expiresAt)
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
	user, err := h.db.GetUserByID(share.CreatedBy)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	root := h.cfg.RootPath
	if user.RootPath != "/" && user.RootPath != "" {
		root = user.RootPath
	}
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
