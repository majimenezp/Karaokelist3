package indexer

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/majimenezp/Karaokelist3/internal/catalog"
)

var trackPrefix = regexp.MustCompile(`^\s*(\d+)\s*[\s._-]+\s*(.*?)\s*$`)
var genericTitle = regexp.MustCompile(`(?i)^track\s*\d+$`)

type Report struct {
	Pairs        int
	CDGOnly      int
	MP3Only      int
	UnreadableID int
	EmptyCDG     int
}

type Indexer struct{}

func New() *Indexer { return &Indexer{} }

type pair struct{ cdg, mp3 string }

func (i *Indexer) Scan(ctx context.Context, root string) ([]catalog.Track, Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, Report{}, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, Report{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, Report{}, err
	}
	if !info.IsDir() {
		return nil, Report{}, fmt.Errorf("library path is not a directory: %s", root)
	}
	pairs := map[string]*pair{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".cdg" && ext != ".mp3" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := strings.ToLower(strings.TrimSuffix(filepath.Clean(rel), filepath.Ext(rel)))
		if pairs[key] == nil {
			pairs[key] = &pair{}
		}
		if ext == ".cdg" {
			pairs[key].cdg = path
		} else {
			pairs[key].mp3 = path
		}
		return nil
	})
	if err != nil {
		return nil, Report{}, err
	}

	report := Report{}
	tracks := make([]catalog.Track, 0, len(pairs))
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, report, err
		}
		files := pairs[key]
		if files.cdg == "" {
			report.MP3Only++
			continue
		}
		if files.mp3 == "" {
			report.CDGOnly++
			continue
		}
		info, err := os.Stat(files.cdg)
		if err != nil {
			return nil, report, err
		}
		if info.Size() == 0 {
			report.EmptyCDG++
			continue
		}
		track, id3Err := readTrack(ctx, files.cdg, files.mp3)
		if id3Err != nil {
			if ctx.Err() != nil {
				return nil, report, ctx.Err()
			}
			report.UnreadableID++
		}
		track.SearchKey = catalog.Normalize(strings.Join([]string{track.Title, track.Artist, track.Album, track.Collection, track.Genre}, " "))
		tracks = append(tracks, track)
	}
	if len(tracks) == 0 {
		return nil, report, fmt.Errorf("no matching .cdg+.mp3 pairs found under %s", root)
	}
	report.Pairs = len(tracks)
	return tracks, report, nil
}

func readTrack(ctx context.Context, cdgPath, mp3Path string) (catalog.Track, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Track{}, err
	}
	base := strings.TrimSuffix(filepath.Base(mp3Path), filepath.Ext(mp3Path))
	trackNo, filenameArtist, filenameTitle := parseFilename(base)
	track := catalog.Track{
		Title:       filenameTitle,
		Artist:      filenameArtist,
		Collection:  filepath.Base(filepath.Dir(mp3Path)),
		TrackNumber: trackNo,
		CDGPath:     cdgPath,
		MP3Path:     mp3Path,
	}
	if track.Title == "" {
		track.Title = base
	}
	duration, err := catalog.CDGDuration(cdgPath)
	if err != nil {
		return track, err
	}
	track.DurationSeconds = duration

	file, err := os.Open(mp3Path)
	if err != nil {
		return track, err
	}
	defer file.Close()
	tag, err := id3v2.ParseReader(contextReader{ctx: ctx, reader: file}, id3v2.Options{Parse: true, ParseFrames: []string{"Title", "Artist", "Album", "Genre"}})
	if err != nil {
		return track, err
	}
	if err := ctx.Err(); err != nil {
		return track, err
	}
	if value := cleanTag(tag.Title()); value != "" && !genericTitle.MatchString(value) {
		track.Title = value
	}
	if value := cleanTag(tag.Artist()); value != "" {
		track.Artist = value
	}
	if value := cleanTag(tag.Album()); value != "" && !strings.EqualFold(value, "unknown") {
		track.Album = value
	}
	track.Genre = cleanTag(tag.Genre())
	return track, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func parseFilename(base string) (int, string, string) {
	trackNo := 0
	remainder := strings.TrimSpace(base)
	if match := trackPrefix.FindStringSubmatch(remainder); match != nil {
		trackNo, _ = strconv.Atoi(match[1])
		remainder = strings.TrimSpace(match[2])
	}
	parts := strings.SplitN(remainder, " - ", 2)
	if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
		return trackNo, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return trackNo, "", remainder
}

func cleanTag(value string) string {
	return strings.TrimSpace(strings.Trim(value, "\x00\ufeff"))
}
