package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourname/fileship/internal/model"
)

func TestAgentHubAndClientEndToEnd(t *testing.T) {
	remoteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteDir, "hello.txt"), []byte("from remote agent"), 0644); err != nil {
		t.Fatal(err)
	}

	heartbeatCh := make(chan string, 1)
	hub := NewHub(func(agentID int64, hostname, osInfo, rootPath string, diskUsage int64) {
		select {
		case heartbeatCh <- osInfo:
		default:
		}
	})

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fsa_testtoken" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hub.ServeSession(1, conn)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = RunClient(ctx, srv.URL, "fsa_testtoken", remoteDir)
	}()

	select {
	case <-heartbeatCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agent heartbeat")
	}

	if !hub.IsOnline(1) {
		t.Fatal("expected agent 1 to be online")
	}

	// List files on remote agent
	res, err := hub.Request(ctx, 1, "list", map[string]string{"path": ""}, nil)
	if err != nil || res.Status != http.StatusOK {
		t.Fatalf("list failed: err=%v res=%+v", err, res)
	}
	var listRes model.ListResult
	if err := json.Unmarshal(res.Meta, &listRes); err != nil {
		t.Fatal(err)
	}
	if len(listRes.Files) != 1 || listRes.Files[0].Name != "hello.txt" {
		t.Fatalf("unexpected remote files: %+v", listRes.Files)
	}

	// Read file from remote agent
	fileRes, err := hub.Request(ctx, 1, "read_file", map[string]string{"path": "hello.txt"}, nil)
	if err != nil || fileRes.Status != http.StatusOK || string(fileRes.Data) != "from remote agent" {
		t.Fatalf("read_file failed: err=%v res=%+v", err, fileRes)
	}

	// Upload file to remote agent
	upRes, err := hub.Request(ctx, 1, "write_file", map[string]string{"path": "sub/uploaded.txt"}, []byte("new payload"))
	if err != nil || upRes.Status != http.StatusCreated {
		t.Fatalf("write_file failed: err=%v res=%+v", err, upRes)
	}
	diskData, err := os.ReadFile(filepath.Join(remoteDir, "sub", "uploaded.txt"))
	if err != nil || string(diskData) != "new payload" {
		t.Fatalf("uploaded file not written on remote agent: %v %q", err, string(diskData))
	}

	// Path traversal blocked on remote agent
	travRes, err := hub.Request(ctx, 1, "read_file", map[string]string{"path": "../outside.txt"}, nil)
	if err != nil || travRes.Status == http.StatusOK || !strings.Contains(travRes.Error, "path outside root") {
		t.Fatalf("expected path traversal rejection on agent, got: %+v", travRes)
	}
}
