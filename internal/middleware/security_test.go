package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersAreSetAndHandlersCanOverrideCSP(t *testing.T) {
	handler := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file" {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		}
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "SAMEORIGIN",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": appCSP,
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS must not be sent without TLS")
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/file", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Fatalf("file handler CSP was not preserved: %q", got)
	}
}

func TestSecurityHeadersSendHSTSOverTLS(t *testing.T) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS missing with TLS enabled")
	}
}

func TestClientKeySeparatesIPv6HostsAndGroupsOneSubnet(t *testing.T) {
	if clientKey("203.0.113.7:5000") != "203.0.113.7" {
		t.Fatal("IPv4 key wrong")
	}
	a := clientKey("[2001:db8:1:1::1]:5000")
	b := clientKey("[2001:db8:1:1::ffff]:6000")
	c := clientKey("[2001:db8:2:2::1]:5000")
	if a != b {
		t.Fatalf("same /64 should share a key: %q vs %q", a, b)
	}
	if a == c || a == "[" {
		t.Fatalf("different networks must not share a key: %q vs %q", a, c)
	}
}
