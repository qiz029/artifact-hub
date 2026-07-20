package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getTestArtifact(t *testing.T, server http.Handler, artifactID string) Artifact {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/artifacts/%s", artifactID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get artifact: status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var artifact Artifact
	if err := json.Unmarshal(recorder.Body.Bytes(), &artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	return artifact
}

func deleteTestArtifact(t *testing.T, server http.Handler, artifactID string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/artifacts/%s", artifactID), nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete artifact: status = %d, body = %s", recorder.Code, recorder.Body)
	}
}

func findRef(refs []ArtifactRef, artifactID string) *ArtifactRef {
	for index := range refs {
		if refs[index].ArtifactID.String() == artifactID {
			return &refs[index]
		}
	}
	return nil
}

func markdownLinkingTo(target Artifact) string {
	return fmt.Sprintf("# Doc\n\nSee [the reference](/a/%s/%s).\n", target.ID, target.Slug)
}

func TestLinksAndBacklinks(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	target := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Target", "slug": "target"}, "target.md", "# Target\n")
	source := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Source", "slug": "source"}, "source.md", markdownLinkingTo(target))

	gotSource := getTestArtifact(t, server, source.ID.String())
	if len(gotSource.Links) != 1 {
		t.Fatalf("source links = %+v, want exactly one", gotSource.Links)
	}
	link := gotSource.Links[0]
	if link.ArtifactID != target.ID || link.SeriesID != target.SeriesID || link.Slug != target.Slug || link.Title != target.Title || link.CollectionID != collection.ID {
		t.Fatalf("link ref = %+v, want latest target %+v", link, target)
	}

	gotTarget := getTestArtifact(t, server, target.ID.String())
	if len(gotTarget.Backlinks) != 1 {
		t.Fatalf("target backlinks = %+v, want exactly one", gotTarget.Backlinks)
	}
	backlink := gotTarget.Backlinks[0]
	if backlink.ArtifactID != source.ID || backlink.SeriesID != source.SeriesID || backlink.Title != source.Title {
		t.Fatalf("backlink ref = %+v, want source %+v", backlink, source)
	}
}

func TestNewVersionWithoutLinkReplacesLinkSet(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	target := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Target", "slug": "target"}, "target.md", "# Target\n")
	fields := map[string]string{"title": "Source", "slug": "source"}
	mustUpload(t, server, collection.ID.String(), fields, "source.md", markdownLinkingTo(target))
	second := mustUpload(t, server, collection.ID.String(), fields, "source.md", "# Doc\n\nNo links anymore.\n")

	if links := getTestArtifact(t, server, second.ID.String()).Links; len(links) != 0 {
		t.Fatalf("v2 links = %+v, want none after the link was removed", links)
	}
	if backlinks := getTestArtifact(t, server, target.ID.String()).Backlinks; len(backlinks) != 0 {
		t.Fatalf("target backlinks = %+v, want none after the link was removed", backlinks)
	}
}

func TestNewVersionKeepingLinkLeavesExactlyOneRow(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	target := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Target", "slug": "target"}, "target.md", "# Target\n")
	fields := map[string]string{"title": "Source", "slug": "source"}
	first := mustUpload(t, server, collection.ID.String(), fields, "source.md", markdownLinkingTo(target))
	second := mustUpload(t, server, collection.ID.String(), fields, "source.md", markdownLinkingTo(target)+"More text.\n")

	if links := getTestArtifact(t, server, second.ID.String()).Links; len(links) != 1 {
		t.Fatalf("v2 links = %+v, want exactly one", links)
	}
	if backlinks := getTestArtifact(t, server, target.ID.String()).Backlinks; len(backlinks) != 1 {
		t.Fatalf("target backlinks = %+v, want exactly one", backlinks)
	}
	// The recorded source moved to the latest version.
	if links := getTestArtifact(t, server, first.ID.String()).Links; len(links) != 1 || links[0].ArtifactID != target.ID {
		t.Fatalf("reading v1 of the series: links = %+v, want the series link set", links)
	}
}

func TestSelfSeriesLinkSkipped(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	fields := map[string]string{"title": "Source", "slug": "source"}
	first := mustUpload(t, server, collection.ID.String(), fields, "source.md", "# Doc v1\n")
	// v2 links to v1's public URL: same series, must be skipped.
	second := mustUpload(t, server, collection.ID.String(), fields, "source.md", markdownLinkingTo(first))

	if links := getTestArtifact(t, server, second.ID.String()).Links; len(links) != 0 {
		t.Fatalf("self-series link was recorded: %+v", links)
	}
	if backlinks := getTestArtifact(t, server, first.ID.String()).Backlinks; len(backlinks) != 0 {
		t.Fatalf("self-series backlink was recorded: %+v", backlinks)
	}
}

func TestLinkToNonexistentArtifactSkipped(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	source := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Source", "slug": "source"}, "source.md",
		"# Doc\n\nSee [missing](/a/11111111-1111-1111-1111-111111111111/whatever).\n")

	if links := getTestArtifact(t, server, source.ID.String()).Links; len(links) != 0 {
		t.Fatalf("link to nonexistent artifact was recorded: %+v", links)
	}
}

func TestDeleteLastVersionCleansInboundLinks(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	targetFields := map[string]string{"title": "Target", "slug": "target"}
	targetV1 := mustUpload(t, server, collection.ID.String(), targetFields, "target.md", "# Target v1\n")
	targetV2 := mustUpload(t, server, collection.ID.String(), targetFields, "target.md", "# Target v2\n")
	source := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Source", "slug": "source"}, "source.md", markdownLinkingTo(targetV1))

	deleteTestArtifact(t, server, targetV1.ID.String())
	if links := getTestArtifact(t, server, source.ID.String()).Links; len(links) != 1 || links[0].ArtifactID != targetV2.ID {
		t.Fatalf("after deleting target v1: links = %+v, want the link resolved to v2", links)
	}
	deleteTestArtifact(t, server, targetV2.ID.String())
	if links := getTestArtifact(t, server, source.ID.String()).Links; len(links) != 0 {
		t.Fatalf("after deleting the whole target series: links = %+v, want none", links)
	}
}

func TestDeleteSourceCascadesBacklinks(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	target := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Target", "slug": "target"}, "target.md", "# Target\n")
	source := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Source", "slug": "source"}, "source.md", markdownLinkingTo(target))

	deleteTestArtifact(t, server, source.ID.String())
	if backlinks := getTestArtifact(t, server, target.ID.String()).Backlinks; len(backlinks) != 0 {
		t.Fatalf("after deleting the source: backlinks = %+v, want none", backlinks)
	}
}

func TestIdempotentReplayKeepsLinkState(t *testing.T) {
	server := testServer(t)
	collection := createTestCollection(t, server)

	target := mustUpload(t, server, collection.ID.String(),
		map[string]string{"title": "Target", "slug": "target"}, "target.md", "# Target\n")
	fields := map[string]string{"title": "Source", "slug": "source"}
	content := markdownLinkingTo(target)
	first := mustUpload(t, server, collection.ID.String(), fields, "source.md", content)

	replay, status := uploadTestArtifact(t, server, collection.ID.String(), fields, "source.md", content)
	if status != http.StatusOK || replay.ID != first.ID {
		t.Fatalf("replay: status = %d id = %s, want 200 with %s", status, replay.ID, first.ID)
	}
	if links := getTestArtifact(t, server, first.ID.String()).Links; len(links) != 1 {
		t.Fatalf("after replay: links = %+v, want exactly one", links)
	}
	if backlinks := getTestArtifact(t, server, target.ID.String()).Backlinks; len(backlinks) != 1 {
		t.Fatalf("after replay: backlinks = %+v, want exactly one", backlinks)
	}
}
