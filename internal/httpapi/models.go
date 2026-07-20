package httpapi

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Collection struct {
	ID            uuid.UUID `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Color         string    `json:"color"`
	ArtifactCount int       `json:"artifactCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ArtifactRef struct {
	ArtifactID   uuid.UUID `json:"artifactId"`
	SeriesID     uuid.UUID `json:"seriesId"`
	Slug         string    `json:"slug"`
	Title        string    `json:"title"`
	CollectionID uuid.UUID `json:"collectionId"`
}

type Artifact struct {
	ID               uuid.UUID       `json:"id"`
	CollectionID     uuid.UUID       `json:"collectionId"`
	CollectionName   string          `json:"collectionName,omitempty"`
	SeriesID         uuid.UUID       `json:"seriesId"`
	Version          int             `json:"version"`
	Slug             string          `json:"slug"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Type             string          `json:"type"`
	MediaType        string          `json:"mediaType"`
	OriginalFilename string          `json:"originalFilename"`
	SizeBytes        int64           `json:"sizeBytes"`
	SHA256           string          `json:"sha256"`
	Tags             []string        `json:"tags"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        time.Time       `json:"createdAt"`
	ContentURL       string          `json:"contentUrl"`
	PublicURL        string          `json:"publicUrl"`
	Links            []ArtifactRef   `json:"links,omitempty"`
	Backlinks        []ArtifactRef   `json:"backlinks,omitempty"`
}
