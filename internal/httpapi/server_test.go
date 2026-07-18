package httpapi

import (
	"net/http"
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

func TestWriteArtifactContentRendersMarkdownPublicPage(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/a/11111111-1111-1111-1111-111111111111/release-notes", nil)
	recorder := httptest.NewRecorder()
	artifact := artifactContent{
		content:   []byte("# Release notes\n\nA useful **document** with $E = mc^2$.\n\n| Status | Owner |\n| --- | --- |\n| Ready | Todd |\n\n```mermaid\ngraph TD\n  A --> B\n```\n\n<script>alert('no')</script>"),
		mediaType: "text/markdown",
		filename:  "release-notes.md",
		title:     "Release notes",
		hash:      "abc123",
	}

	writeArtifactContent(recorder, req, artifact)

	response := recorder.Result()
	if got := response.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want rendered HTML", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want rendered Markdown pages to revalidate", got)
	}
	if got := response.Header.Get("ETag"); got == `"sha256-abc123"` {
		t.Fatalf("ETag = %q, want the final rendered representation hash", got)
	}
	body := recorder.Body.String()
	for _, fragment := range []string{"<!doctype html>", "<h1>Release notes</h1>", "<strong>document</strong>", "<table>", `class="language-mermaid"`, `data-code-copy="enabled"`, `data-math-render="enabled"`, `href="/markdown-assets/katex/katex.min.css"`, `src="/markdown-assets/enhance.js"`, `data-reader-toc`, `class="settings-pill"`, `class="settings-icon"`, `aria-controls="reader-settings-panel"`, `data-reader-font`, `data-reader-size`, `data-reader-leading`, "Artifact Hub"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("rendered page missing %q", fragment)
		}
	}
	if got := strings.Count(body, `<option value=`); got != 10 {
		t.Fatalf("font option count = %d, want 10", got)
	}
	if got := strings.Count(body, `<span>Font family</span>`); got != 1 {
		t.Fatalf("Font family label count = %d, want 1", got)
	}
	if strings.Contains(body, `class="site-header"`) {
		t.Fatal("rendered Markdown page included the application header")
	}
	if !strings.Contains(body, `background: #f3f0e8`) {
		t.Fatal("rendered Markdown page missing the warm paper background")
	}
	if strings.Contains(body, "<script>") {
		t.Fatal("rendered page included raw HTML from Markdown")
	}
	if csp := response.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("Markdown page CSP = %q, want same-origin enhancement scripts", csp)
	}
}

func TestWriteArtifactContentKeepsHTMLPublicPageUnchanged(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/a/11111111-1111-1111-1111-111111111111/demo", nil)
	recorder := httptest.NewRecorder()
	rawHTML := "<!doctype html><html><body><h1>Own design</h1></body></html>"

	writeArtifactContent(recorder, req, artifactContent{
		content:   []byte(rawHTML),
		mediaType: "text/html",
		filename:  "demo.html",
		title:     "Demo",
		hash:      "def456",
	})

	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != rawHTML {
		t.Fatalf("HTML artifact changed: %q", got)
	}
	if csp := recorder.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox ") {
		t.Fatalf("HTML artifact CSP = %q, want sandbox", csp)
	}
}

func TestWriteArtifactContentKeepsMarkdownAPIRaw(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/artifacts/11111111-1111-1111-1111-111111111111/content", nil)
	recorder := httptest.NewRecorder()
	rawMarkdown := "# Release notes\n\nStill source text."

	writeArtifactContent(recorder, req, artifactContent{
		content:   []byte(rawMarkdown),
		mediaType: "text/markdown",
		filename:  "release-notes.md",
		title:     "Release notes",
		hash:      "abc123",
	})

	if got := recorder.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != rawMarkdown {
		t.Fatalf("Markdown API body changed: %q", got)
	}
}
