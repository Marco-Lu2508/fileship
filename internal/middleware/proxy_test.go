package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardedForIsIgnoredUnlessPeerIsTrusted(t *testing.T) {
	t.Cleanup(func() { _ = SetTrustedProxies("") })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:4000"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	_ = SetTrustedProxies("")
	if got := requestClient(req); got != "198.51.100.9" {
		t.Fatalf("untrusted setup must use the peer address, got %q", got)
	}

	// A forged header from an untrusted peer is still ignored.
	if err := SetTrustedProxies("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if got := requestClient(req); got != "198.51.100.9" {
		t.Fatalf("header from untrusted peer must be ignored, got %q", got)
	}

	// Behind a trusted proxy the real client is the rightmost untrusted entry,
	// so values the client prepended itself cannot spoof the key.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:4000"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.20, 10.0.0.7")
	if got := requestClient(req); got != "203.0.113.20" {
		t.Fatalf("expected rightmost untrusted client, got %q", got)
	}
}

func TestSetTrustedProxiesRejectsGarbage(t *testing.T) {
	t.Cleanup(func() { _ = SetTrustedProxies("") })
	if err := SetTrustedProxies("not-an-ip"); err == nil {
		t.Fatal("expected an error for an invalid entry")
	}
	if err := SetTrustedProxies("192.168.1.1, 172.18.0.0/16, ::1"); err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
}

func TestLoginLimiterBlocksBruteForce(t *testing.T) {
	rl := NewRateLimiter(0.001, 3)
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	codes := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "[2001:db8::1]:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	if codes[2] != http.StatusOK || codes[3] != http.StatusTooManyRequests {
		t.Fatalf("unexpected sequence %v", codes)
	}
}
