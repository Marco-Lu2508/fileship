package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourname/fileship/internal/auth"
)

func TestCSRFDoubleSubmitCookie(t *testing.T) {
	mw := CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// GET sets csrf_token cookie
	getReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	getRec := httptest.NewRecorder()
	mw.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", getRec.Code)
	}
	cookies := getRec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "csrf_token" || cookies[0].Value == "" {
		t.Fatalf("expected csrf_token cookie, got %+v", cookies)
	}
	csrfCookie := cookies[0]

	// POST without cookie/header fails
	postNoToken := httptest.NewRequest(http.MethodPost, "/api/files/mkdir", nil)
	postNoTokenRec := httptest.NewRecorder()
	mw.ServeHTTP(postNoTokenRec, postNoToken)
	if postNoTokenRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", postNoTokenRec.Code)
	}

	// POST with mismatched header fails
	postMismatch := httptest.NewRequest(http.MethodPost, "/api/files/mkdir", nil)
	postMismatch.AddCookie(csrfCookie)
	postMismatch.Header.Set("X-CSRF-Token", "wrong-token")
	postMismatchRec := httptest.NewRecorder()
	mw.ServeHTTP(postMismatchRec, postMismatch)
	if postMismatchRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched CSRF token, got %d", postMismatchRec.Code)
	}

	// POST with matching cookie and header succeeds
	postValid := httptest.NewRequest(http.MethodPost, "/api/files/mkdir", nil)
	postValid.AddCookie(csrfCookie)
	postValid.Header.Set("X-CSRF-Token", csrfCookie.Value)
	postValidRec := httptest.NewRecorder()
	mw.ServeHTTP(postValidRec, postValid)
	if postValidRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid CSRF token, got %d", postValidRec.Code)
	}
}

func TestAuthAndAdminMiddleware(t *testing.T) {
	secret := "mw-secret"
	pending, _ := auth.GeneratePendingToken(secret, 1, true)
	userTok, _ := auth.GenerateAccessToken(secret, 2, false)
	adminTok, _ := auth.GenerateAccessToken(secret, 1, true)

	protected := AuthWithAPIToken(secret, func(raw string) (int64, bool, error) {
		if raw == "fsk_valid" {
			return 99, false, nil
		}
		return 0, false, errors.New("invalid api token")
	})(AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	for _, tc := range []struct {
		name   string
		header string
		status int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"pending 2fa token", "Bearer " + pending, http.StatusUnauthorized},
		{"non-admin token", "Bearer " + userTok, http.StatusForbidden},
		{"non-admin api token", "Bearer fsk_valid", http.StatusForbidden},
		{"invalid api token", "Bearer fsk_invalid", http.StatusUnauthorized},
		{"admin token", "Bearer " + adminTok, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s: expected %d, got %d", tc.name, tc.status, rec.Code)
		}
	}
}

func TestRateLimiterBlocksBurstOverflow(t *testing.T) {
	rl := NewRateLimiter(1, 2)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
		req.RemoteAddr = "192.0.2.10:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d unexpectedly blocked: %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on burst overflow, got %d", rec.Code)
	}
}
