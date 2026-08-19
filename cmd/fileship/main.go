package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"

	"github.com/yourname/fileship/internal/config"
	"github.com/yourname/fileship/internal/db"
	"github.com/yourname/fileship/internal/handler"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/static"
	"github.com/yourname/fileship/internal/ws"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.RootPath, 0755); err != nil {
		log.Fatal("cannot create root path:", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatal("db:", err)
	}
	defer database.Close()

	adminPass := os.Getenv("ADMIN_PASSWORD")
	if adminPass == "" {
		b := make([]byte, 12)
		rand.Read(b)
		adminPass = hex.EncodeToString(b)
		log.Printf("⚠️  ADMIN_PASSWORD not set — generated: %s (set ADMIN_PASSWORD env var!)", adminPass)
	}
	if err := database.CreateAdminIfNotExists("admin", adminPass, "/"); err != nil {
		log.Fatal("admin setup:", err)
	}

	hub := ws.NewHub(cfg.Port)
	h := handler.New(cfg, database, hub)

	staticFS := static.FileServer()
	mux := http.NewServeMux()
	routes := h.Routes()
	mux.Handle("/api/", routes)
	mux.Handle("/ws", routes)
	mux.Handle("/s/", routes)
	mux.Handle("/webdav", http.HandlerFunc(h.WebDAV))
	mux.Handle("/webdav/", http.HandlerFunc(h.WebDAV))
	mux.Handle("/", staticFS)

	// CSRF Middleware um den gesamten Mux
	root := middleware.CSRF(cfg.TLSEnabled())(mux)

	addr := ":" + cfg.Port
	if cfg.TLSEnabled() {
		log.Printf("🚀 Fileship running on https://localhost:%s (TLS)", cfg.Port)
		log.Printf("   Root: %s | DB: %s", cfg.RootPath, cfg.DBPath)
		if err := http.ListenAndServeTLS(addr, cfg.TLSCert, cfg.TLSKey, root); err != nil {
			log.Fatal(err)
		}
	} else {
		log.Printf("🚀 Fileship running on http://localhost:%s", cfg.Port)
		log.Printf("   Root: %s | DB: %s", cfg.RootPath, cfg.DBPath)
		if err := http.ListenAndServe(addr, root); err != nil {
			log.Fatal(err)
		}
	}
}
