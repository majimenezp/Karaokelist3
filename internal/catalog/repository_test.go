package catalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRefreshPreservesIDsAndRequestMediaSnapshot(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	first := Track{Title: "Canción A", Artist: "Artista A", DurationSeconds: 60, CDGPath: "/music/a.cdg", MP3Path: "/music/a.mp3"}
	second := Track{Title: "Canción B", Artist: "Artista B", DurationSeconds: 90, CDGPath: "/music/b.cdg", MP3Path: "/music/b.mp3"}
	if err := repo.Refresh(ctx, []Track{first, second}); err != nil {
		t.Fatal(err)
	}
	a, err := repo.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	request, err := repo.Enqueue(ctx, a.ID, "Prueba", "")
	if err != nil {
		t.Fatal(err)
	}
	first.Title = "Canción A actualizada"
	if err := repo.Refresh(ctx, []Track{second, first}); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != first.Title || updated.CDGPath != first.CDGPath {
		t.Fatalf("track ID %d changed identity: got %+v", a.ID, updated)
	}
	snapshot, err := repo.Request(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "Canción A" || snapshot.CDGPath != first.CDGPath || snapshot.DurationSeconds != 60 {
		t.Fatalf("request media snapshot changed after refresh: %+v", snapshot)
	}
	if err := repo.Refresh(ctx, []Track{second}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repo.Request(ctx, request.ID)
	if err != nil || snapshot.CDGPath != first.CDGPath {
		t.Fatalf("request snapshot should survive removal from catalog: %+v, err=%v", snapshot, err)
	}
}

func TestRefreshKeepsIdentityWhenCDGCaseIsRenamed(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	original := Track{Title: "Song", Artist: "Artist", DurationSeconds: 20, CDGPath: "/music/Song.cdg", MP3Path: "/music/Song.mp3"}
	if err := repo.Refresh(ctx, []Track{original}); err != nil {
		t.Fatal(err)
	}
	before, err := repo.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	request, err := repo.Enqueue(ctx, before.ID, "Singer", "")
	if err != nil {
		t.Fatal(err)
	}
	renamed := original
	renamed.CDGPath = "/music/song.cdg"
	if err := repo.Refresh(ctx, []Track{renamed}); err != nil {
		t.Fatalf("Refresh after CDG-only case rename: %v", err)
	}
	after, err := repo.Get(ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID || after.CDGPath != renamed.CDGPath || after.MP3Path != original.MP3Path {
		t.Fatalf("renamed CDG identity = %+v; want ID %d and new CDG path", after, before.ID)
	}
	snapshot, err := repo.Request(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CDGPath != original.CDGPath || snapshot.MP3Path != original.MP3Path {
		t.Fatalf("request snapshot changed on catalog path rename: %+v", snapshot)
	}
}
