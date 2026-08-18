package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/yourname/fileship/internal/auth"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/model"
)
// --- Unzip ---

func (h *Handler) unzipFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ZipPath string `json:"zip_path"`
		DestDir string `json:"dest_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	root := h.userRoot(r)
	if err := fsvc.Unzip(root, body.ZipPath, body.DestDir); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	h.db.LogAction(claims.UserID, "", "unzip", body.ZipPath+"->"+body.DestDir, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "unzip", Path: body.DestDir})
	w.WriteHeader(http.StatusCreated)
}

// --- Text Editor ---

func (h *Handler) readText(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	content, err := fsvc.ReadTextFile(h.userRoot(r), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"content": content})
}

func (h *Handler) writeText(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := fsvc.WriteTextFile(h.userRoot(r), body.Path, body.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	h.db.LogAction(claims.UserID, "", "edit", body.Path, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "edit", Path: body.Path})
	w.WriteHeader(http.StatusNoContent)
}

// --- Thumbnails ---

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	// Token auch aus Query-Parameter akzeptieren (für img src)
	if token := r.URL.Query().Get("token"); token != "" {
		claims, err := auth.ParseAccessToken(h.cfg.JWTSecret, token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = claims
	}

	rel := r.URL.Query().Get("path")
	root := h.userRoot(r)
	thumbDir := thumbDirFromDB(h.cfg.DBPath)

	if thumbName, ok := fsvc.ThumbnailExists(thumbDir, rel); ok {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, filepath.Join(thumbDir, thumbName))
		return
	}

	if !fsvc.IsImage(rel) {
		http.Error(w, "not an image", http.StatusBadRequest)
		return
	}

	thumbName, err := fsvc.GenerateThumbnail(root, rel, thumbDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(thumbDir, thumbName))
}

// --- Settings (User-Einstellungen) ---

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	root := h.userRoot(r)
	diskUsage, _ := fsvc.DirSize(root)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"quota_bytes":   quota,
		"allowed_types": allowedTypes,
		"disk_usage":    diskUsage,
		"root_path":     root,
	})
}

// --- i18n: Sprache aus Accept-Language ---

func (h *Handler) i18n(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		accept := r.Header.Get("Accept-Language")
		if len(accept) >= 2 {
			lang = accept[:2]
		}
	}
	langFile := filepath.Join("./frontend/dist/locales", lang+".json")
	if _, err := os.Stat(langFile); err != nil {
		langFile = "./frontend/dist/locales/en.json"
	}
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, langFile)
}

// thumbDir helper — sauber aus DBPath ableiten
func thumbDirFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	return filepath.Join(dir, ".thumbs")
}

// URL-safe path helper
func decodePath(s string) string {
	p, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return p
}
