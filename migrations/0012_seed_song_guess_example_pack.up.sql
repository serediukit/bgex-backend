-- Seed the official "Mixed Hits (example)" pack (Step 5): a fixed-UUID
-- pack row plus its version 1 document, inlined verbatim from
-- internal/games/song_guess/packdata/example.v1.json (embedded in the
-- binary via go:embed -- see packdata.ExampleV1() / packdata.ExampleV1ID /
-- packdata.ExampleV1Version / packdata.ExampleV1Slug). Dollar-quoting avoids
-- hand-escaping quotes/apostrophes in the JSON body.
--
-- internal/games/song_guess/packdata/example_test.go asserts the embedded
-- JSON parses with zero fatal issues and that its derived TotalSongs/media
-- shape matches what this migration writes below (there is no live
-- database in this environment to run the migration against, so it is
-- UNVERIFIED against a real Postgres instance -- run `make migrate-up`
-- against a real database before relying on this in production).

INSERT INTO song_guess.packs (id, slug, name, author_label, created_by, is_official, is_public)
VALUES ('00000000-0000-0000-0000-0000000000f0', 'example-hits', 'Mixed Hits (example)', 'bgex', NULL, TRUE, TRUE)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO song_guess.pack_versions (pack_id, version, status, doc, doc_sha256, validated, total_songs, published_at)
VALUES ('00000000-0000-0000-0000-0000000000f0', 1, 'published',
        $sgjson$
{
  "packName": "Mixed Hits (example)",
  "packAuthor": "bgex",
  "version": 2,
  "languages": [
    {
      "name": "Ukrainian",
      "buckets": {
        "4": [
          {
            "id": "ua-song-1",
            "title": "Song Title",
            "artist": "Band",
            "line": ["First", "second", "third", "fourth"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=sgua0001xyz",
              "startSec": 20,
              "endSec": 30
            }
          },
          {
            "id": "ua-song-2",
            "title": "Друга пісня",
            "artist": "Гурт",
            "line": ["Один", "два", "три", "чотири"]
          }
        ],
        "5": [
          {
            "id": "ua-song-3",
            "title": "Третя пісня",
            "artist": "Гурт",
            "line": ["Раз", "два", "три", "чотири", "п'ять"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=sgua0002xyz",
              "startSec": 10,
              "endSec": 25
            }
          },
          {
            "id": "ua-song-4",
            "title": "Четверта пісня",
            "artist": "Гурт",
            "line": ["Ще", "одна", "коротка", "пісня", "тут"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=sgua0003xyz",
              "startSec": 5,
              "endSec": 20
            }
          }
        ]
      }
    },
    {
      "name": "English",
      "buckets": {
        "4": [
          {
            "id": "en-song-1",
            "title": "Song Four",
            "artist": "Artist A",
            "line": ["One", "two", "three", "four"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=sgen0001xyz",
              "startSec": 15,
              "endSec": 25
            }
          },
          {
            "id": "en-song-2",
            "title": "Song Four B",
            "artist": "Artist B",
            "line": ["Alpha", "Bravo", "Charlie", "Delta"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=sgen0002xyz",
              "startSec": 8,
              "endSec": 18
            }
          }
        ],
        "5": [
          {
            "id": "queen-bohemian",
            "title": "Bohemian Rhapsody",
            "artist": "Queen",
            "line": ["Is this", "the real", "life", "or just", "fantasy"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=fJ9rUzIMcZQ",
              "startSec": 49,
              "endSec": 60
            }
          },
          {
            "id": "en-song-3",
            "title": "Song Five B",
            "artist": "Artist C",
            "line": ["Is it", "really", "true", "or just", "a dream"],
            "youtube": {
              "url": "https://www.youtube.com/watch?v=sgen0003xyz",
              "startSec": 30,
              "endSec": 45
            }
          }
        ]
      }
    }
  ]
}
$sgjson$,
        '1ed22798c34b9108c5174214f50fedf76b882e117a5401fc0bc4d327ac3559ba', TRUE, 8, now());
