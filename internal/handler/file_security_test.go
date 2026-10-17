//go:build cgo

package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yourname/fileship/internal/auth"
	fsvc "github.com/yourname/fileship/internal/fs"
	"github.com/yourname/fileship/internal/model"
)

func createHandlerTestArchive(t *testing.T, root string, names ...string) {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		entry.Write([]byte("extracted contents"))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "archive.zip"), data.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestUnzipEnforcesEveryEntryBeforeExtraction(t *testing.T) {
	for _, constraint := range []string{"nested permission", "quota", "file type"} {
		t.Run(constraint, func(t *testing.T) {
			h, _ := testWorkspace(t)
			if err := h.db.CreateUser("extractor", "test-password", false, "/"); err != nil {
				t.Fatal(err)
			}
			user, err := h.db.GetUserByUsername("extractor")
			if err != nil {
				t.Fatal(err)
			}
			claims := &auth.Claims{UserID: user.ID}
			createHandlerTestArchive(t, h.cfg.RootPath, "public.txt", "private/notes.txt")
			switch constraint {
			case "nested permission":
				err = h.db.SetPathPermission(model.PathPermission{UserID: user.ID, Path: "output/private", CanRead: true, CanWrite: false})
			case "quota":
				usage, sizeErr := fsvc.DirSize(h.cfg.RootPath)
				if sizeErr != nil {
					t.Fatal(sizeErr)
				}
				err = h.db.SetUserQuota(user.ID, usage+1)
			case "file type":
				err = h.db.SetUserAllowedTypes(user.ID, "application/pdf")
			}
			if err != nil {
				t.Fatal(err)
			}
			req := requestWithClaims("POST", "/api/files/unzip", claims)
			req.Body = io.NopCloser(strings.NewReader(`{"zip_path":"archive.zip","dest_dir":"output"}`))
			w := httptest.NewRecorder()
			h.unzipFile(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("constraint bypassed: %d %s", w.Code, w.Body.String())
			}
			if _, err := os.Stat(filepath.Join(h.cfg.RootPath, "output")); !os.IsNotExist(err) {
				t.Fatal("rejected archive created files")
			}
		})
	}
}

func TestTextEditorEnforcesQuotaAndFileType(t *testing.T) {
	h, claims := testWorkspace(t)
	if err := os.WriteFile(filepath.Join(h.cfg.RootPath, "notes.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := h.db.SetUserQuota(claims.UserID, 5); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		content string
		status  int
	}{{"123456", http.StatusForbidden}, {"12345", http.StatusNoContent}, {"x", http.StatusNoContent}} {
		req := requestWithClaims("PUT", "/api/files/text", claims)
		body, _ := json.Marshal(map[string]string{"path": "notes.txt", "content": test.content})
		req.Body = io.NopCloser(bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.writeText(w, req)
		if w.Code != test.status {
			t.Fatalf("content %q: %d %s", test.content, w.Code, w.Body.String())
		}
	}
	if err := h.db.SetUserAllowedTypes(claims.UserID, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	req := requestWithClaims("PUT", "/api/files/text", claims)
	req.Body = io.NopCloser(strings.NewReader(`{"path":"notes.txt","content":"new"}`))
	w := httptest.NewRecorder()
	h.writeText(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("file type restriction bypassed: %d %s", w.Code, w.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(h.cfg.RootPath, "notes.txt"))
	if err != nil || string(content) != "x" {
		t.Fatalf("rejected edit changed file: %q, %v", content, err)
	}
}

func TestWebDAVRequiresCompletedTwoFactorLogin(t *testing.T) {
	h, claims := testWorkspace(t)
	h.cfg.JWTSecret = "webdav-test-secret"
	if err := os.WriteFile(filepath.Join(h.cfg.RootPath, "notes.txt"), []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := h.db.TOTPSetSecret(claims.UserID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	if err := h.db.TOTPEnable(claims.UserID, []string{"backup-code"}); err != nil {
		t.Fatal(err)
	}
	pending, err := auth.GeneratePendingToken(h.cfg.JWTSecret, claims.UserID, true)
	if err != nil {
		t.Fatal(err)
	}
	access, err := auth.GenerateAccessToken(h.cfg.JWTSecret, claims.UserID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"pending token", "basic auth", "completed token"} {
		req := httptest.NewRequest(http.MethodGet, "/webdav/notes.txt", nil)
		switch method {
		case "pending token":
			req.Header.Set("Authorization", "Bearer "+pending)
		case "basic auth":
			req.SetBasicAuth("admin", "test-only-password")
		case "completed token":
			req.Header.Set("Authorization", "Bearer "+access)
		}
		w := httptest.NewRecorder()
		h.WebDAV(w, req)
		if method == "completed token" {
			if w.Code != http.StatusOK || w.Body.String() != "private" {
				t.Fatalf("completed login cannot read WebDAV: %d %s", w.Code, w.Body.String())
			}
		} else if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s bypassed 2FA: %d %s", method, w.Code, w.Body.String())
		}
	}
}

func TestListingEnforcesReadRulesBeforePagination(t *testing.T) {
	h, _ := testWorkspace(t)
	if err := h.db.CreateUser("reader", "test-password", false, "/"); err != nil {
		t.Fatal(err)
	}
	user, err := h.db.GetUserByUsername("reader")
	if err != nil {
		t.Fatal(err)
	}
	claims := &auth.Claims{UserID: user.ID}
	for _, name := range []string{"a-secret.txt", "b-public.txt", "c-public.txt"} {
		if err := os.WriteFile(filepath.Join(h.cfg.RootPath, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(h.cfg.RootPath, "private"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a-secret.txt", "private"} {
		if err := h.db.SetPathPermission(model.PathPermission{UserID: user.ID, Path: path, CanRead: false}); err != nil {
			t.Fatal(err)
		}
	}
	for page, name := range []string{"b-public.txt", "c-public.txt"} {
		w := httptest.NewRecorder()
		h.listFiles(w, requestWithClaims("GET", "/api/files?per_page=1&page="+strconv.Itoa(page+1), claims))
		var result model.ListResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || result.Total != 2 || len(result.Files) != 1 || result.Files[0].Name != name {
			t.Fatalf("listing disclosed denied entries or wrong page: %d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.listFiles(w, requestWithClaims("GET", "/api/files?path=private", claims))
	if w.Code != http.StatusForbidden {
		t.Fatalf("denied directory listed: %d %s", w.Code, w.Body.String())
	}
}

func TestDownloadAndPreviewRejectDirectoriesAndEnforceCSP(t *testing.T) {
	h, claims := testWorkspace(t)
	if err := os.Mkdir(filepath.Join(h.cfg.RootPath, "folder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cfg.RootPath, "folder", "index.html"), []byte("<script>alert(1)</script>"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, endpoint := range []func(http.ResponseWriter, *http.Request){h.downloadFile, h.previewFile} {
		w := httptest.NewRecorder()
		endpoint(w, requestWithClaims("GET", "/api/files/download?path=folder", claims))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected directory download/preview to return 400, got %d", w.Code)
		}
	}

	w := httptest.NewRecorder()
	h.previewFile(w, requestWithClaims("GET", "/api/files/preview?path=folder/index.html", claims))
	if w.Code != http.StatusOK {
		t.Fatalf("expected file preview 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Fatalf("expected sandboxed CSP header on preview, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff header on preview, got %q", got)
	}
}

func TestTouchEnforcesAllowedTypesAndDeleteChecksDescendants(t *testing.T) {
	h, _ := testWorkspace(t)
	if err := h.db.CreateUser("member", "test-password", false, "/"); err != nil {
		t.Fatal(err)
	}
	user, err := h.db.GetUserByUsername("member")
	if err != nil {
		t.Fatal(err)
	}
	claims := &auth.Claims{UserID: user.ID}

	if err := h.db.SetUserAllowedTypes(user.ID, "text/plain"); err != nil {
		t.Fatal(err)
	}
	touchReq := requestWithClaims("POST", "/api/files/touch", claims)
	touchReq.Body = io.NopCloser(strings.NewReader(`{"path":"script.sh"}`))
	touchRec := httptest.NewRecorder()
	h.touch(touchRec, touchReq)
	if touchRec.Code != http.StatusForbidden {
		t.Fatalf("expected touch of disallowed type to return 403, got %d", touchRec.Code)
	}

	if err := os.MkdirAll(filepath.Join(h.cfg.RootPath, "parent", "locked"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := h.db.SetPathPermission(model.PathPermission{
		UserID:    user.ID,
		Path:      "parent/locked",
		CanRead:   true,
		CanWrite:  true,
		CanDelete: false,
	}); err != nil {
		t.Fatal(err)
	}
	delReq := requestWithClaims("DELETE", "/api/files?path=parent", claims)
	delRec := httptest.NewRecorder()
	h.deleteFile(delRec, delReq)
	if delRec.Code != http.StatusForbidden {
		t.Fatalf("expected deleting parent with protected child to return 403, got %d", delRec.Code)
	}
	if _, err := os.Stat(filepath.Join(h.cfg.RootPath, "parent", "locked")); err != nil {
		t.Fatalf("protected child was removed despite 403: %v", err)
	}
}

