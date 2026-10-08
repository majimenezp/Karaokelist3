package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/majimenezp/Karaokelist3/internal/catalog"
	"github.com/majimenezp/Karaokelist3/internal/indexer"
	"github.com/majimenezp/Karaokelist3/internal/web"
)

func main() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	defaultDB := filepath.Join(configDir, "KaraokeList", "catalog.sqlite")
	listen := flag.String("listen", ":8080", "HTTP listen address")
	library := flag.String("library", os.Getenv("KARAOKE_LIBRARY"), "Root directory containing CDG+MP3 pairs")
	dbPath := flag.String("db", defaultDB, "SQLite catalog path")
	adminToken := flag.String("admin-token", os.Getenv("KARAOKE_ADMIN_TOKEN"), "Admin token; generated if omitted")
	flag.Parse()
	if *library == "" {
		log.Fatal("provide -library or set KARAOKE_LIBRARY")
	}
	if *adminToken == "" {
		*adminToken, err = newToken()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Admin token: %s", *adminToken)
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatal(err)
	}

	repo, err := catalog.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	index := indexer.New()
	handler := web.New(repo, index, *library, *adminToken)
	server := &http.Server{
		Addr:              *listen,
		Handler:           handler,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdown)
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.ListenAndServe() }()
	log.Printf("KaraokeList listening on %s; library %s", *listen, *library)
	log.Printf("Web: /catalog | /artists | /genres | /greetings | /admin | /projector | /lyrics")

	var serveErr error
	select {
	case sig := <-shutdown:
		log.Printf("received %s; shutting down", sig)
		_ = shutdownHTTPServer(server, handler, 10*time.Second)
		serveErr = <-serveResult
	case serveErr = <-serveResult:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("HTTP server stopped: %v", serveErr)
			_ = shutdownHTTPServer(server, handler, 10*time.Second)
		}
	}
	if err := handler.Close(); err != nil {
		log.Printf("close web server: %v", err)
	}
	if err := repo.Close(); err != nil {
		log.Printf("close catalog: %v", err)
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatal(serveErr)
	}
}

func shutdownHTTPServer(server *http.Server, handler *web.Server, timeout time.Duration) error {
	handler.BeginShutdown()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP graceful shutdown: %v", err)
		if closeErr := server.Close(); closeErr != nil {
			log.Printf("force close HTTP server: %v", closeErr)
		}
		return err
	}
	return nil
}

func newToken() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
