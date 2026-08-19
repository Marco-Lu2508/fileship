package handler

import (
	"net/http"

	"github.com/yourname/fileship/internal/auth"
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
