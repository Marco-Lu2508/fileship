package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var distFS embed.FS

func LocaleFile(lang string) ([]byte, error) {
	return fs.ReadFile(distFS, "dist/locales/"+lang+".json")
}

func FileServer() http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path[1:]
			if path == "" {
				http.ServeFileFS(w, r, dist, "index.html")
				return
			} else if _, err := fs.Stat(dist, path); err != nil {
				http.ServeFileFS(w, r, dist, "index.html")
				return
		}
		fileServer.ServeHTTP(w, r)
	})
}
