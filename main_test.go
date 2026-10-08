package main

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majimenezp/Karaokelist3/internal/catalog"
	"github.com/majimenezp/Karaokelist3/internal/web"
)

func TestShutdownHTTPServerWaitsForActiveSSE(t *testing.T) {
	repo, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	app := web.New(repo, nil, "", "")
	var active atomic.Int32
	tracked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		app.ServeHTTP(w, r)
	})
	httpServer := &http.Server{Handler: tracked}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		app.BeginShutdown()
		_ = httpServer.Close()
		_ = app.Close()
		_ = repo.Close()
	})

	response, err := http.Get("http://" + listener.Addr().String() + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	if !scanner.Scan() {
		t.Fatalf("SSE did not send its initial event: %v", scanner.Err())
	}
	if active.Load() != 1 {
		t.Fatalf("active handlers before shutdown = %d, want 1", active.Load())
	}

	readDone := make(chan error, 1)
	go func() {
		for scanner.Scan() {
		}
		readDone <- scanner.Err()
	}()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdownHTTPServer(httpServer, app, 3*time.Second) }()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("read SSE after shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSE connection remained open after shutdown began")
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("graceful shutdown: %v", err)
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("shutdown returned with %d active handlers", got)
	}
	if err := <-serveResult; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
	}
	if _, err := http.Get("http://" + listener.Addr().String() + "/healthz"); err == nil {
		t.Fatal("server accepted a request after shutdown")
	}
}
