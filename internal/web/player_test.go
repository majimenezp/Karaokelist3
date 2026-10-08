package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/majimenezp/Karaokelist3/internal/catalog"
)

func playerFixture(t *testing.T) (*catalog.Repository, *playerController, catalog.Track, catalog.Request) {
	t.Helper()
	dir := t.TempDir()
	cdg, mp3 := filepath.Join(dir, "song.cdg"), filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(cdg, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp3, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := catalog.Open(filepath.Join(dir, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	track := catalog.Track{Title: "Song", Artist: "Artist", DurationSeconds: 10, CDGPath: cdg, MP3Path: mp3}
	if err := repo.Refresh(context.Background(), []catalog.Track{track}); err != nil {
		t.Fatal(err)
	}
	track, err = repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	request, err := repo.Enqueue(context.Background(), track.ID, "Singer", "dedication")
	if err != nil {
		t.Fatal(err)
	}
	p := newPlayerController(repo, nil)
	t.Cleanup(p.Close)
	return repo, p, track, request
}

func TestStopThenPlayDoesNotDuplicateQueuedRequest(t *testing.T) {
	repo, p, _, request := playerFixture(t)
	ctx := context.Background()
	if _, err := p.Control(ctx, "play", 0, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Control(ctx, "stop", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Control(ctx, "play", 0, 0); !errors.Is(err, errNoPausedTrack) {
		t.Fatalf("resume after stop error = %v, want errNoPausedTrack", err)
	}
	item, err := repo.Request(ctx, request.ID)
	if err != nil || item.Status != catalog.RequestQueued {
		t.Fatalf("request after stop/resume = %+v, %v; want queued", item, err)
	}
}

func TestSwitchingToLibraryTrackClearsRequestMetadata(t *testing.T) {
	repo, p, track, request := playerFixture(t)
	ctx := context.Background()
	if _, err := p.Control(ctx, "play", 0, request.ID); err != nil {
		t.Fatal(err)
	}
	state, err := p.Control(ctx, "play", track.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state.RequestID != 0 || state.MediaRequestID != 0 || state.Requester != "" || state.Message != "" {
		t.Fatalf("stale request metadata after direct play: %+v", state)
	}
	item, err := repo.Request(ctx, request.ID)
	if err != nil || item.Status != catalog.RequestDone {
		t.Fatalf("interrupted request = %+v, %v; want done", item, err)
	}
}

func TestAdvanceAnnouncesThenStartsNextRequest(t *testing.T) {
	repo, p, _, first := playerFixture(t)
	ctx := context.Background()
	second, err := repo.Enqueue(ctx, first.TrackID, "Next singer", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTransitionDelay(ctx, 15); err != nil {
		t.Fatal(err)
	}
	started := time.Unix(100, 0)
	if _, err := p.Control(ctx, "play", 0, first.ID); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.state.StartedAt = started.UnixMilli()
	p.mu.Unlock()
	p.mu.Lock()
	changed := p.advanceLocked(started.Add(10 * time.Second))
	p.mu.Unlock()
	if !changed {
		t.Fatal("advanceLocked did not complete the finished song")
	}
	if state := p.State(); state.Status != "announcing" || state.NextRequestID != second.ID {
		t.Fatalf("announcement state = %+v", state)
	}
	p.mu.Lock()
	changed = p.advanceLocked(started.Add(25 * time.Second))
	p.mu.Unlock()
	if !changed {
		t.Fatal("advanceLocked did not start the announced request")
	}
	if state := p.State(); state.Status != "playing" || state.RequestID != second.ID || state.Requester != "Next singer" {
		t.Fatalf("next request state = %+v", state)
	}
}

func TestUnavailableAnnouncedRequestSkipsAndContinuesQueue(t *testing.T) {
	repo, p, firstTrack, unavailable := playerFixture(t)
	ctx := context.Background()
	dir := filepath.Dir(firstTrack.CDGPath)
	cdgB, mp3B := filepath.Join(dir, "next.cdg"), filepath.Join(dir, "next.mp3")
	if err := os.WriteFile(cdgB, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp3B, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	secondTrack := catalog.Track{Title: "Next song", Artist: "Next artist", DurationSeconds: 5, CDGPath: cdgB, MP3Path: mp3B}
	if err := repo.Refresh(ctx, []catalog.Track{firstTrack, secondTrack}); err != nil {
		t.Fatal(err)
	}
	secondTrack, err := repo.Get(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Enqueue(ctx, secondTrack.ID, "Next singer", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTransitionDelay(ctx, 15); err != nil {
		t.Fatal(err)
	}
	started := time.Unix(200, 0)
	if _, err := p.Control(ctx, "play", firstTrack.ID, 0); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.state.StartedAt = started.UnixMilli()
	changed := p.advanceLocked(started.Add(10 * time.Second))
	p.mu.Unlock()
	if !changed || p.State().NextRequestID != unavailable.ID {
		t.Fatalf("first request was not announced: %+v", p.State())
	}
	if err := os.Remove(firstTrack.CDGPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(firstTrack.MP3Path); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	changed = p.advanceLocked(started.Add(25 * time.Second))
	p.mu.Unlock()
	if !changed {
		t.Fatal("player did not advance after the announced media disappeared")
	}
	state := p.State()
	if state.Status != "announcing" || state.NextRequestID != second.ID {
		t.Fatalf("next request was not announced after skipping missing media: %+v", state)
	}
	firstRequest, err := repo.Request(ctx, unavailable.ID)
	if err != nil || firstRequest.Status != catalog.RequestSkipped {
		t.Fatalf("unavailable request = %+v, %v; want skipped", firstRequest, err)
	}
	p.mu.Lock()
	changed = p.advanceLocked(started.Add(40 * time.Second))
	p.mu.Unlock()
	if !changed {
		t.Fatal("player did not start the next reproducible request")
	}
	state = p.State()
	if state.Status != "playing" || state.RequestID != second.ID {
		t.Fatalf("playback after unavailable request = %+v", state)
	}
	p.mu.Lock()
	changed = p.advanceLocked(started.Add(45 * time.Second))
	p.mu.Unlock()
	if !changed || p.State().Status != "ended" {
		t.Fatalf("player without more queued requests did not end cleanly: %+v", p.State())
	}
}

func TestLegacyRequestDurationComesFromSnapshotCDG(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.sqlite")
	cdgSnapshot, mp3Snapshot := filepath.Join(dir, "requested.cdg"), filepath.Join(dir, "requested.mp3")
	cdgCurrent, mp3Current := filepath.Join(dir, "current.cdg"), filepath.Join(dir, "current.mp3")
	if err := os.WriteFile(cdgSnapshot, make([]byte, 24*300*10), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp3Snapshot, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cdgCurrent, make([]byte, 24*300*99), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp3Current, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`
		CREATE TABLE tracks (
			id INTEGER PRIMARY KEY, cdg_path TEXT NOT NULL UNIQUE, mp3_path TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL, artist TEXT NOT NULL DEFAULT '', album TEXT NOT NULL DEFAULT '',
			collection TEXT NOT NULL DEFAULT '', genre TEXT NOT NULL DEFAULT '', track_number INTEGER NOT NULL DEFAULT 0,
			duration_seconds REAL NOT NULL DEFAULT 0, search_key TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE song_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT, track_id INTEGER NOT NULL, title TEXT NOT NULL,
			artist TEXT NOT NULL DEFAULT '', requester TEXT NOT NULL, message TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued', requested_at TEXT NOT NULL, cdg_path TEXT NOT NULL, mp3_path TEXT NOT NULL
		);
	`)
	if err == nil {
		_, err = legacyDB.Exec(`INSERT INTO tracks(id,cdg_path,mp3_path,title,duration_seconds) VALUES(1,?,?,?,99)`, cdgCurrent, mp3Current, "Current song")
	}
	if err == nil {
		_, err = legacyDB.Exec(`INSERT INTO song_requests(id,track_id,title,artist,requester,message,status,requested_at,cdg_path,mp3_path) VALUES(50,1,'Requested song','Original artist','Singer','','queued','2026-10-08T00:00:00Z',?,?)`, cdgSnapshot, mp3Snapshot)
	}
	missingCDG := filepath.Join(dir, "missing.cdg")
	if err == nil {
		_, err = legacyDB.Exec(`INSERT INTO song_requests(id,track_id,title,artist,requester,message,status,requested_at,cdg_path,mp3_path) VALUES(51,1,'Missing song','Original artist','Singer','','queued','2026-10-08T00:00:00Z',?,?)`, missingCDG, mp3Snapshot)
	}
	closeErr := legacyDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	repo, err := catalog.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	p := newPlayerController(repo, nil)
	defer p.Close()
	state, err := p.Control(context.Background(), "play", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if state.Duration != 10 || state.MediaRequestID != 50 {
		t.Fatalf("migrated request playback = %+v; want 10s from its CDG snapshot, not 99s from track ID", state)
	}
	if _, err := p.Control(context.Background(), "play", 0, 51); !errors.Is(err, errTrackNotFound) {
		t.Fatalf("missing snapshot playback error = %v, want errTrackNotFound", err)
	}
	missing, err := repo.Request(context.Background(), 51)
	if err != nil || missing.Status != catalog.RequestSkipped {
		t.Fatalf("missing legacy request = %+v, %v; want skipped", missing, err)
	}
}

func TestSignalBroadcastsToEverySubscriber(t *testing.T) {
	s := &Server{updates: make(map[chan struct{}]struct{})}
	a, removeA := s.subscribe()
	defer removeA()
	b, removeB := s.subscribe()
	defer removeB()
	s.signal()
	for name, ch := range map[string]chan struct{}{"a": a, "b": b} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %s did not receive broadcast", name)
		}
	}
}

func TestRequestMediaUsesSnapshotAfterCatalogRemoval(t *testing.T) {
	repo, _, track, request := playerFixture(t)
	if err := repo.Refresh(context.Background(), []catalog.Track{{Title: "Other", Artist: "Other", DurationSeconds: 1, CDGPath: "/other.cdg", MP3Path: "/other.mp3"}}); err != nil {
		t.Fatal(err)
	}
	server := New(repo, nil, "", "")
	defer server.Close()
	req := httptest.NewRequest(http.MethodGet, "/request-media/"+strconv.FormatInt(request.ID, 10)+"/mp3", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != string([]byte{1}) {
		t.Fatalf("request media response = status %d body %q; expected snapshot %s", w.Code, w.Body.String(), track.MP3Path)
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	body := strings.NewReader(strings.Repeat(" ", maxJSONBody+1))
	r := httptest.NewRequest("POST", "/", body)
	w := httptest.NewRecorder()
	var value struct{}
	if decodeJSON(w, r, &value) {
		t.Fatal("decodeJSON accepted oversized body")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}
