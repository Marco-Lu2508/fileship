package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"

	"github.com/yourname/fileship/internal/static"

	fsvc "github.com/yourname/fileship/internal/fs"
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
	claims, username := h.claimsUser(r)
	h.db.LogAction(claims.UserID, username, "unzip", body.ZipPath+"->"+body.DestDir, r.RemoteAddr)
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
	claims, username := h.claimsUser(r)
	h.db.LogAction(claims.UserID, username, "edit", body.Path, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "edit", Path: body.Path})
	w.WriteHeader(http.StatusNoContent)
}

// --- Thumbnails ---

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	root := h.userRoot(r)
	thumbDir := thumbDirFromDB(h.cfg.DBPath)

	if thumbName, ok := fsvc.ThumbnailExists(thumbDir, rel); ok {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, filepath.Join(thumbDir, thumbName))
		return
	}

	// Bild-Thumbnail
	if fsvc.IsImage(rel) {
		thumbName, err := fsvc.GenerateThumbnail(root, rel, thumbDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, filepath.Join(thumbDir, thumbName))
		return
	}

	// Video-Thumbnail (optional, nur wenn ffmpeg verfügbar)
	if fsvc.IsVideo(rel) {
		thumbName, err := fsvc.GenerateVideoThumbnail(root, rel, thumbDir)
		if err != nil {
			// ffmpeg nicht verfügbar oder fehlgeschlagen — 404
			http.Error(w, "thumbnail not available", http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, filepath.Join(thumbDir, thumbName))
		return
	}

	http.Error(w, "not an image or video", http.StatusBadRequest)
}

// --- Settings (User-Einstellungen) ---

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.claimsUser(r)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	root := h.userRoot(r)

	// DirSize mit Timeout — nicht blockieren wenn root sehr groß ist
	sizeCh := make(chan int64, 1)
	go func() {
		s, _ := fsvc.DirSize(root)
		sizeCh <- s
	}()

	var diskUsage int64
	select {
	case diskUsage = <-sizeCh:
	case <-r.Context().Done():
		diskUsage = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"quota_bytes":   quota,
		"allowed_types": allowedTypes,
		"disk_usage":    diskUsage,
		"root_path":     root,
	})
}

// --- i18n: Sprache aus Accept-Language ---

// validLang erlaubt nur 2-Buchstaben Sprach-Codes
var validLang = regexp.MustCompile(`^[a-z]{2}$`)

func (h *Handler) i18n(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		accept := r.Header.Get("Accept-Language")
		if len(accept) >= 2 {
			lang = accept[:2]
		}
	}
	// Whitelist: nur a-z, genau 2 Zeichen
	if !validLang.MatchString(lang) {
		lang = "en"
	}
	content, err := static.LocaleFile(lang)
	if err != nil {
		content, err = static.LocaleFile("en")
	}
	if err != nil {
		http.Error(w, "translation unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(content)
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
