package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/yourname/fileship/internal/db"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/model"
)

var validUploadID = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,64}$`)

func (h *Handler) chunkDir() string {
	return filepath.Join(filepath.Dir(h.cfg.DBPath), ".fileship_chunks")
}

// uploadChunkStatus gibt zurück, wie viele Bytes für eine Upload-ID bereits empfangen wurden (Resume-Support).
func (h *Handler) uploadChunkStatus(w http.ResponseWriter, r *http.Request) {
	uploadID := r.URL.Query().Get("upload_id")
	if !validUploadID.MatchString(uploadID) {
		http.Error(w, "invalid upload_id", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	partPath := filepath.Join(h.chunkDir(), strconv.FormatInt(claims.UserID, 10)+"_"+uploadID+".part")
	var received int64
	if info, err := os.Stat(partPath); err == nil && info.Mode().IsRegular() {
		received = info.Size()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"upload_id":      uploadID,
		"received_bytes": received,
	})
}

// uploadChunk verarbeitet einen einzelnen Datei-Chunk und stellt bei Abschluss die Zieldatei atomar fertig.
func (h *Handler) uploadChunk(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uploadID := q.Get("upload_id")
	if !validUploadID.MatchString(uploadID) {
		http.Error(w, "invalid upload_id", http.StatusBadRequest)
		return
	}
	dir := q.Get("path")
	filename := q.Get("filename")
	offset, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
	totalSize, _ := strconv.ParseInt(q.Get("total_size"), 10, 64)
	isFinal := q.Get("done") == "1" || q.Get("done") == "true"

	if filename == "" || offset < 0 || totalSize < 0 {
		http.Error(w, "invalid chunk parameters", http.StatusBadRequest)
		return
	}

	targetRel, err := fsvc.CleanRelativePath(filepath.ToSlash(filepath.Join(dir, filename)))
	if err != nil {
		http.Error(w, "invalid target path", http.StatusBadRequest)
		return
	}

	root := h.userRoot(r)
	targetAbs, err := fsvc.Resolve(root, targetRel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	claims, username := h.claimsUser(r)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return
	}
	effectiveTypes := allowedTypes
	if effectiveTypes == "" {
		effectiveTypes = h.cfg.AllowedTypes
	}
	if !fsvc.TypeAllowed(targetRel, effectiveTypes) {
		http.Error(w, "file type not allowed: "+filepath.Base(targetRel), http.StatusForbidden)
		return
	}

	if !claims.IsAdmin {
		perms, permErr := h.db.ListPathPermissions(claims.UserID)
		if permErr != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		for cur := targetRel; ; {
			if !db.PathPermissionAllowed(perms, cur, "write") {
				http.Error(w, "permission denied for path: "+cur, http.StatusForbidden)
				return
			}
			if cur == "" {
				break
			}
			parent := filepath.ToSlash(filepath.Dir(cur))
			if parent == "." || parent == cur {
				cur = ""
			} else {
				cur = parent
			}
		}
	}

	if quota > 0 {
		usage, sizeErr := fsvc.DirSize(root)
		if sizeErr != nil {
			http.Error(w, "cannot calculate storage usage", http.StatusInternalServerError)
			return
		}
		if info, statErr := os.Stat(targetAbs); statErr == nil && info.Mode().IsRegular() {
			usage -= info.Size()
		}
		if usage > quota || totalSize > quota-usage {
			http.Error(w, "storage quota exceeded", http.StatusForbidden)
			return
		}
	}

	if err := os.MkdirAll(h.chunkDir(), 0755); err != nil {
		http.Error(w, "cannot create chunk directory", http.StatusInternalServerError)
		return
	}
	partPath := filepath.Join(h.chunkDir(), strconv.FormatInt(claims.UserID, 10)+"_"+uploadID+".part")

	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	partFile, err := os.OpenFile(partPath, flags, 0644)
	if err != nil {
		http.Error(w, "cannot open chunk file", http.StatusInternalServerError)
		return
	}
	info, err := partFile.Stat()
	if err != nil {
		partFile.Close()
		http.Error(w, "cannot stat chunk file", http.StatusInternalServerError)
		return
	}
	if offset > info.Size() {
		partFile.Close()
		http.Error(w, "chunk offset beyond current size", http.StatusConflict)
		return
	}
	if _, err := partFile.Seek(offset, io.SeekStart); err != nil {
		partFile.Close()
		http.Error(w, "cannot seek chunk file", http.StatusInternalServerError)
		return
	}

	maxChunk := int64(32 << 20) // 32 MiB max per single chunk request
	written, err := io.Copy(partFile, io.LimitReader(r.Body, maxChunk+1))
	if err != nil || written > maxChunk {
		partFile.Close()
		http.Error(w, "failed to write chunk", http.StatusBadRequest)
		return
	}
	newSize := offset + written
	if err := partFile.Truncate(newSize); err != nil {
		partFile.Close()
		http.Error(w, "failed to finalize chunk", http.StatusInternalServerError)
		return
	}
	if err := partFile.Close(); err != nil {
		http.Error(w, "failed to close chunk", http.StatusInternalServerError)
		return
	}

	if isFinal || (totalSize > 0 && newSize >= totalSize) {
		if totalSize > 0 && newSize != totalSize {
			os.Remove(partPath)
			http.Error(w, "uploaded size mismatch", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
			http.Error(w, "cannot create target directory", http.StatusInternalServerError)
			return
		}
		if err := fsvc.Transfer(partPath, targetAbs); err != nil {
			http.Error(w, "cannot move completed file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h.db.LogAction(claims.UserID, username, "upload", targetRel, r.RemoteAddr)
		h.hub.Broadcast(model.WSEvent{Type: "upload", Path: dir})
		h.indexer.TriggerReindex(claims.UserID, root)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"completed":      true,
			"received_bytes": newSize,
			"path":           targetRel,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"completed":      false,
		"received_bytes": newSize,
	})
}
