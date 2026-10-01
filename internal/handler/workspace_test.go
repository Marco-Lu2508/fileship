//go:build cgo

package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/config"
	"github.com/yourname/fileship/internal/db"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/ws"
)

func testWorkspace(t *testing.T) (*Handler, *auth.Claims) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "data")
	os.Mkdir(root, 0755)
	cfg := &config.Config{RootPath: root, DBPath: filepath.Join(base, "fileship.db"), MaxUploadSize: 1 << 20}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.CreateAdminIfNotExists("admin", "test-only-password", "/"); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, database, ws.NewHub("test-secret")), &auth.Claims{UserID: user.ID, IsAdmin: true}
}

func requestWithClaims(method, target string, claims *auth.Claims) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(context.WithValue(req.Context(), middleware.ClaimsKey, claims))
}

func TestDeleteFailureNeverFallsBackToPermanentDeletion(t *testing.T) {
	h, claims := testWorkspace(t)
	path := filepath.Join(h.cfg.RootPath, "report.txt")
	os.WriteFile(path, []byte("important"), 0644)
	// Make the recovery location unusable.
	os.WriteFile(h.trashDir(), []byte("blocked"), 0644)
	w := httptest.NewRecorder()
	h.deleteFile(w, requestWithClaims("DELETE", "/api/files?path=report.txt", claims))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "important" {
		t.Fatalf("original lost: %q, %v", content, err)
	}
}

func TestTrashDatabaseFailurePreservesOriginal(t *testing.T) {
	h, claims := testWorkspace(t)
	path := filepath.Join(h.cfg.RootPath, "report.txt")
	os.WriteFile(path, []byte("important"), 0644)
	if _, err := h.db.Exec("DROP TABLE trash"); err != nil {
		t.Fatal(err)
	}
	if err := h.SoftDelete(claims.UserID, h.cfg.RootPath, "report.txt"); err == nil {
		t.Fatal("expected metadata failure")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("original moved without recovery metadata", err)
	}
}

func TestDeleteWorkspaceRootIsRejected(t *testing.T) {
	h, claims := testWorkspace(t)
	for _, path := range []string{"", ".", "folder/.."} {
		w := httptest.NewRecorder()
		h.deleteFile(w, requestWithClaims("DELETE", "/api/files?path="+url.QueryEscape(path), claims))
		if w.Code != http.StatusBadRequest {
			t.Errorf("root %q got %d", path, w.Code)
		}
	}
	if _, err := os.Stat(h.cfg.RootPath); err != nil {
		t.Fatal("workspace removed")
	}
}

func TestTrashRoundTripAcrossDataAndDatabaseVolumes(t *testing.T) {
	h, claims := testWorkspace(t)
	base, err := os.MkdirTemp("/dev/shm", "fileship-handler-")
	if err != nil {
		t.Skip("tmpfs unavailable")
	}
	defer os.RemoveAll(base)
	h.cfg.RootPath = base
	os.WriteFile(filepath.Join(base, "report.txt"), []byte("restore me"), 0644)
	if err := h.SoftDelete(claims.UserID, base, "report.txt"); err != nil {
		t.Fatal(err)
	}
	items, err := h.db.TrashList(claims.UserID)
	if err != nil || len(items) != 1 {
		t.Fatalf("trash metadata: %v, %v", items, err)
	}
	if _, err := os.Stat(filepath.Join(base, "report.txt")); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
	req := requestWithClaims("POST", "/api/trash/restore", claims)
	route := chi.NewRouteContext()
	route.URLParams.Add("name", items[0].TrashName)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	w := httptest.NewRecorder()
	h.trashRestore(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(base, "report.txt"))
	if err != nil || string(content) != "restore me" {
		t.Fatal("restore lost data", err)
	}
}

func TestFolderUploadKeepsRelativePath(t *testing.T) {
	h, claims := testWorkspace(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("path", "projects")
	form.WriteField("relative_path", "notes/nested/report.txt")
	file, _ := form.CreateFormFile("files", "report.txt")
	file.Write([]byte("folder content"))
	form.Close()
	req := httptest.NewRequest("POST", "/api/files/upload", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsKey, claims))
	w := httptest.NewRecorder()
	h.uploadFile(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(h.cfg.RootPath, "projects/notes/nested/report.txt"))
	if err != nil || string(content) != "folder content" {
		t.Fatal("folder structure lost", err)
	}
}
