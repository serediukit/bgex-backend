package song_guess

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// validPackDoc returns a minimal, fully valid v2 pack document as a
// map[string]any so individual test cases can mutate arbitrary paths
// before marshaling and feeding it to ParsePack — mirroring
// ttr.validMapDoc's approach in maploader_test.go.
//
// Pack: 2 languages, 4 songs total (even, no single-song cells so the base
// document parses with zero warnings):
//
//	english:   bucket 4 -> song-1 (has youtube), song-2
//	ukrainian: bucket 5 -> song-3, song-4
func validPackDoc() map[string]any {
	return map[string]any{
		"packName":   "Test Pack",
		"packAuthor": "Tester",
		"version":    2,
		"languages": []any{
			map[string]any{
				"name": "english",
				"buckets": map[string]any{
					"4": []any{
						map[string]any{
							"id": "song-1", "title": "Song One", "artist": "Artist One",
							"line":    []any{"one", "two", "three", "four"},
							"youtube": map[string]any{"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "startSec": 10, "endSec": 20},
						},
						map[string]any{
							"id": "song-2", "title": "Song Two", "artist": "Artist Two",
							"line": []any{"a", "b", "c", "d"},
						},
					},
				},
			},
			map[string]any{
				"name": "ukrainian",
				"buckets": map[string]any{
					"5": []any{
						map[string]any{
							"id": "song-3", "title": "Song Three", "artist": "Artist Three",
							"line": []any{"a", "b", "c", "d", "e"},
						},
						map[string]any{
							"id": "song-4", "title": "Song Four", "artist": "Artist Four",
							"line": []any{"f", "g", "h", "i", "j"},
						},
					},
				},
			},
		},
	}
}

// bigLanguage returns a single-language document fragment with n unique,
// valid songs in bucket "4" — used to exercise the total-song-count caps
// without tripping any other fatal rule.
func bigLanguage(n int) map[string]any {
	songs := make([]any, 0, n)
	for i := range n {
		songs = append(songs, map[string]any{
			"id": fmt.Sprintf("big-song-%d", i), "title": "T", "artist": "A",
			"line": []any{"w", "x", "y", "z"},
		})
	}
	return map[string]any{
		"name":    "big",
		"buckets": map[string]any{"4": songs},
	}
}

// --- generic JSON fixture navigation helpers, mirroring ttr's must[T]/*Of
// helpers in maploader_test.go ---

func must[T any](v any) T {
	t, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("type assertion failed: got %T, want %T", v, *new(T)))
	}
	return t
}

func cloneDoc(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal base doc: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal base doc: %v", err)
	}
	return out
}

func languagesOf(doc map[string]any) []any { return must[[]any](doc["languages"]) }
func languageAt(doc map[string]any, i int) map[string]any {
	return must[map[string]any](languagesOf(doc)[i])
}
func bucketsOf(lang map[string]any) map[string]any { return must[map[string]any](lang["buckets"]) }
func songsAt(lang map[string]any, key string) []any {
	return must[[]any](bucketsOf(lang)[key])
}

func songAt(lang map[string]any, key string, i int) map[string]any {
	return must[map[string]any](songsAt(lang, key)[i])
}

func hasFatalAtPath(err error, path string) bool {
	var verr *ValidationError
	if !errors.As(err, &verr) {
		return false
	}
	for _, iss := range verr.Fatal() {
		if iss.Path == path {
			return true
		}
	}
	return false
}

func hasWarnAtPath(err error, path string) bool {
	var verr *ValidationError
	if !errors.As(err, &verr) {
		return false
	}
	for _, iss := range verr.Warnings() {
		if iss.Path == path {
			return true
		}
	}
	return false
}

// assertFatalIssue checks that ParsePack refused to parse — pack is nil and
// a fatal issue is present at path — mirroring ttr's ParseMap assertions.
func assertFatalIssue(t *testing.T, pack *Pack, err error, path string) {
	t.Helper()
	if pack != nil {
		t.Fatalf("ParsePack() pack = %+v, want nil (fatal issue expected at %q)", pack, path)
	}
	if err == nil {
		t.Fatalf("ParsePack() error = nil, want a fatal issue at %q", path)
	}
	if !hasFatalAtPath(err, path) {
		t.Fatalf("ParsePack() error = %v, want a fatal issue at path %q", err, path)
	}
}

// assertParsedOK checks that ParsePack succeeded with a non-nil pack. When
// warnPath is non-empty it also asserts a warning (and no fatal issue) at
// that path; otherwise it asserts a nil error.
func assertParsedOK(t *testing.T, pack *Pack, err error, warnPath string) {
	t.Helper()
	if pack == nil {
		t.Fatalf("ParsePack() pack = nil, err = %v, want a non-nil pack", err)
	}
	if warnPath == "" {
		if err != nil {
			t.Fatalf("ParsePack() error = %v, want nil", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("ParsePack() error = nil, want a warning at %q", warnPath)
	}
	if !hasWarnAtPath(err, warnPath) {
		t.Fatalf("ParsePack() error = %v, want a warning at path %q", err, warnPath)
	}
	var verr *ValidationError
	if errors.As(err, &verr) && len(verr.Fatal()) != 0 {
		t.Fatalf("ParsePack() error carries fatal issues %v, want none (warning-only case)", verr.Fatal())
	}
}

func TestParsePack(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(doc map[string]any)
		wantFatalPath string // asserted against err.(*ValidationError).Fatal() when set
		wantWarnPath  string // asserted against err.(*ValidationError).Warnings() when set
		wantOK        bool   // pack must be non-nil
	}{
		{
			name:   "valid minimal doc, no issues at all",
			mutate: func(map[string]any) {},
			wantOK: true,
		},
		{
			name:          "version 3",
			mutate:        func(doc map[string]any) { doc["version"] = 3 },
			wantFatalPath: "version",
		},
		{
			name:          "empty packName",
			mutate:        func(doc map[string]any) { doc["packName"] = "   " },
			wantFatalPath: "packName",
		},
		{
			name:          "zero languages",
			mutate:        func(doc map[string]any) { doc["languages"] = []any{} },
			wantFatalPath: "languages",
		},
		{
			name: "duplicate language name",
			mutate: func(doc map[string]any) {
				languageAt(doc, 1)["name"] = "english"
			},
			wantFatalPath: "languages[1].name",
		},
		{
			name: "bucket key not a positive integer",
			mutate: func(doc map[string]any) {
				lang := languageAt(doc, 0)
				b := bucketsOf(lang)
				b["zero"] = b["4"]
				delete(b, "4")
			},
			wantFatalPath: `languages[0].buckets["zero"]`,
		},
		{
			name: "len(line) != bucket",
			mutate: func(doc map[string]any) {
				songAt(languageAt(doc, 0), "4", 0)["line"] = []any{"one", "two", "three"}
			},
			wantFatalPath: `languages[0].buckets["4"][0].line`,
		},
		{
			name: "whitespace-only block",
			mutate: func(doc map[string]any) {
				songAt(languageAt(doc, 0), "4", 0)["line"] = []any{"one", "   ", "three", "four"}
			},
			wantFatalPath: `languages[0].buckets["4"][0].line[1]`,
		},
		{
			name: "duplicate song id across the whole pack",
			mutate: func(doc map[string]any) {
				songAt(languageAt(doc, 1), "5", 0)["id"] = "song-1"
			},
			wantFatalPath: `languages[1].buckets["5"][0].id`,
		},
		{
			// M1 regression: "04" and "4" both normalize to the same integer
			// via strconv.Atoi, so without a canonical-form check they parse
			// as two distinct Cell rows sharing (language, bucket=4) —
			// cellFor only ever resolves the first, so the second becomes an
			// unreachable-but-still-unplayed cell and the game can deadlock
			// permanently in PHASE_ROUND_SELECT (see round.go/state.go).
			// Confirmed to FAIL before the fix: ParsePack currently accepts
			// this document (pack non-nil, err nil) instead of reporting a
			// fatal issue.
			name: `duplicate normalized bucket key ("04" collides with "4")`,
			mutate: func(doc map[string]any) {
				lang := languageAt(doc, 0)
				b := bucketsOf(lang)
				b["04"] = []any{
					map[string]any{"id": "song-04-a", "title": "T", "artist": "A", "line": []any{"a", "b", "c", "d"}},
					map[string]any{"id": "song-04-b", "title": "T", "artist": "A", "line": []any{"e", "f", "g", "h"}},
				}
			},
			wantFatalPath: `languages[0].buckets["04"]`,
		},
		{
			// Same class of bug via a leading '+': strconv.Atoi("+4") also
			// parses to 4 with no error.
			name: `bucket key with a leading plus sign ("+4") is not canonical`,
			mutate: func(doc map[string]any) {
				lang := languageAt(doc, 0)
				b := bucketsOf(lang)
				b["+4"] = []any{
					map[string]any{"id": "song-plus-a", "title": "T", "artist": "A", "line": []any{"a", "b", "c", "d"}},
					map[string]any{"id": "song-plus-b", "title": "T", "artist": "A", "line": []any{"e", "f", "g", "h"}},
				}
			},
			wantFatalPath: `languages[0].buckets["+4"]`,
		},
		{
			name: "youtube url does not parse to a video id",
			mutate: func(doc map[string]any) {
				songAt(languageAt(doc, 0), "4", 0)["youtube"] = map[string]any{
					"url": "https://example.com/watch?v=dQw4w9WgXcQ", "startSec": 0, "endSec": 10,
				}
			},
			wantFatalPath: `languages[0].buckets["4"][0].youtube.url`,
		},
		{
			name: "youtube startSec negative",
			mutate: func(doc map[string]any) {
				songAt(languageAt(doc, 0), "4", 0)["youtube"] = map[string]any{
					"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "startSec": -1, "endSec": 10,
				}
			},
			wantFatalPath: `languages[0].buckets["4"][0].youtube.startSec`,
		},
		{
			name: "youtube endSec <= startSec",
			mutate: func(doc map[string]any) {
				songAt(languageAt(doc, 0), "4", 0)["youtube"] = map[string]any{
					"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "startSec": 10, "endSec": 10,
				}
			},
			wantFatalPath: `languages[0].buckets["4"][0].youtube.endSec`,
		},
		{
			// The regression proof: this case was written and confirmed to
			// FAIL against ParsePack before the odd-total-songs check was
			// added (see the worker report — a pass-before-the-fix test
			// proves nothing per root CLAUDE.md).
			name: "odd total song count (3)",
			mutate: func(doc map[string]any) {
				b4 := songsAt(languageAt(doc, 0), "4")
				b4 = append(b4, map[string]any{
					"id": "song-5", "title": "T5", "artist": "A5",
					"line": []any{"w", "x", "y", "z"},
				})
				bucketsOf(languageAt(doc, 0))["4"] = b4
				// Drop one song from the other language's bucket so the
				// total is 3 (odd) rather than 5 (also odd, but let's keep
				// the fixture obviously minimal): 3 (english) + 0
				// (ukrainian) would trip "< 2" instead, so leave ukrainian's
				// 2 songs in place: english=3, ukrainian=2, total=5 (odd).
			},
			wantFatalPath: "totalSongs",
		},
		{
			name: "total songs < 2",
			mutate: func(doc map[string]any) {
				doc["languages"] = []any{
					map[string]any{
						"name": "only",
						"buckets": map[string]any{
							"4": []any{
								map[string]any{"id": "song-x", "title": "T", "artist": "A", "line": []any{"a", "b", "c", "d"}},
							},
						},
					},
				}
			},
			wantFatalPath: "totalSongs",
		},
		{
			name: "more than 1000 songs",
			mutate: func(doc map[string]any) {
				doc["languages"] = []any{bigLanguage(1002)}
			},
			wantFatalPath: "totalSongs",
		},
		{
			name: "more than 64 blocks in a line",
			mutate: func(doc map[string]any) {
				line := make([]any, 65)
				for i := range line {
					line[i] = fmt.Sprintf("w%d", i)
				}
				doc["languages"] = append(languagesOf(doc), map[string]any{
					"name": "big-blocks",
					"buckets": map[string]any{
						"65": []any{
							map[string]any{"id": "big-1", "title": "T", "artist": "A", "line": line},
							map[string]any{"id": "big-2", "title": "T", "artist": "A", "line": line},
						},
					},
				})
			},
			wantFatalPath: `languages[2].buckets["65"][0].line`,
		},
		{
			name: "block longer than 200 chars",
			mutate: func(doc map[string]any) {
				s := songAt(languageAt(doc, 0), "4", 0)
				line := must[[]any](s["line"])
				line[0] = strings.Repeat("x", 201)
				s["line"] = line
			},
			wantFatalPath: `languages[0].buckets["4"][0].line[0]`,
		},
		{
			name: "more than 32 languages",
			mutate: func(doc map[string]any) {
				langs := make([]any, 0, 33)
				for i := range 33 {
					langs = append(langs, map[string]any{
						"name": fmt.Sprintf("lang-%d", i),
						"buckets": map[string]any{
							"4": []any{
								map[string]any{"id": fmt.Sprintf("song-%d-a", i), "title": "T", "artist": "A", "line": []any{"a", "b", "c", "d"}},
								map[string]any{"id": fmt.Sprintf("song-%d-b", i), "title": "T", "artist": "A", "line": []any{"a", "b", "c", "d"}},
							},
						},
					})
				}
				doc["languages"] = langs
			},
			wantFatalPath: "languages",
		},
		{
			name: "single-song cell warns but still parses",
			mutate: func(doc map[string]any) {
				lang0 := languageAt(doc, 0)
				b4 := songsAt(lang0, "4")
				b4 = append(b4, map[string]any{
					"id": "song-5", "title": "T5", "artist": "A5",
					"line": []any{"w", "x", "y", "z"},
				})
				bucketsOf(lang0)["4"] = b4

				lang1 := languageAt(doc, 1)
				bucketsOf(lang1)["5"] = []any{songAt(lang1, "5", 0)}
			},
			wantOK:       true,
			wantWarnPath: `languages[1].buckets["5"]`,
		},
		{
			name: "more than 500 songs warns but still parses",
			mutate: func(doc map[string]any) {
				doc["languages"] = []any{bigLanguage(502)}
			},
			wantOK:       true,
			wantWarnPath: "totalSongs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := cloneDoc(t, validPackDoc())
			tt.mutate(doc)
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("marshal mutated doc: %v", err)
			}

			pack, err := ParsePack(raw)

			if tt.wantFatalPath != "" {
				assertFatalIssue(t, pack, err, tt.wantFatalPath)
				return
			}

			if !tt.wantOK {
				t.Fatalf("test case %q sets neither wantFatalPath nor wantOK", tt.name)
			}
			assertParsedOK(t, pack, err, tt.wantWarnPath)
		})
	}
}

// TestParsePack_AppendixAGoldenPath parses the rules file's Appendix A
// example pack, padded with one extra song per bucket so the total song
// count is even (4 rather than 2) and neither (language x bucket) cell
// warns for holding only one song — an unambiguous happy path.
func TestParsePack_AppendixAGoldenPath(t *testing.T) {
	doc := map[string]any{
		"packName":   "Mixed Hits",
		"packAuthor": "Valentyn",
		"version":    2,
		"languages": []any{
			map[string]any{
				"name": "Ukrainian",
				"buckets": map[string]any{
					"4": []any{
						map[string]any{
							"id": "ua-song-1", "title": "Song Title", "artist": "Band",
							"line":    []any{"First", "second", "third", "fourth"},
							"youtube": map[string]any{"url": "https://www.youtube.com/watch?v=xxxxxxxxxxx", "startSec": 20, "endSec": 30},
						},
						map[string]any{
							"id": "ua-song-2", "title": "Song Title 2", "artist": "Band 2",
							"line": []any{"fifth", "sixth", "seventh", "eighth"},
						},
					},
				},
			},
			map[string]any{
				"name": "English",
				"buckets": map[string]any{
					"5": []any{
						map[string]any{
							"id": "queen-bohemian", "title": "Bohemian Rhapsody", "artist": "Queen",
							"line":    []any{"Is this", "the real", "life", "or just", "fantasy"},
							"youtube": map[string]any{"url": "https://www.youtube.com/watch?v=fJ9rUzIMcZQ", "startSec": 49, "endSec": 60},
						},
						map[string]any{
							"id": "en-song-2", "title": "Another Song", "artist": "Another Artist",
							"line": []any{"one", "two", "three", "four", "five"},
						},
					},
				},
			},
		},
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}

	pack, err := ParsePack(raw)
	if err != nil {
		t.Fatalf("ParsePack() error = %v, want nil", err)
	}
	if pack == nil {
		t.Fatal("ParsePack() pack = nil, want non-nil")
	}

	assertGoldenAggregates(t, pack)
	assertGoldenSong(t, pack)
}

// assertGoldenAggregates checks the pack-level counts and orderings the
// golden-path document is expected to produce.
func assertGoldenAggregates(t *testing.T, pack *Pack) {
	t.Helper()
	if got, want := pack.TotalSongs, 4; got != want {
		t.Errorf("TotalSongs = %d, want %d", got, want)
	}
	if got, want := pack.CountAt("Ukrainian", 4), 2; got != want {
		t.Errorf("CountAt(Ukrainian, 4) = %d, want %d", got, want)
	}
	if got, want := pack.CountAt("English", 5), 2; got != want {
		t.Errorf("CountAt(English, 5) = %d, want %d", got, want)
	}
	if got, want := pack.CountAt("French", 4), 0; got != want {
		t.Errorf("CountAt(French, 4) = %d, want %d", got, want)
	}
	if got, want := pack.BucketKeys(), []int{4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("BucketKeys() = %v, want %v", got, want)
	}
	if got, want := pack.LanguageNames(), []string{"Ukrainian", "English"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LanguageNames() = %v, want %v", got, want)
	}
}

// assertGoldenSong checks SongByID for both a present and an absent id.
func assertGoldenSong(t *testing.T, pack *Pack) {
	t.Helper()
	song := pack.SongByID("queen-bohemian")
	if song == nil {
		t.Fatal("SongByID(queen-bohemian) = nil, want a song")
	}
	if song.Title != "Bohemian Rhapsody" || song.Artist != "Queen" {
		t.Errorf("SongByID(queen-bohemian) = %+v, want title/artist Bohemian Rhapsody/Queen", song)
	}
	if song.Media == nil || song.Media.VideoID != "fJ9rUzIMcZQ" {
		t.Errorf("SongByID(queen-bohemian).Media = %+v, want VideoID fJ9rUzIMcZQ", song.Media)
	}
	if song.Media.StartSec != 49 || song.Media.EndSec != 60 {
		t.Errorf("SongByID(queen-bohemian).Media = %+v, want StartSec/EndSec 49/60", song.Media)
	}

	if got := pack.SongByID("does-not-exist"); got != nil {
		t.Errorf("SongByID(does-not-exist) = %+v, want nil", got)
	}
}

func TestParseVideoID(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantID string
		wantOK bool
	}{
		{"watch on youtube.com", "https://youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"watch on www.youtube.com", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"watch on m.youtube.com", "https://m.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"youtu.be short link", "https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"embed on www.youtube.com", "https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"embed on www.youtube-nocookie.com", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"10-char id rejected", "https://www.youtube.com/watch?v=dQw4w9WgXc", "", false},
		{"12-char id rejected", "https://www.youtube.com/watch?v=dQw4w9WgXcQQ", "", false},
		{"javascript scheme rejected", "javascript:alert(1)", "", false},
		{"bare id with no url rejected", "dQw4w9WgXcQ", "", false},
		{"http scheme rejected", "http://www.youtube.com/watch?v=dQw4w9WgXcQ", "", false},
		{"unrelated host rejected", "https://example.com/watch?v=dQw4w9WgXcQ", "", false},
		{"youtu.be with extra path segment rejected", "https://youtu.be/dQw4w9WgXcQ/extra", "", false},
		{"empty url rejected", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := parseVideoID(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("parseVideoID(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if ok && id != tt.wantID {
				t.Errorf("parseVideoID(%q) = %q, want %q", tt.raw, id, tt.wantID)
			}
		})
	}
}
