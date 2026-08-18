package ws

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/yourname/fileship/internal/model"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	allowedHost string
}

func NewHub(allowedHost string) *Hub {
	return &Hub{
		clients:     make(map[*websocket.Conn]struct{}),
		allowedHost: allowedHost,
	}
}

func (h *Hub) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // native clients
			}
			// Origin muss mit dem Host übereinstimmen
			return r.Host == r.Header.Get("Host") ||
				strings.Contains(origin, r.Host)
		},
	}
}

func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	up := h.upgrader()
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *Hub) Broadcast(event model.WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}
