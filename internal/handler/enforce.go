package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/yourname/fileship/internal/auth"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/middleware"
)

// checkPerm prüft eine Permission und schreibt 403 wenn verweigert.
// Gibt true zurück wenn die Operation erlaubt ist.
func (h *Handler) checkPerm(w http.ResponseWriter, r *http.Request, path, action string) bool {
	claims, ok := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	// Admins sind immer erlaubt
	if claims.IsAdmin {
		return true
	}
	allowed, err := h.db.CheckPermission(claims.UserID, path, action)
	if err != nil || !allowed {
		http.Error(w, "permission denied for path: "+path, http.StatusForbidden)
		return false
	}
	return true
}

// Validate every source and target in a selected tree, including more specific
// permission rules, MIME restrictions and the storage needed by a copy.
func (h *Handler) checkTransfer(w http.ResponseWriter, r *http.Request, src, dst string, moving bool) bool {
	root := h.userRoot(r)
	source, err := fsvc.Resolve(root, src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	if _, err = fsvc.Resolve(root, dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	claims, _ := h.claimsUser(r)
	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return false
	}
	var size int64
	err = filepath.Walk(source, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := r.Context().Err(); err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("cannot transfer non-regular file")
		}
		suffix, _ := filepath.Rel(source, current)
		original := filepath.ToSlash(filepath.Join(src, suffix))
		target := filepath.ToSlash(filepath.Join(dst, suffix))
		if !claims.IsAdmin {
			for _, check := range []struct{ path, action string }{{original, "read"}, {target, "write"}} {
				allowed, err := h.db.CheckPermission(claims.UserID, check.path, check.action)
				if err != nil {
					return err
				}
				if !allowed {
					return fmt.Errorf("permission denied for path: %s", check.path)
				}
			}
			if moving {
				allowed, err := h.db.CheckPermission(claims.UserID, original, "write")
				if err != nil {
					return err
				}
				if !allowed {
					return fmt.Errorf("permission denied for path: %s", original)
				}
			}
		}
		if info.Mode().IsRegular() {
			size += info.Size()
			if allowedTypes != "" && !fsvc.TypeAllowed(target, allowedTypes) {
				return fmt.Errorf("file type not allowed: %s", target)
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return false
	}
	if !moving && quota > 0 {
		usage, err := fsvc.DirSize(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return false
		}
		if usage > quota || size > quota-usage {
			http.Error(w, "storage quota exceeded", http.StatusForbidden)
			return false
		}
	}
	return true
}
