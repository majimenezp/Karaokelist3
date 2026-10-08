package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Track struct {
	ID              int64
	Title           string
	Artist          string
	Album           string
	Collection      string
	Genre           string
	TrackNumber     int
	DurationSeconds float64
	CDGPath         string
	MP3Path         string
	SearchKey       string
}

type Facet struct {
	Label string
	Value string
	Count int
}

type Request struct {
	ID              int64   `json:"id"`
	TrackID         int64   `json:"trackId"`
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	Requester       string  `json:"requester"`
	Message         string  `json:"message"`
	Status          string  `json:"status"`
	RequestedAt     string  `json:"requestedAt"`
	DurationSeconds float64 `json:"durationSeconds"`
	MP3Path         string  `json:"-"`
	CDGPath         string  `json:"-"`
}

const (
	RequestQueued  = "queued"
	RequestPlaying = "playing"
	RequestDone    = "done"
	RequestSkipped = "skipped"
)

type Greeting struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
	Title     string `json:"title,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Source    string `json:"source"`
}

type Repository struct{ db *sql.DB }

const requestColumns = `id,track_id,title,artist,requester,message,status,requested_at,duration_seconds,cdg_path,mp3_path`

func CDGDuration(path string) (float64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return float64(info.Size()) / 24 / 300, nil
}

func Open(path string) (*Repository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY,
		cdg_path TEXT NOT NULL UNIQUE,
		mp3_path TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		artist TEXT NOT NULL DEFAULT '',
		album TEXT NOT NULL DEFAULT '',
		collection TEXT NOT NULL DEFAULT '',
		genre TEXT NOT NULL DEFAULT '',
		track_number INTEGER NOT NULL DEFAULT 0,
		duration_seconds REAL NOT NULL DEFAULT 0,
		search_key TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS tracks_artist_idx ON tracks(artist COLLATE NOCASE);
	CREATE INDEX IF NOT EXISTS tracks_genre_idx ON tracks(genre COLLATE NOCASE);
	CREATE INDEX IF NOT EXISTS tracks_title_idx ON tracks(title COLLATE NOCASE);
	CREATE TABLE IF NOT EXISTS song_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		track_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		artist TEXT NOT NULL DEFAULT '',
		requester TEXT NOT NULL,
		message TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'queued',
		requested_at TEXT NOT NULL,
		duration_seconds REAL NOT NULL DEFAULT 0,
		cdg_path TEXT NOT NULL,
		mp3_path TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS song_requests_status_idx ON song_requests(status,id);
	CREATE TABLE IF NOT EXISTS greetings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	INSERT OR IGNORE INTO settings(key,value) VALUES('transition_delay_seconds','15');`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "song_requests", "duration_seconds", `REAL NOT NULL DEFAULT 0`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`UPDATE song_requests SET status=? WHERE status=?`, RequestQueued, RequestPlaying); err != nil {
		db.Close()
		return nil, err
	}
	return &Repository{db: db}, nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks`).Scan(&count)
	return count, err
}

func (r *Repository) Refresh(ctx context.Context, tracks []Track) error {
	if len(tracks) == 0 {
		return fmt.Errorf("index produced no CDG+MP3 pairs; catalog was left unchanged")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS scanned_track_paths (cdg_path TEXT PRIMARY KEY, mp3_path TEXT NOT NULL)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM scanned_track_paths`); err != nil {
		return err
	}
	pathStmt, err := tx.PrepareContext(ctx, `INSERT INTO scanned_track_paths(cdg_path,mp3_path) VALUES(?,?)`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()
	for _, track := range tracks {
		if _, err := pathStmt.ExecContext(ctx, track.CDGPath, track.MP3Path); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tracks
		SET cdg_path=(SELECT s.cdg_path FROM scanned_track_paths s WHERE s.mp3_path=tracks.mp3_path)
		WHERE NOT EXISTS (SELECT 1 FROM scanned_track_paths s WHERE s.cdg_path=tracks.cdg_path)
		AND EXISTS (SELECT 1 FROM scanned_track_paths s WHERE s.mp3_path=tracks.mp3_path)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tracks WHERE NOT EXISTS (SELECT 1 FROM scanned_track_paths s WHERE s.cdg_path=tracks.cdg_path)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO tracks
		(cdg_path,mp3_path,title,artist,album,collection,genre,track_number,duration_seconds,search_key)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(cdg_path) DO UPDATE SET
		mp3_path=excluded.mp3_path,title=excluded.title,artist=excluded.artist,album=excluded.album,
		collection=excluded.collection,genre=excluded.genre,track_number=excluded.track_number,
		duration_seconds=excluded.duration_seconds,search_key=excluded.search_key`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, track := range tracks {
		if _, err := stmt.ExecContext(ctx, track.CDGPath, track.MP3Path, track.Title, track.Artist, track.Album, track.Collection, track.Genre, track.TrackNumber, track.DurationSeconds, track.SearchKey); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM scanned_track_paths`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Search(ctx context.Context, query, artist, genre string, limit, offset int) ([]Track, int, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0, 5)
	if query != "" {
		where = append(where, `instr(search_key, ?) > 0`)
		args = append(args, Normalize(query))
	}
	if artist == "__none" {
		where = append(where, `artist = ''`)
	} else if artist != "" {
		where = append(where, `artist = ?`)
		args = append(args, artist)
	}
	if genre == "__none" {
		where = append(where, `genre = ''`)
	} else if genre != "" {
		where = append(where, `genre = ?`)
		args = append(args, genre)
	}
	condition := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := r.db.QueryContext(ctx, `SELECT id,title,artist,album,collection,genre,track_number,duration_seconds,cdg_path,mp3_path
		FROM tracks WHERE `+condition+` ORDER BY artist COLLATE NOCASE,title COLLATE NOCASE,collection COLLATE NOCASE LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	results := make([]Track, 0, limit)
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Collection, &t.Genre, &t.TrackNumber, &t.DurationSeconds, &t.CDGPath, &t.MP3Path); err != nil {
			return nil, 0, err
		}
		results = append(results, t)
	}
	return results, total, rows.Err()
}

func (r *Repository) Facets(ctx context.Context, kind string) ([]Facet, error) {
	column := ""
	switch kind {
	case "artist":
		column = "artist"
	case "genre":
		column = "genre"
	default:
		return nil, fmt.Errorf("unsupported facet %q", kind)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+column+`,COUNT(*) FROM tracks GROUP BY `+column+` ORDER BY `+column+` COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Facet, 0)
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		label := value
		facetValue := value
		if value == "" {
			label = "Sin " + map[string]string{"artist": "artista", "genre": "género"}[kind]
			facetValue = "__none"
		}
		items = append(items, Facet{Label: label, Value: facetValue, Count: count})
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (Track, error) {
	var t Track
	err := r.db.QueryRowContext(ctx, `SELECT id,title,artist,album,collection,genre,track_number,duration_seconds,cdg_path,mp3_path FROM tracks WHERE id=?`, id).
		Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Collection, &t.Genre, &t.TrackNumber, &t.DurationSeconds, &t.CDGPath, &t.MP3Path)
	return t, err
}

func (r *Repository) Enqueue(ctx context.Context, trackID int64, requester, message string) (Request, error) {
	track, err := r.Get(ctx, trackID)
	if err != nil {
		return Request{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx, `INSERT INTO song_requests(track_id,title,artist,requester,message,status,requested_at,duration_seconds,cdg_path,mp3_path) VALUES(?,?,?,?,?,?,?,?,?,?)`, track.ID, track.Title, track.Artist, requester, message, RequestQueued, now, track.DurationSeconds, track.CDGPath, track.MP3Path)
	if err != nil {
		return Request{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Request{}, err
	}
	return Request{ID: id, TrackID: track.ID, Title: track.Title, Artist: track.Artist, Requester: requester, Message: message, Status: RequestQueued, RequestedAt: now, DurationSeconds: track.DurationSeconds, MP3Path: track.MP3Path, CDGPath: track.CDGPath}, nil
}

func (r *Repository) Requests(ctx context.Context, history bool) ([]Request, error) {
	query := `SELECT ` + requestColumns + ` FROM song_requests`
	if !history {
		query += ` WHERE status IN (?,?)`
	}
	query += ` ORDER BY id`
	var rows *sql.Rows
	var err error
	if history {
		rows, err = r.db.QueryContext(ctx, query)
	} else {
		rows, err = r.db.QueryContext(ctx, query, RequestQueued, RequestPlaying)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Request, 0)
	for rows.Next() {
		var item Request
		if err := rows.Scan(&item.ID, &item.TrackID, &item.Title, &item.Artist, &item.Requester, &item.Message, &item.Status, &item.RequestedAt, &item.DurationSeconds, &item.CDGPath, &item.MP3Path); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Request(ctx context.Context, id int64) (Request, error) {
	var item Request
	err := r.db.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM song_requests WHERE id=?`, id).
		Scan(&item.ID, &item.TrackID, &item.Title, &item.Artist, &item.Requester, &item.Message, &item.Status, &item.RequestedAt, &item.DurationSeconds, &item.CDGPath, &item.MP3Path)
	return item, err
}

func (r *Repository) NextRequest(ctx context.Context) (Request, error) {
	var item Request
	err := r.db.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM song_requests WHERE status=? ORDER BY id LIMIT 1`, RequestQueued).
		Scan(&item.ID, &item.TrackID, &item.Title, &item.Artist, &item.Requester, &item.Message, &item.Status, &item.RequestedAt, &item.DurationSeconds, &item.CDGPath, &item.MP3Path)
	return item, err
}

func (r *Repository) SetRequestStatus(ctx context.Context, id int64, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE song_requests SET status=? WHERE id=?`, status, id)
	return err
}

func (r *Repository) AddGreeting(ctx context.Context, name, message string) (Greeting, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx, `INSERT INTO greetings(name,message,created_at) VALUES(?,?,?)`, name, message, now)
	if err != nil {
		return Greeting{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Greeting{}, err
	}
	return Greeting{ID: id, Name: name, Message: message, CreatedAt: now, Source: "greeting"}, nil
}

func (r *Repository) Greetings(ctx context.Context) ([]Greeting, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,message,created_at,title,artist,source FROM (
		SELECT id,requester AS name,message,requested_at AS created_at,title,artist,'song' AS source FROM song_requests WHERE trim(message)<>''
		UNION ALL
		SELECT id,name,message,created_at,'' AS title,'' AS artist,'greeting' AS source FROM greetings
	) ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Greeting, 0)
	for rows.Next() {
		var item Greeting
		if err := rows.Scan(&item.ID, &item.Name, &item.Message, &item.CreatedAt, &item.Title, &item.Artist, &item.Source); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) TickerGreetings(ctx context.Context) ([]Greeting, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,message,created_at FROM greetings ORDER BY id DESC LIMIT 40`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Greeting, 0)
	for rows.Next() {
		var item Greeting
		if err := rows.Scan(&item.ID, &item.Name, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Source = "greeting"
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ClearParty(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM song_requests WHERE status IN (?,?)`, RequestQueued, RequestPlaying); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE song_requests SET message='' WHERE message<>''`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM greetings`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) TransitionDelay(ctx context.Context) (int, error) {
	var value string
	if err := r.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='transition_delay_seconds'`).Scan(&value); err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func (r *Repository) SetTransitionDelay(ctx context.Context, seconds int) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('transition_delay_seconds',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(seconds))
	return err
}
