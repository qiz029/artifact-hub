ALTER TABLE artifacts
    DROP CONSTRAINT artifacts_artifact_type_check;

ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_artifact_type_check
    CHECK (artifact_type IN ('html', 'markdown', 'json', 'csv'));
