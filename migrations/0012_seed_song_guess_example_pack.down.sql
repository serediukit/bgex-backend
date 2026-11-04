-- Reverses migration 0012: delete the seeded "example-hits" pack by slug.
-- song_guess.pack_versions rows cascade via the FK to song_guess.packs.

DELETE FROM song_guess.packs WHERE slug = 'example-hits';
