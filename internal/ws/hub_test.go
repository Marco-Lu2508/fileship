package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/model"
)

func TestHubRejectsInvalidAndTwoFactorPendingTokens(t *testing.T) {
	const secret = "ws-test-secret"
	hub := NewHub(secret)

	pending, err := auth.GeneratePendingToken(secret, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{
		"missing":     "",
		"invalid":     "not-a-jwt",
		"2fa_pending": pending,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws?token="+token, nil)
			w := httptest.NewRecorder()
			hub.Handle(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 for %s token, got %d", name, w.Code)
			}
		})
	}
}

func TestHubBroadcastsConcurrentlyToAuthenticatedClients(t *testing.T) {
	const secret = "ws-test-secret"
	hub := NewHub(secret)
	srv := httptest.NewServer(http.HandlerFunc(hub.Handle))
	defer srv.Close()

	token, err := auth.GenerateAccessToken(secret, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	const count = 20
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.Broadcast(model.WSEvent{Type: "upload", Path: "docs"})
		}()
	}

	for i := 0; i < count; i++ {
		var event model.WSEvent
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read %d failed: %v", i, err)
		}
		if event.Type != "upload" || event.Path != "docs" {
			t.Fatalf("unexpected event: %+v", event)
		}
	}
	wg.Wait()
}
