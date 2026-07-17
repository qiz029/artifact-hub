package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestArtifactFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		filename string
		wantType string
		wantErr  bool
	}{
		{"report.html", "html", false},
		{"notes.MD", "markdown", false},
		{"image.svg", "", true},
	}
	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			got, _, err := artifactFormat(test.filename, []byte("content"))
			if (err != nil) != test.wantErr || got != test.wantType {
				t.Fatalf("artifactFormat() = %q, %v", got, err)
			}
		})
	}
}

func TestParseTagsDeduplicatesAndTrims(t *testing.T) {
	t.Parallel()
	got := parseTags("Design, ui, design,  release ")
	want := []string{"Design", "ui", "release"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parseTags() = %#v, want %#v", got, want)
	}
}

func TestUniqueSlug(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if got := uniqueSlug("Q3 Roadmap / 设计", id); got != "q3-roadmap-11111111" {
		t.Fatalf("uniqueSlug() = %q", got)
	}
}

func TestMatchesETag(t *testing.T) {
	t.Parallel()
	etag := `"sha256-abc123"`
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "exact", header: etag, want: true},
		{name: "weak", header: `W/"sha256-abc123"`, want: true},
		{name: "list", header: `"other", "sha256-abc123"`, want: true},
		{name: "wildcard", header: "*", want: true},
		{name: "different", header: `"sha256-def456"`, want: false},
		{name: "missing", header: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := matchesETag(test.header, etag); got != test.want {
				t.Fatalf("matchesETag(%q, %q) = %v, want %v", test.header, etag, got, test.want)
			}
		})
	}
}

func TestAddURLsCreatesReadableStableLink(t *testing.T) {
	t.Parallel()
	s := &Server{publicURL: "https://hub.example"}
	artifact := Artifact{
		ID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Slug: "release-notes-11111111",
	}
	req := httptest.NewRequest("GET", "http://internal/api/collections", nil)

	s.addURLs(req, &artifact)

	wantPublic := "https://hub.example/a/11111111-1111-1111-1111-111111111111/release-notes-11111111"
	if artifact.PublicURL != wantPublic {
		t.Fatalf("PublicURL = %q, want %q", artifact.PublicURL, wantPublic)
	}
	wantContent := "https://hub.example/api/artifacts/11111111-1111-1111-1111-111111111111/content"
	if artifact.ContentURL != wantContent {
		t.Fatalf("ContentURL = %q, want %q", artifact.ContentURL, wantContent)
	}
}
