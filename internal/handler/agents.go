package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/yourname/fileship/internal/agent"
	"github.com/yourname/fileship/internal/model"
)

var agentUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// agentWS akzeptiert eingehende WebSocket-Verbindungen von entfernten Fileship-Agenten.
func (h *Handler) agentWS(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if !strings.HasPrefix(token, "fsa_") {
		http.Error(w, "unauthorized agent token", http.StatusUnauthorized)
		return
	}
	dev, err := h.db.ValidateAgentToken(token)
	if err != nil || dev == nil {
		http.Error(w, "invalid agent token", http.StatusUnauthorized)
		return
	}
	conn, err := agentUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.hub.Broadcast(model.WSEvent{Type: "agent_status", Path: strconv.FormatInt(dev.ID, 10)})
	h.agentHub.ServeSession(dev.ID, conn)
	h.hub.Broadcast(model.WSEvent{Type: "agent_status", Path: strconv.FormatInt(dev.ID, 10)})
}

func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.claimsUser(r)
	list, err := h.db.ListAgentDevices(claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []model.AgentDevice{}
	}
	for i := range list {
		list[i].Online = h.agentHub.IsOnline(list[i].ID)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) createAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		RootPath string `json:"root_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "device name required", http.StatusBadRequest)
		return
	}
	claims, username := h.claimsUser(r)
	dev, err := h.db.CreateAgentDevice(claims.UserID, strings.TrimSpace(body.Name), strings.TrimSpace(body.RootPath))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.db.LogAction(claims.UserID, username, "agent_create", dev.Name, r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dev)
}

func (h *Handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	claims, username := h.claimsUser(r)
	h.agentHub.Disconnect(id)
	if err := h.db.DeleteAgentDevice(claims.UserID, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.db.LogAction(claims.UserID, username, "agent_delete", strconv.FormatInt(id, 10), r.RemoteAddr)
	w.WriteHeader(http.StatusNoContent)
}

// requestAgentID liest optional die Ziel-Agent-ID aus Header oder Query.
func requestAgentID(r *http.Request) int64 {
	raw := r.Header.Get("X-Agent-ID")
	if raw == "" {
		raw = r.URL.Query().Get("agent_id")
	}
	if raw == "" {
		return 0
	}
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

// agentRouteMiddleware leitet /api/files* Anfragen an einen verbundenen Remote-Agenten weiter,
// sobald X-Agent-ID oder ?agent_id= gesetzt ist.
func (h *Handler) agentRouteMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentID := requestAgentID(r)
		if agentID <= 0 || !strings.HasPrefix(r.URL.Path, "/api/files") {
			next.ServeHTTP(w, r)
			return
		}
		claims, _ := h.claimsUser(r)
		dev, err := h.db.GetAgentDeviceByID(claims.UserID, agentID)
		if err != nil || dev == nil {
			http.Error(w, "agent device not found", http.StatusNotFound)
			return
		}
		if !h.agentHub.IsOnline(agentID) {
			http.Error(w, "remote device is offline", http.StatusServiceUnavailable)
			return
		}
		h.serveAgentFileRequest(w, r, agentID)
	})
}

func (h *Handler) serveAgentFileRequest(w http.ResponseWriter, r *http.Request, agentID int64) {
	ctx := r.Context()
	q := r.URL.Query()
	path := r.URL.Path

	switch {
	case path == "/api/files" && r.Method == http.MethodGet:
		res, err := h.agentHub.Request(ctx, agentID, "list", map[string]string{
			"path":     q.Get("path"),
			"search":   q.Get("search"),
			"sort_by":  q.Get("sort_by"),
			"sort_asc": q.Get("sort_asc"),
			"page":     q.Get("page"),
			"per_page": q.Get("per_page"),
		}, nil)
		writeAgentJSON(w, res, err)

	case path == "/api/files" && r.Method == http.MethodDelete:
		res, err := h.agentHub.Request(ctx, agentID, "delete", map[string]string{"path": q.Get("path")}, nil)
		writeAgentEmpty(w, res, err)

	case path == "/api/files/info" && r.Method == http.MethodGet:
		res, err := h.agentHub.Request(ctx, agentID, "info", map[string]string{"path": q.Get("path")}, nil)
		writeAgentJSON(w, res, err)

	case path == "/api/files/search" && r.Method == http.MethodGet:
		res, err := h.agentHub.Request(ctx, agentID, "search", map[string]string{"q": q.Get("q")}, nil)
		writeAgentJSON(w, res, err)

	case path == "/api/files/mkdir" && r.Method == http.MethodPost:
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		res, err := h.agentHub.Request(ctx, agentID, "mkdir", map[string]string{"path": body.Path}, nil)
		writeAgentEmpty(w, res, err)

	case path == "/api/files/touch" && r.Method == http.MethodPost:
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		res, err := h.agentHub.Request(ctx, agentID, "touch", map[string]string{"path": body.Path}, nil)
		writeAgentEmpty(w, res, err)

	case path == "/api/files/rename" && r.Method == http.MethodPost:
		var body struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		res, err := h.agentHub.Request(ctx, agentID, "rename", map[string]string{"src": body.Old, "dst": body.New}, nil)
		writeAgentEmpty(w, res, err)

	case (path == "/api/files/move" || path == "/api/files/copy") && r.Method == http.MethodPost:
		var body struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		act := "move"
		if strings.HasSuffix(path, "/copy") {
			act = "copy"
		}
		res, err := h.agentHub.Request(ctx, agentID, act, map[string]string{"src": body.Src, "dst": body.Dst}, nil)
		writeAgentEmpty(w, res, err)

	case path == "/api/files/text" && r.Method == http.MethodGet:
		res, err := h.agentHub.Request(ctx, agentID, "read_text", map[string]string{"path": q.Get("path")}, nil)
		writeAgentJSON(w, res, err)

	case path == "/api/files/text" && r.Method == http.MethodPut:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		res, err := h.agentHub.Request(ctx, agentID, "write_text", map[string]string{
			"path":    body.Path,
			"content": body.Content,
		}, nil)
		writeAgentEmpty(w, res, err)

	case (path == "/api/files/download" || path == "/api/files/preview") && r.Method == http.MethodGet:
		res, err := h.agentHub.Request(ctx, agentID, "read_file", map[string]string{"path": q.Get("path")}, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if res.Status >= 400 {
			http.Error(w, res.Error, res.Status)
			return
		}
		mime := res.Params["mime_type"]
		if mime == "" {
			mime = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'unsafe-inline'; sandbox")
		if strings.HasSuffix(path, "/download") {
			w.Header().Set("Content-Disposition", "attachment")
		} else {
			w.Header().Set("Content-Disposition", "inline")
		}
		http.ServeContent(w, r, res.Params["name"], time.Now(), bytes.NewReader(res.Data))

	case path == "/api/files/thumb" && r.Method == http.MethodGet:
		res, err := h.agentHub.Request(ctx, agentID, "thumb", map[string]string{"path": q.Get("path")}, nil)
		if err != nil || res.Status >= 400 {
			http.Error(w, "thumbnail not available", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Write(res.Data)

	case path == "/api/files/upload" && r.Method == http.MethodPost:
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			http.Error(w, "cannot parse upload", http.StatusBadRequest)
			return
		}
		dir := r.FormValue("path")
		relOverride := r.FormValue("relative_path")
		files := r.MultipartForm.File["files"]
		for _, fh := range files {
			f, err := fh.Open()
			if err != nil {
				http.Error(w, "cannot open uploaded file", http.StatusBadRequest)
				return
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				http.Error(w, "cannot read uploaded file", http.StatusBadRequest)
				return
			}
			name := filepath.Base(fh.Filename)
			if len(files) == 1 && relOverride != "" {
				name = relOverride
			}
			rel := filepath.ToSlash(filepath.Join(dir, name))
			res, err := h.agentHub.Request(ctx, agentID, "write_file", map[string]string{"path": rel}, data)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if res.Status >= 400 {
				http.Error(w, res.Error, res.Status)
				return
			}
		}
		w.WriteHeader(http.StatusCreated)

	default:
		http.Error(w, "operation not supported on remote agent", http.StatusNotImplemented)
	}
}

func writeAgentJSON(w http.ResponseWriter, res *agent.Message, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if res.Status >= 400 {
		http.Error(w, res.Error, res.Status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.Status)
	w.Write(res.Meta)
}

func writeAgentEmpty(w http.ResponseWriter, res *agent.Message, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if res.Status >= 400 {
		http.Error(w, res.Error, res.Status)
		return
	}
	w.WriteHeader(res.Status)
}
