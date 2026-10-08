package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScanSkipsEmptyCDG(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.cdg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.mp3"), []byte("not an mp3"), 0o600); err != nil {
		t.Fatal(err)
	}
	tracks, report, err := New().Scan(context.Background(), root)
	if err == nil || len(tracks) != 0 || report.EmptyCDG != 1 {
		t.Fatalf("Scan() = (%d tracks, %+v, %v), want empty-CDG report and error", len(tracks), report, err)
	}
}

func TestScanHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := New().Scan(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context.Canceled", err)
	}
}
