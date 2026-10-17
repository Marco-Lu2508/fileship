package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/yourname/fileship/internal/db"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/model"
	"github.com/yourname/fileship/internal/static"
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
	if !h.checkPerm(w, r, body.ZipPath, "read") || !h.checkPerm(w, r, body.DestDir, "write") {
		return
	}
	plan, err := fsvc.PrepareUnzip(root, body.ZipPath, body.DestDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer plan.Close()
	claims, username := h.claimsUser(r)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return
	}
	var perms []model.PathPermission
	if !claims.IsAdmin {
		perms, err = h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
	}
	var incoming int64
	for _, entry := range plan.Entries {
		// Implicit parents also need write access. A rule on a directory must
		// not be bypassed by selecting only a deeper child in the ZIP.
		if !claims.IsAdmin {
			for path := entry.Path; path != "."; path = filepath.ToSlash(filepath.Dir(path)) {
				if !db.PathPermissionAllowed(perms, path, "write") {
					http.Error(w, "permission denied for path: "+path, http.StatusForbidden)
					return
				}
			}
		}
		if !entry.IsDir {
			if !fsvc.TypeAllowed(entry.Path, allowedTypes) {
				http.Error(w, "file type not allowed: "+entry.Path, http.StatusForbidden)
				return
			}
			incoming += entry.Size
		}
	}
	if quota > 0 {
		usage, err := fsvc.DirSize(root)
		if err != nil {
			http.Error(w, "cannot calculate storage usage", http.StatusInternalServerError)
			return
		}
		if usage > quota || incoming > quota-usage {
			http.Error(w, "storage quota exceeded", http.StatusForbidden)
			return
		}
	}
	if err := plan.Extract(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.db.LogAction(claims.UserID, username, "unzip", body.ZipPath+"->"+body.DestDir, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "unzip", Path: body.DestDir})
	h.indexer.TriggerReindex(claims.UserID, root)
	w.WriteHeader(http.StatusCreated)
}

// --- Text Editor ---

func (h *Handler) readText(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if !h.checkPerm(w, r, path, "read") {
		return
	}
	content, err := fsvc.ReadTextFile(h.userRoot(r), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"content": content})
}

func (h *Handler) writeText(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 7<<20)
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.checkPerm(w, r, body.Path, "write") {
		return
	}
	if len(body.Content) > 1<<20 {
		http.Error(w, "file too large for editor (max 1MB)", http.StatusRequestEntityTooLarge)
		return
	}
	claims, username := h.claimsUser(r)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return
	}
	if !fsvc.TypeAllowed(body.Path, allowedTypes) {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return
	}
	root := h.userRoot(r)
	abs, err := fsvc.Resolve(root, body.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if quota > 0 {
		usage, err := fsvc.DirSize(root)
		if err != nil {
			http.Error(w, "cannot calculate storage usage", http.StatusInternalServerError)
			return
		}
		if info, err := os.Stat(abs); err == nil && info.Mode().IsRegular() {
			usage -= info.Size()
		} else if err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if usage > quota || int64(len(body.Content)) > quota-usage {
			http.Error(w, "storage quota exceeded", http.StatusForbidden)
			return
		}
	}
	if err := fsvc.WriteTextFile(root, body.Path, body.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.db.LogAction(claims.UserID, username, "edit", body.Path, r.RemoteAddr)
	h.hub.Broadcast(model.WSEvent{Type: "edit", Path: body.Path})
	h.indexer.TriggerReindex(claims.UserID, root)
	w.WriteHeader(http.StatusNoContent)
}

// --- Thumbnails ---

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if !h.checkPerm(w, r, rel, "read") {
		return
	}
	root := h.userRoot(r)
	thumbDir := thumbDirFromDB(h.cfg.DBPath)

	if thumbName, ok := fsvc.ThumbnailExists(root, thumbDir, rel); ok {
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
