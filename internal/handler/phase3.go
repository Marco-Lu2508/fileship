package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/db"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// ── Trash ─────────────────────────────────────────────────────────────────────

const trashRetentionDays = 30

func (h *Handler) trashDir() string {
	return db.TrashDir(h.cfg.DBPath)
}

// SoftDelete verschiebt eine Datei/Ordner in den Papierkorb
func (h *Handler) SoftDelete(userID int64, root, relPath string) error {
	abs, err := fsvc.Resolve(root, relPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}

	trashDir := h.trashDir()
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return err
	}

	trashName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(abs))
	trashPath := filepath.Join(trashDir, trashName)

	if err := os.Rename(abs, trashPath); err != nil {
		return err
	}

	size := info.Size()
	if info.IsDir() {
		size, _ = fsvc.DirSize(trashPath)
	}

	return h.db.TrashPut(userID, relPath, trashName, info.Name(), info.IsDir(), size, trashRetentionDays)
}

func (h *Handler) trashList(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.claimsUser(r)
	// Abgelaufene Einträge bereinigen
	expired, _ := h.db.TrashPurgeExpired(claims.UserID)
	for _, name := range expired {
		os.RemoveAll(filepath.Join(h.trashDir(), name))
	}
	items, err := h.db.TrashList(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []model.TrashItem{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) trashDeletePermanent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	claims, _ := h.claimsUser(r)
	item, err := h.db.TrashGet(claims.UserID, name)
	if err != nil || item == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	os.RemoveAll(filepath.Join(h.trashDir(), item.TrashName))
	h.db.TrashDelete(claims.UserID, name)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) trashRestore(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	claims, _ := h.claimsUser(r)
	item, err := h.db.TrashGet(claims.UserID, name)
	if err != nil || item == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	trashPath := filepath.Join(h.trashDir(), item.TrashName)
	root := h.userRoot(r)

	restorePath, resolveErr := fsvc.Resolve(root, item.OrigPath)
	if resolveErr != nil {
		restorePath = filepath.Join(root, item.Name)
	}

	// Konflikt: Suffix anfügen
	if _, statErr := os.Stat(restorePath); statErr == nil {
		ext := filepath.Ext(restorePath)
		base := strings.TrimSuffix(restorePath, ext)
		restorePath = fmt.Sprintf("%s_restored_%d%s", base, time.Now().Unix(), ext)
	}

	if err := os.MkdirAll(filepath.Dir(restorePath), 0755); err != nil {
		http.Error(w, "could not restore", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(trashPath, restorePath); err != nil {
		http.Error(w, "could not restore: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.db.TrashDelete(claims.UserID, name)
	h.hub.Broadcast(model.WSEvent{Type: "restore", Path: item.OrigPath})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) trashEmpty(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.claimsUser(r)
	items, _ := h.db.TrashList(claims.UserID)
	for _, item := range items {
		os.RemoveAll(filepath.Join(h.trashDir(), item.TrashName))
		h.db.TrashDelete(claims.UserID, item.TrashName)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Sources ───────────────────────────────────────────────────────────────────

func (h *Handler) listSources(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.claimsUser(r)
	sources, err := h.db.ListSources(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sources == nil {
		sources = []model.UserSource{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources)
}

func (h *Handler) createSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		RootPath  string `json:"root_path"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.RootPath == "" {
		http.Error(w, "name and root_path required", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)

	src, err := h.db.CreateSource(claims.UserID, body.Name, body.RootPath, body.IsDefault)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(src)
}

func (h *Handler) updateSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var body struct {
		Name      string `json:"name"`
		RootPath  string `json:"root_path"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	if err := h.db.UpdateSource(claims.UserID, id, body.Name, body.RootPath, body.IsDefault); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	h.db.DeleteSource(claims.UserID, id)
	w.WriteHeader(http.StatusNoContent)
}

// ── 2FA / TOTP ────────────────────────────────────────────────────────────────

func (h *Handler) twoFAStatus(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	enabled := h.db.TOTPIsEnabled(claims.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

func (h *Handler) twoFASetup(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	user, _ := h.db.GetUserByID(claims.UserID)

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := h.db.TOTPSetSecret(claims.UserID, secret); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	uri := auth.TOTPProvisioningURI(secret, user.Username, "Fileship")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"secret":           secret,
		"provisioning_uri": uri,
	})
}

func (h *Handler) twoFAEnable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		http.Error(w, "code required", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)

	secret, _, err := h.db.TOTPGetSecret(claims.UserID)
	if err != nil || secret == "" {
		http.Error(w, "run /api/me/2fa/setup first", http.StatusBadRequest)
		return
	}

	if !auth.VerifyTOTP(secret, body.Code) {
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	}

	backupCodes, err := auth.GenerateBackupCodes()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := h.db.TOTPEnable(claims.UserID, backupCodes); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":      true,
		"backup_codes": backupCodes,
	})
}

func (h *Handler) twoFADisable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		http.Error(w, "password required", http.StatusBadRequest)
		return
	}
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	user, _ := h.db.GetUserByID(claims.UserID)
	if user == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		http.Error(w, "wrong password", http.StatusUnauthorized)
		return
	}

	h.db.TOTPDisable(claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}
