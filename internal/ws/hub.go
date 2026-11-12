package ws

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/model"
)

// sendBuffer bounds how many pending events a single client may queue.
// Clients slower than this drop live events (the frontend refetches on
// reconnect anyway) instead of blocking every other client.
const sendBuffer = 64

type client struct {
	conn *websocket.Conn
	send chan []byte
}

// writer is the only goroutine that writes to the connection
// (gorilla/websocket requires at most one concurrent writer).
func (c *client) writer() {
	for data := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	secret  string
}

func NewHub(secret string) *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
		secret:  secret,
	}
}

func (h *Hub) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // native clients
			}
			parsed, err := url.Parse(origin)
			return err == nil && parsed.Host == r.Host
		},
	}
}

func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	claims, err := auth.ParseAccessToken(h.secret, r.URL.Query().Get("token"))
	if err != nil || claims.TwoFAPending {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	up := h.upgrader()
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, sendBuffer)}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go c.writer()

	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		close(c.send)
		conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// Broadcast queues the event for every connected client without ever
// blocking: a slow consumer drops events for itself only.
func (h *Hub) Broadcast(event model.WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
		}
	}
}
