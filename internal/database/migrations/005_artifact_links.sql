CREATE TABLE artifact_links (
    source_artifact_id uuid NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    target_series_id   uuid NOT NULL,
    kind               text NOT NULL DEFAULT 'content',
    created_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_artifact_id, target_series_id)
);

CREATE INDEX artifact_links_target_idx ON artifact_links (target_series_id);
