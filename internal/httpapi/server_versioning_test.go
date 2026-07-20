package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qiz029/artifact-hub/internal/database"
)

// Integration tests run against a real Postgres when
// ARTIFACT_HUB_TEST_DATABASE_URL is set; otherwise they are skipped.
func testServer(t *testing.T) http.Handler {
	t.Helper()
	dsn := os.Getenv("ARTIFACT_HUB_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARTIFACT_HUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE artifact_links, artifacts, collections"); err != nil {
		t.Fatalf("reset test database: %v", err)
	}
	return New(pool, Options{})
}

func createTestCollection(t *testing.T, server http.Handler) Collection {
	t.Helper()
	body := bytes.NewBufferString(`{"name":"Docs"}`)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/collections", body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create collection: status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var collection Collection
	if err := json.Unmarshal(recorder.Body.Bytes(), &collection); err != nil {
		t.Fatalf("decode collection: %v", err)
	}
	return collection
}

// uploadTestArtifact uploads filename/content with the given extra form fields
// (e.g. title, slug, tags, description) and returns the decoded artifact
// (only valid on 2xx) plus the HTTP status.
func uploadTestArtifact(t *testing.T, server http.Handler, collectionID string, fields map[string]string, filename, content string) (Artifact, int) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field %s: %v", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/collections/%s/artifacts", collectionID), &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var artifact Artifact
	if recorder.Code >= 200 && recorder.Code < 300 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &artifact); err != nil {
			t.Fatalf("decode artifact: %v", err)
		}
	}
	return artifact, recorder.Code
}

func mustUpload(t *testing.T, server http.Handler, collectionID string, fields map[string]string, filename, content string) Artifact {
	t.Helper()
	artifact, status := uploadTestArtifact(t, server, collectionID, fields, filename, content)
	if status != http.StatusCreated {
		t.Fatalf("upload artifact: status = %d, want 201", status)
	}
	return artifact
}

func listTestArtifacts(t *testing.T, server http.Handler, collectionID string) []Artifact {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/collections/%s/artifacts", collectionID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list artifacts: status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var artifacts []Artifact
	if err := json.Unmarshal(recorder.Body.Bytes(), &artifacts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}
	return artifacts
}

func listTestVersions(t *testing.T, server http.Handler, artifactID string) []Artifact {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/artifacts/%s/versions", artifactID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list versions: status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var versions []Artifact
	if err := json.Unmarshal(recorder.Body.Bytes(), &versions); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	return versions
}

func TestSameSlugSameContentIsIdempotentReplay(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)
	fields := map[string]string{"title": "Release notes", "slug": "release-notes"}

	first := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")
	replay, status := uploadTestArtifact(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")

	if status != http.StatusOK {
		t.Fatalf("replay: status = %d, want 200", status)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay: id = %s, want the existing artifact %s", replay.ID, first.ID)
	}
	if replay.Version != 1 || replay.SeriesID != first.SeriesID {
		t.Fatalf("replay: version = %d seriesId = %s, want v1 of %s", replay.Version, replay.SeriesID, first.SeriesID)
	}
	if versions := listTestVersions(t, server, first.ID.String()); len(versions) != 1 {
		t.Fatalf("replay created a new version: %d versions, want 1", len(versions))
	}
}

func TestIdempotentReplayIgnoresMetadataDifferences(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	first := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Release notes", "slug": "release-notes", "tags": "v1", "description": "first"},
		"notes.md", "# v1\n")
	replay, status := uploadTestArtifact(t, server, collection.ID.String(),
		map[string]string{"title": "Release notes", "slug": "release-notes", "tags": "v2,changed", "description": "changed"},
		"notes.md", "# v1\n")

	if status != http.StatusOK {
		t.Fatalf("replay with different metadata: status = %d, want 200", status)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay with different metadata: id = %s, want %s", replay.ID, first.ID)
	}
	if versions := listTestVersions(t, server, first.ID.String()); len(versions) != 1 {
		t.Fatalf("metadata-only change created a version: %d versions, want 1", len(versions))
	}
}

func TestSameSlugChangedContentCreatesNewVersion(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)
	fields := map[string]string{"title": "Release notes", "slug": "release-notes"}

	first := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")
	second, status := uploadTestArtifact(t, server, collection.ID.String(), fields, "notes.md", "# v2\n")

	if status != http.StatusCreated {
		t.Fatalf("changed content: status = %d, want 201", status)
	}
	if second.Version != 2 {
		t.Fatalf("changed content: version = %d, want 2", second.Version)
	}
	if second.SeriesID != first.SeriesID {
		t.Fatalf("changed content: seriesId = %s, want shared series %s", second.SeriesID, first.SeriesID)
	}
	if second.ID == first.ID {
		t.Fatal("changed content reused the existing artifact id")
	}
	if second.Slug != "release-notes" {
		t.Fatalf("changed content: slug = %q, want declared slug verbatim", second.Slug)
	}
}

func TestExplicitNewSlugStartsNewSeries(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	artifact := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Release notes", "slug": "release-notes"}, "notes.md", "# v1\n")

	if artifact.Version != 1 {
		t.Fatalf("new explicit slug: version = %d, want 1", artifact.Version)
	}
	if artifact.Slug != "release-notes" {
		t.Fatalf("new explicit slug: slug = %q, want it used verbatim", artifact.Slug)
	}
	if artifact.SeriesID != artifact.ID {
		t.Fatalf("new explicit slug: seriesId = %s, want artifact id %s", artifact.SeriesID, artifact.ID)
	}
}

func TestUploadWithoutSlugAlwaysStartsNewSeries(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)
	fields := map[string]string{"title": "Release notes"}

	first := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")
	second := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")

	if second.SeriesID == first.SeriesID {
		t.Fatal("upload without slug joined the existing series")
	}
	if second.Version != 1 || second.SeriesID != second.ID {
		t.Fatalf("upload without slug: version = %d seriesId = %s, want v1 with seriesId = id", second.Version, second.SeriesID)
	}
	if second.Slug == first.Slug {
		t.Fatal("upload without slug reused the auto-generated slug")
	}
}

func TestInvalidSlugRejected(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	for _, slug := range []string{"Release-Notes", "-notes", "notes-", strings.Repeat("a", 81), "release_notes"} {
		_, status := uploadTestArtifact(t, server, collection.ID.String(),
			map[string]string{"title": "Release notes", "slug": slug}, "notes.md", "# v1\n")
		if status != http.StatusBadRequest {
			t.Errorf("slug %q: status = %d, want 400", slug, status)
		}
	}
}

func TestListArtifactsReturnsLatestVersionPerSeries(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)
	fields := map[string]string{"title": "Release notes", "slug": "release-notes"}

	first := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")
	second := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v2\n")
	mustUpload(t, server, collection.ID.String(), map[string]string{"title": "Roadmap"}, "roadmap.md", "# plan\n")

	artifacts := listTestArtifacts(t, server, collection.ID.String())
	if len(artifacts) != 2 {
		t.Fatalf("list returned %d artifacts, want 2 latest versions", len(artifacts))
	}
	byID := make(map[string]Artifact, len(artifacts))
	for _, artifact := range artifacts {
		byID[artifact.ID.String()] = artifact
	}
	if _, ok := byID[first.ID.String()]; ok {
		t.Fatal("list included the superseded version 1")
	}
	latest, ok := byID[second.ID.String()]
	if !ok || latest.Version != 2 {
		t.Fatalf("list missing latest version 2 of the series: %+v", artifacts)
	}
}

func TestListArtifactVersionsNewestFirst(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)
	fields := map[string]string{"title": "Release notes", "slug": "release-notes"}

	first := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v1\n")
	second := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v2\n")
	third := mustUpload(t, server, collection.ID.String(), fields, "notes.md", "# v3\n")

	versions := listTestVersions(t, server, first.ID.String())
	if len(versions) != 3 {
		t.Fatalf("list versions returned %d artifacts, want 3", len(versions))
	}
	for index, want := range []Artifact{third, second, first} {
		got := versions[index]
		if got.ID != want.ID || got.Version != want.Version {
			t.Errorf("versions[%d] = id %s v%d, want id %s v%d", index, got.ID, got.Version, want.ID, want.Version)
		}
		if got.SeriesID != first.SeriesID {
			t.Errorf("versions[%d]: seriesId = %s, want %s", index, got.SeriesID, first.SeriesID)
		}
	}

	missing := httptest.NewRecorder()
	server.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/artifacts/11111111-1111-1111-1111-111111111111/versions", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("list versions for unknown artifact: status = %d, want 404", missing.Code)
	}
}
