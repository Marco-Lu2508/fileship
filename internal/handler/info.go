package handler

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	fsvc "github.com/yourname/fileship/internal/fs"
	"hash"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (h *Handler) fileInfo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	rootAbs, err := fsvc.Resolve(h.userRoot(r), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !h.checkPerm(w, r, path, "read") {
		return
	}
	abs, err := fsvc.Resolve(rootAbs, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	result := struct {
		Name     string    `json:"name"`
		Path     string    `json:"path"`
		Size     int64     `json:"size"`
		IsDir    bool      `json:"is_dir"`
		ModTime  time.Time `json:"mod_time"`
		Mode     string    `json:"mode"`
		Mime     string    `json:"mime_type"`
		Files    int       `json:"files"`
		Folders  int       `json:"folders"`
		Checksum string    `json:"checksum,omitempty"`
		Width    int       `json:"width,omitempty"`
		Height   int       `json:"height,omitempty"`
	}{Name: info.Name(), Path: path, Size: info.Size(), IsDir: info.IsDir(), ModTime: info.ModTime(), Mode: info.Mode().String()}
	if info.IsDir() {
		result.Size = 0
		err = filepath.Walk(abs, func(current string, entry os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := r.Context().Err(); err != nil {
				return err
			}
			if current == abs {
				return nil
			}
			if entry.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			rel, relErr := filepath.Rel(rootAbs, current)
			if relErr != nil {
				return relErr
			}
			claims, _ := h.claimsUser(r)
			if !claims.IsAdmin {
				allowed, permErr := h.db.CheckPermission(claims.UserID, filepath.ToSlash(rel), "read")
				if permErr != nil {
					return permErr
				}
				if !allowed {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if entry.IsDir() {
				result.Folders++
			} else if entry.Mode().IsRegular() {
				result.Files++
				result.Size += entry.Size()
			}
			return nil
		})
	} else {
		if !info.Mode().IsRegular() {
			http.Error(w, "not a regular file", http.StatusBadRequest)
			return
		}
		result.Mime, _ = fsvc.DetectMime(h.userRoot(r), path)
		if file, openErr := os.Open(abs); openErr == nil {
			if config, _, decodeErr := image.DecodeConfig(file); decodeErr == nil {
				result.Width, result.Height = config.Width, config.Height
			}
			file.Close()
		}
		if algorithm := r.URL.Query().Get("checksum"); algorithm != "" {
			var digest hash.Hash
			switch algorithm {
			case "md5":
				digest = md5.New()
			case "sha1":
				digest = sha1.New()
			case "sha256":
				digest = sha256.New()
			case "sha512":
				digest = sha512.New()
			default:
				http.Error(w, "unsupported checksum", http.StatusBadRequest)
				return
			}
			if !info.Mode().IsRegular() {
				http.Error(w, "not a regular file", http.StatusBadRequest)
				return
			}
			file, openErr := os.Open(abs)
			if openErr != nil {
				http.Error(w, openErr.Error(), http.StatusBadRequest)
				return
			}
			defer file.Close()
			_, err = io.Copy(digest, cancelReader{r.Context(), file})
			result.Checksum = hex.EncodeToString(digest.Sum(nil))
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
