package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourname/fileship/internal/agent"
	"github.com/yourname/fileship/internal/config"
	"github.com/yourname/fileship/internal/db"
	"github.com/yourname/fileship/internal/handler"
	"github.com/yourname/fileship/internal/middleware"
	"github.com/yourname/fileship/internal/static"
	"github.com/yourname/fileship/internal/ws"
)

func main() {
	agentMode := flag.Bool("agent", os.Getenv("FILESHIP_AGENT") == "1", "Run Fileship as a remote device agent")
	agentServer := flag.String("server", os.Getenv("FILESHIP_SERVER"), "Fileship server URL for agent mode (e.g. http://192.168.1.10:8080)")
	agentToken := flag.String("token", os.Getenv("FILESHIP_TOKEN"), "Agent token (fsa_...)")
	agentRoot := flag.String("root", os.Getenv("ROOT_PATH"), "Root directory to expose in agent mode")
	flag.Parse()

	if *agentMode || *agentToken != "" {
		if *agentServer == "" || *agentToken == "" {
			log.Fatal("Agent mode requires --server <url> and --token <fsa_...>")
		}
		rootDir := *agentRoot
		if rootDir == "" {
			rootDir = "./data"
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		log.Printf("🛰️  Starting Fileship Remote Agent -> %s (Root: %s)", *agentServer, rootDir)
		if err := agent.RunClient(ctx, *agentServer, *agentToken, rootDir); err != nil && ctx.Err() == nil {
			log.Fatal("agent error:", err)
		}
		return
	}

	cfg := config.Load()

	if err := middleware.SetTrustedProxies(os.Getenv("TRUSTED_PROXIES")); err != nil {
		log.Fatal(err)
	}

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

	hub := ws.NewHub(cfg.JWTSecret)
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
	root := middleware.SecurityHeaders(cfg.TLSEnabled())(middleware.CSRF(cfg.TLSEnabled())(mux))

	// No Read/WriteTimeout on purpose: uploads, downloads and video streams can
	// legitimately run for a long time. ReadHeaderTimeout stops slowloris.
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-stop
		log.Print("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Print("shutdown: ", err)
		}
	}()

	var serveErr error
	if cfg.TLSEnabled() {
		log.Printf("🚀 Fileship running on https://localhost:%s (TLS)", cfg.Port)
		log.Printf("   Root: %s | DB: %s", cfg.RootPath, cfg.DBPath)
		serveErr = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	} else {
		log.Printf("🚀 Fileship running on http://localhost:%s", cfg.Port)
		log.Printf("   Root: %s | DB: %s", cfg.RootPath, cfg.DBPath)
		serveErr = srv.ListenAndServe()
	}
	if serveErr != http.ErrServerClosed {
		log.Fatal(serveErr)
	}
	// ListenAndServe returns as soon as Shutdown starts; wait until active
	// requests have drained so the deferred database.Close() is safe.
	<-shutdownDone
}
