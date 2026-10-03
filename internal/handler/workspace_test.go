//go:build cgo

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/fileship/internal/auth"
	"github.com/yourname/fileship/internal/config"
	"github.com/yourname/fileship/internal/db"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/model"
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

func TestTransferEnforcesDestinationAndNestedPermissions(t *testing.T) {
	h, _ := testWorkspace(t)
	h.db.CreateUser("reader", "test-password", false, "/")
	user, _ := h.db.GetUserByUsername("reader")
	claims := &auth.Claims{UserID: user.ID}
	os.Mkdir(filepath.Join(h.cfg.RootPath, "docs"), 0755)
	os.Mkdir(filepath.Join(h.cfg.RootPath, "restricted"), 0755)
	os.WriteFile(filepath.Join(h.cfg.RootPath, "docs", "secret.txt"), []byte("private"), 0644)
	h.db.SetPathPermission(model.PathPermission{UserID: user.ID, Path: "restricted", CanRead: true, CanWrite: false})
	for _, mode := range []string{"copy", "move"} {
		req := requestWithClaims("POST", "/api/files/"+mode, claims)
		req.Body = io.NopCloser(strings.NewReader(`{"src":"docs","dst":"restricted/docs"}`))
		w := httptest.NewRecorder()
		if mode == "copy" {
			h.copyFile(w, req)
		} else {
			h.moveFile(w, req)
		}
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s target permission: %d %s", mode, w.Code, w.Body.String())
		}
	}
	h.db.SetPathPermission(model.PathPermission{UserID: user.ID, Path: "docs/secret.txt", CanRead: false, CanWrite: false})
	req := requestWithClaims("POST", "/api/files/copy", claims)
	req.Body = io.NopCloser(strings.NewReader(`{"src":"docs","dst":"copy"}`))
	w := httptest.NewRecorder()
	h.copyFile(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("nested permission: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(h.cfg.RootPath, "copy")); !os.IsNotExist(err) {
		t.Fatal("copied restricted descendant")
	}
}

func TestCopyEnforcesQuota(t *testing.T) {
	h, claims := testWorkspace(t)
	os.WriteFile(filepath.Join(h.cfg.RootPath, "a.txt"), []byte("12345"), 0644)
	h.db.SetUserQuota(claims.UserID, 8)
	req := requestWithClaims("POST", "/api/files/copy", claims)
	req.Body = io.NopCloser(strings.NewReader(`{"src":"a.txt","dst":"b.txt"}`))
	w := httptest.NewRecorder()
	h.copyFile(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "quota") {
		t.Fatalf("quota: %d %s", w.Code, w.Body.String())
	}
}

func TestFileInfoChecksumAndDirectorySize(t *testing.T) {
	h, claims := testWorkspace(t)
	os.Mkdir(filepath.Join(h.cfg.RootPath, "docs"), 0755)
	os.WriteFile(filepath.Join(h.cfg.RootPath, "docs", "a.txt"), []byte("abc"), 0644)
	w := httptest.NewRecorder()
	h.fileInfo(w, requestWithClaims("GET", "/api/files/info?path=docs/a.txt&checksum=sha256", claims))
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result["checksum"] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("checksum: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.fileInfo(w, requestWithClaims("GET", "/api/files/info?path=docs", claims))
	json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result["size"] != float64(3) || result["files"] != float64(1) {
		t.Fatalf("directory: %d %s", w.Code, w.Body.String())
	}
}

func TestArchiveChecksDescendantReadPermissionBeforeStreaming(t *testing.T) {
	h, _ := testWorkspace(t)
	h.db.CreateUser("reader", "test-password", false, "/")
	user, _ := h.db.GetUserByUsername("reader")
	claims := &auth.Claims{UserID: user.ID}
	os.Mkdir(filepath.Join(h.cfg.RootPath, "docs"), 0755)
	os.WriteFile(filepath.Join(h.cfg.RootPath, "docs", "secret.txt"), []byte("private"), 0644)
	h.db.SetPathPermission(model.PathPermission{UserID: user.ID, Path: "docs/secret.txt", CanRead: false})
	for _, endpoint := range []string{"archive", "zip-multi", "zip"} {
		req := requestWithClaims("POST", "/api/files/"+endpoint+"?path=docs", claims)
		req.Body = io.NopCloser(strings.NewReader(`{"paths":["docs"],"format":"zip"}`))
		w := httptest.NewRecorder()
		switch endpoint {
		case "archive":
			h.downloadArchive(w, req)
		case "zip-multi":
			h.zipMulti(w, req)
		case "zip":
			h.zipDir(w, req)
		}
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", endpoint, w.Code, w.Body.String())
		}
	}
}

func TestSearchFiltersUnreadableResults(t *testing.T) {
	h, _ := testWorkspace(t)
	if err := h.db.CreateUser("reader", "test-password", false, "/"); err != nil {
		t.Fatal(err)
	}
	user, err := h.db.GetUserByUsername("reader")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"secret.txt", "public.txt"} {
		if err := os.WriteFile(filepath.Join(h.cfg.RootPath, path), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.db.SetPathPermission(model.PathPermission{UserID: user.ID, Path: "secret.txt", CanRead: false}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.searchFiles(w, requestWithClaims("GET", "/api/files/search?q=txt", &auth.Claims{UserID: user.ID}))
	var files []model.FileInfo
	if err := json.Unmarshal(w.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "public.txt" {
		t.Fatalf("search disclosed denied files: %s", w.Body.String())
	}
}

func requestWithClaims(method, target string, claims *auth.Claims) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(context.WithValue(req.Context(), middleware.ClaimsKey, claims))
}

func TestSharesCanOnlyBeListedAndRevokedByTheirOwner(t *testing.T) {
	h, owner := testWorkspace(t)
	if err := h.db.CreateUser("other", "test-password", false, "/"); err != nil {
		t.Fatal(err)
	}
	otherUser, err := h.db.GetUserByUsername("other")
	if err != nil {
		t.Fatal(err)
	}
	other := &auth.Claims{UserID: otherUser.ID}
	token, err := h.db.CreateShareLink("notes.txt", false, owner.UserID, nil, "", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.listShares(w, requestWithClaims("GET", "/api/shares", other))
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("other user's shares leaked: %s", w.Body.String())
	}
	for _, claims := range []*auth.Claims{other, owner} {
		req := requestWithClaims("DELETE", "/api/shares/"+token, claims)
		route := chi.NewRouteContext()
		route.URLParams.Add("token", token)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
		w = httptest.NewRecorder()
		h.deleteShare(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
		}
		share, _ := h.db.GetShareLink(token)
		if claims == other && share == nil {
			t.Fatal("another user revoked the owner's share")
		}
		if claims == owner && share != nil {
			t.Fatal("owner could not revoke share")
		}
	}
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
