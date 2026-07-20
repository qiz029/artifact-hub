ALTER TABLE artifacts ADD COLUMN series_id uuid;
ALTER TABLE artifacts ADD COLUMN version int NOT NULL DEFAULT 1;

-- artifacts_are_immutable rejects every UPDATE, so it must be disabled for
-- the one-time backfill that turns each existing artifact into its own series.
ALTER TABLE artifacts DISABLE TRIGGER artifacts_are_immutable;
UPDATE artifacts SET series_id = id;
ALTER TABLE artifacts ENABLE TRIGGER artifacts_are_immutable;

ALTER TABLE artifacts ALTER COLUMN series_id SET NOT NULL;

ALTER TABLE artifacts DROP CONSTRAINT artifacts_collection_id_slug_key;
ALTER TABLE artifacts ADD CONSTRAINT artifacts_collection_id_slug_version_key UNIQUE (collection_id, slug, version);

CREATE INDEX artifacts_series_id_idx ON artifacts (series_id);
