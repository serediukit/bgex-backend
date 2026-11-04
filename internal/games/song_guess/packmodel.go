package song_guess

import "sort"

// Pack is a fully parsed and validated song pack document (rules §4, pack
// format v2). Construct one via ParsePack — the zero value has no derived
// aggregates.
//
// Languages carries the selection table's row order (document order);
// within each Language, Buckets is normalized into an ascending-block-count
// slice — the wire document's map[string][]Song is the only map anywhere in
// this feature, and it dies right here (Req 4, Req 5).
type Pack struct {
	Name        string
	AuthorLabel string
	Languages   []Language
	TotalSongs  int
}

// Language is one row of the selection table (rules §5.1.1): a named group
// of songs, itself grouped into Buckets ordered by ascending block count.
type Language struct {
	Name    string
	Buckets []Bucket
}

// Bucket is a group of songs sharing the same block count (rules §2, §4.1).
// Songs is in document order.
type Bucket struct {
	Blocks int
	Songs  []Song
}

// Song is one song entry: a single line pre-sliced into Blocks (rules
// §4.1). Line[i] is an opaque block string, possibly multiple words (rules
// §2, Req 3) — points count blocks, never words.
type Song struct {
	ID     string
	Title  string
	Artist string
	Line   []string
	// Media is the song's optional YouTube fragment, or nil if the document
	// carried no "youtube" field. Set only once ParsePack has confirmed the
	// URL parses to a valid video id.
	Media *Media
}

// Media is a song's YouTube fragment, server-parsed to a bare VideoID —
// never a raw URL (Req 30, Q6). Only [StartSec, EndSec] ever plays.
type Media struct {
	VideoID  string
	StartSec int32
	EndSec   int32
}

// CountAt returns the number of songs stored under the given (language,
// blocks) intersection, or 0 if the language or bucket does not exist.
func (p *Pack) CountAt(language string, blocks int) int {
	for _, l := range p.Languages {
		if l.Name != language {
			continue
		}
		for _, b := range l.Buckets {
			if b.Blocks == blocks {
				return len(b.Songs)
			}
		}
		return 0
	}
	return 0
}

// SongByID returns the song with the given id, or nil if no song in the
// pack carries it. ParsePack rejects duplicate ids across the whole pack,
// so at most one match ever exists.
func (p *Pack) SongByID(id string) *Song {
	for _, l := range p.Languages {
		for _, b := range l.Buckets {
			for i := range b.Songs {
				if b.Songs[i].ID == id {
					return &b.Songs[i]
				}
			}
		}
	}
	return nil
}

// LanguageNames returns the pack's language names in document order — the
// selection table's row order (rules §5.1.1, Req 4).
func (p *Pack) LanguageNames() []string {
	names := make([]string, len(p.Languages))
	for i, l := range p.Languages {
		names[i] = l.Name
	}
	return names
}

// BucketKeys returns the ascending union of block counts across every
// language — the selection table's column order (rules §5.1.1, Req 4).
func (p *Pack) BucketKeys() []int {
	seen := make(map[int]bool)
	for _, l := range p.Languages {
		for _, b := range l.Buckets {
			seen[b.Blocks] = true
		}
	}
	keys := make([]int, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
