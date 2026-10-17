package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/agent"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/config"
	"github.com/yourname/fileship/internal/db"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/model"
	"github.com/yourname/fileship/internal/ws"
)

type Handler struct {
	cfg      *config.Config
	db       *db.DB
	hub      *ws.Hub
	indexer  *db.Indexer
	agentHub *agent.Hub
}

func New(cfg *config.Config, database *db.DB, hub *ws.Hub) *Handler {
	ix := db.NewIndexer(database)
	ah := agent.NewHub(database.UpdateAgentHeartbeat)
	return &Handler{cfg: cfg, db: database, hub: hub, indexer: ix, agentHub: ah}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	loginLimiter := middleware.NewRateLimiter(5, 10)
	globalLimiter := middleware.NewRateLimiter(50, 100)
	r.Use(globalLimiter.Middleware)

	r.Post("/api/auth/login", loginLimiter.Middleware(http.HandlerFunc(h.login)).ServeHTTP)
	r.Post("/api/auth/refresh", h.refresh)
	r.Post("/api/auth/logout", h.logout)
	r.Post("/api/auth/2fa/verify", h.twoFAVerify) // Pending-Token → vollständiges Token
	r.Get("/s/{token}", h.PublicShare)
	r.Post("/s/{token}/upload", h.PublicShareUpload)
	r.Get("/ws", h.hub.Handle)
	r.Get("/api/agent/ws", h.agentWS)

	// Auth-Middleware mit API-Token-Support
	authMW := middleware.AuthWithAPIToken(h.cfg.JWTSecret, h.db.ValidateAPIToken)

	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(h.agentRouteMiddleware)

		r.Get("/api/files", h.listFiles)
		r.Get("/api/files/info", h.fileInfo)
		r.Post("/api/files/upload", h.uploadFile)
		r.Get("/api/files/upload/chunk", h.uploadChunkStatus)
		r.Post("/api/files/upload/chunk", h.uploadChunk)
		r.Delete("/api/files", h.deleteFile)
		r.Post("/api/files/mkdir", h.mkdir)
		r.Post("/api/files/touch", h.touch)
		r.Post("/api/files/rename", h.renameFile)
		r.Post("/api/files/copy", h.copyFile)
		r.Get("/api/files/download", h.downloadFile)
		r.Get("/api/files/preview", h.previewFile)
		r.Post("/api/files/zip-multi", h.zipMulti)
		r.Post("/api/files/archive", h.downloadArchive)
		r.Get("/api/files/zip", h.zipDir)
		r.Post("/api/files/move", h.moveFile)
		r.Get("/api/files/search", h.searchFiles)
		r.Post("/api/files/unzip", h.unzipFile)
		r.Get("/api/files/text", h.readText)
		r.Put("/api/files/text", h.writeText)
		r.Get("/api/files/thumb", h.thumbnail)
		r.Post("/api/files/reindex", h.reindex)
		r.Get("/api/i18n", h.i18n)

		r.Get("/api/me", h.me)
		r.Post("/api/me/password", h.changePassword)
		r.Get("/api/me/settings", h.getSettings)

		// API-Tokens
		r.Get("/api/me/tokens", h.listAPITokens)
		r.Post("/api/me/tokens", h.createAPIToken)
		r.Delete("/api/me/tokens/{id}", h.deleteAPIToken)

		// Favoriten
		r.Get("/api/me/favorites", h.listFavorites)
		r.Post("/api/me/favorites", h.addFavorite)
		r.Delete("/api/me/favorites", h.removeFavorite)

		r.Get("/api/shares", h.listShares)
		r.Post("/api/shares", h.createShare)
		r.Delete("/api/shares/{token}", h.deleteShare)

		// Phase 3: Trash, Sources, Agents, 2FA
		r.Get("/api/trash", h.trashList)
		r.Delete("/api/trash/{name}", h.trashDeletePermanent)
		r.Post("/api/trash/{name}/restore", h.trashRestore)
		r.Delete("/api/trash", h.trashEmpty)
		r.Get("/api/me/sources", h.listSources)
		r.Post("/api/me/sources", h.createSource)
		r.Put("/api/me/sources/{id}", h.updateSource)
		r.Delete("/api/me/sources/{id}", h.deleteSource)
		r.Get("/api/me/agents", h.listAgents)
		r.Post("/api/me/agents", h.createAgent)
		r.Delete("/api/me/agents/{id}", h.deleteAgent)
		r.Post("/api/me/2fa/setup", h.twoFASetup)
		r.Post("/api/me/2fa/enable", h.twoFAEnable)
		r.Delete("/api/me/2fa", h.twoFADisable)
		r.Get("/api/me/2fa/status", h.twoFAStatus)

		r.Group(func(r chi.Router) {
			r.Use(middleware.AdminOnly)
			r.Get("/api/users", h.listUsers)
			r.Post("/api/users", h.createUser)
			r.Delete("/api/users/{id}", h.deleteUser)
			r.Patch("/api/users/{id}", h.updateUser)
			r.Put("/api/users/{id}/quota", h.setQuota)
			r.Get("/api/audit", h.auditLog)
			r.Get("/api/stats", h.stats)
			// Pfad-Permissions (Admin verwaltet sie für User)
			r.Get("/api/users/{id}/permissions", h.listPermissions)
			r.Post("/api/users/{id}/permissions", h.setPermission)
			r.Delete("/api/permissions/{pid}", h.deletePermission)
		})
	})

	return r
}

// --- Auth ---

// dummyHash für Timing-Attack-Schutz — wird beim Login verwendet wenn User nicht existiert
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-timing-protection"), bcrypt.DefaultCost)

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	user, err := h.db.GetUserByUsername(req.Username)
	if err != nil {
		// Dummy-Vergleich um Timing-Angriffe zu verhindern
		bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	// 2FA prüfen — wenn aktiv: Pending-Token zurückgeben
	if h.db.TOTPIsEnabled(user.ID) {
		pending, err := auth.GeneratePendingToken(h.cfg.JWTSecret, user.ID, user.IsAdmin)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"requires_2fa":  true,
			"pending_token": pending,
		})
		return
	}

	access, err := auth.GenerateAccessToken(h.cfg.JWTSecret, user.ID, user.IsAdmin)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	refresh, expires, err := auth.GenerateRefreshToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	h.db.SaveRefreshToken(refresh, user.ID, expires)
	json.NewEncoder(w).Encode(model.TokenPair{AccessToken: access, RefreshToken: refresh})
}

// twoFAVerify tauscht ein Pending-Token gegen ein vollständiges Token ein
func (h *Handler) twoFAVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PendingToken string `json:"pending_token"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Pending-Token parsen
	claims, err := auth.ParseAccessToken(h.cfg.JWTSecret, body.PendingToken)
	if err != nil || !claims.TwoFAPending {
		http.Error(w, "invalid pending token", http.StatusUnauthorized)
		return
	}

	// TOTP-Code oder Backup-Code prüfen
	secret, enabled, _ := h.db.TOTPGetSecret(claims.UserID)
	if !enabled {
		http.Error(w, "2FA not enabled", http.StatusBadRequest)
		return
	}

	ok := auth.VerifyTOTP(secret, body.Code)
	if !ok {
		ok = h.db.TOTPUseBackupCode(claims.UserID, body.Code)
	}
	if !ok {
		http.Error(w, "invalid 2FA code", http.StatusUnauthorized)
		return
	}

	// Vollständiges Token ausstellen
	access, err := auth.GenerateAccessToken(h.cfg.JWTSecret, claims.UserID, claims.IsAdmin)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	refresh, expires, err := auth.GenerateRefreshToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	h.db.SaveRefreshToken(refresh, claims.UserID, expires)
	json.NewEncoder(w).Encode(model.TokenPair{AccessToken: access, RefreshToken: refresh})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	userID, err := h.db.GetRefreshToken(body.RefreshToken)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	user, err := h.db.GetUserByID(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}
	h.db.DeleteRefreshToken(body.RefreshToken)
	access, _ := auth.GenerateAccessToken(h.cfg.JWTSecret, user.ID, user.IsAdmin)
	newRefresh, expires, _ := auth.GenerateRefreshToken()
	h.db.SaveRefreshToken(newRefresh, user.ID, expires)
	json.NewEncoder(w).Encode(model.TokenPair{AccessToken: access, RefreshToken: newRefresh})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	h.db.DeleteRefreshToken(body.RefreshToken)
	w.WriteHeader(http.StatusNoContent)
}

// --- Files ---

func (h *Handler) userRoot(r *http.Request) string {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		return h.cfg.RootPath
	}
	baseRoot := h.resolveRoot(user.RootPath)

	rawSrcID := r.Header.Get("X-Source-ID")
	if rawSrcID == "" {
		rawSrcID = r.URL.Query().Get("source_id")
	}
	if rawSrcID != "" {
		if srcID, err := strconv.ParseInt(rawSrcID, 10, 64); err == nil && srcID > 0 {
			if src := h.db.GetSourceByID(claims.UserID, srcID); src != nil {
				if user.IsAdmin && filepath.IsAbs(src.RootPath) {
					return filepath.Clean(src.RootPath)
				}
				if resolved, err := fsvc.Resolve(baseRoot, src.RootPath); err == nil {
					return resolved
				}
			}
		}
	}
	return baseRoot
}

// claimsUser gibt Claims + Username zurück
func (h *Handler) claimsUser(r *http.Request) (*auth.Claims, string) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		return claims, ""
	}
	return claims, user.Username
}

func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !h.checkPerm(w, r, q.Get("path"), "read") {
		return
	}
	claims, _ := h.claimsUser(r)
	var allow func(model.FileInfo) bool
	if !claims.IsAdmin {
		permissions, err := h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		allow = func(file model.FileInfo) bool {
			return db.PathPermissionAllowed(permissions, file.Path, "read")
		}
	}
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	sortAsc := q.Get("sort_asc") != "false"

	opts := model.ListOptions{
		Path:    q.Get("path"),
		Search:  q.Get("search"),
		SortBy:  q.Get("sort_by"),
		SortAsc: sortAsc,
		Page:    page,
		PerPage: perPage,
	}
	result, err := fsvc.ListPagedFiltered(h.userRoot(r), opts, allow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Ordnergröße für Unterordner einfügen (gecacht, non-blocking)
	root := h.userRoot(r)

	// Favoriten-Set für diesen User laden
	starredSet := h.db.FavoritePathSet(claims.UserID)

	for i, f := range result.Files {
		if f.IsDir {
			result.Files[i].DirSize = h.getDirSizeCached(root, f.Path)
		}
		if starredSet[f.Path] {
			result.Files[i].Starred = true
		}
	}

	// Index-Trigger beim ersten Listing (falls Index noch leer)
	if h.db.IndexCountForUser(claims.UserID) == 0 {
		h.indexer.TriggerReindex(claims.UserID, root)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// getDirSizeCached gibt gecachte Ordnergröße zurück oder startet Berechnung im Hintergrund
func (h *Handler) getDirSizeCached(root, relPath string) int64 {
	cacheKey := root + "|" + relPath
	if size, ok := h.db.DirSizeCacheGet(cacheKey, 5*time.Minute); ok {
		return size
	}
	go func() {
		abs, err := fsvc.Resolve(root, relPath)
		if err != nil {
			return
		}
		if size, err := fsvc.DirSize(abs); err == nil {
			h.db.DirSizeCacheSet(cacheKey, size)
		}
	}()
	return 0
}

func (h *Handler) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()
	dir := r.FormValue("path")
	if !h.checkPerm(w, r, dir, "write") {
		return
	}
	root := h.userRoot(r)
	claims, username := h.claimsUser(r)
	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > 100 {
		http.Error(w, "too many files (max 100)", http.StatusBadRequest)
		return
	}

	// Multipart parsers strip directories from filenames; carry folder paths explicitly.
	relativePath := r.FormValue("relative_path")
	if relativePath != "" && len(files) != 1 {
		http.Error(w, "relative_path requires one file", http.StatusBadRequest)
		return
	}

	quota, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return
	}
	var perms []model.PathPermission
	if !claims.IsAdmin {
		perms, err = h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
	}

	// Preflight all files before writing any entry so a rejected batch item
	// never leaves partial uploads behind.
	targetRels := make([]string, len(files))
	var incoming int64
	for i, fh := range files {
		name := fh.Filename
		if relativePath != "" {
			name = relativePath
		}
		safeName, err := fsvc.CleanRelativePath(name)
		if err != nil {
			http.Error(w, "invalid upload path", http.StatusBadRequest)
			return
		}
		rel := filepath.ToSlash(filepath.Join(dir, safeName))
		abs, err := fsvc.Resolve(root, rel)
		if err != nil {
			http.Error(w, "invalid upload path", http.StatusBadRequest)
			return
		}
		if !claims.IsAdmin {
			for p := rel; p != "." && p != ""; p = filepath.ToSlash(filepath.Dir(p)) {
				if !db.PathPermissionAllowed(perms, p, "write") {
					http.Error(w, "permission denied for path: "+p, http.StatusForbidden)
					return
				}
			}
		}
		if allowedTypes != "" && !fsvc.TypeAllowed(safeName, allowedTypes) {
			http.Error(w, "file type not allowed: "+safeName, http.StatusForbidden)
			return
		}
		incoming += fh.Size
		if info, statErr := os.Stat(abs); statErr == nil && info.Mode().IsRegular() {
			incoming -= info.Size()
		}
		targetRels[i] = rel
	}

	if quota > 0 {
		usage, err := fsvc.DirSize(root)
		if err != nil {
			http.Error(w, "cannot calculate storage usage", http.StatusInternalServerError)
			return
		}
		if usage > quota || incoming > quota-usage {
			http.Error(w, "storage quota exceeded", http.StatusForbidden)
			return
		}
	}

	for i, fh := range files {
		rel := targetRels[i]
		f, err := fh.Open()
		if err != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}
		if err := fsvc.SaveUpload(root, rel, f); err != nil {
			f.Close()
			http.Error(w, "could not save file", http.StatusInternalServerError)
			return
		}
		f.Close()
		h.db.LogAction(claims.UserID, username, "upload", rel, r.RemoteAddr)
	}
	h.hub.Broadcast(model.WSEvent{Type: "upload", Path: dir})
	// Index aktualisieren
	h.indexer.TriggerReindex(claims.UserID, root)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	cleanPath, err := fsvc.CleanRelativePath(path)
	if err != nil {
		http.Error(w, "cannot delete workspace root", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	root := h.userRoot(r)
	abs, err := fsvc.Resolve(root, cleanPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !claims.IsAdmin {
		perms, err := h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		if !db.PathPermissionAllowed(perms, cleanPath, "delete") {
			http.Error(w, "permission denied for path: "+cleanPath, http.StatusForbidden)
			return
		}
		if info, statErr := os.Lstat(abs); statErr == nil && info.IsDir() {
			rootAbs, _ := fsvc.Resolve(root, "")
			var denied string
			_ = filepath.Walk(abs, func(current string, _ os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, relErr := filepath.Rel(rootAbs, current)
				if relErr != nil {
					return relErr
				}
				rel = filepath.ToSlash(rel)
				if !db.PathPermissionAllowed(perms, rel, "delete") {
					denied = rel
					return filepath.SkipAll
				}
				return nil
			})
			if denied != "" {
				http.Error(w, "permission denied for path: "+denied, http.StatusForbidden)
				return
			}
		}
	}

	// Soft-Delete: in Papierkorb verschieben (permanent=true überspringt das)
	permanent := r.URL.Query().Get("permanent") == "true"
	if permanent {
		if err := fsvc.Delete(root, cleanPath); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		if err := h.SoftDelete(claims.UserID, root, cleanPath); err != nil {
			http.Error(w, "could not move to trash: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	h.db.IndexDelete(claims.UserID, cleanPath)
	h.hub.Broadcast(model.WSEvent{Type: "delete", Path: cleanPath})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) touch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cleanPath, err := fsvc.CleanRelativePath(body.Path)
	if err != nil {
		http.Error(w, "file path required", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	if !claims.IsAdmin {
		perms, err := h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		for p := cleanPath; p != "."; p = filepath.ToSlash(filepath.Dir(p)) {
			if !db.PathPermissionAllowed(perms, p, "write") {
				http.Error(w, "permission denied for path: "+p, http.StatusForbidden)
				return
			}
		}
	}
	_, allowedTypes, err := h.db.GetUserQuotaAndTypes(claims.UserID)
	if err != nil {
		http.Error(w, "cannot read permissions", http.StatusInternalServerError)
		return
	}
	if allowedTypes != "" && !fsvc.TypeAllowed(cleanPath, allowedTypes) {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return
	}
	root := h.userRoot(r)
	if err := fsvc.CreateFile(root, cleanPath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.indexer.TriggerReindex(claims.UserID, root)
	h.hub.Broadcast(model.WSEvent{Type: "create", Path: cleanPath})
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) mkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cleanPath, err := fsvc.CleanRelativePath(body.Path)
	if err != nil {
		http.Error(w, "folder path required", http.StatusBadRequest)
		return
	}
	claims, _ := h.claimsUser(r)
	if !claims.IsAdmin {
		perms, err := h.db.ListPathPermissions(claims.UserID)
		if err != nil {
			http.Error(w, "cannot read permissions", http.StatusInternalServerError)
			return
		}
		for p := cleanPath; p != "."; p = filepath.ToSlash(filepath.Dir(p)) {
			if !db.PathPermissionAllowed(perms, p, "write") {
				http.Error(w, "permission denied for path: "+p, http.StatusForbidden)
				return
			}
		}
	}
	root := h.userRoot(r)
	if err := fsvc.Mkdir(root, cleanPath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.indexer.TriggerReindex(claims.UserID, root)
	h.hub.Broadcast(model.WSEvent{Type: "mkdir", Path: cleanPath})
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) renameFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if !h.checkPerm(w, r, body.Old, "write") {
		return
	}
	if !h.checkPerm(w, r, body.New, "write") {
		return
	}
	if !h.checkTransfer(w, r, body.Old, body.New, true) {
		return
	}
	claims, _ := h.claimsUser(r)
	if err := fsvc.Rename(h.userRoot(r), body.Old, body.New); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.db.IndexRename(claims.UserID, body.Old, body.New)
	h.hub.Broadcast(model.WSEvent{Type: "rename", Path: body.New})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	rel, err := fsvc.CleanRelativePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "file path required", http.StatusBadRequest)
		return
	}
	if !h.checkPerm(w, r, rel, "read") {
		return
	}
	root := h.userRoot(r)
	abs, err := fsvc.Resolve(root, rel)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "not a regular file", http.StatusBadRequest)
		return
	}
	mime, _ := fsvc.DetectMime(root, rel)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeFile(w, r, abs)
}

func (h *Handler) previewFile(w http.ResponseWriter, r *http.Request) {
	rel, err := fsvc.CleanRelativePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "file path required", http.StatusBadRequest)
		return
	}
	if !h.checkPerm(w, r, rel, "read") {
		return
	}
	root := h.userRoot(r)
	abs, err := fsvc.Resolve(root, rel)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "not a regular file", http.StatusBadRequest)
		return
	}
	mime, _ := fsvc.DetectMime(root, rel)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	http.ServeFile(w, r, abs)
}

func (h *Handler) zipDir(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	h.serveArchive(w, r, []string{path}, "zip")
}

// --- Me ---

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// --- Users (Admin) ---

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.db.ListUsersWithQuota()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
		RootPath string `json:"root_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.RootPath == "" {
		body.RootPath = "/"
	}
	if err := h.db.CreateUser(body.Username, body.Password, body.IsAdmin, body.RootPath); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	// Eigenen Account nicht löschen
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	if claims.UserID == id {
		http.Error(w, "cannot delete your own account", http.StatusBadRequest)
		return
	}
	// Letzten Admin nicht löschen
	var adminCount int
	h.db.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&adminCount)
	if adminCount <= 1 {
		var isAdmin bool
		h.db.QueryRow("SELECT is_admin FROM users WHERE id = ?", id).Scan(&isAdmin)
		if isAdmin {
			http.Error(w, "cannot delete the last admin", http.StatusBadRequest)
			return
		}
	}
	h.db.DeleteUser(id)
	w.WriteHeader(http.StatusNoContent)
}
