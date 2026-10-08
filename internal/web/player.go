package web

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/majimenezp/Karaokelist3/internal/catalog"
)

var (
	errTrackNotFound       = errors.New("track not found")
	errRequestNotFound     = errors.New("request not found")
	errNoPausedTrack       = errors.New("no paused song to resume")
	errDurationUnknown     = errors.New("track duration is unavailable")
	errInvalidPlayerAction = errors.New("unknown action")
)

type playerState struct {
	Status         string  `json:"status"`
	TrackID        int64   `json:"trackId"`
	Title          string  `json:"title"`
	Artist         string  `json:"artist"`
	Requester      string  `json:"requester"`
	Message        string  `json:"message"`
	RequestID      int64   `json:"requestId"`
	MediaRequestID int64   `json:"mediaRequestId"`
	NextRequestID  int64   `json:"nextRequestId"`
	NextTitle      string  `json:"nextTitle"`
	NextArtist     string  `json:"nextArtist"`
	NextRequester  string  `json:"nextRequester"`
	TransitionAt   int64   `json:"transitionAt"`
	RemainingDelay int64   `json:"remainingDelay"`
	Countdown      int64   `json:"countdown"`
	Duration       float64 `json:"duration"`
	Offset         float64 `json:"offset"`
	StartedAt      int64   `json:"startedAt"`
	ServerNow      int64   `json:"serverNow"`
}

type playerController struct {
	repo     *catalog.Repository
	mu       sync.Mutex
	state    playerState
	notify   func()
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	closeOne sync.Once
}

func newPlayerController(repo *catalog.Repository, notify func()) *playerController {
	ctx, cancel := context.WithCancel(context.Background())
	p := &playerController{repo: repo, notify: notify, ctx: ctx, cancel: cancel, done: make(chan struct{}), state: playerState{Status: "stopped"}}
	go p.run()
	return p
}

func (p *playerController) Close() {
	p.closeOne.Do(p.cancel)
	<-p.done
}

func (p *playerController) run() {
	defer close(p.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case now := <-ticker.C:
			p.advance(now)
		}
	}
}

func (p *playerController) signal() {
	if p.notify != nil {
		p.notify()
	}
}

func (p *playerController) State() playerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.state
	state.ServerNow = time.Now().UnixMilli()
	if state.Status == "playing" {
		state.StartedAt = p.state.StartedAt
		state.Offset += float64(state.ServerNow-state.StartedAt) / 1000
	}
	if state.Status == "announcing" {
		state.Countdown = max64(0, (state.TransitionAt-state.ServerNow+999)/1000)
	} else if state.Status == "announcement-paused" {
		state.Countdown = max64(0, (state.RemainingDelay+999)/1000)
	}
	return state
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (p *playerController) Control(ctx context.Context, action string, trackID, requestID int64) (playerState, error) {
	p.mu.Lock()
	_, err := p.controlLocked(ctx, action, trackID, requestID, time.Now())
	p.mu.Unlock()
	if err != nil {
		return playerState{}, err
	}
	p.signal()
	return p.State(), nil
}

func (p *playerController) controlLocked(ctx context.Context, action string, trackID, requestID int64, now time.Time) (playerState, error) {
	switch action {
	case "play":
		if p.state.Status == "announcement-paused" && requestID == 0 && trackID == 0 {
			p.state.TransitionAt = now.UnixMilli() + p.state.RemainingDelay
			p.state.RemainingDelay = 0
			p.state.Status = "announcing"
			return p.state, nil
		}
		if requestID > 0 {
			item, err := p.repo.Request(ctx, requestID)
			if err != nil || (item.Status != catalog.RequestQueued && item.Status != catalog.RequestPlaying) {
				return playerState{}, errRequestNotFound
			}
			if p.state.RequestID > 0 && p.state.RequestID != item.ID && (p.state.Status == "playing" || p.state.Status == "paused") {
				if err := p.repo.SetRequestStatus(ctx, p.state.RequestID, catalog.RequestDone); err != nil {
					return playerState{}, err
				}
			}
			if p.state.RequestID == item.ID && p.state.Status == "playing" {
				return p.state, nil
			}
			if err := p.startRequestLocked(ctx, item, now); err != nil {
				return playerState{}, err
			}
			return p.state, nil
		}
		if trackID > 0 {
			track, err := p.repo.Get(ctx, trackID)
			if err != nil {
				return playerState{}, errTrackNotFound
			}
			if track.DurationSeconds <= 0 {
				return playerState{}, errDurationUnknown
			}
			if p.state.RequestID > 0 && (p.state.Status == "playing" || p.state.Status == "paused") {
				if err := p.repo.SetRequestStatus(ctx, p.state.RequestID, catalog.RequestDone); err != nil {
					return playerState{}, err
				}
			}
			p.state = playerState{Status: "playing", TrackID: track.ID, Title: track.Title, Artist: track.Artist, Duration: track.DurationSeconds, StartedAt: now.UnixMilli()}
			return p.state, nil
		}
		if p.state.Status == "paused" && p.state.TrackID > 0 {
			p.state.StartedAt = now.UnixMilli()
			p.state.Status = "playing"
			return p.state, nil
		}
		return playerState{}, errNoPausedTrack
	case "pause":
		if p.state.Status == "announcing" {
			p.state.RemainingDelay = max64(0, p.state.TransitionAt-now.UnixMilli())
			p.state.TransitionAt = 0
			p.state.Status = "announcement-paused"
		} else if p.state.Status == "playing" {
			p.state.Offset += float64(now.UnixMilli()-p.state.StartedAt) / 1000
			p.state.Status = "paused"
		}
		return p.state, nil
	case "next", "skip":
		if p.state.RequestID > 0 && (p.state.Status == "playing" || p.state.Status == "paused") {
			status := catalog.RequestDone
			if action == "skip" {
				status = catalog.RequestSkipped
			}
			if err := p.repo.SetRequestStatus(ctx, p.state.RequestID, status); err != nil {
				return playerState{}, err
			}
		}
		p.state = playerState{Status: "stopped"}
		item, err := p.nextPlayableLocked(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return p.state, nil
		}
		if err != nil {
			return playerState{}, err
		}
		if err := p.startRequestLocked(ctx, item, now); err != nil {
			return playerState{}, err
		}
		return p.state, nil
	case "stop":
		if p.state.RequestID > 0 && (p.state.Status == "playing" || p.state.Status == "paused") {
			if err := p.repo.SetRequestStatus(ctx, p.state.RequestID, catalog.RequestQueued); err != nil {
				return playerState{}, err
			}
		}
		p.state = playerState{Status: "stopped"}
		return p.state, nil
	default:
		return playerState{}, errInvalidPlayerAction
	}
}

func (p *playerController) ClearParty(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.repo.ClearParty(ctx); err != nil {
		return err
	}
	p.state = playerState{Status: "stopped"}
	p.signal()
	return nil
}

func (p *playerController) startRequestLocked(ctx context.Context, item catalog.Request, now time.Time) error {
	duration := item.DurationSeconds
	if duration <= 0 {
		var err error
		duration, err = catalog.CDGDuration(item.CDGPath)
		if err != nil {
			if os.IsNotExist(err) {
				if updateErr := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); updateErr != nil {
					return updateErr
				}
				return errTrackNotFound
			}
			return err
		}
	}
	if duration <= 0 || item.CDGPath == "" || item.MP3Path == "" {
		if err := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); err != nil {
			return err
		}
		return errDurationUnknown
	}
	if _, err := os.Stat(item.CDGPath); err != nil {
		if e := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); e != nil {
			return e
		}
		return errTrackNotFound
	}
	if _, err := os.Stat(item.MP3Path); err != nil {
		if e := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); e != nil {
			return e
		}
		return errTrackNotFound
	}
	if err := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestPlaying); err != nil {
		return err
	}
	p.state = playerState{Status: "playing", TrackID: item.TrackID, Title: item.Title, Artist: item.Artist, Requester: item.Requester, Message: item.Message, RequestID: item.ID, MediaRequestID: item.ID, Duration: duration, StartedAt: now.UnixMilli()}
	return nil
}

func (p *playerController) nextPlayableLocked(ctx context.Context) (catalog.Request, error) {
	for {
		item, err := p.repo.NextRequest(ctx)
		if err != nil {
			return catalog.Request{}, err
		}
		duration := item.DurationSeconds
		if duration <= 0 {
			var durationErr error
			duration, durationErr = catalog.CDGDuration(item.CDGPath)
			if durationErr != nil && !os.IsNotExist(durationErr) {
				return catalog.Request{}, durationErr
			}
		}
		if duration <= 0 {
			if err := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); err != nil {
				return catalog.Request{}, err
			}
			continue
		}
		if _, err := os.Stat(item.CDGPath); err != nil {
			if e := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); e != nil {
				return catalog.Request{}, e
			}
			continue
		}
		if _, err := os.Stat(item.MP3Path); err != nil {
			if e := p.repo.SetRequestStatus(ctx, item.ID, catalog.RequestSkipped); e != nil {
				return catalog.Request{}, e
			}
			continue
		}
		item.DurationSeconds = duration
		return item, nil
	}
}

func (p *playerController) advance(now time.Time) {
	p.mu.Lock()
	changed := p.advanceLocked(now)
	p.mu.Unlock()
	if changed {
		p.signal()
	}
}

func (p *playerController) advanceLocked(now time.Time) bool {
	nowMS := now.UnixMilli()
	switch p.state.Status {
	case "playing":
		position := p.state.Offset + float64(nowMS-p.state.StartedAt)/1000
		if p.state.Duration <= 0 {
			p.state.Status = "unplayable"
			return true
		}
		if position < p.state.Duration {
			return false
		}
		p.state.Offset = p.state.Duration
		if p.state.RequestID > 0 {
			if err := p.repo.SetRequestStatus(context.Background(), p.state.RequestID, catalog.RequestDone); err != nil {
				log.Printf("complete request: %v", err)
			}
		}
		return p.announceNextLocked(now)
	case "announcing":
		if nowMS < p.state.TransitionAt {
			return false
		}
		item, err := p.repo.Request(context.Background(), p.state.NextRequestID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && item.Status != catalog.RequestQueued) {
			item, err = p.nextPlayableLocked(context.Background())
		} else if err != nil {
			log.Printf("read announced request: %v", err)
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
			return true
		}
		if errors.Is(err, sql.ErrNoRows) {
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
			return true
		}
		if err != nil {
			log.Printf("find announced request: %v", err)
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
			return true
		}
		if err := p.startRequestLocked(context.Background(), item, now); err != nil {
			if errors.Is(err, errTrackNotFound) || errors.Is(err, errDurationUnknown) {
				return p.announceNextLocked(now)
			}
			log.Printf("start queued request: %v", err)
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
		}
		return true
	}
	return false
}

func (p *playerController) announceNextLocked(now time.Time) bool {
	ctx := context.Background()
	for {
		item, err := p.nextPlayableLocked(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
			return true
		}
		if err != nil {
			log.Printf("find next request: %v", err)
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
			return true
		}
		delay, err := p.repo.TransitionDelay(ctx)
		if err != nil {
			log.Printf("read transition delay: %v", err)
			delay = 15
		}
		p.state.NextRequestID = item.ID
		p.state.NextTitle = item.Title
		p.state.NextArtist = item.Artist
		p.state.NextRequester = item.Requester
		p.state.RemainingDelay = 0
		p.state.TransitionAt = now.UnixMilli() + int64(delay)*1000
		p.state.Status = "announcing"
		if delay > 0 {
			return true
		}
		if err := p.startRequestLocked(ctx, item, now); err == nil {
			return true
		} else if !errors.Is(err, errTrackNotFound) && !errors.Is(err, errDurationUnknown) {
			log.Printf("start queued request: %v", err)
			p.state.Status = "ended"
			p.clearAnnouncementLocked()
			return true
		}
	}
}

func (p *playerController) clearAnnouncementLocked() {
	p.state.NextRequestID = 0
	p.state.NextTitle = ""
	p.state.NextArtist = ""
	p.state.NextRequester = ""
	p.state.TransitionAt = 0
	p.state.RemainingDelay = 0
}
