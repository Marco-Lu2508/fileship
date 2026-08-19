package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/model"
)

// ── API Tokens ────────────────────────────────────

func (h *Handler) listAPITokens(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	tokens, err := h.db.ListAPITokens(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tokens == nil {
		tokens = []model.APIToken{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokens)
}

func (h *Handler) createAPIToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		ExpiresIn *int   `json:"expires_in_days,omitempty"` // 0 oder nil = kein Ablauf
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)

	var expiresAt *time.Time
	if body.ExpiresIn != nil && *body.ExpiresIn > 0 {
		t := time.Now().AddDate(0, 0, *body.ExpiresIn)
		expiresAt = &t
	}

	plain, tok, err := h.db.CreateAPIToken(claims.UserID, body.Name, expiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tok.PlainToken = plain // einmalig zurückgeben

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tok)
}

func (h *Handler) deleteAPIToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	h.db.DeleteAPIToken(id, claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// ── Favorites ─────────────────────────────────────

func (h *Handler) listFavorites(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	favs, err := h.db.ListFavorites(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if favs == nil {
		favs = []model.Favorite{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(favs)
}

func (h *Handler) addFavorite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	if err := h.db.AddFavorite(claims.UserID, body.Path, body.Name, body.IsDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) removeFavorite(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	h.db.RemoveFavorite(claims.UserID, path)
	w.WriteHeader(http.StatusNoContent)
}

// ── Path Permissions (Admin only) ─────────────────

func (h *Handler) listPermissions(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	perms, err := h.db.ListPathPermissions(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if perms == nil {
		perms = []model.PathPermission{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(perms)
}

func (h *Handler) setPermission(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var p model.PathPermission
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	p.UserID = userID
	if err := h.db.SetPathPermission(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) deletePermission(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(chi.URLParam(r, "pid"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	h.db.DeletePathPermission(pid)
	w.WriteHeader(http.StatusNoContent)
}
