ALTER TABLE ttr.map_versions
    ADD COLUMN validated BOOLEAN NOT NULL DEFAULT TRUE;
COMMENT ON COLUMN ttr.map_versions.validated IS
    'FALSE for a work-in-progress draft saved with ?validate=false. publish always re-runs ParseMap, so an unvalidated draft can never be published or played.';
