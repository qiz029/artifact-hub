package httpapi

import (
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
