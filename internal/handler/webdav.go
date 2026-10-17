package handler

import (
	"context"
	"net/http"
	"os"
	"path"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/webdav"

	"github.com/yourname/fileship/internal/auth"
	fsvc "github.com/yourname/fileship/internal/fs"
)

func (h *Handler) WebDAV(w http.ResponseWriter, r *http.Request) {
	var root string
	var userID int64
	var isAdmin bool

	// JWT Bearer Token (z.B. von Browser)
	if bearer := r.Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
		claims, err := auth.ParseAccessToken(h.cfg.JWTSecret, strings.TrimPrefix(bearer, "Bearer "))
		if err != nil || claims.TwoFAPending {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := h.db.GetUserByID(claims.UserID)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, isAdmin = user.ID, user.IsAdmin
		root = h.resolveRoot(user.RootPath)
	} else {
		// Basic Auth für native Clients (macOS Finder, Windows Explorer, Cyberduck)
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Fileship WebDAV"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := h.db.GetUserByUsername(username)
		if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Fileship WebDAV"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.db.TOTPIsEnabled(user.ID) {
			http.Error(w, "2fa_required: use a completed Bearer login for WebDAV", http.StatusUnauthorized)
			return
		}
		userID, isAdmin = user.ID, user.IsAdmin
		h.db.LogAction(user.ID, user.Username, "webdav:"+r.Method, r.URL.Path, r.RemoteAddr)
		root = h.resolveRoot(user.RootPath)
	}

	// The generic WebDAV handler cannot enforce Fileship's per-path rules,
	// storage quotas, or file-type policy. Refuse the endpoint for accounts
	// with those restrictions rather than allowing a second, unrestricted path
	// to the same workspace.
	if !isAdmin {
		permissions, err := h.db.ListPathPermissions(userID)
		if err != nil {
			http.Error(w, "cannot read account restrictions", http.StatusInternalServerError)
			return
		}
		quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(userID)
		if err != nil {
			http.Error(w, "cannot read account restrictions", http.StatusInternalServerError)
			return
		}
		if len(permissions) > 0 || quota > 0 || allowedTypes != "" || h.cfg.AllowedTypes != "" {
			http.Error(w, "WebDAV is unavailable while path, storage, or file-type restrictions are configured", http.StatusForbidden)
			return
		}
	}

	dav := &webdav.Handler{
		FileSystem: safeWebDAVFileSystem{root: root, base: webdav.Dir(root)},
		LockSystem: webdav.NewMemLS(),
		Prefix:     "/webdav",
	}
	dav.ServeHTTP(w, r)
}

// safeWebDAVFileSystem resolves each operation through the same path and
// symlink checks used by the REST file API.
type safeWebDAVFileSystem struct {
	root string
	base webdav.FileSystem
}

func (fs safeWebDAVFileSystem) clean(name string) (string, error) {
	if strings.ContainsAny(name, "\\\x00") {
		return "", fsvc.ErrForbidden
	}
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "." || name == "" {
		return "", nil
	}
	return fsvc.CleanRelativePath(name)
}

func (fs safeWebDAVFileSystem) validate(name string) (string, error) {
	rel, err := fs.clean(name)
	if err != nil {
		return "", err
	}
	if rel != "" {
		if _, err := fsvc.Resolve(fs.root, rel); err != nil {
			return "", err
		}
	}
	return rel, nil
}

func (fs safeWebDAVFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	rel, err := fs.validate(name)
	if err != nil || rel == "" {
		if err != nil {
			return err
		}
		return os.ErrInvalid
	}
	return fs.base.Mkdir(ctx, rel, perm)
}

func (fs safeWebDAVFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	rel, err := fs.validate(name)
	if err != nil {
		return nil, err
	}
	if rel == "" {
		rel = "/"
	}
	return fs.base.OpenFile(ctx, rel, flag, perm)
}

func (fs safeWebDAVFileSystem) RemoveAll(ctx context.Context, name string) error {
	rel, err := fs.validate(name)
	if err != nil || rel == "" {
		if err != nil {
			return err
		}
		return os.ErrInvalid
	}
	return fs.base.RemoveAll(ctx, rel)
}

func (fs safeWebDAVFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldRel, err := fs.validate(oldName)
	if err != nil {
		return err
	}
	newRel, err := fs.validate(newName)
	if err != nil {
		return err
	}
	if oldRel == "" || newRel == "" {
		return os.ErrInvalid
	}
	return fs.base.Rename(ctx, oldRel, newRel)
}

func (fs safeWebDAVFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	rel, err := fs.validate(name)
	if err != nil {
		return nil, err
	}
	if rel == "" {
		rel = "/"
	}
	return fs.base.Stat(ctx, rel)
}

func (h *Handler) resolveRoot(userRootPath string) string {
	if userRootPath == "/" || userRootPath == "" {
		return h.cfg.RootPath
	}
	return userRootPath
}
