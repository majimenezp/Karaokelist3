package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/majimenezp/Karaokelist3/internal/catalog"
	"github.com/majimenezp/Karaokelist3/internal/indexer"
)

//go:embed templates/* static/*
var assets embed.FS

const pageSize = 100
const maxJSONBody = 16 << 10

type Server struct {
	repo       *catalog.Repository
	indexer    *indexer.Indexer
	library    string
	adminToken string
	templates  *template.Template
	player     *playerController
	updatesMu  sync.Mutex
	updates    map[chan struct{}]struct{}
	mux        *http.ServeMux
	shutdownCh chan struct{}
	shutdown   sync.Once
}

type CatalogPage struct {
	Tracks     []catalog.Track
	Artists    []catalog.Facet
	Genres     []catalog.Facet
	Query      string
	Artist     string
	Genre      string
	Page       int
	Pages      int
	Total      int
	Indexed    int
	IndexError string
}

type DirectoryPage struct {
	Title  string
	Kind   string
	Items  []catalog.Facet
	Tracks int
}

type AdminPage struct {
	Library         string
	Tracks          []catalog.Track
	Count           int
	TransitionDelay int
}

func New(repo *catalog.Repository, scanner *indexer.Indexer, library, token string) *Server {
	t := template.Must(template.New("pages").Funcs(template.FuncMap{
		"inc": func(value int) int { return value + 1 },
		"dec": func(value int) int { return value - 1 },
	}).ParseFS(assets, "templates/*.html"))
	s := &Server{repo: repo, indexer: scanner, library: library, adminToken: token, templates: t, updates: make(map[chan struct{}]struct{}), shutdownCh: make(chan struct{})}
	s.player = newPlayerController(repo, s.signal)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.home)
	mux.HandleFunc("/catalog", s.catalog)
	mux.HandleFunc("/artists", s.directory("artist"))
	mux.HandleFunc("/genres", s.directory("genre"))
	mux.HandleFunc("/admin", s.admin)
	mux.HandleFunc("/greetings", s.page("templates/greetings.html"))
	mux.HandleFunc("/projector", s.page("templates/projector.html"))
	mux.HandleFunc("/lyrics", s.page("templates/lyrics.html"))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(mustSub(assets, "static")))))
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/state", s.stateHandler)
	mux.HandleFunc("/api/admin/index", s.indexLibrary)
	mux.HandleFunc("/api/admin/clear-party", s.clearParty)
	mux.HandleFunc("/api/admin/transition-delay", s.transitionDelay)
	mux.HandleFunc("/api/control", s.control)
	mux.HandleFunc("/api/profile", s.profile)
	mux.HandleFunc("/api/requests", s.requests)
	mux.HandleFunc("/api/greetings", s.greetings)
	mux.HandleFunc("/api/ticker", s.tickerGreetings)
	mux.HandleFunc("/media/", s.media)
	mux.HandleFunc("/request-media/", s.requestMedia)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	s.mux = mux
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }
func (s *Server) BeginShutdown()                                   { s.shutdown.Do(func() { close(s.shutdownCh) }) }
func (s *Server) Close() error {
	s.BeginShutdown()
	s.player.Close()
	return nil
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	result, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return result
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/catalog", http.StatusTemporaryRedirect)
}

func (s *Server) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, err := assets.ReadFile(name)
		if err != nil {
			http.Error(w, "page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	}
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	page := parsePage(r.URL.Query().Get("page"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	artist := r.URL.Query().Get("artist")
	genre := r.URL.Query().Get("genre")
	tracks, total, err := s.repo.Search(r.Context(), query, artist, genre, pageSize, (page-1)*pageSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	artists, err := s.repo.Facets(r.Context(), "artist")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	genres, err := s.repo.Facets(r.Context(), "genre")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	count, err := s.repo.Count(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	s.render(w, "templates/catalog.html", CatalogPage{Tracks: tracks, Artists: artists, Genres: genres, Query: query, Artist: artist, Genre: genre, Page: page, Pages: pages, Total: total, Indexed: count})
}

func (s *Server) directory(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		items, err := s.repo.Facets(r.Context(), kind)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		title := "Artistas"
		if kind == "genre" {
			title = "Géneros"
		}
		count, _ := s.repo.Count(r.Context())
		s.render(w, "templates/directory.html", DirectoryPage{Title: title, Kind: kind, Items: items, Tracks: count})
	}
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tracks, _, err := s.repo.Search(r.Context(), "", "", "", 1000, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	count, err := s.repo.Count(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	delay, err := s.repo.TransitionDelay(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "templates/admin.html", AdminPage{Library: s.library, Tracks: tracks, Count: count, TransitionDelay: delay})
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, path.Base(name), data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) indexLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body) {
		return
	}
	tracks, report, err := s.indexer.Scan(r.Context(), s.library)
	if err == nil {
		err = s.repo.Refresh(r.Context(), tracks)
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"error": err.Error(), "report": report})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) clearParty(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.player.ClearParty(r.Context()); err != nil {
		http.Error(w, "No se pudo limpiar la cola y los mensajes", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, s.player.State())
}

func (s *Server) transitionDelay(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		seconds, err := s.repo.TransitionDelay(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"seconds": seconds})
	case http.MethodPost:
		var body struct {
			Seconds int `json:"seconds"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Seconds < 0 || body.Seconds > 120 {
			http.Error(w, "El tiempo debe estar entre 0 y 120 segundos", http.StatusBadRequest)
			return
		}
		if err := s.repo.SetTransitionDelay(r.Context(), body.Seconds); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"seconds": body.Seconds})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	return s.adminToken != "" && r.Header.Get("X-Admin-Token") == s.adminToken
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "media" || (parts[2] != "cdg" && parts[2] != "mp3") {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	track, err := s.repo.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveMedia(w, r, parts[2], track.CDGPath, track.MP3Path)
}

func (s *Server) requestMedia(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "request-media" || (parts[2] != "cdg" && parts[2] != "mp3") {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	item, err := s.repo.Request(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveMedia(w, r, parts[2], item.CDGPath, item.MP3Path)
}

func serveMedia(w http.ResponseWriter, r *http.Request, kind, cdgPath, mp3Path string) {
	file, contentType := cdgPath, "application/octet-stream"
	if kind == "mp3" {
		file, contentType = mp3Path, "audio/mpeg"
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, file)
}

func (s *Server) stateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.player.State())
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		name, _ := profileName(r)
		writeJSON(w, http.StatusOK, map[string]string{"name": name})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if len([]rune(body.Name)) < 1 || len([]rune(body.Name)) > 50 {
			http.Error(w, "El nombre debe tener entre 1 y 50 caracteres", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "karaoke_name", Value: url.QueryEscape(body.Name), Path: "/", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		writeJSON(w, http.StatusOK, map[string]string{"name": body.Name})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func profileName(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("karaoke_name")
	if err != nil {
		return "", false
	}
	name, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(name), true
}

func (s *Server) requests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		items, err := s.repo.Requests(r.Context(), false)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name, ok := profileName(r)
	if !ok || name == "" {
		http.Error(w, "Primero indica tu nombre", http.StatusBadRequest)
		return
	}
	var body struct {
		TrackID int64  `json:"trackId"`
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if len([]rune(body.Message)) > 280 {
		http.Error(w, "El mensaje no puede exceder 280 caracteres", http.StatusBadRequest)
		return
	}
	item, err := s.repo.Enqueue(r.Context(), body.TrackID, name, body.Message)
	if err != nil {
		http.Error(w, "No se pudo agregar la canción", http.StatusBadRequest)
		return
	}
	s.signal()
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) greetings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.repo.Greetings(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		name, ok := profileName(r)
		if !ok || name == "" {
			http.Error(w, "Primero indica tu nombre", http.StatusBadRequest)
			return
		}
		var body struct {
			Message string `json:"message"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Message = strings.TrimSpace(body.Message)
		if len([]rune(body.Message)) < 1 || len([]rune(body.Message)) > 280 {
			http.Error(w, "El saludo debe tener entre 1 y 280 caracteres", http.StatusBadRequest)
			return
		}
		item, err := s.repo.AddGreeting(r.Context(), name, body.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.signal()
		writeJSON(w, http.StatusCreated, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) tickerGreetings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items, err := s.repo.TickerGreetings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) signal() {
	s.updatesMu.Lock()
	defer s.updatesMu.Unlock()
	for subscriber := range s.updates {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}

func (s *Server) subscribe() (chan struct{}, func()) {
	updates := make(chan struct{}, 1)
	s.updatesMu.Lock()
	s.updates[updates] = struct{}{}
	s.updatesMu.Unlock()
	return updates, func() { s.updatesMu.Lock(); delete(s.updates, updates); s.updatesMu.Unlock() }
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	updates, unsubscribe := s.subscribe()
	defer unsubscribe()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		data, _ := json.Marshal(s.player.State())
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-s.shutdownCh:
			return
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-updates:
		}
	}
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var request struct {
		Action    string `json:"action"`
		TrackID   int64  `json:"trackId"`
		RequestID int64  `json:"requestId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	state, err := s.player.Control(r.Context(), request.Action, request.TrackID, request.RequestID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, errTrackNotFound), errors.Is(err, errRequestNotFound):
			status = http.StatusNotFound
		case errors.Is(err, errNoPausedTrack), errors.Is(err, errDurationUnknown):
			status = http.StatusConflict
		case errors.Is(err, errInvalidPlayerAction):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json: %v", err)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request", http.StatusBadRequest)
		}
		return false
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	return true
}

func parsePage(value string) int {
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 {
		return 1
	}
	return page
}
