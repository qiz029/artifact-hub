CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE collections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description text NOT NULL DEFAULT '',
    color varchar(7) NOT NULL DEFAULT '#5E6AD2' CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    collection_id uuid NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    slug text NOT NULL,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '',
    artifact_type text NOT NULL CHECK (artifact_type IN ('html', 'markdown')),
    media_type text NOT NULL,
    original_filename text NOT NULL,
    content bytea NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    sha256 char(64) NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    tags text[] NOT NULL DEFAULT '{}',
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (collection_id, slug)
);

CREATE INDEX artifacts_collection_created_idx ON artifacts (collection_id, created_at DESC);
CREATE INDEX artifacts_tags_idx ON artifacts USING gin (tags);
CREATE INDEX artifacts_metadata_idx ON artifacts USING gin (metadata);

CREATE FUNCTION reject_artifact_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'artifacts are immutable; create a new artifact instead';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER artifacts_are_immutable
BEFORE UPDATE ON artifacts
FOR EACH ROW EXECUTE FUNCTION reject_artifact_update();
