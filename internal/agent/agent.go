package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/model"
)

const maxAgentMessageBytes = 64 << 20 // 64 MiB per RPC frame

// Message ist das RPC-Protokoll zwischen Fileship-Server und Remote-Agent über WebSocket.
type Message struct {
	ID     string            `json:"id,omitempty"`
	Type   string            `json:"type"` // "hello", "req", "res"
	Action string            `json:"action,omitempty"`
	Params map[string]string `json:"params,omitempty"`
	Data   []byte            `json:"data,omitempty"`
	Meta   json.RawMessage   `json:"meta,omitempty"`
	Error  string            `json:"error,omitempty"`
	Status int               `json:"status,omitempty"`
}

type session struct {
	agentID int64
	conn    *websocket.Conn
	writeMu sync.Mutex
	pendMu  sync.Mutex
	pending map[string]chan *Message
	closed  chan struct{}
}

func (s *session) send(msg *Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return s.conn.WriteJSON(msg)
}

func (s *session) close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
		_ = s.conn.Close()
		s.pendMu.Lock()
		for id, ch := range s.pending {
			close(ch)
			delete(s.pending, id)
		}
		s.pendMu.Unlock()
	}
}

// Hub verwaltet alle verbundenen Remote-Agenten auf dem Fileship-Server.
type Hub struct {
	mu          sync.RWMutex
	sessions    map[int64]*session
	reqSeq       atomic.Uint64
	onHeartbeat func(agentID int64, hostname, osInfo, rootPath string, diskUsage int64)
}

func NewHub(onHeartbeat func(agentID int64, hostname, osInfo, rootPath string, diskUsage int64)) *Hub {
	return &Hub{
		sessions:    make(map[int64]*session),
		onHeartbeat: onHeartbeat,
	}
}

func (h *Hub) IsOnline(agentID int64) bool {
	h.mu.RLock()
	_, ok := h.sessions[agentID]
	h.mu.RUnlock()
	return ok
}

func (h *Hub) Disconnect(agentID int64) {
	h.mu.Lock()
	sess, ok := h.sessions[agentID]
	if ok {
		delete(h.sessions, agentID)
	}
	h.mu.Unlock()
	if ok {
		sess.close()
	}
}

// ServeSession übernimmt eine authentifizierte WebSocket-Verbindung eines Agenten.
func (h *Hub) ServeSession(agentID int64, conn *websocket.Conn) {
	conn.SetReadLimit(maxAgentMessageBytes)
	sess := &session{
		agentID: agentID,
		conn:    conn,
		pending: make(map[string]chan *Message),
		closed:  make(chan struct{}),
	}

	h.mu.Lock()
	if old, ok := h.sessions[agentID]; ok {
		old.close()
	}
	h.sessions[agentID] = sess
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		if h.sessions[agentID] == sess {
			delete(h.sessions, agentID)
		}
		h.mu.Unlock()
		sess.close()
	}()

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "hello":
			if h.onHeartbeat != nil && msg.Params != nil {
				usage, _ := strconv.ParseInt(msg.Params["disk_usage"], 10, 64)
				h.onHeartbeat(
					agentID,
					msg.Params["hostname"],
					msg.Params["os_info"],
					msg.Params["root_path"],
					usage,
				)
			}
		case "res":
			sess.pendMu.Lock()
			ch, ok := sess.pending[msg.ID]
			if ok {
				delete(sess.pending, msg.ID)
			}
			sess.pendMu.Unlock()
			if ok {
				ch <- &msg
			}
		}
	}
}

// Request sendet einen RPC-Aufruf an einen verbundenen Agenten und wartet auf die Antwort.
func (h *Hub) Request(ctx context.Context, agentID int64, action string, params map[string]string, data []byte) (*Message, error) {
	h.mu.RLock()
	sess, ok := h.sessions[agentID]
	h.mu.RUnlock()
	if !ok {
		return nil, errors.New("agent offline")
	}

	reqID := strconv.FormatUint(h.reqSeq.Add(1), 10)
	ch := make(chan *Message, 1)
	sess.pendMu.Lock()
	select {
	case <-sess.closed:
		sess.pendMu.Unlock()
		return nil, errors.New("agent offline")
	default:
		sess.pending[reqID] = ch
	}
	sess.pendMu.Unlock()

	defer func() {
		sess.pendMu.Lock()
		delete(sess.pending, reqID)
		sess.pendMu.Unlock()
	}()

	if err := sess.send(&Message{
		ID:     reqID,
		Type:   "req",
		Action: action,
		Params: params,
		Data:   data,
	}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-sess.closed:
		return nil, errors.New("agent disconnected")
	case res, ok := <-ch:
		if !ok || res == nil {
			return nil, errors.New("agent disconnected")
		}
		return res, nil
	}
}

// ── Agent Client (läuft auf den entfernten Geräten) ──────────────────────────

// WSURL wandelt eine http(s):// Server-URL in die ws(s):///api/agent/ws URL um.
func WSURL(serverURL string) string {
	u := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if strings.HasPrefix(u, "https://") {
		u = "wss://" + strings.TrimPrefix(u, "https://")
	} else if strings.HasPrefix(u, "http://") {
		u = "ws://" + strings.TrimPrefix(u, "http://")
	} else if !strings.HasPrefix(u, "ws://") && !strings.HasPrefix(u, "wss://") {
		u = "ws://" + u
	}
	if !strings.HasSuffix(u, "/api/agent/ws") {
		u += "/api/agent/ws"
	}
	return u
}

// RunClient verbindet sich dauerhaft (mit Auto-Reconnect) zum Fileship-Server.
func RunClient(ctx context.Context, serverURL, token, rootPath string) error {
	if err := os.MkdirAll(rootPath, 0755); err != nil {
		return fmt.Errorf("cannot create agent root %s: %w", rootPath, err)
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return err
	}
	wsEndpoint := WSURL(serverURL)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := connectAndServe(ctx, wsEndpoint, token, rootAbs)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("⚠️  Agent connection lost (%v) — reconnecting in 3s...", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func connectAndServe(ctx context.Context, wsEndpoint, token, rootAbs string) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsEndpoint, headers)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(maxAgentMessageBytes)

	var writeMu sync.Mutex
	send := func(msg *Message) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
		return conn.WriteJSON(msg)
	}

	sendHello := func() {
		host, _ := os.Hostname()
		usage, _ := fsvc.DirSize(rootAbs)
		_ = send(&Message{
			Type: "hello",
			Params: map[string]string{
				"hostname":   host,
				"os_info":    runtime.GOOS + "/" + runtime.GOARCH,
				"root_path":  rootAbs,
				"disk_usage": strconv.FormatInt(usage, 10),
			},
		})
	}
	sendHello()
	log.Printf("✅ Fileship Agent connected to %s (Root: %s)", wsEndpoint, rootAbs)

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sendHello()
			}
		}
	}()

	for {
		var req Message
		if err := conn.ReadJSON(&req); err != nil {
			return err
		}
		if req.Type != "req" {
			continue
		}
		go func(r Message) {
			res := ExecuteAction(rootAbs, &r)
			res.ID = r.ID
			res.Type = "res"
			_ = send(res)
		}(req)
	}
}

// ExecuteAction führt eine angeforderte Dateioperation innerhalb von rootAbs aus.
func ExecuteAction(rootAbs string, req *Message) *Message {
	p := req.Params
	if p == nil {
		p = map[string]string{}
	}
	switch req.Action {
	case "list":
		page, _ := strconv.Atoi(p["page"])
		perPage, _ := strconv.Atoi(p["per_page"])
		sortAsc := p["sort_asc"] != "false"
		res, err := fsvc.ListPagedFiltered(rootAbs, model.ListOptions{
			Path:    p["path"],
			Search:  p["search"],
			SortBy:  p["sort_by"],
			SortAsc: sortAsc,
			Page:    page,
			PerPage: perPage,
		}, nil)
		if err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		raw, _ := json.Marshal(res)
		return &Message{Status: http.StatusOK, Meta: raw}

	case "search":
		res, err := fsvc.Search(rootAbs, p["q"])
		if err != nil {
			return errMsg(http.StatusInternalServerError, err)
		}
		raw, _ := json.Marshal(res)
		return &Message{Status: http.StatusOK, Meta: raw}

	case "info":
		rel := p["path"]
		abs, err := fsvc.Resolve(rootAbs, rel)
		if err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return errMsg(http.StatusNotFound, err)
		}
		out := map[string]any{
			"name":     info.Name(),
			"path":     rel,
			"size":     info.Size(),
			"is_dir":   info.IsDir(),
			"mod_time": info.ModTime(),
		}
		if info.IsDir() {
			entries, _ := os.ReadDir(abs)
			out["item_count"] = len(entries)
			size, _ := fsvc.DirSize(abs)
			out["size"] = size
		} else {
			mime, _ := fsvc.DetectMime(rootAbs, rel)
			out["mime_type"] = mime
			if info.Size() <= 50<<20 {
				if f, err := os.Open(abs); err == nil {
					h := sha256.New()
					_, _ = io.Copy(h, f)
					f.Close()
					out["sha256"] = hex.EncodeToString(h.Sum(nil))
				}
			}
		}
		raw, _ := json.Marshal(out)
		return &Message{Status: http.StatusOK, Meta: raw}

	case "mkdir":
		if _, err := fsvc.CleanRelativePath(p["path"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		if err := fsvc.Mkdir(rootAbs, p["path"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		return &Message{Status: http.StatusCreated}

	case "touch":
		if _, err := fsvc.CleanRelativePath(p["path"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		if err := fsvc.CreateFile(rootAbs, p["path"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		return &Message{Status: http.StatusCreated}

	case "delete":
		if err := fsvc.Delete(rootAbs, p["path"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		return &Message{Status: http.StatusNoContent}

	case "rename", "move":
		if err := fsvc.Rename(rootAbs, p["src"], p["dst"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		return &Message{Status: http.StatusNoContent}

	case "copy":
		if err := fsvc.Copy(rootAbs, p["src"], p["dst"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		return &Message{Status: http.StatusCreated}

	case "read_text":
		content, err := fsvc.ReadTextFile(rootAbs, p["path"])
		if err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		raw, _ := json.Marshal(map[string]string{"content": content})
		return &Message{Status: http.StatusOK, Meta: raw}

	case "write_text":
		if err := fsvc.WriteTextFile(rootAbs, p["path"], p["content"]); err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		return &Message{Status: http.StatusNoContent}

	case "read_file":
		clean, err := fsvc.CleanRelativePath(p["path"])
		if err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		abs, err := fsvc.Resolve(rootAbs, clean)
		if err != nil {
			return errMsg(http.StatusForbidden, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return errMsg(http.StatusNotFound, err)
		}
		if !info.Mode().IsRegular() {
			return errMsg(http.StatusBadRequest, errors.New("not a regular file"))
		}
		if info.Size() > maxAgentMessageBytes-1024*1024 {
			return errMsg(http.StatusRequestEntityTooLarge, errors.New("file too large for agent stream"))
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return errMsg(http.StatusInternalServerError, err)
		}
		mime, _ := fsvc.DetectMime(rootAbs, clean)
		return &Message{
			Status: http.StatusOK,
			Params: map[string]string{
				"mime_type": mime,
				"name":      info.Name(),
			},
			Data: data,
		}

	case "write_file":
		clean, err := fsvc.CleanRelativePath(p["path"])
		if err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		if err := fsvc.SaveUpload(rootAbs, clean, bytes.NewReader(req.Data)); err != nil {
			return errMsg(http.StatusInternalServerError, err)
		}
		return &Message{Status: http.StatusCreated}

	case "thumb":
		clean, err := fsvc.CleanRelativePath(p["path"])
		if err != nil {
			return errMsg(http.StatusBadRequest, err)
		}
		thumbDir := filepath.Join(rootAbs, ".fileship_thumbs")
		var thumbName string
		if existing, ok := fsvc.ThumbnailExists(rootAbs, thumbDir, clean); ok {
			thumbName = existing
		} else if fsvc.IsImage(clean) {
			thumbName, err = fsvc.GenerateThumbnail(rootAbs, clean, thumbDir)
			if err != nil {
				return errMsg(http.StatusInternalServerError, err)
			}
		} else {
			return errMsg(http.StatusNotFound, errors.New("not an image"))
		}
		data, err := os.ReadFile(filepath.Join(thumbDir, thumbName))
		if err != nil {
			return errMsg(http.StatusNotFound, err)
		}
		return &Message{
			Status: http.StatusOK,
			Params: map[string]string{"mime_type": "image/jpeg"},
			Data:   data,
		}

	default:
		return errMsg(http.StatusBadRequest, fmt.Errorf("unknown action: %s", req.Action))
	}
}

func errMsg(status int, err error) *Message {
	return &Message{
		Status: status,
		Error:  err.Error(),
	}
}
