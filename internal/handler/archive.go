package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yourname/fileship/internal/db"
	fsvc "github.com/yourname/fileship/internal/fs"
)

func (h *Handler) downloadArchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths  []string `json:"paths"`
		Format string   `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 || len(body.Paths) > 500 {
		http.Error(w, "invalid archive selection", http.StatusBadRequest)
		return
	}
	h.serveArchive(w, r, body.Paths, body.Format)
}

func (h *Handler) serveArchive(w http.ResponseWriter, r *http.Request, paths []string, format string) {
	if format == "" {
		format = "zip"
	}
	extension, mime := "", ""
	switch format {
	case "zip":
		extension = "zip"
		mime = "application/zip"
	case "tar":
		extension = "tar"
		mime = "application/x-tar"
	case "targz":
		extension = "tar.gz"
		mime = "application/gzip"
	default:
		http.Error(w, "unsupported archive format", http.StatusBadRequest)
		return
	}
	entries, err := fsvc.PrepareArchive(h.userRoot(r), paths)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	if !claims.IsAdmin {
		perms, err := h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		for _, entry := range entries {
			permName := h.permPath(r, entry.Name)
			if !db.PathPermissionAllowed(perms, permName, "read") {
				http.Error(w, "permission denied for path: "+permName, http.StatusForbidden)
				return
			}
		}
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="fileship-download.`+extension+`"`)
	fsvc.WriteArchive(entries, format, w)
}
