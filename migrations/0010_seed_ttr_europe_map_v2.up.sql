-- Seed Europe map_version 2 (Step 7 / requirement 23): the colour-corrected,
-- pixel-space-angle layout, inlined verbatim from
-- internal/games/ttr/mapdata/europe.v2.json (embedded in the binary via
-- go:embed -- see mapdata.EuropeV2). Dollar-quoting avoids hand-escaping
-- single quotes in the JSON body.
--
-- Relative to v1 (migration 0008): 15 route colours corrected against the
-- official board scan, a 9th double route added (Budapest-Wien, ids 45
-- White / 99 Red, mutually paired), and every slot angle regenerated with
-- the corrected pixel-space formula (see mapmodel.go's Slot doc comment).
-- v1 itself is untouched -- this INSERT only ever adds a new
-- (map_id, version) row, never modifies the existing one.
--
-- internal/games/ttr/mapdata/europe_v2_test.go asserts this file's JSON
-- body is *semantically* identical (parsed-structure comparison, never
-- byte comparison -- JSONB does not preserve byte-for-byte formatting) to
-- mapdata.EuropeV2, and that doc_sha256 below matches
-- ttr.DocSHA256(mapdata.EuropeV2).
--
-- The dev database already contains a hand-authored v2 row from the
-- editor session that produced this document (see
-- agents-workspace/plan/ttr-map-editor/europe-v2-notes.md). The row this
-- migration installs on a *fresh* database is byte-identical (in JSON
-- structure) to that hand-authored one, so re-applying this migration
-- against a database that already holds it must be a clean no-op.
--
-- That idempotency must not silently paper over a *different* pre-existing
-- (europe, 2) row -- e.g. a leftover, never-validated draft from an aborted
-- editor session occupying the version-2 slot. A bare
-- `ON CONFLICT (map_id, version) DO NOTHING` cannot tell that apart from the
-- safe case above: it would leave Europe pinned to v1 with a stray draft
-- sitting on version 2, silently, with no error and no log. The DO block
-- plus ON CONFLICT ... DO UPDATE below make the two cases behave
-- differently instead:
--   * an existing DRAFT is logged via RAISE NOTICE and then replaced by the
--     DO UPDATE, since a draft was never meant to be the final state of
--     this slot;
--   * an existing PUBLISHED row with a *different* doc_sha256 -- which
--     should never happen, since published rows are immutable -- aborts
--     the whole migration with RAISE EXCEPTION rather than silently
--     leaving the inconsistency in place;
--   * an existing PUBLISHED row with the *same* doc_sha256 -- the dev DB's
--     actual state -- trips neither branch above, and the DO UPDATE's
--     `WHERE status = 'draft'` guard then declines to touch a row that is
--     already published: a clean no-op, matching 0008's idempotent-insert
--     pattern for v1.
--
-- WARNING: this migration's down migration (0010_*.down.sql) fails if a
-- ttr.game_states row pins (map_id, map_version) = (europe, 2) -- the FK is
-- NO ACTION. This mirrors an existing, accepted limitation in 0008's down
-- migration for v1; see the comment there. Do not attempt to fix 0008.

DO $seed_check$
DECLARE
    existing ttr.map_versions%ROWTYPE;
BEGIN
    SELECT * INTO existing FROM ttr.map_versions
    WHERE map_id = '00000000-0000-0000-0000-0000000000e0' AND version = 2;

    IF FOUND THEN
        IF existing.status = 'published'
           AND existing.doc_sha256 <> '59de2e248d0cb4f6f0a5a59d6cba3393e63f3ac2fb5b75f59c76997879343548' THEN
            RAISE EXCEPTION 'ttr.map_versions (europe, 2) is already published with a different document (doc_sha256=%) -- refusing to silently diverge from the seeded Europe v2; resolve by hand', existing.doc_sha256;
        ELSIF existing.status = 'draft' THEN
            RAISE NOTICE 'ttr.map_versions (europe, 2) held a draft (doc_sha256=%, validated=%) before this migration; replacing it with the seeded published Europe v2', existing.doc_sha256, existing.validated;
        END IF;
    END IF;
END
$seed_check$;

INSERT INTO ttr.map_versions (map_id, version, status, doc, doc_sha256, validated, published_at)
VALUES ('00000000-0000-0000-0000-0000000000e0', 2, 'published',
        $ttrjson$
{
  "name": "Europe",
  "rules": {
    "cities": [
      {
        "id": "edinburgh",
        "name": "Edinburgh"
      },
      {
        "id": "london",
        "name": "London"
      },
      {
        "id": "amsterdam",
        "name": "Amsterdam"
      },
      {
        "id": "bruxelles",
        "name": "Bruxelles"
      },
      {
        "id": "dieppe",
        "name": "Dieppe"
      },
      {
        "id": "brest",
        "name": "Brest"
      },
      {
        "id": "paris",
        "name": "Paris"
      },
      {
        "id": "pamplona",
        "name": "Pamplona"
      },
      {
        "id": "madrid",
        "name": "Madrid"
      },
      {
        "id": "lisboa",
        "name": "Lisboa"
      },
      {
        "id": "cadiz",
        "name": "Cadiz"
      },
      {
        "id": "barcelona",
        "name": "Barcelona"
      },
      {
        "id": "marseille",
        "name": "Marseille"
      },
      {
        "id": "zurich",
        "name": "Zurich"
      },
      {
        "id": "frankfurt",
        "name": "Frankfurt"
      },
      {
        "id": "essen",
        "name": "Essen"
      },
      {
        "id": "kobenhavn",
        "name": "Kobenhavn"
      },
      {
        "id": "berlin",
        "name": "Berlin"
      },
      {
        "id": "munchen",
        "name": "Munchen"
      },
      {
        "id": "wien",
        "name": "Wien"
      },
      {
        "id": "venezia",
        "name": "Venezia"
      },
      {
        "id": "roma",
        "name": "Roma"
      },
      {
        "id": "palermo",
        "name": "Palermo"
      },
      {
        "id": "brindisi",
        "name": "Brindisi"
      },
      {
        "id": "zagrab",
        "name": "Zagrab"
      },
      {
        "id": "sarajevo",
        "name": "Sarajevo"
      },
      {
        "id": "budapest",
        "name": "Budapest"
      },
      {
        "id": "sofia",
        "name": "Sofia"
      },
      {
        "id": "athina",
        "name": "Athina"
      },
      {
        "id": "smyrna",
        "name": "Smyrna"
      },
      {
        "id": "constantinople",
        "name": "Constantinople"
      },
      {
        "id": "bucuresti",
        "name": "Bucuresti"
      },
      {
        "id": "kyiv",
        "name": "Kyiv"
      },
      {
        "id": "sevastopol",
        "name": "Sevastopol"
      },
      {
        "id": "angora",
        "name": "Angora"
      },
      {
        "id": "erzurum",
        "name": "Erzurum"
      },
      {
        "id": "sochi",
        "name": "Sochi"
      },
      {
        "id": "rostov",
        "name": "Rostov"
      },
      {
        "id": "kharkov",
        "name": "Kharkov"
      },
      {
        "id": "moskva",
        "name": "Moskva"
      },
      {
        "id": "smolensk",
        "name": "Smolensk"
      },
      {
        "id": "wilno",
        "name": "Wilno"
      },
      {
        "id": "petrograd",
        "name": "Petrograd"
      },
      {
        "id": "stockholm",
        "name": "Stockholm"
      },
      {
        "id": "riga",
        "name": "Riga"
      },
      {
        "id": "danzig",
        "name": "Danzig"
      },
      {
        "id": "warszawa",
        "name": "Warszawa"
      }
    ],
    "routes": [
      {
        "a": "edinburgh",
        "b": "london",
        "id": 1,
        "pair": 2,
        "color": "Orange",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "edinburgh",
        "b": "london",
        "id": 2,
        "pair": 1,
        "color": "Black",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "london",
        "b": "amsterdam",
        "id": 3,
        "pair": null,
        "color": "Gray",
        "locos": 2,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "london",
        "b": "dieppe",
        "id": 4,
        "pair": 5,
        "color": "Gray",
        "locos": 1,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "london",
        "b": "dieppe",
        "id": 5,
        "pair": 4,
        "color": "Gray",
        "locos": 1,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "brest",
        "b": "dieppe",
        "id": 6,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "brest",
        "b": "paris",
        "id": 7,
        "pair": null,
        "color": "Black",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "brest",
        "b": "pamplona",
        "id": 8,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "paris",
        "b": "dieppe",
        "id": 9,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 1,
        "tunnel": false
      },
      {
        "a": "paris",
        "b": "zurich",
        "id": 10,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 3,
        "tunnel": true
      },
      {
        "a": "paris",
        "b": "pamplona",
        "id": 11,
        "pair": 12,
        "color": "Blue",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "paris",
        "b": "pamplona",
        "id": 12,
        "pair": 11,
        "color": "Green",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "paris",
        "b": "marseille",
        "id": 13,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "bruxelles",
        "b": "dieppe",
        "id": 14,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "bruxelles",
        "b": "paris",
        "id": 15,
        "pair": 16,
        "color": "Yellow",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "bruxelles",
        "b": "paris",
        "id": 16,
        "pair": 15,
        "color": "Red",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "bruxelles",
        "b": "amsterdam",
        "id": 17,
        "pair": null,
        "color": "Black",
        "locos": 0,
        "length": 1,
        "tunnel": false
      },
      {
        "a": "bruxelles",
        "b": "frankfurt",
        "id": 18,
        "pair": null,
        "color": "White",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "paris",
        "b": "frankfurt",
        "id": 19,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "marseille",
        "b": "pamplona",
        "id": 20,
        "pair": null,
        "color": "Red",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "marseille",
        "b": "barcelona",
        "id": 21,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "pamplona",
        "b": "barcelona",
        "id": 22,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "zurich",
        "b": "marseille",
        "id": 23,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "marseille",
        "b": "roma",
        "id": 24,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": true
      },
      {
        "a": "lisboa",
        "b": "cadiz",
        "id": 25,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "madrid",
        "b": "lisboa",
        "id": 26,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "madrid",
        "b": "cadiz",
        "id": 27,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "madrid",
        "b": "pamplona",
        "id": 28,
        "pair": 29,
        "color": "Black",
        "locos": 0,
        "length": 3,
        "tunnel": true
      },
      {
        "a": "madrid",
        "b": "pamplona",
        "id": 29,
        "pair": 28,
        "color": "White",
        "locos": 0,
        "length": 3,
        "tunnel": true
      },
      {
        "a": "madrid",
        "b": "barcelona",
        "id": 30,
        "pair": null,
        "color": "Yellow",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "amsterdam",
        "b": "essen",
        "id": 31,
        "pair": null,
        "color": "Yellow",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "amsterdam",
        "b": "frankfurt",
        "id": 32,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "frankfurt",
        "b": "essen",
        "id": 33,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "essen",
        "b": "berlin",
        "id": 34,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "kobenhavn",
        "b": "essen",
        "id": 35,
        "pair": null,
        "color": "Gray",
        "locos": 1,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "frankfurt",
        "b": "berlin",
        "id": 36,
        "pair": 37,
        "color": "Black",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "frankfurt",
        "b": "berlin",
        "id": 37,
        "pair": 36,
        "color": "Red",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "frankfurt",
        "b": "munchen",
        "id": 38,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "munchen",
        "b": "zurich",
        "id": 39,
        "pair": null,
        "color": "Yellow",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "munchen",
        "b": "venezia",
        "id": 40,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "munchen",
        "b": "wien",
        "id": 41,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "zurich",
        "b": "venezia",
        "id": 42,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "berlin",
        "b": "wien",
        "id": 43,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "wien",
        "b": "zagrab",
        "id": 44,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "budapest",
        "b": "wien",
        "id": 45,
        "pair": 99,
        "color": "White",
        "locos": 0,
        "length": 1,
        "tunnel": false
      },
      {
        "a": "budapest",
        "b": "zagrab",
        "id": 46,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "berlin",
        "b": "warszawa",
        "id": 47,
        "pair": 48,
        "color": "Purple",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "berlin",
        "b": "warszawa",
        "id": 48,
        "pair": 47,
        "color": "Yellow",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "berlin",
        "b": "danzig",
        "id": 49,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "warszawa",
        "b": "wien",
        "id": 50,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "venezia",
        "b": "roma",
        "id": 51,
        "pair": null,
        "color": "Black",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "venezia",
        "b": "zagrab",
        "id": 52,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "roma",
        "b": "brindisi",
        "id": 53,
        "pair": null,
        "color": "White",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "roma",
        "b": "palermo",
        "id": 54,
        "pair": null,
        "color": "Gray",
        "locos": 1,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "brindisi",
        "b": "palermo",
        "id": 55,
        "pair": null,
        "color": "Gray",
        "locos": 1,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "brindisi",
        "b": "athina",
        "id": 56,
        "pair": null,
        "color": "Gray",
        "locos": 1,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "palermo",
        "b": "smyrna",
        "id": 57,
        "pair": null,
        "color": "Gray",
        "locos": 2,
        "length": 6,
        "tunnel": false
      },
      {
        "a": "zagrab",
        "b": "sarajevo",
        "id": 58,
        "pair": null,
        "color": "Red",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "budapest",
        "b": "sarajevo",
        "id": 59,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "budapest",
        "b": "kyiv",
        "id": 60,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 6,
        "tunnel": true
      },
      {
        "a": "bucuresti",
        "b": "budapest",
        "id": 61,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": true
      },
      {
        "a": "bucuresti",
        "b": "kyiv",
        "id": 62,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "kyiv",
        "b": "warszawa",
        "id": 63,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "sarajevo",
        "b": "athina",
        "id": 64,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "sarajevo",
        "b": "sofia",
        "id": 65,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "sofia",
        "b": "constantinople",
        "id": 66,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "bucuresti",
        "b": "constantinople",
        "id": 67,
        "pair": null,
        "color": "Yellow",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "bucuresti",
        "b": "sofia",
        "id": 68,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "athina",
        "b": "sofia",
        "id": 69,
        "pair": null,
        "color": "Purple",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "athina",
        "b": "smyrna",
        "id": 70,
        "pair": null,
        "color": "Gray",
        "locos": 1,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "sevastopol",
        "b": "bucuresti",
        "id": 71,
        "pair": null,
        "color": "White",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "constantinople",
        "b": "smyrna",
        "id": 72,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "constantinople",
        "b": "angora",
        "id": 73,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": true
      },
      {
        "a": "smyrna",
        "b": "angora",
        "id": 74,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 3,
        "tunnel": true
      },
      {
        "a": "angora",
        "b": "erzurum",
        "id": 75,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "sevastopol",
        "b": "constantinople",
        "id": 76,
        "pair": null,
        "color": "Gray",
        "locos": 2,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "sevastopol",
        "b": "erzurum",
        "id": 77,
        "pair": null,
        "color": "Gray",
        "locos": 2,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "sochi",
        "b": "erzurum",
        "id": 78,
        "pair": null,
        "color": "Red",
        "locos": 0,
        "length": 3,
        "tunnel": true
      },
      {
        "a": "sevastopol",
        "b": "sochi",
        "id": 79,
        "pair": null,
        "color": "Gray",
        "locos": 1,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "sochi",
        "b": "rostov",
        "id": 80,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "sevastopol",
        "b": "rostov",
        "id": 81,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "rostov",
        "b": "kharkov",
        "id": 82,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "kharkov",
        "b": "kyiv",
        "id": 83,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "kharkov",
        "b": "moskva",
        "id": 84,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "moskva",
        "b": "smolensk",
        "id": 85,
        "pair": null,
        "color": "Orange",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "moskva",
        "b": "petrograd",
        "id": 86,
        "pair": null,
        "color": "White",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "petrograd",
        "b": "wilno",
        "id": 87,
        "pair": null,
        "color": "Blue",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "petrograd",
        "b": "riga",
        "id": 88,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "petrograd",
        "b": "stockholm",
        "id": 89,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 8,
        "tunnel": true
      },
      {
        "a": "stockholm",
        "b": "kobenhavn",
        "id": 90,
        "pair": 91,
        "color": "Yellow",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "stockholm",
        "b": "kobenhavn",
        "id": 91,
        "pair": 90,
        "color": "White",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "riga",
        "b": "danzig",
        "id": 92,
        "pair": null,
        "color": "Black",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "danzig",
        "b": "warszawa",
        "id": 93,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "warszawa",
        "b": "wilno",
        "id": 94,
        "pair": null,
        "color": "Red",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "wilno",
        "b": "riga",
        "id": 95,
        "pair": null,
        "color": "Green",
        "locos": 0,
        "length": 4,
        "tunnel": false
      },
      {
        "a": "wilno",
        "b": "smolensk",
        "id": 96,
        "pair": null,
        "color": "Yellow",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "wilno",
        "b": "kyiv",
        "id": 97,
        "pair": null,
        "color": "Gray",
        "locos": 0,
        "length": 2,
        "tunnel": false
      },
      {
        "a": "smolensk",
        "b": "kyiv",
        "id": 98,
        "pair": null,
        "color": "Red",
        "locos": 0,
        "length": 3,
        "tunnel": false
      },
      {
        "a": "budapest",
        "b": "wien",
        "id": 99,
        "pair": 45,
        "color": "Red",
        "locos": 0,
        "length": 1,
        "tunnel": false
      }
    ],
    "players": {
      "max": 5,
      "min": 2
    },
    "tickets": [
      {
        "a": "lisboa",
        "b": "danzig",
        "id": 1,
        "long": true,
        "points": 20
      },
      {
        "a": "brest",
        "b": "petrograd",
        "id": 2,
        "long": true,
        "points": 20
      },
      {
        "a": "palermo",
        "b": "moskva",
        "id": 3,
        "long": true,
        "points": 20
      },
      {
        "a": "kobenhavn",
        "b": "erzurum",
        "id": 4,
        "long": true,
        "points": 21
      },
      {
        "a": "edinburgh",
        "b": "athina",
        "id": 5,
        "long": true,
        "points": 21
      },
      {
        "a": "cadiz",
        "b": "stockholm",
        "id": 6,
        "long": true,
        "points": 21
      },
      {
        "a": "athina",
        "b": "angora",
        "id": 7,
        "long": false,
        "points": 5
      },
      {
        "a": "budapest",
        "b": "sofia",
        "id": 8,
        "long": false,
        "points": 5
      },
      {
        "a": "frankfurt",
        "b": "kobenhavn",
        "id": 9,
        "long": false,
        "points": 5
      },
      {
        "a": "rostov",
        "b": "erzurum",
        "id": 10,
        "long": false,
        "points": 5
      },
      {
        "a": "sofia",
        "b": "smyrna",
        "id": 11,
        "long": false,
        "points": 5
      },
      {
        "a": "kyiv",
        "b": "petrograd",
        "id": 12,
        "long": false,
        "points": 6
      },
      {
        "a": "zurich",
        "b": "brindisi",
        "id": 13,
        "long": false,
        "points": 6
      },
      {
        "a": "zurich",
        "b": "budapest",
        "id": 14,
        "long": false,
        "points": 6
      },
      {
        "a": "warszawa",
        "b": "smolensk",
        "id": 15,
        "long": false,
        "points": 6
      },
      {
        "a": "zagrab",
        "b": "brindisi",
        "id": 16,
        "long": false,
        "points": 6
      },
      {
        "a": "paris",
        "b": "zagrab",
        "id": 17,
        "long": false,
        "points": 7
      },
      {
        "a": "brest",
        "b": "marseille",
        "id": 18,
        "long": false,
        "points": 7
      },
      {
        "a": "london",
        "b": "berlin",
        "id": 19,
        "long": false,
        "points": 7
      },
      {
        "a": "edinburgh",
        "b": "paris",
        "id": 20,
        "long": false,
        "points": 7
      },
      {
        "a": "amsterdam",
        "b": "pamplona",
        "id": 21,
        "long": false,
        "points": 7
      },
      {
        "a": "roma",
        "b": "smyrna",
        "id": 22,
        "long": false,
        "points": 8
      },
      {
        "a": "palermo",
        "b": "constantinople",
        "id": 23,
        "long": false,
        "points": 8
      },
      {
        "a": "sarajevo",
        "b": "sevastopol",
        "id": 24,
        "long": false,
        "points": 8
      },
      {
        "a": "madrid",
        "b": "dieppe",
        "id": 25,
        "long": false,
        "points": 8
      },
      {
        "a": "barcelona",
        "b": "bruxelles",
        "id": 26,
        "long": false,
        "points": 8
      },
      {
        "a": "paris",
        "b": "wien",
        "id": 27,
        "long": false,
        "points": 8
      },
      {
        "a": "barcelona",
        "b": "munchen",
        "id": 28,
        "long": false,
        "points": 8
      },
      {
        "a": "brest",
        "b": "venezia",
        "id": 29,
        "long": false,
        "points": 8
      },
      {
        "a": "smolensk",
        "b": "rostov",
        "id": 30,
        "long": false,
        "points": 8
      },
      {
        "a": "marseille",
        "b": "essen",
        "id": 31,
        "long": false,
        "points": 8
      },
      {
        "a": "kyiv",
        "b": "sochi",
        "id": 32,
        "long": false,
        "points": 8
      },
      {
        "a": "madrid",
        "b": "zurich",
        "id": 33,
        "long": false,
        "points": 8
      },
      {
        "a": "berlin",
        "b": "bucuresti",
        "id": 34,
        "long": false,
        "points": 8
      },
      {
        "a": "bruxelles",
        "b": "danzig",
        "id": 35,
        "long": false,
        "points": 9
      },
      {
        "a": "berlin",
        "b": "roma",
        "id": 36,
        "long": false,
        "points": 9
      },
      {
        "a": "angora",
        "b": "kharkov",
        "id": 37,
        "long": false,
        "points": 10
      },
      {
        "a": "riga",
        "b": "bucuresti",
        "id": 38,
        "long": false,
        "points": 10
      },
      {
        "a": "essen",
        "b": "kyiv",
        "id": 39,
        "long": false,
        "points": 10
      },
      {
        "a": "venezia",
        "b": "constantinople",
        "id": 40,
        "long": false,
        "points": 10
      },
      {
        "a": "london",
        "b": "wien",
        "id": 41,
        "long": false,
        "points": 10
      },
      {
        "a": "athina",
        "b": "wilno",
        "id": 42,
        "long": false,
        "points": 11
      },
      {
        "a": "stockholm",
        "b": "wien",
        "id": 43,
        "long": false,
        "points": 11
      },
      {
        "a": "berlin",
        "b": "moskva",
        "id": 44,
        "long": false,
        "points": 12
      },
      {
        "a": "amsterdam",
        "b": "wilno",
        "id": 45,
        "long": false,
        "points": 12
      },
      {
        "a": "frankfurt",
        "b": "smolensk",
        "id": 46,
        "long": false,
        "points": 13
      }
    ],
    "trains_per_player": 45,
    "stations_per_player": 3
  },
  "layout": {
    "slot": {
      "width": 0.022,
      "height": 0.009,
      "corner_radius": 0.003
    },
    "cities": {
      "kyiv": {
        "x": 0.7523,
        "y": 0.4165,
        "label_anchor": "n"
      },
      "riga": {
        "x": 0.6404,
        "y": 0.1721,
        "label_anchor": "n"
      },
      "roma": {
        "x": 0.4378,
        "y": 0.7381,
        "label_anchor": "n"
      },
      "wien": {
        "x": 0.5053,
        "y": 0.5008,
        "label_anchor": "n"
      },
      "brest": {
        "x": 0.1412,
        "y": 0.494,
        "label_anchor": "n"
      },
      "cadiz": {
        "x": 0.1098,
        "y": 0.94,
        "label_anchor": "n"
      },
      "essen": {
        "x": 0.3419,
        "y": 0.3785,
        "label_anchor": "n"
      },
      "paris": {
        "x": 0.2606,
        "y": 0.4763,
        "label_anchor": "n"
      },
      "sochi": {
        "x": 0.9133,
        "y": 0.6745,
        "label_anchor": "n"
      },
      "sofia": {
        "x": 0.6266,
        "y": 0.708,
        "label_anchor": "n"
      },
      "wilno": {
        "x": 0.6609,
        "y": 0.2571,
        "label_anchor": "n"
      },
      "angora": {
        "x": 0.7932,
        "y": 0.8121,
        "label_anchor": "n"
      },
      "athina": {
        "x": 0.6338,
        "y": 0.8855,
        "label_anchor": "n"
      },
      "berlin": {
        "x": 0.4535,
        "y": 0.3387,
        "label_anchor": "n"
      },
      "danzig": {
        "x": 0.5451,
        "y": 0.2698,
        "label_anchor": "n"
      },
      "dieppe": {
        "x": 0.2384,
        "y": 0.4361,
        "label_anchor": "n"
      },
      "lisboa": {
        "x": 0.06,
        "y": 0.8576,
        "label_anchor": "n"
      },
      "london": {
        "x": 0.2173,
        "y": 0.3766,
        "label_anchor": "n"
      },
      "madrid": {
        "x": 0.155,
        "y": 0.7937,
        "label_anchor": "n"
      },
      "moskva": {
        "x": 0.8763,
        "y": 0.2168,
        "label_anchor": "n"
      },
      "rostov": {
        "x": 0.9124,
        "y": 0.5372,
        "label_anchor": "n"
      },
      "smyrna": {
        "x": 0.6933,
        "y": 0.8689,
        "label_anchor": "n"
      },
      "zagrab": {
        "x": 0.4985,
        "y": 0.5906,
        "label_anchor": "n"
      },
      "zurich": {
        "x": 0.3686,
        "y": 0.532,
        "label_anchor": "n"
      },
      "erzurum": {
        "x": 0.94,
        "y": 0.8121,
        "label_anchor": "n"
      },
      "kharkov": {
        "x": 0.852,
        "y": 0.4338,
        "label_anchor": "n"
      },
      "munchen": {
        "x": 0.4217,
        "y": 0.5034,
        "label_anchor": "n"
      },
      "palermo": {
        "x": 0.4528,
        "y": 0.8802,
        "label_anchor": "n"
      },
      "venezia": {
        "x": 0.4346,
        "y": 0.6049,
        "label_anchor": "n"
      },
      "brindisi": {
        "x": 0.5326,
        "y": 0.7854,
        "label_anchor": "n"
      },
      "budapest": {
        "x": 0.5519,
        "y": 0.5275,
        "label_anchor": "n"
      },
      "pamplona": {
        "x": 0.1908,
        "y": 0.7038,
        "label_anchor": "n"
      },
      "sarajevo": {
        "x": 0.5409,
        "y": 0.6643,
        "label_anchor": "n"
      },
      "smolensk": {
        "x": 0.7789,
        "y": 0.2533,
        "label_anchor": "n"
      },
      "warszawa": {
        "x": 0.5863,
        "y": 0.3496,
        "label_anchor": "n"
      },
      "amsterdam": {
        "x": 0.3051,
        "y": 0.3439,
        "label_anchor": "n"
      },
      "barcelona": {
        "x": 0.2574,
        "y": 0.7572,
        "label_anchor": "n"
      },
      "bruxelles": {
        "x": 0.2955,
        "y": 0.4015,
        "label_anchor": "n"
      },
      "bucuresti": {
        "x": 0.6752,
        "y": 0.6429,
        "label_anchor": "n"
      },
      "edinburgh": {
        "x": 0.1639,
        "y": 0.2097,
        "label_anchor": "n"
      },
      "frankfurt": {
        "x": 0.3711,
        "y": 0.4293,
        "label_anchor": "n"
      },
      "kobenhavn": {
        "x": 0.439,
        "y": 0.2198,
        "label_anchor": "n"
      },
      "marseille": {
        "x": 0.3133,
        "y": 0.6854,
        "label_anchor": "n"
      },
      "petrograd": {
        "x": 0.7492,
        "y": 0.06,
        "label_anchor": "n"
      },
      "stockholm": {
        "x": 0.535,
        "y": 0.0826,
        "label_anchor": "n"
      },
      "sevastopol": {
        "x": 0.8049,
        "y": 0.6358,
        "label_anchor": "n"
      },
      "constantinople": {
        "x": 0.7255,
        "y": 0.7715,
        "label_anchor": "n"
      }
    },
    "routes": {
      "1": {
        "bend": 0,
        "slots": [
          {
            "x": 0.176,
            "y": 0.2269,
            "angle": 65
          },
          {
            "x": 0.1894,
            "y": 0.2686,
            "angle": 65
          },
          {
            "x": 0.2027,
            "y": 0.3103,
            "angle": 65
          },
          {
            "x": 0.2161,
            "y": 0.3521,
            "angle": 65
          }
        ],
        "offset": -0.006
      },
      "2": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1651,
            "y": 0.2342,
            "angle": 65
          },
          {
            "x": 0.1785,
            "y": 0.276,
            "angle": 65
          },
          {
            "x": 0.1918,
            "y": 0.3177,
            "angle": 65
          },
          {
            "x": 0.2052,
            "y": 0.3594,
            "angle": 65
          }
        ],
        "offset": 0.006
      },
      "3": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2393,
            "y": 0.3684,
            "angle": -14.4
          },
          {
            "x": 0.2831,
            "y": 0.3521,
            "angle": -14.4
          }
        ],
        "offset": 0
      },
      "4": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2279,
            "y": 0.3875,
            "angle": 62.7
          },
          {
            "x": 0.2385,
            "y": 0.4172,
            "angle": 62.7
          }
        ],
        "offset": -0.006
      },
      "5": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2172,
            "y": 0.3955,
            "angle": 62.7
          },
          {
            "x": 0.2278,
            "y": 0.4252,
            "angle": 62.7
          }
        ],
        "offset": 0.006
      },
      "6": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1655,
            "y": 0.4795,
            "angle": -22.3
          },
          {
            "x": 0.2141,
            "y": 0.4506,
            "angle": -22.3
          }
        ],
        "offset": 0
      },
      "7": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1611,
            "y": 0.4911,
            "angle": -5.8
          },
          {
            "x": 0.2009,
            "y": 0.4852,
            "angle": -5.8
          },
          {
            "x": 0.2407,
            "y": 0.4792,
            "angle": -5.8
          }
        ],
        "offset": 0
      },
      "8": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1474,
            "y": 0.5202,
            "angle": 71
          },
          {
            "x": 0.1598,
            "y": 0.5727,
            "angle": 71
          },
          {
            "x": 0.1722,
            "y": 0.6251,
            "angle": 71
          },
          {
            "x": 0.1846,
            "y": 0.6776,
            "angle": 71
          }
        ],
        "offset": 0
      },
      "9": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2495,
            "y": 0.4562,
            "angle": -128.8
          }
        ],
        "offset": 0
      },
      "10": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2786,
            "y": 0.4856,
            "angle": 19.5
          },
          {
            "x": 0.3146,
            "y": 0.5042,
            "angle": 19.5
          },
          {
            "x": 0.3506,
            "y": 0.5227,
            "angle": 19.5
          }
        ],
        "offset": 0
      },
      "11": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2574,
            "y": 0.5083,
            "angle": 114
          },
          {
            "x": 0.2399,
            "y": 0.5652,
            "angle": 114
          },
          {
            "x": 0.2225,
            "y": 0.622,
            "angle": 114
          },
          {
            "x": 0.205,
            "y": 0.6789,
            "angle": 114
          }
        ],
        "offset": -0.006
      },
      "12": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2464,
            "y": 0.5012,
            "angle": 114
          },
          {
            "x": 0.2289,
            "y": 0.5581,
            "angle": 114
          },
          {
            "x": 0.2115,
            "y": 0.6149,
            "angle": 114
          },
          {
            "x": 0.194,
            "y": 0.6718,
            "angle": 114
          }
        ],
        "offset": 0.006
      },
      "13": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2672,
            "y": 0.5024,
            "angle": 69.9
          },
          {
            "x": 0.2804,
            "y": 0.5547,
            "angle": 69.9
          },
          {
            "x": 0.2935,
            "y": 0.607,
            "angle": 69.9
          },
          {
            "x": 0.3067,
            "y": 0.6593,
            "angle": 69.9
          }
        ],
        "offset": 0
      },
      "14": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2812,
            "y": 0.4102,
            "angle": 157.4
          },
          {
            "x": 0.2527,
            "y": 0.4275,
            "angle": 157.4
          }
        ],
        "offset": 0
      },
      "15": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2917,
            "y": 0.4251,
            "angle": 124.2
          },
          {
            "x": 0.2743,
            "y": 0.4625,
            "angle": 124.2
          }
        ],
        "offset": -0.006
      },
      "16": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2818,
            "y": 0.4153,
            "angle": 124.2
          },
          {
            "x": 0.2644,
            "y": 0.4527,
            "angle": 124.2
          }
        ],
        "offset": 0.006
      },
      "17": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3003,
            "y": 0.3727,
            "angle": -76.4
          }
        ],
        "offset": 0
      },
      "18": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3144,
            "y": 0.4085,
            "angle": 14.2
          },
          {
            "x": 0.3522,
            "y": 0.4224,
            "angle": 14.2
          }
        ],
        "offset": 0
      },
      "19": {
        "bend": 0,
        "slots": [
          {
            "x": 0.279,
            "y": 0.4685,
            "angle": -16.3
          },
          {
            "x": 0.3159,
            "y": 0.4528,
            "angle": -16.3
          },
          {
            "x": 0.3527,
            "y": 0.4371,
            "angle": -16.3
          }
        ],
        "offset": 0
      },
      "20": {
        "bend": 0,
        "slots": [
          {
            "x": 0.298,
            "y": 0.6877,
            "angle": 174.1
          },
          {
            "x": 0.2674,
            "y": 0.6923,
            "angle": 174.1
          },
          {
            "x": 0.2367,
            "y": 0.6969,
            "angle": 174.1
          },
          {
            "x": 0.2061,
            "y": 0.7015,
            "angle": 174.1
          }
        ],
        "offset": 0
      },
      "21": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3063,
            "y": 0.6944,
            "angle": 138.6
          },
          {
            "x": 0.2923,
            "y": 0.7123,
            "angle": 138.6
          },
          {
            "x": 0.2784,
            "y": 0.7303,
            "angle": 138.6
          },
          {
            "x": 0.2644,
            "y": 0.7482,
            "angle": 138.6
          }
        ],
        "offset": 0
      },
      "22": {
        "bend": 0,
        "slots": [
          {
            "x": 0.2074,
            "y": 0.7171,
            "angle": 28.9
          },
          {
            "x": 0.2408,
            "y": 0.7438,
            "angle": 28.9
          }
        ],
        "offset": 0
      },
      "23": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3548,
            "y": 0.5704,
            "angle": 117.7
          },
          {
            "x": 0.3271,
            "y": 0.6471,
            "angle": 117.7
          }
        ],
        "offset": 0
      },
      "24": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3289,
            "y": 0.692,
            "angle": 16.2
          },
          {
            "x": 0.36,
            "y": 0.7052,
            "angle": 16.2
          },
          {
            "x": 0.3911,
            "y": 0.7183,
            "angle": 16.2
          },
          {
            "x": 0.4222,
            "y": 0.7315,
            "angle": 16.2
          }
        ],
        "offset": 0
      },
      "25": {
        "bend": 0,
        "slots": [
          {
            "x": 0.0724,
            "y": 0.8782,
            "angle": 48.7
          },
          {
            "x": 0.0973,
            "y": 0.9194,
            "angle": 48.7
          }
        ],
        "offset": 0
      },
      "26": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1392,
            "y": 0.8044,
            "angle": 155.2
          },
          {
            "x": 0.1075,
            "y": 0.8256,
            "angle": 155.2
          },
          {
            "x": 0.0758,
            "y": 0.847,
            "angle": 155.2
          }
        ],
        "offset": 0
      },
      "27": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1475,
            "y": 0.8181,
            "angle": 114.2
          },
          {
            "x": 0.1324,
            "y": 0.8669,
            "angle": 114.2
          },
          {
            "x": 0.1173,
            "y": 0.9156,
            "angle": 114.2
          }
        ],
        "offset": 0
      },
      "28": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1558,
            "y": 0.7743,
            "angle": -59.9
          },
          {
            "x": 0.1677,
            "y": 0.7444,
            "angle": -59.9
          },
          {
            "x": 0.1796,
            "y": 0.7144,
            "angle": -59.9
          }
        ],
        "offset": -0.006
      },
      "29": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1662,
            "y": 0.7831,
            "angle": -59.9
          },
          {
            "x": 0.1781,
            "y": 0.7531,
            "angle": -59.9
          },
          {
            "x": 0.19,
            "y": 0.7232,
            "angle": -59.9
          }
        ],
        "offset": 0.006
      },
      "30": {
        "bend": 0,
        "slots": [
          {
            "x": 0.1806,
            "y": 0.7846,
            "angle": -13.8
          },
          {
            "x": 0.2318,
            "y": 0.7663,
            "angle": -13.8
          }
        ],
        "offset": 0
      },
      "31": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3112,
            "y": 0.3497,
            "angle": 32.9
          },
          {
            "x": 0.3235,
            "y": 0.3612,
            "angle": 32.9
          },
          {
            "x": 0.3358,
            "y": 0.3727,
            "angle": 32.9
          }
        ],
        "offset": 0
      },
      "32": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3216,
            "y": 0.3652,
            "angle": 41.7
          },
          {
            "x": 0.3546,
            "y": 0.408,
            "angle": 41.7
          }
        ],
        "offset": 0
      },
      "33": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3638,
            "y": 0.4166,
            "angle": -129.9
          },
          {
            "x": 0.3492,
            "y": 0.3912,
            "angle": -129.9
          }
        ],
        "offset": 0
      },
      "34": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3698,
            "y": 0.3686,
            "angle": -13.8
          },
          {
            "x": 0.4256,
            "y": 0.3486,
            "angle": -13.8
          }
        ],
        "offset": 0
      },
      "35": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4228,
            "y": 0.2463,
            "angle": 131.7
          },
          {
            "x": 0.3905,
            "y": 0.2991,
            "angle": 131.7
          },
          {
            "x": 0.3581,
            "y": 0.352,
            "angle": 131.7
          }
        ],
        "offset": 0
      },
      "36": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3812,
            "y": 0.4072,
            "angle": -37.1
          },
          {
            "x": 0.4087,
            "y": 0.377,
            "angle": -37.1
          },
          {
            "x": 0.4361,
            "y": 0.3468,
            "angle": -37.1
          }
        ],
        "offset": -0.006
      },
      "37": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3885,
            "y": 0.4212,
            "angle": -37.1
          },
          {
            "x": 0.4159,
            "y": 0.391,
            "angle": -37.1
          },
          {
            "x": 0.4434,
            "y": 0.3608,
            "angle": -37.1
          }
        ],
        "offset": 0.006
      },
      "38": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3838,
            "y": 0.4478,
            "angle": 45.2
          },
          {
            "x": 0.4091,
            "y": 0.4849,
            "angle": 45.2
          }
        ],
        "offset": 0
      },
      "39": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4084,
            "y": 0.5106,
            "angle": 159.7
          },
          {
            "x": 0.3819,
            "y": 0.5249,
            "angle": 159.7
          }
        ],
        "offset": 0
      },
      "40": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4249,
            "y": 0.5288,
            "angle": 79.5
          },
          {
            "x": 0.4314,
            "y": 0.5795,
            "angle": 79.5
          }
        ],
        "offset": 0
      },
      "41": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4356,
            "y": 0.503,
            "angle": -1.2
          },
          {
            "x": 0.4635,
            "y": 0.5021,
            "angle": -1.2
          },
          {
            "x": 0.4914,
            "y": 0.5012,
            "angle": -1.2
          }
        ],
        "offset": 0
      },
      "42": {
        "bend": 0,
        "slots": [
          {
            "x": 0.3851,
            "y": 0.5502,
            "angle": 37.2
          },
          {
            "x": 0.4181,
            "y": 0.5867,
            "angle": 37.2
          }
        ],
        "offset": 0
      },
      "43": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4621,
            "y": 0.3657,
            "angle": 65.1
          },
          {
            "x": 0.4794,
            "y": 0.4198,
            "angle": 65.1
          },
          {
            "x": 0.4967,
            "y": 0.4738,
            "angle": 65.1
          }
        ],
        "offset": 0
      },
      "44": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5036,
            "y": 0.5233,
            "angle": 96.3
          },
          {
            "x": 0.5002,
            "y": 0.5681,
            "angle": 96.3
          }
        ],
        "offset": 0
      },
      "45": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5264,
            "y": 0.5223,
            "angle": -158.5
          }
        ],
        "offset": -0.006
      },
      "46": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5386,
            "y": 0.5433,
            "angle": 140.9
          },
          {
            "x": 0.5119,
            "y": 0.5748,
            "angle": 140.9
          }
        ],
        "offset": 0
      },
      "47": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4704,
            "y": 0.3313,
            "angle": 3.2
          },
          {
            "x": 0.5036,
            "y": 0.3341,
            "angle": 3.2
          },
          {
            "x": 0.5368,
            "y": 0.3368,
            "angle": 3.2
          },
          {
            "x": 0.57,
            "y": 0.3395,
            "angle": 3.2
          }
        ],
        "offset": -0.006
      },
      "48": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4698,
            "y": 0.3488,
            "angle": 3.2
          },
          {
            "x": 0.503,
            "y": 0.3515,
            "angle": 3.2
          },
          {
            "x": 0.5362,
            "y": 0.3542,
            "angle": 3.2
          },
          {
            "x": 0.5694,
            "y": 0.357,
            "angle": 3.2
          }
        ],
        "offset": 0.006
      },
      "49": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4688,
            "y": 0.3272,
            "angle": -27.3
          },
          {
            "x": 0.4993,
            "y": 0.3042,
            "angle": -27.3
          },
          {
            "x": 0.5298,
            "y": 0.2813,
            "angle": -27.3
          }
        ],
        "offset": 0
      },
      "50": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5762,
            "y": 0.3685,
            "angle": 127.9
          },
          {
            "x": 0.5559,
            "y": 0.4063,
            "angle": 127.9
          },
          {
            "x": 0.5357,
            "y": 0.4441,
            "angle": 127.9
          },
          {
            "x": 0.5154,
            "y": 0.4819,
            "angle": 127.9
          }
        ],
        "offset": 0
      },
      "51": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4354,
            "y": 0.6382,
            "angle": 88
          },
          {
            "x": 0.437,
            "y": 0.7048,
            "angle": 88
          }
        ],
        "offset": 0
      },
      "52": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4506,
            "y": 0.6013,
            "angle": -8.7
          },
          {
            "x": 0.4825,
            "y": 0.5942,
            "angle": -8.7
          }
        ],
        "offset": 0
      },
      "53": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4615,
            "y": 0.7499,
            "angle": 18.9
          },
          {
            "x": 0.5089,
            "y": 0.7736,
            "angle": 18.9
          }
        ],
        "offset": 0
      },
      "54": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4397,
            "y": 0.7559,
            "angle": 81.3
          },
          {
            "x": 0.4434,
            "y": 0.7914,
            "angle": 81.3
          },
          {
            "x": 0.4472,
            "y": 0.8269,
            "angle": 81.3
          },
          {
            "x": 0.4509,
            "y": 0.8624,
            "angle": 81.3
          }
        ],
        "offset": 0
      },
      "55": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5193,
            "y": 0.8012,
            "angle": 140.8
          },
          {
            "x": 0.4927,
            "y": 0.8328,
            "angle": 140.8
          },
          {
            "x": 0.4661,
            "y": 0.8644,
            "angle": 140.8
          }
        ],
        "offset": 0
      },
      "56": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5453,
            "y": 0.7979,
            "angle": 34.2
          },
          {
            "x": 0.5706,
            "y": 0.8229,
            "angle": 34.2
          },
          {
            "x": 0.5959,
            "y": 0.848,
            "angle": 34.2
          },
          {
            "x": 0.6212,
            "y": 0.873,
            "angle": 34.2
          }
        ],
        "offset": 0
      },
      "57": {
        "bend": 0,
        "slots": [
          {
            "x": 0.4728,
            "y": 0.8793,
            "angle": -1.9
          },
          {
            "x": 0.5129,
            "y": 0.8774,
            "angle": -1.9
          },
          {
            "x": 0.553,
            "y": 0.8755,
            "angle": -1.9
          },
          {
            "x": 0.5931,
            "y": 0.8736,
            "angle": -1.9
          },
          {
            "x": 0.6332,
            "y": 0.8717,
            "angle": -1.9
          },
          {
            "x": 0.6733,
            "y": 0.8698,
            "angle": -1.9
          }
        ],
        "offset": 0
      },
      "58": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5056,
            "y": 0.6029,
            "angle": 50.1
          },
          {
            "x": 0.5197,
            "y": 0.6274,
            "angle": 50.1
          },
          {
            "x": 0.5338,
            "y": 0.652,
            "angle": 50.1
          }
        ],
        "offset": 0
      },
      "59": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5501,
            "y": 0.5503,
            "angle": 96.7
          },
          {
            "x": 0.5464,
            "y": 0.5959,
            "angle": 96.7
          },
          {
            "x": 0.5427,
            "y": 0.6415,
            "angle": 96.7
          }
        ],
        "offset": 0
      },
      "60": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5686,
            "y": 0.5183,
            "angle": -20.8
          },
          {
            "x": 0.602,
            "y": 0.4998,
            "angle": -20.8
          },
          {
            "x": 0.6354,
            "y": 0.4813,
            "angle": -20.8
          },
          {
            "x": 0.6688,
            "y": 0.4628,
            "angle": -20.8
          },
          {
            "x": 0.7022,
            "y": 0.4443,
            "angle": -20.8
          },
          {
            "x": 0.7356,
            "y": 0.4258,
            "angle": -20.8
          }
        ],
        "offset": 0
      },
      "61": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6598,
            "y": 0.6285,
            "angle": -147.2
          },
          {
            "x": 0.629,
            "y": 0.5996,
            "angle": -147.2
          },
          {
            "x": 0.5981,
            "y": 0.5708,
            "angle": -147.2
          },
          {
            "x": 0.5673,
            "y": 0.5419,
            "angle": -147.2
          }
        ],
        "offset": 0
      },
      "62": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6848,
            "y": 0.6146,
            "angle": -63.6
          },
          {
            "x": 0.7041,
            "y": 0.558,
            "angle": -63.6
          },
          {
            "x": 0.7234,
            "y": 0.5014,
            "angle": -63.6
          },
          {
            "x": 0.7427,
            "y": 0.4448,
            "angle": -63.6
          }
        ],
        "offset": 0
      },
      "63": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7315,
            "y": 0.4081,
            "angle": -164.5
          },
          {
            "x": 0.69,
            "y": 0.3914,
            "angle": -164.5
          },
          {
            "x": 0.6485,
            "y": 0.3747,
            "angle": -164.5
          },
          {
            "x": 0.6071,
            "y": 0.358,
            "angle": -164.5
          }
        ],
        "offset": 0
      },
      "64": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5525,
            "y": 0.6919,
            "angle": 58.6
          },
          {
            "x": 0.5757,
            "y": 0.7473,
            "angle": 58.6
          },
          {
            "x": 0.599,
            "y": 0.8026,
            "angle": 58.6
          },
          {
            "x": 0.6222,
            "y": 0.8579,
            "angle": 58.6
          }
        ],
        "offset": 0
      },
      "65": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5623,
            "y": 0.6752,
            "angle": 19.3
          },
          {
            "x": 0.6052,
            "y": 0.6971,
            "angle": 19.3
          }
        ],
        "offset": 0
      },
      "66": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6431,
            "y": 0.7186,
            "angle": 23.8
          },
          {
            "x": 0.6761,
            "y": 0.7397,
            "angle": 23.8
          },
          {
            "x": 0.709,
            "y": 0.7609,
            "angle": 23.8
          }
        ],
        "offset": 0
      },
      "67": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6836,
            "y": 0.6643,
            "angle": 60.4
          },
          {
            "x": 0.7003,
            "y": 0.7072,
            "angle": 60.4
          },
          {
            "x": 0.7171,
            "y": 0.7501,
            "angle": 60.4
          }
        ],
        "offset": 0
      },
      "68": {
        "bend": 0,
        "slots": [
          {
            "x": 0.663,
            "y": 0.6592,
            "angle": 137.4
          },
          {
            "x": 0.6388,
            "y": 0.6917,
            "angle": 137.4
          }
        ],
        "offset": 0
      },
      "69": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6326,
            "y": 0.8559,
            "angle": -93.4
          },
          {
            "x": 0.6302,
            "y": 0.7968,
            "angle": -93.4
          },
          {
            "x": 0.6278,
            "y": 0.7376,
            "angle": -93.4
          }
        ],
        "offset": 0
      },
      "70": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6487,
            "y": 0.8813,
            "angle": -10.9
          },
          {
            "x": 0.6784,
            "y": 0.8731,
            "angle": -10.9
          }
        ],
        "offset": 0
      },
      "71": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7887,
            "y": 0.6367,
            "angle": 177.8
          },
          {
            "x": 0.7563,
            "y": 0.6385,
            "angle": 177.8
          },
          {
            "x": 0.7238,
            "y": 0.6402,
            "angle": 177.8
          },
          {
            "x": 0.6914,
            "y": 0.642,
            "angle": 177.8
          }
        ],
        "offset": 0
      },
      "72": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7175,
            "y": 0.7958,
            "angle": 115.7
          },
          {
            "x": 0.7013,
            "y": 0.8446,
            "angle": 115.7
          }
        ],
        "offset": 0
      },
      "73": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7424,
            "y": 0.7817,
            "angle": 22.4
          },
          {
            "x": 0.7763,
            "y": 0.8019,
            "angle": 22.4
          }
        ],
        "offset": 0
      },
      "74": {
        "bend": 0,
        "slots": [
          {
            "x": 0.71,
            "y": 0.8594,
            "angle": -21.4
          },
          {
            "x": 0.7433,
            "y": 0.8405,
            "angle": -21.4
          },
          {
            "x": 0.7766,
            "y": 0.8216,
            "angle": -21.4
          }
        ],
        "offset": 0
      },
      "75": {
        "bend": 0,
        "slots": [
          {
            "x": 0.8177,
            "y": 0.8121,
            "angle": 0
          },
          {
            "x": 0.8666,
            "y": 0.8121,
            "angle": 0
          },
          {
            "x": 0.9155,
            "y": 0.8121,
            "angle": 0
          }
        ],
        "offset": 0
      },
      "76": {
        "bend": 0,
        "slots": [
          {
            "x": 0.795,
            "y": 0.6528,
            "angle": 130.4
          },
          {
            "x": 0.7751,
            "y": 0.6867,
            "angle": 130.4
          },
          {
            "x": 0.7553,
            "y": 0.7206,
            "angle": 130.4
          },
          {
            "x": 0.7354,
            "y": 0.7545,
            "angle": 130.4
          }
        ],
        "offset": 0
      },
      "77": {
        "bend": 0,
        "slots": [
          {
            "x": 0.8218,
            "y": 0.6578,
            "angle": 41.9
          },
          {
            "x": 0.8556,
            "y": 0.7019,
            "angle": 41.9
          },
          {
            "x": 0.8893,
            "y": 0.746,
            "angle": 41.9
          },
          {
            "x": 0.9231,
            "y": 0.7901,
            "angle": 41.9
          }
        ],
        "offset": 0
      },
      "78": {
        "bend": 0,
        "slots": [
          {
            "x": 0.9178,
            "y": 0.6974,
            "angle": 74.2
          },
          {
            "x": 0.9267,
            "y": 0.7433,
            "angle": 74.2
          },
          {
            "x": 0.9356,
            "y": 0.7892,
            "angle": 74.2
          }
        ],
        "offset": 0
      },
      "79": {
        "bend": 0,
        "slots": [
          {
            "x": 0.832,
            "y": 0.6455,
            "angle": 13.8
          },
          {
            "x": 0.8862,
            "y": 0.6648,
            "angle": 13.8
          }
        ],
        "offset": 0
      },
      "80": {
        "bend": 0,
        "slots": [
          {
            "x": 0.9131,
            "y": 0.6402,
            "angle": -90.5
          },
          {
            "x": 0.9126,
            "y": 0.5715,
            "angle": -90.5
          }
        ],
        "offset": 0
      },
      "81": {
        "bend": 0,
        "slots": [
          {
            "x": 0.8183,
            "y": 0.6235,
            "angle": -32.2
          },
          {
            "x": 0.8452,
            "y": 0.5988,
            "angle": -32.2
          },
          {
            "x": 0.8721,
            "y": 0.5742,
            "angle": -32.2
          },
          {
            "x": 0.899,
            "y": 0.5495,
            "angle": -32.2
          }
        ],
        "offset": 0
      },
      "82": {
        "bend": 0,
        "slots": [
          {
            "x": 0.8973,
            "y": 0.5114,
            "angle": -130.4
          },
          {
            "x": 0.8671,
            "y": 0.4597,
            "angle": -130.4
          }
        ],
        "offset": 0
      },
      "83": {
        "bend": 0,
        "slots": [
          {
            "x": 0.8395,
            "y": 0.4316,
            "angle": -173.2
          },
          {
            "x": 0.8146,
            "y": 0.4273,
            "angle": -173.2
          },
          {
            "x": 0.7897,
            "y": 0.423,
            "angle": -173.2
          },
          {
            "x": 0.7648,
            "y": 0.4187,
            "angle": -173.2
          }
        ],
        "offset": 0
      },
      "84": {
        "bend": 0,
        "slots": [
          {
            "x": 0.855,
            "y": 0.4067,
            "angle": -80.7
          },
          {
            "x": 0.8611,
            "y": 0.3524,
            "angle": -80.7
          },
          {
            "x": 0.8672,
            "y": 0.2982,
            "angle": -80.7
          },
          {
            "x": 0.8733,
            "y": 0.2439,
            "angle": -80.7
          }
        ],
        "offset": 0
      },
      "85": {
        "bend": 0,
        "slots": [
          {
            "x": 0.852,
            "y": 0.2259,
            "angle": 165.6
          },
          {
            "x": 0.8033,
            "y": 0.2442,
            "angle": 165.6
          }
        ],
        "offset": 0
      },
      "86": {
        "bend": 0,
        "slots": [
          {
            "x": 0.8604,
            "y": 0.1972,
            "angle": -139.7
          },
          {
            "x": 0.8286,
            "y": 0.158,
            "angle": -139.7
          },
          {
            "x": 0.7969,
            "y": 0.1188,
            "angle": -139.7
          },
          {
            "x": 0.7651,
            "y": 0.0796,
            "angle": -139.7
          }
        ],
        "offset": 0
      },
      "87": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7382,
            "y": 0.0846,
            "angle": 123.1
          },
          {
            "x": 0.7161,
            "y": 0.1339,
            "angle": 123.1
          },
          {
            "x": 0.694,
            "y": 0.1832,
            "angle": 123.1
          },
          {
            "x": 0.6719,
            "y": 0.2325,
            "angle": 123.1
          }
        ],
        "offset": 0
      },
      "88": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7356,
            "y": 0.074,
            "angle": 144.7
          },
          {
            "x": 0.7084,
            "y": 0.102,
            "angle": 144.7
          },
          {
            "x": 0.6812,
            "y": 0.1301,
            "angle": 144.7
          },
          {
            "x": 0.654,
            "y": 0.1581,
            "angle": 144.7
          }
        ],
        "offset": 0
      },
      "89": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7358,
            "y": 0.0614,
            "angle": 175.9
          },
          {
            "x": 0.709,
            "y": 0.0642,
            "angle": 175.9
          },
          {
            "x": 0.6823,
            "y": 0.0671,
            "angle": 175.9
          },
          {
            "x": 0.6555,
            "y": 0.0699,
            "angle": 175.9
          },
          {
            "x": 0.6287,
            "y": 0.0727,
            "angle": 175.9
          },
          {
            "x": 0.6019,
            "y": 0.0755,
            "angle": 175.9
          },
          {
            "x": 0.5752,
            "y": 0.0784,
            "angle": 175.9
          },
          {
            "x": 0.5484,
            "y": 0.0812,
            "angle": 175.9
          }
        ],
        "offset": 0
      },
      "90": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5232,
            "y": 0.1117,
            "angle": 135.5
          },
          {
            "x": 0.4912,
            "y": 0.1574,
            "angle": 135.5
          },
          {
            "x": 0.4592,
            "y": 0.2032,
            "angle": 135.5
          }
        ],
        "offset": -0.006
      },
      "91": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5148,
            "y": 0.0992,
            "angle": 135.5
          },
          {
            "x": 0.4828,
            "y": 0.145,
            "angle": 135.5
          },
          {
            "x": 0.4508,
            "y": 0.1907,
            "angle": 135.5
          }
        ],
        "offset": 0.006
      },
      "92": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6245,
            "y": 0.1884,
            "angle": 144.8
          },
          {
            "x": 0.5928,
            "y": 0.221,
            "angle": 144.8
          },
          {
            "x": 0.561,
            "y": 0.2535,
            "angle": 144.8
          }
        ],
        "offset": 0
      },
      "93": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5554,
            "y": 0.2897,
            "angle": 53.1
          },
          {
            "x": 0.576,
            "y": 0.3297,
            "angle": 53.1
          }
        ],
        "offset": 0
      },
      "94": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5987,
            "y": 0.3342,
            "angle": -40.4
          },
          {
            "x": 0.6236,
            "y": 0.3034,
            "angle": -40.4
          },
          {
            "x": 0.6485,
            "y": 0.2725,
            "angle": -40.4
          }
        ],
        "offset": 0
      },
      "95": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6583,
            "y": 0.2465,
            "angle": -109.3
          },
          {
            "x": 0.6532,
            "y": 0.2252,
            "angle": -109.3
          },
          {
            "x": 0.6481,
            "y": 0.204,
            "angle": -109.3
          },
          {
            "x": 0.643,
            "y": 0.1827,
            "angle": -109.3
          }
        ],
        "offset": 0
      },
      "96": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6806,
            "y": 0.2565,
            "angle": -1.3
          },
          {
            "x": 0.7199,
            "y": 0.2552,
            "angle": -1.3
          },
          {
            "x": 0.7592,
            "y": 0.2539,
            "angle": -1.3
          }
        ],
        "offset": 0
      },
      "97": {
        "bend": 0,
        "slots": [
          {
            "x": 0.6838,
            "y": 0.297,
            "angle": 50.2
          },
          {
            "x": 0.7294,
            "y": 0.3767,
            "angle": 50.2
          }
        ],
        "offset": 0
      },
      "98": {
        "bend": 0,
        "slots": [
          {
            "x": 0.7745,
            "y": 0.2805,
            "angle": 103.3
          },
          {
            "x": 0.7656,
            "y": 0.3349,
            "angle": 103.3
          },
          {
            "x": 0.7567,
            "y": 0.3893,
            "angle": 103.3
          }
        ],
        "offset": 0
      },
      "99": {
        "bend": 0,
        "slots": [
          {
            "x": 0.5308,
            "y": 0.506,
            "angle": -158.5
          }
        ],
        "offset": 0.006
      }
    },
    "view_box": {
      "width": 1600,
      "height": 1100
    },
    "background": {
      "width": 1600,
      "height": 1100,
      "asset_id": null
    }
  },
  "schema_version": 1
}
$ttrjson$::jsonb,
        '59de2e248d0cb4f6f0a5a59d6cba3393e63f3ac2fb5b75f59c76997879343548', TRUE, now())
ON CONFLICT (map_id, version) DO UPDATE
    SET doc          = EXCLUDED.doc,
        doc_sha256   = EXCLUDED.doc_sha256,
        status       = EXCLUDED.status,
        validated    = EXCLUDED.validated,
        published_at = EXCLUDED.published_at,
        updated_at   = now()
    WHERE ttr.map_versions.status = 'draft';
