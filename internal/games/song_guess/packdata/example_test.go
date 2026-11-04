// Package packdata_test validates the embedded example pack document
// (Step 5) against the parser built in Step 3, and guards against drift
// between the embedded JSON and the SQL seed migration that inlines it.
//
// This lives in an external "packdata_test" package, not "song_guess_test"
// or a package-internal test, because it needs both song_guess.ParsePack
// (the parser, one directory up) and packdata.ExampleV1 (the embedded doc,
// this directory) in the same test binary. song_guess does not import
// packdata in production code, and packdata does not import song_guess, so
// packdata_test importing both creates no import cycle — exactly the shape
// internal/games/ttr/mapdata/europe_test.go (package mapdata_test,
// importing both ttr and ttr/mapdata) already uses for the identical
// problem.
package packdata_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/serediukit/bgex-backend/internal/games/song_guess"
	"github.com/serediukit/bgex-backend/internal/games/song_guess/packdata"
)

// wantTotalSongs is the song count migration 0012 writes into
// song_guess.pack_versions.total_songs for this exact document. Keeping it
// as a named constant makes the "matches what the migration writes"
// assertion below self-documenting.
const wantTotalSongs = 8

// TestExamplePackParses asserts the embedded example document passes every
// ParsePack validation (rules §4.2, Req 37-38) with zero fatal issues.
func TestExamplePackParses(t *testing.T) {
	pack, err := song_guess.ParsePack(packdata.ExampleV1())
	if pack == nil {
		t.Fatalf("ParsePack(packdata.ExampleV1()) returned nil pack, err:\n%v", err)
	}
	var verr *song_guess.ValidationError
	if errors.As(err, &verr) && len(verr.Fatal()) > 0 {
		t.Fatalf("ParsePack(packdata.ExampleV1()) had fatal issues: %v", verr.Fatal())
	}
}

// TestExamplePackSongCount asserts the pack's total song count is even (the
// parity validator, Req 5/38) and equal to the count migration 0012 writes
// into total_songs — a future edit to example.v1.json that changes the song
// count without updating the migration is exactly the drift this test
// exists to catch.
func TestExamplePackSongCount(t *testing.T) {
	pack := mustParseExample(t)

	if pack.TotalSongs%2 != 0 {
		t.Errorf("TotalSongs = %d, want an even number (parity validator)", pack.TotalSongs)
	}
	if pack.TotalSongs != wantTotalSongs {
		t.Errorf("TotalSongs = %d, want %d (must match migration 0012's total_songs literal)", pack.TotalSongs, wantTotalSongs)
	}
}

// TestExamplePackLineLengthsMatchBuckets asserts every song's line has
// exactly as many blocks as the bucket it is stored under (rules §4.2, Req
// 2) — ParsePack already enforces this fatally, but this documents the
// guarantee explicitly against the parsed Pack.
func TestExamplePackLineLengthsMatchBuckets(t *testing.T) {
	pack := mustParseExample(t)

	for _, lang := range pack.Languages {
		for _, bucket := range lang.Buckets {
			for _, s := range bucket.Songs {
				if len(s.Line) != bucket.Blocks {
					t.Errorf("language %q bucket %d: song %q has len(line) = %d, want %d",
						lang.Name, bucket.Blocks, s.ID, len(s.Line), bucket.Blocks)
				}
			}
		}
	}
}

// TestExamplePackHasBothMediaShapes asserts the seed exercises both the
// "with youtube" and "no player" (test 13) paths: at least one song has a
// server-parsed Media with a valid 11-char VideoID, and at least one has no
// Media at all.
func TestExamplePackHasBothMediaShapes(t *testing.T) {
	pack := mustParseExample(t)

	var withMedia, withoutMedia int
	for _, lang := range pack.Languages {
		for _, bucket := range lang.Buckets {
			for _, s := range bucket.Songs {
				if s.Media == nil {
					withoutMedia++
					continue
				}
				withMedia++
				if len(s.Media.VideoID) != 11 {
					t.Errorf("song %q: VideoID %q has length %d, want 11", s.ID, s.Media.VideoID, len(s.Media.VideoID))
				}
			}
		}
	}

	if withMedia == 0 {
		t.Error("no song in the example pack carries a youtube fragment; test 13's playback path has no data")
	}
	if withoutMedia == 0 {
		t.Error("every song in the example pack carries a youtube fragment; test 13's no-player path has no data")
	}
}

// TestExamplePackNoSingleSongCells asserts no (language x bucket) cell holds
// exactly one song — Req 38's soft warning condition — so the depletion
// scenario (test 9) is exercisable without also tripping the one-song
// warning on every cell.
func TestExamplePackNoSingleSongCells(t *testing.T) {
	pack := mustParseExample(t)

	for _, lang := range pack.Languages {
		for _, bucket := range lang.Buckets {
			if n := len(bucket.Songs); n == 1 {
				t.Errorf("language %q bucket %d: has exactly 1 song, which becomes unavailable after the first pick", lang.Name, bucket.Blocks)
			}
		}
	}
}

func mustParseExample(t *testing.T) *song_guess.Pack {
	t.Helper()
	pack, err := song_guess.ParsePack(packdata.ExampleV1())
	if pack == nil {
		t.Fatalf("ParsePack(packdata.ExampleV1()) returned nil pack, err:\n%v", err)
	}
	return pack
}

// TestSeedMigrationMatchesEmbeddedJSON reads
// migrations/0012_seed_song_guess_example_pack.up.sql from disk, extracts
// the JSON body between the $sgjson$ dollar-quote delimiters, and asserts
// it is byte-identical to packdata.ExampleV1() and that the doc_sha256
// literal in the SQL equals the sha256 of that same JSON. Byte-identity is
// the correct check here specifically: doc_sha256 is a hash of exact bytes,
// so the recorded sha is only valid if the SQL body and the embedded file
// are byte-for-byte the same input to sha256 — a semantic-only comparison
// could pass while the hash on the row was computed from different bytes.
// This is a source-to-source check performed on the two on-disk copies of
// the seed, before either ever reaches Postgres; it complements, rather
// than replaces, the semantic JSONB comparison already done against a live
// database (up -> down -> up round-tripped, and the stored `doc` was
// unmarshalled and compared field-for-field against packdata.ExampleV1() —
// see the Step 5 verification), which is the right way to compare a value
// read back from JSONB per the "JSONB doesn't preserve byte-for-byte
// formatting" hazard (root CLAUDE.md). Together the two checks guard both
// ends: this test catches the embedded file and the seed migration
// drifting apart on disk (e.g. a truncated copy-paste, or an edit to one
// without the other), and the live-database check catches Postgres
// altering the document in a way that would matter.
func TestSeedMigrationMatchesEmbeddedJSON(t *testing.T) {
	sqlPath := migrationPath(t, "0012_seed_song_guess_example_pack.up.sql")
	sql, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("read %s: %v", sqlPath, err)
	}

	const tag = "$sgjson$"
	body := string(sql)
	first := indexAfter(t, body, tag, 0)
	// The generator that authored this file places a newline immediately
	// after the opening tag; skip it so the extracted body starts exactly
	// like the embedded file (which starts with "{").
	if first < len(body) && body[first] == '\n' {
		first++
	}
	second := indexAfter(t, body, tag, first)
	extracted := body[first : second-len(tag)]

	want := string(packdata.ExampleV1())
	if extracted != want {
		t.Fatalf("migration JSON body does not match packdata.ExampleV1() byte-for-byte (extracted %d bytes, want %d bytes)",
			len(extracted), len(want))
	}

	wantSHA := sha256Hex(packdata.ExampleV1())
	if !containsLiteral(body, wantSHA) {
		t.Errorf("migration does not contain doc_sha256 literal %q (sha256 of packdata.ExampleV1())", wantSHA)
	}
}

// indexAfter returns the index immediately after the next occurrence of tag
// in s at or after from, failing the test if tag is not found.
func indexAfter(t *testing.T, s, tag string, from int) int {
	t.Helper()
	i := indexFrom(s, tag, from)
	if i < 0 {
		t.Fatalf("tag %q not found in migration starting at offset %d", tag, from)
	}
	return i + len(tag)
}

func indexFrom(s, substr string, from int) int {
	if from > len(s) {
		return -1
	}
	i := indexOf(s[from:], substr)
	if i < 0 {
		return -1
	}
	return i + from
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func containsLiteral(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

// sha256Hex returns the hex-encoded sha256 digest of doc, matching the
// encoding migration 0012's doc_sha256 literal uses.
func sha256Hex(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// migrationPath resolves the absolute path to a file under the repo's
// migrations/ directory, relative to this test file's own location
// (internal/games/song_guess/packdata/), so it works regardless of the test
// runner's working directory.
func migrationPath(t *testing.T, filename string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file:      internal/games/song_guess/packdata/example_test.go
	// repo root:      ../../../../
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	path := filepath.Join(root, "migrations", filename)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("resolved migration path %s does not exist: %v", path, err)
	}
	return path
}
