package song_guess

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// supportedPackVersion is the only pack format version ParsePack accepts
// (rules §4.1: `"version": 2`).
const supportedPackVersion = 2

// Validation caps, all fatal (Req 37, Q8). The 512 KiB document byte cap is
// enforced by the handler's http.MaxBytesReader (Step 13), not here.
const (
	maxSongsTotal    = 1000
	maxBlocksPerLine = 64
	maxBlockChars    = 200
	maxLanguages     = 32
	// warnSongsTotal is a soft threshold (Req 38): parsing still succeeds
	// above it, but a warning is surfaced to the pack author.
	warnSongsTotal = 500
)

// videoIDPattern matches a bare YouTube video id (Req 30, Q6).
var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ValidationIssue is one problem found while parsing a pack document,
// located by a JSON-path-like string rooted at the document (e.g.
// `languages[1].buckets["5"][2].line`).
type ValidationIssue struct {
	Path    string
	Message string
}

// ValidationError is every problem found while parsing a pack document,
// split into Fatal (parsing/publishing must be refused) and Warnings
// (parsing succeeds; surfaced to the pack author) — Requirements 37-38 and
// rules §4.2/§8.1. It implements error so ParsePack can return every
// problem through a single error return value, mirroring
// ttr.ValidationErrors.
type ValidationError struct {
	fatal    []ValidationIssue
	warnings []ValidationIssue
}

// Fatal returns every fatal validation issue found. A non-empty result
// means ParsePack returned a nil *Pack.
func (e *ValidationError) Fatal() []ValidationIssue { return e.fatal }

// Warnings returns every warning found, regardless of whether parsing
// ultimately succeeded.
func (e *ValidationError) Warnings() []ValidationIssue { return e.warnings }

// Error renders the fatal issues if any, else a warning summary.
func (e *ValidationError) Error() string {
	if len(e.fatal) == 0 {
		if len(e.warnings) == 0 {
			return "no validation issues"
		}
		return fmt.Sprintf("%d pack validation warning(s)", len(e.warnings))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d pack validation error(s):", len(e.fatal))
	for _, iss := range e.fatal {
		fmt.Fprintf(&b, "\n  - %s: %s", iss.Path, iss.Message)
	}
	return b.String()
}

// Unwrap lets errors.Is(err, ErrInvalidPackDoc) succeed exactly when e
// carries at least one fatal issue — i.e. exactly when parsing failed. A
// *ValidationError returned alongside a non-nil *Pack (warnings only) does
// not unwrap to ErrInvalidPackDoc, since that case is a parse success.
func (e *ValidationError) Unwrap() error {
	if len(e.fatal) > 0 {
		return ErrInvalidPackDoc
	}
	return nil
}

// --- wire document shape (rules §4.1) ---

type wireDoc struct {
	PackName   string         `json:"packName"`
	PackAuthor string         `json:"packAuthor"`
	Version    int            `json:"version"`
	Languages  []wireLanguage `json:"languages"`
}

type wireLanguage struct {
	Name string `json:"name"`
	// Buckets is keyed by block count (decimal string, since JSON object
	// keys must be strings). This is the only map in the whole feature —
	// ParsePack normalizes it into Language.Buckets immediately.
	Buckets map[string][]wireSong `json:"buckets"`
}

type wireSong struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Artist  string       `json:"artist"`
	Line    []string     `json:"line"`
	YouTube *wireYouTube `json:"youtube"`
}

type wireYouTube struct {
	URL      string `json:"url"`
	StartSec int32  `json:"startSec"`
	EndSec   int32  `json:"endSec"`
}

// ParsePack decodes and fully validates a pack document per rules §4.2 and
// Requirements 37-38.
//
// On a fatal problem it returns (nil, *ValidationError) with Fatal()
// non-empty. On success — with or without warnings — it returns a non-nil
// *Pack; if there are also warnings, the error return is a *ValidationError
// whose Fatal() is empty and Warnings() is not. Callers must check the
// returned *Pack, not a bare err != nil, to decide whether parsing
// succeeded.
func ParsePack(raw []byte) (*Pack, error) {
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &ValidationError{fatal: []ValidationIssue{{Path: "", Message: err.Error()}}}
	}

	var fatal, warnings []ValidationIssue
	addFatal := func(path, msg string) { fatal = append(fatal, ValidationIssue{Path: path, Message: msg}) }
	addWarn := func(path, msg string) { warnings = append(warnings, ValidationIssue{Path: path, Message: msg}) }

	if doc.Version != supportedPackVersion {
		addFatal("version", fmt.Sprintf("unsupported version %d, expected %d", doc.Version, supportedPackVersion))
	}
	if strings.TrimSpace(doc.PackName) == "" {
		addFatal("packName", "packName must not be empty")
	}
	if len(doc.Languages) == 0 {
		addFatal("languages", "at least one language is required")
	}
	if len(doc.Languages) > maxLanguages {
		addFatal("languages", fmt.Sprintf("too many languages: %d (max %d)", len(doc.Languages), maxLanguages))
	}

	langs, totalSongs := parseLanguages(doc.Languages, addFatal, addWarn)

	if totalSongs > maxSongsTotal {
		addFatal("totalSongs", fmt.Sprintf("too many songs: %d (max %d)", totalSongs, maxSongsTotal))
	}
	if totalSongs < 2 {
		addFatal("totalSongs", fmt.Sprintf("at least 2 songs are required, got %d", totalSongs))
	}
	if totalSongs%2 != 0 {
		addFatal("totalSongs", fmt.Sprintf("total song count must be even so each team gets total/2 active rounds, got %d", totalSongs))
	}
	if totalSongs > warnSongsTotal {
		addWarn("totalSongs", fmt.Sprintf("large pack: %d songs (soft threshold %d)", totalSongs, warnSongsTotal))
	}

	if len(fatal) > 0 {
		return nil, &ValidationError{fatal: fatal, warnings: warnings}
	}

	pack := &Pack{
		Name:        doc.PackName,
		AuthorLabel: doc.PackAuthor,
		Languages:   langs,
		TotalSongs:  totalSongs,
	}

	if len(warnings) > 0 {
		return pack, &ValidationError{warnings: warnings}
	}
	return pack, nil
}

// parseLanguages walks every language/bucket/song, reporting problems via
// addFatal/addWarn, and returns the normalized []Language plus the total
// song count across the whole pack. It never stops at the first problem —
// like ttr.ParseMap, it reports every issue found in one pass.
func parseLanguages(
	wireLangs []wireLanguage,
	addFatal, addWarn func(path, msg string),
) ([]Language, int) {
	langs := make([]Language, 0, len(wireLangs))
	seenLangNames := make(map[string]bool, len(wireLangs))
	seenSongIDs := make(map[string]bool)
	totalSongs := 0

	for i, wl := range wireLangs {
		langPath := fmt.Sprintf("languages[%d]", i)
		if seenLangNames[wl.Name] {
			addFatal(langPath+".name", fmt.Sprintf("duplicate language name %q", wl.Name))
		} else {
			seenLangNames[wl.Name] = true
		}

		buckets, n := parseBuckets(langPath, wl.Buckets, seenSongIDs, addFatal, addWarn)
		totalSongs += n
		langs = append(langs, Language{Name: wl.Name, Buckets: buckets})
	}

	return langs, totalSongs
}

// bucketKeysAscending returns the keys of buckets sorted ascending by their
// integer value where possible, falling back to lexical order for keys that
// don't parse as an integer (so validation output is still deterministic).
func bucketKeysAscending(buckets map[string][]wireSong) []string {
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		ai, aerr := strconv.Atoi(keys[a])
		bi, berr := strconv.Atoi(keys[b])
		if aerr == nil && berr == nil {
			return ai < bi
		}
		return keys[a] < keys[b]
	})
	return keys
}

// parseBuckets normalizes one language's wire bucket map into an
// ascending-block-count []Bucket, reporting per-bucket and per-song
// problems along the way. It returns the normalized buckets plus the
// number of songs found (valid or not — every wire song counts toward the
// pack's total).
func parseBuckets(
	langPath string,
	wireBuckets map[string][]wireSong,
	seenSongIDs map[string]bool,
	addFatal, addWarn func(path, msg string),
) ([]Bucket, int) {
	keys := bucketKeysAscending(wireBuckets)
	buckets := make([]Bucket, 0, len(keys))
	total := 0

	for _, key := range keys {
		songsWire := wireBuckets[key]
		bucketPath := fmt.Sprintf("%s.buckets[%q]", langPath, key)

		// The key must be the canonical decimal form of blockCount — not
		// merely something strconv.Atoi can parse. Without this,
		// strconv.Atoi("4") == strconv.Atoi("04") == strconv.Atoi("+4") == 4,
		// so two distinct JSON keys in the same language would normalize to
		// the same bucket and buildCells (engine.go) would emit two
		// pb.Cell rows with an identical (language, bucket) — cellFor
		// (state.go) only ever resolves the first, permanently stranding the
		// second cell's songs as "still unplayed" with no select_cell able
		// to reach them (a permanent PHASE_ROUND_SELECT deadlock, the exact
		// turn-order-deadlock class root CLAUDE.md warns about). Matches the
		// client mirror's check (song-guess-validate.ts: `String(bucket) ===
		// key`), and kills the whole class rather than only "04".
		blockCount, err := strconv.Atoi(key)
		validKey := err == nil && blockCount > 0 && strconv.Itoa(blockCount) == key
		if !validKey {
			addFatal(bucketPath, fmt.Sprintf("bucket key %q must be the canonical decimal form of a positive integer", key))
		}
		if validKey && len(songsWire) == 1 {
			addWarn(bucketPath, "only one song in this (language x bucket) cell; it becomes unavailable after the first pick")
		}

		songs := make([]Song, 0, len(songsWire))
		for j, ws := range songsWire {
			total++
			songPath := fmt.Sprintf("%s[%d]", bucketPath, j)
			songs = append(songs, parseSong(songPath, ws, blockCount, validKey, seenSongIDs, addFatal))
		}

		buckets = append(buckets, Bucket{Blocks: blockCount, Songs: songs})
	}

	return buckets, total
}

// parseSong validates a single wire song against its bucket's expected
// block count (rules §4.2) and returns the normalized Song regardless of
// what it finds — callers only use the result once the caller has confirmed
// zero fatal issues overall.
func parseSong(
	songPath string,
	ws wireSong,
	blockCount int,
	validKey bool,
	seenSongIDs map[string]bool,
	addFatal func(path, msg string),
) Song {
	if validKey && len(ws.Line) != blockCount {
		addFatal(songPath+".line", fmt.Sprintf("len(line) = %d, must equal bucket %d", len(ws.Line), blockCount))
	}
	if len(ws.Line) > maxBlocksPerLine {
		addFatal(songPath+".line", fmt.Sprintf("too many blocks: %d (max %d)", len(ws.Line), maxBlocksPerLine))
	}
	for k, block := range ws.Line {
		if strings.TrimSpace(block) == "" {
			addFatal(fmt.Sprintf("%s.line[%d]", songPath, k), "block must not be empty or whitespace-only")
		}
		if n := utf8.RuneCountInString(block); n > maxBlockChars {
			addFatal(fmt.Sprintf("%s.line[%d]", songPath, k), fmt.Sprintf("block too long: %d chars (max %d)", n, maxBlockChars))
		}
	}

	if seenSongIDs[ws.ID] {
		addFatal(songPath+".id", fmt.Sprintf("duplicate song id %q", ws.ID))
	} else {
		seenSongIDs[ws.ID] = true
	}

	var media *Media
	if ws.YouTube != nil {
		videoID, ok := parseVideoID(ws.YouTube.URL)
		if !ok {
			addFatal(songPath+".youtube.url", fmt.Sprintf("could not parse a YouTube video id from %q", ws.YouTube.URL))
		}
		if ws.YouTube.StartSec < 0 {
			addFatal(songPath+".youtube.startSec", "startSec must be >= 0")
		}
		if ws.YouTube.EndSec <= ws.YouTube.StartSec {
			addFatal(songPath+".youtube.endSec", "endSec must be > startSec")
		}
		if ok {
			media = &Media{VideoID: videoID, StartSec: ws.YouTube.StartSec, EndSec: ws.YouTube.EndSec}
		}
	}

	return Song{ID: ws.ID, Title: ws.Title, Artist: ws.Artist, Line: ws.Line, Media: media}
}

// parseVideoID extracts a bare YouTube video id from rawURL, accepting only
// the forms rules §4.1/§12 and Req 30 name:
//
//   - https://{youtube.com,www.youtube.com,m.youtube.com}/watch?v=<id>
//   - https://youtu.be/<id>
//   - https://www.youtube.com/embed/<id>
//   - https://www.youtube-nocookie.com/embed/<id>
//
// Anything else — including a non-https scheme such as "javascript:", a
// bare id with no URL, or an id not exactly 11 chars of [A-Za-z0-9_-] — is
// rejected. The returned id, never the raw URL, is what Media stores and
// what the view ever emits (Req 30, Q6).
func parseVideoID(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())

	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		if u.Path == "/watch" {
			return validateVideoID(u.Query().Get("v"))
		}
		return matchEmbedPath(u.Path)
	case "youtu.be":
		id := strings.TrimPrefix(u.Path, "/")
		if id == "" || strings.Contains(id, "/") {
			return "", false
		}
		return validateVideoID(id)
	case "youtube-nocookie.com", "www.youtube-nocookie.com":
		return matchEmbedPath(u.Path)
	default:
		return "", false
	}
}

// matchEmbedPath extracts the id segment from an "/embed/<id>" path.
func matchEmbedPath(path string) (string, bool) {
	const prefix = "/embed/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(path, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return validateVideoID(id)
}

// validateVideoID checks id against the 11-char YouTube video id shape.
func validateVideoID(id string) (string, bool) {
	if !videoIDPattern.MatchString(id) {
		return "", false
	}
	return id, true
}
