package static

import "testing"

func TestEmbeddedFrontend(t *testing.T) {
	for _, path := range []string{"dist/index.html", "dist/locales/en.json", "dist/locales/de.json"} {
		content, err := distFS.ReadFile(path)
		if err != nil {
			t.Fatalf("embedded frontend file %q is missing: %v", path, err)
		}
		if len(content) == 0 {
			t.Fatalf("embedded frontend file %q is empty", path)
		}
	}
}
