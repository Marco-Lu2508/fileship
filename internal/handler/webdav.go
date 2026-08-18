package handler

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/webdav"

	"github.com/yourname/fileship/internal/auth"
)

func (h *Handler) WebDAV(w http.ResponseWriter, r *http.Request) {
	var root string

	// JWT Bearer Token (z.B. von Browser)
	if bearer := r.Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
		claims, err := auth.ParseAccessToken(h.cfg.JWTSecret, strings.TrimPrefix(bearer, "Bearer "))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := h.db.GetUserByID(claims.UserID)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
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
		h.db.LogAction(user.ID, user.Username, "webdav:"+r.Method, r.URL.Path, r.RemoteAddr)
		root = h.resolveRoot(user.RootPath)
	}

	dav := &webdav.Handler{
		FileSystem: webdav.Dir(root),
		LockSystem: webdav.NewMemLS(),
		Prefix:     "/webdav",
	}
	dav.ServeHTTP(w, r)
}

func (h *Handler) resolveRoot(userRootPath string) string {
	if userRootPath == "/" || userRootPath == "" {
		return h.cfg.RootPath
	}
	return userRootPath
}
