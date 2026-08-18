package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// CSRF — Double Submit Cookie Pattern
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Safe methods brauchen keinen CSRF Check
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			// Token setzen falls noch nicht vorhanden
			if _, err := r.Cookie("csrf_token"); err != nil {
				token := newToken()
				http.SetCookie(w, &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					SameSite: http.SameSiteStrictMode,
					HttpOnly: false, // muss vom JS lesbar sein
				})
			}
			next.ServeHTTP(w, r)
			return
		}

		// API Auth Endpoints sind JWT-geschützt, kein CSRF nötig
		if r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/refresh" || r.URL.Path == "/api/auth/logout" {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie("csrf_token")
		if err != nil {
			http.Error(w, "missing csrf token", http.StatusForbidden)
			return
		}
		header := r.Header.Get("X-CSRF-Token")
		if header == "" || header != cookie.Value {
			http.Error(w, "invalid csrf token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
