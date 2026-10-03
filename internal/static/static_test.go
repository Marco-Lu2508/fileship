package static

import (
	"net/http/httptest"
	"testing"
)

func TestEmbeddedFrontend(t *testing.T) {
	for _, path := range []string{"dist/index.html", "dist/locales/en.json", "dist/locales/de.json", "dist/locales/fr.json", "dist/locales/es.json", "dist/locales/it.json", "dist/locales/pt.json", "dist/fonts/fonts.css", "dist/fonts/OFL.txt"} {
		content, err := distFS.ReadFile(path)
		if err != nil {
			t.Fatalf("embedded frontend file %q is missing: %v", path, err)
		}
		if len(content) == 0 {
			t.Fatalf("embedded frontend file %q is empty", path)
		}
	}
}

func TestSPARoutesRevalidateAndMissingAssetsReturn404(t *testing.T) {
	server := FileServer()
	for _, path := range []string{"/", "/admin"} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: status %d cache %q", path, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	for _, path := range []string{"/assets/missing.js", "/locales/missing.json", "/fonts/missing.woff2"} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("%s must not serve HTML: %d", path, w.Code)
		}
	}
}

func TestLocalFontsAreServedAsWOFF2(t *testing.T) {
	for _, path := range []string{"/fonts/ibm-plex-sans-latin.woff2", "/fonts/ibm-plex-sans-latin-ext.woff2"} {
		w := httptest.NewRecorder()
		FileServer().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() < 4 || string(w.Body.Bytes()[:4]) != "wOF2" {
			t.Fatalf("%s: expected embedded WOFF2 font, got status %d", path, w.Code)
		}
	}
}
