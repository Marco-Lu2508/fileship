package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/yourname/fileship/internal/auth"
)

type contextKey string

const ClaimsKey contextKey = "claims"

func extractToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	if r.Method == http.MethodGet {
		return r.URL.Query().Get("access_token")
	}
	return ""
}

func validateJWT(w http.ResponseWriter, secret, token string) (*auth.Claims, bool) {
	claims, err := auth.ParseAccessToken(secret, token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	// Pending-Token (2FA noch nicht abgeschlossen) darf keine echten Requests machen
	if claims.TwoFAPending {
		http.Error(w, "2fa_required", http.StatusUnauthorized)
		return nil, false
	}
	return claims, true
}

// Auth prüft Bearer JWT (ohne API-Token-Support)
func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			claims, ok := validateJWT(w, secret, token)
			if !ok {
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ClaimsKey, claims)))
		})
	}
}

// AuthWithAPIToken prüft JWT oder lang-lebiges API-Token (fsk_...)
func AuthWithAPIToken(secret string, validateFn func(string) (int64, bool, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// API-Token (fsk_...) hat Vorrang vor JWT
			if strings.HasPrefix(token, "fsk_") {
				userID, isAdmin, err := validateFn(token)
				if err != nil {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				claims := &auth.Claims{UserID: userID, IsAdmin: isAdmin}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ClaimsKey, claims)))
				return
			}

			claims, ok := validateJWT(w, secret, token)
			if !ok {
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ClaimsKey, claims)))
		})
	}
}

func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsKey).(*auth.Claims)
		if !ok || !claims.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
