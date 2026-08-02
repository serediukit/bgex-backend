-- Seed the official Europe map (plan Q4/Q5): a fixed-UUID map row plus its
-- version 1 document, inlined verbatim from
-- internal/games/ttr/mapdata/europe.v1.json (embedded in the binary via
-- go:embed — see mapdata.EuropeV1 / mapdata.EuropeMapID /
-- mapdata.EuropeVersion). Dollar-quoting avoids hand-escaping single quotes
-- in the JSON body.
--
-- internal/games/ttr/mapdata/europe_test.go / seed_test.go assert this file
-- is byte-identical to the embedded JSON and that doc_sha256 below matches
-- ttr.DocSHA256(mapdata.EuropeV1) — there is no live database in this
-- environment to run the migration against, so it is UNVERIFIED against a
-- real Postgres instance. Run `make migrate-up` against a real database
-- before relying on this in production.

INSERT INTO ttr.maps (id, slug, name, is_official)
VALUES ('00000000-0000-0000-0000-0000000000e0', 'europe', 'Europe', TRUE)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO ttr.map_versions (map_id, version, status, doc, doc_sha256, published_at)
VALUES ('00000000-0000-0000-0000-0000000000e0', 1, 'published',
        $ttrjson$
{
  "schema_version": 1,
  "name": "Europe",
  "rules": {
    "players": {
      "min": 2,
      "max": 5
    },
    "trains_per_player": 45,
    "stations_per_player": 3,
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
        "id": 1,
        "a": "edinburgh",
        "b": "london",
        "color": "Orange",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": 2
      },
      {
        "id": 2,
        "a": "edinburgh",
        "b": "london",
        "color": "Black",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": 1
      },
      {
        "id": 3,
        "a": "london",
        "b": "amsterdam",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 2,
        "pair": null
      },
      {
        "id": 4,
        "a": "london",
        "b": "dieppe",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 1,
        "pair": 5
      },
      {
        "id": 5,
        "a": "london",
        "b": "dieppe",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 1,
        "pair": 4
      },
      {
        "id": 6,
        "a": "brest",
        "b": "dieppe",
        "color": "Orange",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 7,
        "a": "brest",
        "b": "paris",
        "color": "Black",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 8,
        "a": "brest",
        "b": "pamplona",
        "color": "Purple",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 9,
        "a": "paris",
        "b": "dieppe",
        "color": "White",
        "length": 1,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 10,
        "a": "paris",
        "b": "zurich",
        "color": "Gray",
        "length": 3,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 11,
        "a": "paris",
        "b": "pamplona",
        "color": "Blue",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": 12
      },
      {
        "id": 12,
        "a": "paris",
        "b": "pamplona",
        "color": "Green",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": 11
      },
      {
        "id": 13,
        "a": "paris",
        "b": "marseille",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 14,
        "a": "bruxelles",
        "b": "dieppe",
        "color": "Green",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 15,
        "a": "bruxelles",
        "b": "paris",
        "color": "Yellow",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": 16
      },
      {
        "id": 16,
        "a": "bruxelles",
        "b": "paris",
        "color": "Red",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": 15
      },
      {
        "id": 17,
        "a": "bruxelles",
        "b": "amsterdam",
        "color": "Black",
        "length": 1,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 18,
        "a": "bruxelles",
        "b": "frankfurt",
        "color": "White",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 19,
        "a": "paris",
        "b": "frankfurt",
        "color": "Orange",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 20,
        "a": "marseille",
        "b": "pamplona",
        "color": "Red",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 21,
        "a": "marseille",
        "b": "barcelona",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 22,
        "a": "pamplona",
        "b": "barcelona",
        "color": "White",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 23,
        "a": "zurich",
        "b": "marseille",
        "color": "Purple",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 24,
        "a": "marseille",
        "b": "roma",
        "color": "Gray",
        "length": 4,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 25,
        "a": "lisboa",
        "b": "cadiz",
        "color": "Blue",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 26,
        "a": "madrid",
        "b": "lisboa",
        "color": "Purple",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 27,
        "a": "madrid",
        "b": "cadiz",
        "color": "Orange",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 28,
        "a": "madrid",
        "b": "pamplona",
        "color": "Black",
        "length": 3,
        "tunnel": true,
        "locos": 0,
        "pair": 29
      },
      {
        "id": 29,
        "a": "madrid",
        "b": "pamplona",
        "color": "White",
        "length": 3,
        "tunnel": true,
        "locos": 0,
        "pair": 28
      },
      {
        "id": 30,
        "a": "madrid",
        "b": "barcelona",
        "color": "Yellow",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 31,
        "a": "amsterdam",
        "b": "essen",
        "color": "Yellow",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 32,
        "a": "amsterdam",
        "b": "frankfurt",
        "color": "Blue",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 33,
        "a": "frankfurt",
        "b": "essen",
        "color": "Green",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 34,
        "a": "essen",
        "b": "berlin",
        "color": "Blue",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 35,
        "a": "kobenhavn",
        "b": "essen",
        "color": "Gray",
        "length": 3,
        "tunnel": false,
        "locos": 1,
        "pair": null
      },
      {
        "id": 36,
        "a": "frankfurt",
        "b": "berlin",
        "color": "Black",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": 37
      },
      {
        "id": 37,
        "a": "frankfurt",
        "b": "berlin",
        "color": "Red",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": 36
      },
      {
        "id": 38,
        "a": "frankfurt",
        "b": "munchen",
        "color": "Purple",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 39,
        "a": "munchen",
        "b": "zurich",
        "color": "Yellow",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 40,
        "a": "munchen",
        "b": "venezia",
        "color": "Blue",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 41,
        "a": "munchen",
        "b": "wien",
        "color": "Orange",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 42,
        "a": "zurich",
        "b": "venezia",
        "color": "Green",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 43,
        "a": "berlin",
        "b": "wien",
        "color": "Red",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 44,
        "a": "wien",
        "b": "zagrab",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 45,
        "a": "budapest",
        "b": "wien",
        "color": "White",
        "length": 1,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 46,
        "a": "budapest",
        "b": "zagrab",
        "color": "Orange",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 47,
        "a": "berlin",
        "b": "warszawa",
        "color": "Purple",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": 48
      },
      {
        "id": 48,
        "a": "berlin",
        "b": "warszawa",
        "color": "Yellow",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": 47
      },
      {
        "id": 49,
        "a": "berlin",
        "b": "danzig",
        "color": "Gray",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 50,
        "a": "warszawa",
        "b": "wien",
        "color": "Green",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 51,
        "a": "venezia",
        "b": "roma",
        "color": "Black",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 52,
        "a": "venezia",
        "b": "zagrab",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 53,
        "a": "roma",
        "b": "brindisi",
        "color": "White",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 54,
        "a": "roma",
        "b": "palermo",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 1,
        "pair": null
      },
      {
        "id": 55,
        "a": "brindisi",
        "b": "palermo",
        "color": "Gray",
        "length": 3,
        "tunnel": false,
        "locos": 1,
        "pair": null
      },
      {
        "id": 56,
        "a": "brindisi",
        "b": "athina",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 1,
        "pair": null
      },
      {
        "id": 57,
        "a": "palermo",
        "b": "smyrna",
        "color": "Gray",
        "length": 6,
        "tunnel": false,
        "locos": 2,
        "pair": null
      },
      {
        "id": 58,
        "a": "zagrab",
        "b": "sarajevo",
        "color": "Red",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 59,
        "a": "budapest",
        "b": "sarajevo",
        "color": "Purple",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 60,
        "a": "budapest",
        "b": "kyiv",
        "color": "Gray",
        "length": 6,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 61,
        "a": "bucuresti",
        "b": "budapest",
        "color": "Gray",
        "length": 4,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 62,
        "a": "bucuresti",
        "b": "kyiv",
        "color": "White",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 63,
        "a": "kyiv",
        "b": "warszawa",
        "color": "Blue",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 64,
        "a": "sarajevo",
        "b": "athina",
        "color": "Red",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 65,
        "a": "sarajevo",
        "b": "sofia",
        "color": "Green",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 66,
        "a": "sofia",
        "b": "constantinople",
        "color": "Blue",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 67,
        "a": "bucuresti",
        "b": "constantinople",
        "color": "Yellow",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 68,
        "a": "bucuresti",
        "b": "sofia",
        "color": "Gray",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 69,
        "a": "athina",
        "b": "sofia",
        "color": "Yellow",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 70,
        "a": "athina",
        "b": "smyrna",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 1,
        "pair": null
      },
      {
        "id": 71,
        "a": "sevastopol",
        "b": "bucuresti",
        "color": "Red",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 72,
        "a": "constantinople",
        "b": "smyrna",
        "color": "Gray",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 73,
        "a": "constantinople",
        "b": "angora",
        "color": "Gray",
        "length": 2,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 74,
        "a": "smyrna",
        "b": "angora",
        "color": "Orange",
        "length": 3,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 75,
        "a": "angora",
        "b": "erzurum",
        "color": "Black",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 76,
        "a": "sevastopol",
        "b": "constantinople",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 2,
        "pair": null
      },
      {
        "id": 77,
        "a": "sevastopol",
        "b": "erzurum",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 2,
        "pair": null
      },
      {
        "id": 78,
        "a": "sochi",
        "b": "erzurum",
        "color": "Red",
        "length": 3,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 79,
        "a": "sevastopol",
        "b": "sochi",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 1,
        "pair": null
      },
      {
        "id": 80,
        "a": "sochi",
        "b": "rostov",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 81,
        "a": "sevastopol",
        "b": "rostov",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 82,
        "a": "rostov",
        "b": "kharkov",
        "color": "Green",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 83,
        "a": "kharkov",
        "b": "kyiv",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 84,
        "a": "kharkov",
        "b": "moskva",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 85,
        "a": "moskva",
        "b": "smolensk",
        "color": "Yellow",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 86,
        "a": "moskva",
        "b": "petrograd",
        "color": "Blue",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 87,
        "a": "petrograd",
        "b": "wilno",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 88,
        "a": "petrograd",
        "b": "riga",
        "color": "Gray",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 89,
        "a": "petrograd",
        "b": "stockholm",
        "color": "White",
        "length": 8,
        "tunnel": true,
        "locos": 0,
        "pair": null
      },
      {
        "id": 90,
        "a": "stockholm",
        "b": "kobenhavn",
        "color": "Yellow",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": 91
      },
      {
        "id": 91,
        "a": "stockholm",
        "b": "kobenhavn",
        "color": "White",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": 90
      },
      {
        "id": 92,
        "a": "riga",
        "b": "danzig",
        "color": "Black",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 93,
        "a": "danzig",
        "b": "warszawa",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 94,
        "a": "warszawa",
        "b": "wilno",
        "color": "Red",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 95,
        "a": "wilno",
        "b": "riga",
        "color": "Green",
        "length": 4,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 96,
        "a": "wilno",
        "b": "smolensk",
        "color": "Yellow",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 97,
        "a": "wilno",
        "b": "kyiv",
        "color": "Gray",
        "length": 2,
        "tunnel": false,
        "locos": 0,
        "pair": null
      },
      {
        "id": 98,
        "a": "smolensk",
        "b": "kyiv",
        "color": "Red",
        "length": 3,
        "tunnel": false,
        "locos": 0,
        "pair": null
      }
    ],
    "tickets": [
      {
        "id": 1,
        "a": "lisboa",
        "b": "danzig",
        "points": 20,
        "long": true
      },
      {
        "id": 2,
        "a": "brest",
        "b": "petrograd",
        "points": 20,
        "long": true
      },
      {
        "id": 3,
        "a": "palermo",
        "b": "moskva",
        "points": 20,
        "long": true
      },
      {
        "id": 4,
        "a": "kobenhavn",
        "b": "erzurum",
        "points": 21,
        "long": true
      },
      {
        "id": 5,
        "a": "edinburgh",
        "b": "athina",
        "points": 21,
        "long": true
      },
      {
        "id": 6,
        "a": "cadiz",
        "b": "stockholm",
        "points": 21,
        "long": true
      },
      {
        "id": 7,
        "a": "athina",
        "b": "angora",
        "points": 5,
        "long": false
      },
      {
        "id": 8,
        "a": "budapest",
        "b": "sofia",
        "points": 5,
        "long": false
      },
      {
        "id": 9,
        "a": "frankfurt",
        "b": "kobenhavn",
        "points": 5,
        "long": false
      },
      {
        "id": 10,
        "a": "rostov",
        "b": "erzurum",
        "points": 5,
        "long": false
      },
      {
        "id": 11,
        "a": "sofia",
        "b": "smyrna",
        "points": 5,
        "long": false
      },
      {
        "id": 12,
        "a": "kyiv",
        "b": "petrograd",
        "points": 6,
        "long": false
      },
      {
        "id": 13,
        "a": "zurich",
        "b": "brindisi",
        "points": 6,
        "long": false
      },
      {
        "id": 14,
        "a": "zurich",
        "b": "budapest",
        "points": 6,
        "long": false
      },
      {
        "id": 15,
        "a": "warszawa",
        "b": "smolensk",
        "points": 6,
        "long": false
      },
      {
        "id": 16,
        "a": "zagrab",
        "b": "brindisi",
        "points": 6,
        "long": false
      },
      {
        "id": 17,
        "a": "paris",
        "b": "zagrab",
        "points": 7,
        "long": false
      },
      {
        "id": 18,
        "a": "brest",
        "b": "marseille",
        "points": 7,
        "long": false
      },
      {
        "id": 19,
        "a": "london",
        "b": "berlin",
        "points": 7,
        "long": false
      },
      {
        "id": 20,
        "a": "edinburgh",
        "b": "paris",
        "points": 7,
        "long": false
      },
      {
        "id": 21,
        "a": "amsterdam",
        "b": "pamplona",
        "points": 7,
        "long": false
      },
      {
        "id": 22,
        "a": "roma",
        "b": "smyrna",
        "points": 8,
        "long": false
      },
      {
        "id": 23,
        "a": "palermo",
        "b": "constantinople",
        "points": 8,
        "long": false
      },
      {
        "id": 24,
        "a": "sarajevo",
        "b": "sevastopol",
        "points": 8,
        "long": false
      },
      {
        "id": 25,
        "a": "madrid",
        "b": "dieppe",
        "points": 8,
        "long": false
      },
      {
        "id": 26,
        "a": "barcelona",
        "b": "bruxelles",
        "points": 8,
        "long": false
      },
      {
        "id": 27,
        "a": "paris",
        "b": "wien",
        "points": 8,
        "long": false
      },
      {
        "id": 28,
        "a": "barcelona",
        "b": "munchen",
        "points": 8,
        "long": false
      },
      {
        "id": 29,
        "a": "brest",
        "b": "venezia",
        "points": 8,
        "long": false
      },
      {
        "id": 30,
        "a": "smolensk",
        "b": "rostov",
        "points": 8,
        "long": false
      },
      {
        "id": 31,
        "a": "marseille",
        "b": "essen",
        "points": 8,
        "long": false
      },
      {
        "id": 32,
        "a": "kyiv",
        "b": "sochi",
        "points": 8,
        "long": false
      },
      {
        "id": 33,
        "a": "madrid",
        "b": "zurich",
        "points": 8,
        "long": false
      },
      {
        "id": 34,
        "a": "berlin",
        "b": "bucuresti",
        "points": 8,
        "long": false
      },
      {
        "id": 35,
        "a": "bruxelles",
        "b": "danzig",
        "points": 9,
        "long": false
      },
      {
        "id": 36,
        "a": "berlin",
        "b": "roma",
        "points": 9,
        "long": false
      },
      {
        "id": 37,
        "a": "angora",
        "b": "kharkov",
        "points": 10,
        "long": false
      },
      {
        "id": 38,
        "a": "riga",
        "b": "bucuresti",
        "points": 10,
        "long": false
      },
      {
        "id": 39,
        "a": "essen",
        "b": "kyiv",
        "points": 10,
        "long": false
      },
      {
        "id": 40,
        "a": "venezia",
        "b": "constantinople",
        "points": 10,
        "long": false
      },
      {
        "id": 41,
        "a": "london",
        "b": "wien",
        "points": 10,
        "long": false
      },
      {
        "id": 42,
        "a": "athina",
        "b": "wilno",
        "points": 11,
        "long": false
      },
      {
        "id": 43,
        "a": "stockholm",
        "b": "wien",
        "points": 11,
        "long": false
      },
      {
        "id": 44,
        "a": "berlin",
        "b": "moskva",
        "points": 12,
        "long": false
      },
      {
        "id": 45,
        "a": "amsterdam",
        "b": "wilno",
        "points": 12,
        "long": false
      },
      {
        "id": 46,
        "a": "frankfurt",
        "b": "smolensk",
        "points": 13,
        "long": false
      }
    ]
  },
  "layout": {
    "view_box": {
      "width": 1600,
      "height": 1100
    },
    "background": {
      "asset_id": null,
      "width": 1600,
      "height": 1100
    },
    "cities": {
      "edinburgh": {
        "x": 0.1639,
        "y": 0.2097,
        "label_anchor": "n"
      },
      "london": {
        "x": 0.2173,
        "y": 0.3766,
        "label_anchor": "n"
      },
      "amsterdam": {
        "x": 0.3051,
        "y": 0.3439,
        "label_anchor": "n"
      },
      "bruxelles": {
        "x": 0.2955,
        "y": 0.4015,
        "label_anchor": "n"
      },
      "dieppe": {
        "x": 0.2384,
        "y": 0.4361,
        "label_anchor": "n"
      },
      "brest": {
        "x": 0.1412,
        "y": 0.494,
        "label_anchor": "n"
      },
      "paris": {
        "x": 0.2606,
        "y": 0.4763,
        "label_anchor": "n"
      },
      "pamplona": {
        "x": 0.1908,
        "y": 0.7038,
        "label_anchor": "n"
      },
      "madrid": {
        "x": 0.155,
        "y": 0.7937,
        "label_anchor": "n"
      },
      "lisboa": {
        "x": 0.06,
        "y": 0.8576,
        "label_anchor": "n"
      },
      "cadiz": {
        "x": 0.1098,
        "y": 0.94,
        "label_anchor": "n"
      },
      "barcelona": {
        "x": 0.2574,
        "y": 0.7572,
        "label_anchor": "n"
      },
      "marseille": {
        "x": 0.3133,
        "y": 0.6854,
        "label_anchor": "n"
      },
      "zurich": {
        "x": 0.3686,
        "y": 0.532,
        "label_anchor": "n"
      },
      "frankfurt": {
        "x": 0.3711,
        "y": 0.4293,
        "label_anchor": "n"
      },
      "essen": {
        "x": 0.3419,
        "y": 0.3785,
        "label_anchor": "n"
      },
      "kobenhavn": {
        "x": 0.439,
        "y": 0.2198,
        "label_anchor": "n"
      },
      "berlin": {
        "x": 0.4535,
        "y": 0.3387,
        "label_anchor": "n"
      },
      "munchen": {
        "x": 0.4217,
        "y": 0.5034,
        "label_anchor": "n"
      },
      "wien": {
        "x": 0.5053,
        "y": 0.5008,
        "label_anchor": "n"
      },
      "venezia": {
        "x": 0.4346,
        "y": 0.6049,
        "label_anchor": "n"
      },
      "roma": {
        "x": 0.4378,
        "y": 0.7381,
        "label_anchor": "n"
      },
      "palermo": {
        "x": 0.4528,
        "y": 0.8802,
        "label_anchor": "n"
      },
      "brindisi": {
        "x": 0.5326,
        "y": 0.7854,
        "label_anchor": "n"
      },
      "zagrab": {
        "x": 0.4985,
        "y": 0.5906,
        "label_anchor": "n"
      },
      "sarajevo": {
        "x": 0.5409,
        "y": 0.6643,
        "label_anchor": "n"
      },
      "budapest": {
        "x": 0.5519,
        "y": 0.5275,
        "label_anchor": "n"
      },
      "sofia": {
        "x": 0.6266,
        "y": 0.708,
        "label_anchor": "n"
      },
      "athina": {
        "x": 0.6338,
        "y": 0.8855,
        "label_anchor": "n"
      },
      "smyrna": {
        "x": 0.6933,
        "y": 0.8689,
        "label_anchor": "n"
      },
      "constantinople": {
        "x": 0.7255,
        "y": 0.7715,
        "label_anchor": "n"
      },
      "bucuresti": {
        "x": 0.6752,
        "y": 0.6429,
        "label_anchor": "n"
      },
      "kyiv": {
        "x": 0.7523,
        "y": 0.4165,
        "label_anchor": "n"
      },
      "sevastopol": {
        "x": 0.8049,
        "y": 0.6358,
        "label_anchor": "n"
      },
      "angora": {
        "x": 0.7932,
        "y": 0.8121,
        "label_anchor": "n"
      },
      "erzurum": {
        "x": 0.94,
        "y": 0.8121,
        "label_anchor": "n"
      },
      "sochi": {
        "x": 0.9133,
        "y": 0.6745,
        "label_anchor": "n"
      },
      "rostov": {
        "x": 0.9124,
        "y": 0.5372,
        "label_anchor": "n"
      },
      "kharkov": {
        "x": 0.852,
        "y": 0.4338,
        "label_anchor": "n"
      },
      "moskva": {
        "x": 0.8763,
        "y": 0.2168,
        "label_anchor": "n"
      },
      "smolensk": {
        "x": 0.7789,
        "y": 0.2533,
        "label_anchor": "n"
      },
      "wilno": {
        "x": 0.6609,
        "y": 0.2571,
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
      "riga": {
        "x": 0.6404,
        "y": 0.1721,
        "label_anchor": "n"
      },
      "danzig": {
        "x": 0.5451,
        "y": 0.2698,
        "label_anchor": "n"
      },
      "warszawa": {
        "x": 0.5863,
        "y": 0.3496,
        "label_anchor": "n"
      }
    },
    "routes": {
      "1": {
        "slots": [
          {
            "x": 0.1763,
            "y": 0.2287,
            "angle": 72.3
          },
          {
            "x": 0.1896,
            "y": 0.2705,
            "angle": 72.3
          },
          {
            "x": 0.203,
            "y": 0.3122,
            "angle": 72.3
          },
          {
            "x": 0.2163,
            "y": 0.3539,
            "angle": 72.3
          }
        ]
      },
      "2": {
        "slots": [
          {
            "x": 0.1649,
            "y": 0.2324,
            "angle": 72.3
          },
          {
            "x": 0.1782,
            "y": 0.2741,
            "angle": 72.3
          },
          {
            "x": 0.1916,
            "y": 0.3158,
            "angle": 72.3
          },
          {
            "x": 0.2049,
            "y": 0.3576,
            "angle": 72.3
          }
        ]
      },
      "3": {
        "slots": [
          {
            "x": 0.2392,
            "y": 0.3684,
            "angle": -20.4
          },
          {
            "x": 0.2832,
            "y": 0.3521,
            "angle": -20.4
          }
        ]
      },
      "4": {
        "slots": [
          {
            "x": 0.2282,
            "y": 0.3895,
            "angle": 70.5
          },
          {
            "x": 0.2388,
            "y": 0.4192,
            "angle": 70.5
          }
        ]
      },
      "5": {
        "slots": [
          {
            "x": 0.2169,
            "y": 0.3935,
            "angle": 70.5
          },
          {
            "x": 0.2275,
            "y": 0.4232,
            "angle": 70.5
          }
        ]
      },
      "6": {
        "slots": [
          {
            "x": 0.1655,
            "y": 0.4795,
            "angle": -30.8
          },
          {
            "x": 0.2141,
            "y": 0.4506,
            "angle": -30.8
          }
        ]
      },
      "7": {
        "slots": [
          {
            "x": 0.1611,
            "y": 0.491,
            "angle": -8.4
          },
          {
            "x": 0.2009,
            "y": 0.4851,
            "angle": -8.4
          },
          {
            "x": 0.2407,
            "y": 0.4793,
            "angle": -8.4
          }
        ]
      },
      "8": {
        "slots": [
          {
            "x": 0.1474,
            "y": 0.5202,
            "angle": 76.7
          },
          {
            "x": 0.1598,
            "y": 0.5727,
            "angle": 76.7
          },
          {
            "x": 0.1722,
            "y": 0.6251,
            "angle": 76.7
          },
          {
            "x": 0.1846,
            "y": 0.6776,
            "angle": 76.7
          }
        ]
      },
      "9": {
        "slots": [
          {
            "x": 0.2495,
            "y": 0.4562,
            "angle": -118.9
          }
        ]
      },
      "10": {
        "slots": [
          {
            "x": 0.2786,
            "y": 0.4856,
            "angle": 27.3
          },
          {
            "x": 0.3146,
            "y": 0.5041,
            "angle": 27.3
          },
          {
            "x": 0.3506,
            "y": 0.5227,
            "angle": 27.3
          }
        ]
      },
      "11": {
        "slots": [
          {
            "x": 0.2576,
            "y": 0.5065,
            "angle": 107.1
          },
          {
            "x": 0.2402,
            "y": 0.5634,
            "angle": 107.1
          },
          {
            "x": 0.2227,
            "y": 0.6202,
            "angle": 107.1
          },
          {
            "x": 0.2053,
            "y": 0.6771,
            "angle": 107.1
          }
        ]
      },
      "12": {
        "slots": [
          {
            "x": 0.2461,
            "y": 0.503,
            "angle": 107.1
          },
          {
            "x": 0.2287,
            "y": 0.5599,
            "angle": 107.1
          },
          {
            "x": 0.2112,
            "y": 0.6167,
            "angle": 107.1
          },
          {
            "x": 0.1938,
            "y": 0.6736,
            "angle": 107.1
          }
        ]
      },
      "13": {
        "slots": [
          {
            "x": 0.2672,
            "y": 0.5024,
            "angle": 75.9
          },
          {
            "x": 0.2804,
            "y": 0.5547,
            "angle": 75.9
          },
          {
            "x": 0.2935,
            "y": 0.607,
            "angle": 75.9
          },
          {
            "x": 0.3067,
            "y": 0.6593,
            "angle": 75.9
          }
        ]
      },
      "14": {
        "slots": [
          {
            "x": 0.2812,
            "y": 0.4102,
            "angle": 148.8
          },
          {
            "x": 0.2527,
            "y": 0.4274,
            "angle": 148.8
          }
        ]
      },
      "15": {
        "slots": [
          {
            "x": 0.2922,
            "y": 0.4227,
            "angle": 115.0
          },
          {
            "x": 0.2748,
            "y": 0.4601,
            "angle": 115.0
          }
        ]
      },
      "16": {
        "slots": [
          {
            "x": 0.2813,
            "y": 0.4177,
            "angle": 115.0
          },
          {
            "x": 0.2639,
            "y": 0.4551,
            "angle": 115.0
          }
        ]
      },
      "17": {
        "slots": [
          {
            "x": 0.3003,
            "y": 0.3727,
            "angle": -80.5
          }
        ]
      },
      "18": {
        "slots": [
          {
            "x": 0.3144,
            "y": 0.4085,
            "angle": 20.2
          },
          {
            "x": 0.3522,
            "y": 0.4224,
            "angle": 20.2
          }
        ]
      },
      "19": {
        "slots": [
          {
            "x": 0.279,
            "y": 0.4685,
            "angle": -23.0
          },
          {
            "x": 0.3158,
            "y": 0.4528,
            "angle": -23.0
          },
          {
            "x": 0.3527,
            "y": 0.4371,
            "angle": -23.0
          }
        ]
      },
      "20": {
        "slots": [
          {
            "x": 0.298,
            "y": 0.6877,
            "angle": 171.5
          },
          {
            "x": 0.2674,
            "y": 0.6923,
            "angle": 171.5
          },
          {
            "x": 0.2367,
            "y": 0.6969,
            "angle": 171.5
          },
          {
            "x": 0.2061,
            "y": 0.7015,
            "angle": 171.5
          }
        ]
      },
      "21": {
        "slots": [
          {
            "x": 0.3063,
            "y": 0.6944,
            "angle": 127.9
          },
          {
            "x": 0.2923,
            "y": 0.7123,
            "angle": 127.9
          },
          {
            "x": 0.2784,
            "y": 0.7303,
            "angle": 127.9
          },
          {
            "x": 0.2644,
            "y": 0.7482,
            "angle": 127.9
          }
        ]
      },
      "22": {
        "slots": [
          {
            "x": 0.2074,
            "y": 0.7171,
            "angle": 38.7
          },
          {
            "x": 0.2408,
            "y": 0.7439,
            "angle": 38.7
          }
        ]
      },
      "23": {
        "slots": [
          {
            "x": 0.3548,
            "y": 0.5704,
            "angle": 109.8
          },
          {
            "x": 0.3271,
            "y": 0.6471,
            "angle": 109.8
          }
        ]
      },
      "24": {
        "slots": [
          {
            "x": 0.3289,
            "y": 0.692,
            "angle": 22.9
          },
          {
            "x": 0.36,
            "y": 0.7052,
            "angle": 22.9
          },
          {
            "x": 0.3911,
            "y": 0.7183,
            "angle": 22.9
          },
          {
            "x": 0.4222,
            "y": 0.7315,
            "angle": 22.9
          }
        ]
      },
      "25": {
        "slots": [
          {
            "x": 0.0725,
            "y": 0.8782,
            "angle": 58.9
          },
          {
            "x": 0.0973,
            "y": 0.9194,
            "angle": 58.9
          }
        ]
      },
      "26": {
        "slots": [
          {
            "x": 0.1392,
            "y": 0.8044,
            "angle": 146.1
          },
          {
            "x": 0.1075,
            "y": 0.8256,
            "angle": 146.1
          },
          {
            "x": 0.0758,
            "y": 0.8469,
            "angle": 146.1
          }
        ]
      },
      "27": {
        "slots": [
          {
            "x": 0.1475,
            "y": 0.8181,
            "angle": 107.2
          },
          {
            "x": 0.1324,
            "y": 0.8668,
            "angle": 107.2
          },
          {
            "x": 0.1173,
            "y": 0.9156,
            "angle": 107.2
          }
        ]
      },
      "28": {
        "slots": [
          {
            "x": 0.1554,
            "y": 0.7765,
            "angle": -68.3
          },
          {
            "x": 0.1673,
            "y": 0.7465,
            "angle": -68.3
          },
          {
            "x": 0.1793,
            "y": 0.7166,
            "angle": -68.3
          }
        ]
      },
      "29": {
        "slots": [
          {
            "x": 0.1665,
            "y": 0.7809,
            "angle": -68.3
          },
          {
            "x": 0.1785,
            "y": 0.751,
            "angle": -68.3
          },
          {
            "x": 0.1904,
            "y": 0.721,
            "angle": -68.3
          }
        ]
      },
      "30": {
        "slots": [
          {
            "x": 0.1806,
            "y": 0.7846,
            "angle": -19.6
          },
          {
            "x": 0.2318,
            "y": 0.7663,
            "angle": -19.6
          }
        ]
      },
      "31": {
        "slots": [
          {
            "x": 0.3112,
            "y": 0.3497,
            "angle": 43.2
          },
          {
            "x": 0.3235,
            "y": 0.3612,
            "angle": 43.2
          },
          {
            "x": 0.3358,
            "y": 0.3727,
            "angle": 43.2
          }
        ]
      },
      "32": {
        "slots": [
          {
            "x": 0.3216,
            "y": 0.3652,
            "angle": 52.3
          },
          {
            "x": 0.3546,
            "y": 0.408,
            "angle": 52.3
          }
        ]
      },
      "33": {
        "slots": [
          {
            "x": 0.3638,
            "y": 0.4166,
            "angle": -119.9
          },
          {
            "x": 0.3492,
            "y": 0.3912,
            "angle": -119.9
          }
        ]
      },
      "34": {
        "slots": [
          {
            "x": 0.3698,
            "y": 0.3685,
            "angle": -19.6
          },
          {
            "x": 0.4256,
            "y": 0.3487,
            "angle": -19.6
          }
        ]
      },
      "35": {
        "slots": [
          {
            "x": 0.4228,
            "y": 0.2462,
            "angle": 121.5
          },
          {
            "x": 0.3904,
            "y": 0.2992,
            "angle": 121.5
          },
          {
            "x": 0.3581,
            "y": 0.352,
            "angle": 121.5
          }
        ]
      },
      "36": {
        "slots": [
          {
            "x": 0.3804,
            "y": 0.4102,
            "angle": -47.7
          },
          {
            "x": 0.4079,
            "y": 0.38,
            "angle": -47.7
          },
          {
            "x": 0.4353,
            "y": 0.3498,
            "angle": -47.7
          }
        ]
      },
      "37": {
        "slots": [
          {
            "x": 0.3893,
            "y": 0.4182,
            "angle": -47.7
          },
          {
            "x": 0.4167,
            "y": 0.388,
            "angle": -47.7
          },
          {
            "x": 0.4442,
            "y": 0.3578,
            "angle": -47.7
          }
        ]
      },
      "38": {
        "slots": [
          {
            "x": 0.3837,
            "y": 0.4478,
            "angle": 55.7
          },
          {
            "x": 0.4091,
            "y": 0.4849,
            "angle": 55.7
          }
        ]
      },
      "39": {
        "slots": [
          {
            "x": 0.4084,
            "y": 0.5105,
            "angle": 151.7
          },
          {
            "x": 0.3819,
            "y": 0.5249,
            "angle": 151.7
          }
        ]
      },
      "40": {
        "slots": [
          {
            "x": 0.4249,
            "y": 0.5288,
            "angle": 82.8
          },
          {
            "x": 0.4314,
            "y": 0.5795,
            "angle": 82.8
          }
        ]
      },
      "41": {
        "slots": [
          {
            "x": 0.4356,
            "y": 0.503,
            "angle": -1.8
          },
          {
            "x": 0.4635,
            "y": 0.5021,
            "angle": -1.8
          },
          {
            "x": 0.4914,
            "y": 0.5012,
            "angle": -1.8
          }
        ]
      },
      "42": {
        "slots": [
          {
            "x": 0.3851,
            "y": 0.5502,
            "angle": 47.8
          },
          {
            "x": 0.4181,
            "y": 0.5867,
            "angle": 47.8
          }
        ]
      },
      "43": {
        "slots": [
          {
            "x": 0.4621,
            "y": 0.3657,
            "angle": 72.3
          },
          {
            "x": 0.4794,
            "y": 0.4198,
            "angle": 72.3
          },
          {
            "x": 0.4967,
            "y": 0.4738,
            "angle": 72.3
          }
        ]
      },
      "44": {
        "slots": [
          {
            "x": 0.5036,
            "y": 0.5232,
            "angle": 94.3
          },
          {
            "x": 0.5002,
            "y": 0.5682,
            "angle": 94.3
          }
        ]
      },
      "45": {
        "slots": [
          {
            "x": 0.5286,
            "y": 0.5141,
            "angle": -150.2
          }
        ]
      },
      "46": {
        "slots": [
          {
            "x": 0.5385,
            "y": 0.5433,
            "angle": 130.2
          },
          {
            "x": 0.5119,
            "y": 0.5748,
            "angle": 130.2
          }
        ]
      },
      "47": {
        "slots": [
          {
            "x": 0.4706,
            "y": 0.3341,
            "angle": 4.7
          },
          {
            "x": 0.5038,
            "y": 0.3368,
            "angle": 4.7
          },
          {
            "x": 0.537,
            "y": 0.3395,
            "angle": 4.7
          },
          {
            "x": 0.5702,
            "y": 0.3423,
            "angle": 4.7
          }
        ]
      },
      "48": {
        "slots": [
          {
            "x": 0.4696,
            "y": 0.346,
            "angle": 4.7
          },
          {
            "x": 0.5028,
            "y": 0.3488,
            "angle": 4.7
          },
          {
            "x": 0.536,
            "y": 0.3515,
            "angle": 4.7
          },
          {
            "x": 0.5692,
            "y": 0.3542,
            "angle": 4.7
          }
        ]
      },
      "49": {
        "slots": [
          {
            "x": 0.4688,
            "y": 0.3272,
            "angle": -36.9
          },
          {
            "x": 0.4993,
            "y": 0.3043,
            "angle": -36.9
          },
          {
            "x": 0.5298,
            "y": 0.2813,
            "angle": -36.9
          }
        ]
      },
      "50": {
        "slots": [
          {
            "x": 0.5762,
            "y": 0.3685,
            "angle": 118.2
          },
          {
            "x": 0.5559,
            "y": 0.4063,
            "angle": 118.2
          },
          {
            "x": 0.5357,
            "y": 0.4441,
            "angle": 118.2
          },
          {
            "x": 0.5154,
            "y": 0.4819,
            "angle": 118.2
          }
        ]
      },
      "51": {
        "slots": [
          {
            "x": 0.4354,
            "y": 0.6382,
            "angle": 88.6
          },
          {
            "x": 0.437,
            "y": 0.7048,
            "angle": 88.6
          }
        ]
      },
      "52": {
        "slots": [
          {
            "x": 0.4506,
            "y": 0.6013,
            "angle": -12.6
          },
          {
            "x": 0.4825,
            "y": 0.5942,
            "angle": -12.6
          }
        ]
      },
      "53": {
        "slots": [
          {
            "x": 0.4615,
            "y": 0.7499,
            "angle": 26.5
          },
          {
            "x": 0.5089,
            "y": 0.7736,
            "angle": 26.5
          }
        ]
      },
      "54": {
        "slots": [
          {
            "x": 0.4397,
            "y": 0.7559,
            "angle": 84.0
          },
          {
            "x": 0.4434,
            "y": 0.7914,
            "angle": 84.0
          },
          {
            "x": 0.4472,
            "y": 0.8269,
            "angle": 84.0
          },
          {
            "x": 0.4509,
            "y": 0.8624,
            "angle": 84.0
          }
        ]
      },
      "55": {
        "slots": [
          {
            "x": 0.5193,
            "y": 0.8012,
            "angle": 130.1
          },
          {
            "x": 0.4927,
            "y": 0.8328,
            "angle": 130.1
          },
          {
            "x": 0.4661,
            "y": 0.8644,
            "angle": 130.1
          }
        ]
      },
      "56": {
        "slots": [
          {
            "x": 0.5453,
            "y": 0.7979,
            "angle": 44.7
          },
          {
            "x": 0.5706,
            "y": 0.8229,
            "angle": 44.7
          },
          {
            "x": 0.5958,
            "y": 0.848,
            "angle": 44.7
          },
          {
            "x": 0.6211,
            "y": 0.873,
            "angle": 44.7
          }
        ]
      },
      "57": {
        "slots": [
          {
            "x": 0.4728,
            "y": 0.8793,
            "angle": -2.7
          },
          {
            "x": 0.5129,
            "y": 0.8774,
            "angle": -2.7
          },
          {
            "x": 0.553,
            "y": 0.8755,
            "angle": -2.7
          },
          {
            "x": 0.5931,
            "y": 0.8736,
            "angle": -2.7
          },
          {
            "x": 0.6332,
            "y": 0.8717,
            "angle": -2.7
          },
          {
            "x": 0.6733,
            "y": 0.8698,
            "angle": -2.7
          }
        ]
      },
      "58": {
        "slots": [
          {
            "x": 0.5056,
            "y": 0.6029,
            "angle": 60.1
          },
          {
            "x": 0.5197,
            "y": 0.6275,
            "angle": 60.1
          },
          {
            "x": 0.5338,
            "y": 0.652,
            "angle": 60.1
          }
        ]
      },
      "59": {
        "slots": [
          {
            "x": 0.5501,
            "y": 0.5503,
            "angle": 94.6
          },
          {
            "x": 0.5464,
            "y": 0.5959,
            "angle": 94.6
          },
          {
            "x": 0.5427,
            "y": 0.6415,
            "angle": 94.6
          }
        ]
      },
      "60": {
        "slots": [
          {
            "x": 0.5686,
            "y": 0.5182,
            "angle": -29.0
          },
          {
            "x": 0.602,
            "y": 0.4997,
            "angle": -29.0
          },
          {
            "x": 0.6354,
            "y": 0.4812,
            "angle": -29.0
          },
          {
            "x": 0.6688,
            "y": 0.4627,
            "angle": -29.0
          },
          {
            "x": 0.7022,
            "y": 0.4442,
            "angle": -29.0
          },
          {
            "x": 0.7356,
            "y": 0.4257,
            "angle": -29.0
          }
        ]
      },
      "61": {
        "slots": [
          {
            "x": 0.6598,
            "y": 0.6285,
            "angle": -136.9
          },
          {
            "x": 0.629,
            "y": 0.5996,
            "angle": -136.9
          },
          {
            "x": 0.5981,
            "y": 0.5708,
            "angle": -136.9
          },
          {
            "x": 0.5673,
            "y": 0.5419,
            "angle": -136.9
          }
        ]
      },
      "62": {
        "slots": [
          {
            "x": 0.6848,
            "y": 0.6146,
            "angle": -71.2
          },
          {
            "x": 0.7041,
            "y": 0.558,
            "angle": -71.2
          },
          {
            "x": 0.7234,
            "y": 0.5014,
            "angle": -71.2
          },
          {
            "x": 0.7427,
            "y": 0.4448,
            "angle": -71.2
          }
        ]
      },
      "63": {
        "slots": [
          {
            "x": 0.7315,
            "y": 0.4081,
            "angle": -158.0
          },
          {
            "x": 0.6901,
            "y": 0.3914,
            "angle": -158.0
          },
          {
            "x": 0.6485,
            "y": 0.3747,
            "angle": -158.0
          },
          {
            "x": 0.6071,
            "y": 0.358,
            "angle": -158.0
          }
        ]
      },
      "64": {
        "slots": [
          {
            "x": 0.5525,
            "y": 0.6919,
            "angle": 67.2
          },
          {
            "x": 0.5757,
            "y": 0.7472,
            "angle": 67.2
          },
          {
            "x": 0.599,
            "y": 0.8025,
            "angle": 67.2
          },
          {
            "x": 0.6222,
            "y": 0.8579,
            "angle": 67.2
          }
        ]
      },
      "65": {
        "slots": [
          {
            "x": 0.5623,
            "y": 0.6752,
            "angle": 27.0
          },
          {
            "x": 0.6052,
            "y": 0.6971,
            "angle": 27.0
          }
        ]
      },
      "66": {
        "slots": [
          {
            "x": 0.6431,
            "y": 0.7186,
            "angle": 32.7
          },
          {
            "x": 0.6761,
            "y": 0.7397,
            "angle": 32.7
          },
          {
            "x": 0.709,
            "y": 0.7609,
            "angle": 32.7
          }
        ]
      },
      "67": {
        "slots": [
          {
            "x": 0.6836,
            "y": 0.6643,
            "angle": 68.6
          },
          {
            "x": 0.7004,
            "y": 0.7072,
            "angle": 68.6
          },
          {
            "x": 0.7171,
            "y": 0.7501,
            "angle": 68.6
          }
        ]
      },
      "68": {
        "slots": [
          {
            "x": 0.6631,
            "y": 0.6592,
            "angle": 126.7
          },
          {
            "x": 0.6388,
            "y": 0.6917,
            "angle": 126.7
          }
        ]
      },
      "69": {
        "slots": [
          {
            "x": 0.6326,
            "y": 0.8559,
            "angle": -92.3
          },
          {
            "x": 0.6302,
            "y": 0.7967,
            "angle": -92.3
          },
          {
            "x": 0.6278,
            "y": 0.7376,
            "angle": -92.3
          }
        ]
      },
      "70": {
        "slots": [
          {
            "x": 0.6487,
            "y": 0.8813,
            "angle": -15.6
          },
          {
            "x": 0.6784,
            "y": 0.873,
            "angle": -15.6
          }
        ]
      },
      "71": {
        "slots": [
          {
            "x": 0.7887,
            "y": 0.6367,
            "angle": 176.9
          },
          {
            "x": 0.7563,
            "y": 0.6385,
            "angle": 176.9
          },
          {
            "x": 0.7238,
            "y": 0.6402,
            "angle": 176.9
          },
          {
            "x": 0.6914,
            "y": 0.642,
            "angle": 176.9
          }
        ]
      },
      "72": {
        "slots": [
          {
            "x": 0.7175,
            "y": 0.7958,
            "angle": 108.3
          },
          {
            "x": 0.7014,
            "y": 0.8446,
            "angle": 108.3
          }
        ]
      },
      "73": {
        "slots": [
          {
            "x": 0.7424,
            "y": 0.7816,
            "angle": 31.0
          },
          {
            "x": 0.7763,
            "y": 0.802,
            "angle": 31.0
          }
        ]
      },
      "74": {
        "slots": [
          {
            "x": 0.71,
            "y": 0.8594,
            "angle": -29.6
          },
          {
            "x": 0.7432,
            "y": 0.8405,
            "angle": -29.6
          },
          {
            "x": 0.7766,
            "y": 0.8216,
            "angle": -29.6
          }
        ]
      },
      "75": {
        "slots": [
          {
            "x": 0.8177,
            "y": 0.8121,
            "angle": 0.0
          },
          {
            "x": 0.8666,
            "y": 0.8121,
            "angle": 0.0
          },
          {
            "x": 0.9155,
            "y": 0.8121,
            "angle": 0.0
          }
        ]
      },
      "76": {
        "slots": [
          {
            "x": 0.795,
            "y": 0.6528,
            "angle": 120.3
          },
          {
            "x": 0.7751,
            "y": 0.6867,
            "angle": 120.3
          },
          {
            "x": 0.7553,
            "y": 0.7206,
            "angle": 120.3
          },
          {
            "x": 0.7354,
            "y": 0.7545,
            "angle": 120.3
          }
        ]
      },
      "77": {
        "slots": [
          {
            "x": 0.8218,
            "y": 0.6578,
            "angle": 52.5
          },
          {
            "x": 0.8556,
            "y": 0.7019,
            "angle": 52.5
          },
          {
            "x": 0.8893,
            "y": 0.746,
            "angle": 52.5
          },
          {
            "x": 0.9231,
            "y": 0.7901,
            "angle": 52.5
          }
        ]
      },
      "78": {
        "slots": [
          {
            "x": 0.9177,
            "y": 0.6974,
            "angle": 79.0
          },
          {
            "x": 0.9266,
            "y": 0.7433,
            "angle": 79.0
          },
          {
            "x": 0.9355,
            "y": 0.7892,
            "angle": 79.0
          }
        ]
      },
      "79": {
        "slots": [
          {
            "x": 0.832,
            "y": 0.6455,
            "angle": 19.6
          },
          {
            "x": 0.8862,
            "y": 0.6648,
            "angle": 19.6
          }
        ]
      },
      "80": {
        "slots": [
          {
            "x": 0.9131,
            "y": 0.6402,
            "angle": -90.4
          },
          {
            "x": 0.9126,
            "y": 0.5715,
            "angle": -90.4
          }
        ]
      },
      "81": {
        "slots": [
          {
            "x": 0.8183,
            "y": 0.6235,
            "angle": -42.5
          },
          {
            "x": 0.8452,
            "y": 0.5988,
            "angle": -42.5
          },
          {
            "x": 0.8721,
            "y": 0.5742,
            "angle": -42.5
          },
          {
            "x": 0.899,
            "y": 0.5495,
            "angle": -42.5
          }
        ]
      },
      "82": {
        "slots": [
          {
            "x": 0.8973,
            "y": 0.5113,
            "angle": -120.3
          },
          {
            "x": 0.8671,
            "y": 0.4597,
            "angle": -120.3
          }
        ]
      },
      "83": {
        "slots": [
          {
            "x": 0.8395,
            "y": 0.4316,
            "angle": -170.2
          },
          {
            "x": 0.8146,
            "y": 0.4273,
            "angle": -170.2
          },
          {
            "x": 0.7897,
            "y": 0.423,
            "angle": -170.2
          },
          {
            "x": 0.7648,
            "y": 0.4187,
            "angle": -170.2
          }
        ]
      },
      "84": {
        "slots": [
          {
            "x": 0.855,
            "y": 0.4067,
            "angle": -83.6
          },
          {
            "x": 0.8611,
            "y": 0.3524,
            "angle": -83.6
          },
          {
            "x": 0.8672,
            "y": 0.2982,
            "angle": -83.6
          },
          {
            "x": 0.8733,
            "y": 0.2439,
            "angle": -83.6
          }
        ]
      },
      "85": {
        "slots": [
          {
            "x": 0.8519,
            "y": 0.2259,
            "angle": 159.5
          },
          {
            "x": 0.8033,
            "y": 0.2442,
            "angle": 159.5
          }
        ]
      },
      "86": {
        "slots": [
          {
            "x": 0.8604,
            "y": 0.1972,
            "angle": -129.0
          },
          {
            "x": 0.8286,
            "y": 0.158,
            "angle": -129.0
          },
          {
            "x": 0.7969,
            "y": 0.1188,
            "angle": -129.0
          },
          {
            "x": 0.7651,
            "y": 0.0796,
            "angle": -129.0
          }
        ]
      },
      "87": {
        "slots": [
          {
            "x": 0.7382,
            "y": 0.0846,
            "angle": 114.1
          },
          {
            "x": 0.7161,
            "y": 0.1339,
            "angle": 114.1
          },
          {
            "x": 0.694,
            "y": 0.1832,
            "angle": 114.1
          },
          {
            "x": 0.6719,
            "y": 0.2325,
            "angle": 114.1
          }
        ]
      },
      "88": {
        "slots": [
          {
            "x": 0.7356,
            "y": 0.074,
            "angle": 134.1
          },
          {
            "x": 0.7084,
            "y": 0.102,
            "angle": 134.1
          },
          {
            "x": 0.6812,
            "y": 0.1301,
            "angle": 134.1
          },
          {
            "x": 0.654,
            "y": 0.1581,
            "angle": 134.1
          }
        ]
      },
      "89": {
        "slots": [
          {
            "x": 0.7358,
            "y": 0.0614,
            "angle": 174.0
          },
          {
            "x": 0.709,
            "y": 0.0642,
            "angle": 174.0
          },
          {
            "x": 0.6823,
            "y": 0.0671,
            "angle": 174.0
          },
          {
            "x": 0.6555,
            "y": 0.0699,
            "angle": 174.0
          },
          {
            "x": 0.6287,
            "y": 0.0727,
            "angle": 174.0
          },
          {
            "x": 0.6019,
            "y": 0.0755,
            "angle": 174.0
          },
          {
            "x": 0.5752,
            "y": 0.0784,
            "angle": 174.0
          },
          {
            "x": 0.5484,
            "y": 0.0812,
            "angle": 174.0
          }
        ]
      },
      "90": {
        "slots": [
          {
            "x": 0.5239,
            "y": 0.1089,
            "angle": 125.0
          },
          {
            "x": 0.4919,
            "y": 0.1546,
            "angle": 125.0
          },
          {
            "x": 0.4599,
            "y": 0.2004,
            "angle": 125.0
          }
        ]
      },
      "91": {
        "slots": [
          {
            "x": 0.5141,
            "y": 0.102,
            "angle": 125.0
          },
          {
            "x": 0.4821,
            "y": 0.1478,
            "angle": 125.0
          },
          {
            "x": 0.4501,
            "y": 0.1935,
            "angle": 125.0
          }
        ]
      },
      "92": {
        "slots": [
          {
            "x": 0.6245,
            "y": 0.1884,
            "angle": 134.3
          },
          {
            "x": 0.5927,
            "y": 0.2209,
            "angle": 134.3
          },
          {
            "x": 0.561,
            "y": 0.2535,
            "angle": 134.3
          }
        ]
      },
      "93": {
        "slots": [
          {
            "x": 0.5554,
            "y": 0.2898,
            "angle": 62.7
          },
          {
            "x": 0.576,
            "y": 0.3296,
            "angle": 62.7
          }
        ]
      },
      "94": {
        "slots": [
          {
            "x": 0.5987,
            "y": 0.3342,
            "angle": -51.1
          },
          {
            "x": 0.6236,
            "y": 0.3034,
            "angle": -51.1
          },
          {
            "x": 0.6485,
            "y": 0.2725,
            "angle": -51.1
          }
        ]
      },
      "95": {
        "slots": [
          {
            "x": 0.6583,
            "y": 0.2465,
            "angle": -103.6
          },
          {
            "x": 0.6532,
            "y": 0.2252,
            "angle": -103.6
          },
          {
            "x": 0.6481,
            "y": 0.204,
            "angle": -103.6
          },
          {
            "x": 0.643,
            "y": 0.1827,
            "angle": -103.6
          }
        ]
      },
      "96": {
        "slots": [
          {
            "x": 0.6806,
            "y": 0.2565,
            "angle": -1.8
          },
          {
            "x": 0.7199,
            "y": 0.2552,
            "angle": -1.8
          },
          {
            "x": 0.7592,
            "y": 0.2539,
            "angle": -1.8
          }
        ]
      },
      "97": {
        "slots": [
          {
            "x": 0.6838,
            "y": 0.2969,
            "angle": 60.2
          },
          {
            "x": 0.7294,
            "y": 0.3766,
            "angle": 60.2
          }
        ]
      },
      "98": {
        "slots": [
          {
            "x": 0.7745,
            "y": 0.2805,
            "angle": 99.3
          },
          {
            "x": 0.7656,
            "y": 0.3349,
            "angle": 99.3
          },
          {
            "x": 0.7567,
            "y": 0.3893,
            "angle": 99.3
          }
        ]
      }
    },
    "slot": {
      "width": 0.022,
      "height": 0.009,
      "corner_radius": 0.003
    }
  }
}
$ttrjson$::jsonb,
        'ecced3f439192b5cfbff921a07b1a87ad0d18b739d69eb1fb3239fbdabdd3aa8', now())
ON CONFLICT (map_id, version) DO NOTHING;
