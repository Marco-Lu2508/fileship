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

func TestSourceCannotBypassPathPermissions(t *testing.T) {
	h, _ := testWorkspace(t)
	if err := h.db.CreateUser("sourced", "test-password", false, "/"); err != nil {
		t.Fatal(err)
	}
	user, err := h.db.GetUserByUsername("sourced")
	if err != nil {
		t.Fatal(err)
	}
	claims := &auth.Claims{UserID: user.ID}

	if err := os.MkdirAll(filepath.Join(h.cfg.RootPath, "projects", "secret"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cfg.RootPath, "projects", "secret", "keys.txt"), []byte("top-secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cfg.RootPath, "projects", "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := h.db.SetPathPermission(model.PathPermission{
		UserID:    user.ID,
		Path:      "projects/secret",
		CanRead:   false,
		CanWrite:  false,
		CanDelete: false,
	}); err != nil {
		t.Fatal(err)
	}

	// Non-admin cannot register a denied directory or traversal path as a source
	for _, badRoot := range []string{"../outside", "/etc", "projects/secret"} {
		req := requestWithClaims("POST", "/api/me/sources", claims)
		req.Body = io.NopCloser(strings.NewReader(`{"name":"bad","root_path":"` + badRoot + `"}`))
		w := httptest.NewRecorder()
		h.createSource(w, req)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusForbidden {
			t.Fatalf("expected source creation for %q to fail, got %d: %s", badRoot, w.Code, w.Body.String())
		}
	}

	// Registering an allowed parent folder as a source succeeds
	createReq := requestWithClaims("POST", "/api/me/sources", claims)
	createReq.Body = io.NopCloser(strings.NewReader(`{"name":"Projects","root_path":"projects"}`))
	createRec := httptest.NewRecorder()
	h.createSource(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected valid source creation 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var src model.UserSource
	if err := json.Unmarshal(createRec.Body.Bytes(), &src); err != nil {
		t.Fatal(err)
	}
	srcID := strconv.FormatInt(src.ID, 10)

	// Listing root of source hides "secret" subfolder
	listReq := requestWithClaims("GET", "/api/files", claims)
	listReq.Header.Set("X-Source-ID", srcID)
	listRec := httptest.NewRecorder()
	h.listFiles(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected source listing 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "secret") {
		t.Fatalf("source listing leaked restricted subfolder: %s", listRec.Body.String())
	}

	// Direct read/write/delete inside restricted subfolder via source is still denied
	dlReq := requestWithClaims("GET", "/api/files/download?path=secret/keys.txt", claims)
	dlReq.Header.Set("X-Source-ID", srcID)
	dlRec := httptest.NewRecorder()
	h.downloadFile(dlRec, dlReq)
	if dlRec.Code != http.StatusForbidden {
		t.Fatalf("expected source download of restricted file to return 403, got %d", dlRec.Code)
	}

	touchReq := requestWithClaims("POST", "/api/files/touch", claims)
	touchReq.Header.Set("X-Source-ID", srcID)
	touchReq.Body = io.NopCloser(strings.NewReader(`{"path":"secret/new.txt"}`))
	touchRec := httptest.NewRecorder()
	h.touch(touchRec, touchReq)
	if touchRec.Code != http.StatusForbidden {
		t.Fatalf("expected source touch in restricted dir to return 403, got %d", touchRec.Code)
	}
}

func TestPublicSharePasswordCookieRateLimitAndSubfolders(t *testing.T) {
	h, claims := testWorkspace(t)
	h.cfg.JWTSecret = "share-cookie-secret"
	if err := os.MkdirAll(filepath.Join(h.cfg.RootPath, "shared", "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cfg.RootPath, "shared", "sub", "hello.txt"), []byte("nested-ok"), 0644); err != nil {
		t.Fatal(err)
	}

	createReq := requestWithClaims("POST", "/api/shares", claims)
	createReq.Body = io.NopCloser(strings.NewReader(`{"path":"shared","is_dir":true,"password":"correct-password"}`))
	createRec := httptest.NewRecorder()
	h.createShare(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("createShare failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]string
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	token := created["token"]

	// POST password login sets HttpOnly cookie and redirects
	postReq := httptest.NewRequest(http.MethodPost, "/s/"+token, strings.NewReader("pw=correct-password"))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.RemoteAddr = "198.51.100.10:1234"
	postRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on valid share password POST, got %d", postRec.Code)
	}
	cookies := postRec.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].HttpOnly {
		t.Fatalf("expected HttpOnly share auth cookie, got %+v", cookies)
	}

	// Browse subfolder using the auth cookie without password in URL
	browseReq := httptest.NewRequest(http.MethodGet, "/s/"+token+"?dir=sub&view=1", nil)
	browseReq.AddCookie(cookies[0])
	browseRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(browseRec, browseReq)
	if browseRec.Code != http.StatusOK || !strings.Contains(browseRec.Body.String(), "hello.txt") {
		t.Fatalf("expected subfolder listing with hello.txt, got %d: %s", browseRec.Code, browseRec.Body.String())
	}

	// Brute-force from another IP triggers 429 Too Many Requests
	got429 := false
	for i := 0; i < 15; i++ {
		badReq := httptest.NewRequest(http.MethodGet, "/s/"+token+"?pw=wrong", nil)
		badReq.RemoteAddr = "198.51.100.99:4321"
		badRec := httptest.NewRecorder()
		h.Routes().ServeHTTP(badRec, badReq)
		if badRec.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected share password brute-force to be rate-limited with 429")
	}
}
