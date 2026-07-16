package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxArtifactSize = 10 << 20

var (
	slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)
	hexColor    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

type Options struct {
	FrontendDir string
	PublicURL   string
}

type Server struct {
	db          *pgxpool.Pool
	frontendDir string
	publicURL   string
}

func New(db *pgxpool.Pool, options Options) http.Handler {
	s := &Server{db: db, frontendDir: options.FrontendDir, publicURL: strings.TrimRight(options.PublicURL, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/collections", s.listCollections)
	mux.HandleFunc("POST /api/collections", s.createCollection)
	mux.HandleFunc("GET /api/collections/{collectionID}/artifacts", s.listArtifacts)
	mux.HandleFunc("POST /api/collections/{collectionID}/artifacts", s.createArtifact)
	mux.HandleFunc("GET /api/artifacts/{artifactID}", s.getArtifact)
	mux.HandleFunc("GET /api/artifacts/{artifactID}/content", s.getArtifactContent)
	mux.HandleFunc("DELETE /api/artifacts/{artifactID}", s.deleteArtifact)
	mux.HandleFunc("GET /a/{artifactID}", s.getArtifactContent)
	mux.HandleFunc("/", s.frontend)
	return requestLogger(securityHeaders(mux))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.slug, c.name, c.description, c.color, count(a.id), c.created_at
		FROM collections c LEFT JOIN artifacts a ON a.collection_id = c.id
		GROUP BY c.id ORDER BY c.created_at ASC`)
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer rows.Close()
	collections := make([]Collection, 0)
	for rows.Next() {
		var collection Collection
		if err := rows.Scan(&collection.ID, &collection.Slug, &collection.Name, &collection.Description, &collection.Color, &collection.ArtifactCount, &collection.CreatedAt); err != nil {
			writeDBError(w, err)
			return
		}
		collections = append(collections, collection)
	}
	writeJSON(w, http.StatusOK, collections)
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 120 {
		writeError(w, http.StatusBadRequest, "name must be between 1 and 120 characters")
		return
	}
	if input.Color == "" {
		input.Color = "#5E6AD2"
	}
	if !hexColor.MatchString(input.Color) {
		writeError(w, http.StatusBadRequest, "color must be a six-digit hex color")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	collection := Collection{ID: uuid.New(), Name: input.Name, Description: input.Description, Color: input.Color}
	collection.Slug = uniqueSlug(input.Name, collection.ID)
	err := s.db.QueryRow(ctx, `
		INSERT INTO collections (id, slug, name, description, color)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at`, collection.ID, collection.Slug, collection.Name, collection.Description, collection.Color).Scan(&collection.CreatedAt)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, collection)
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	collectionID, err := uuid.Parse(r.PathValue("collectionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.collection_id, a.slug, a.title, a.description, a.artifact_type,
		       a.media_type, a.original_filename, a.size_bytes, a.sha256, a.tags,
		       a.metadata, a.created_at
		FROM artifacts a
		WHERE a.collection_id = $1
		  AND ($2 = '' OR a.title ILIKE '%' || $2 || '%' OR a.description ILIKE '%' || $2 || '%' OR array_to_string(a.tags, ' ') ILIKE '%' || $2 || '%')
		ORDER BY a.created_at DESC`, collectionID, query)
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer rows.Close()
	artifacts := make([]Artifact, 0)
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			writeDBError(w, err)
			return
		}
		s.addURLs(r, &artifact)
		artifacts = append(artifacts, artifact)
	}
	writeJSON(w, http.StatusOK, artifacts)
}

func (s *Server) createArtifact(w http.ResponseWriter, r *http.Request) {
	collectionID, err := uuid.Parse(r.PathValue("collectionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxArtifactSize+(1<<20))
	if err := r.ParseMultipartForm(maxArtifactSize + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "upload must be multipart form data and no larger than 10 MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	content, err := readArtifact(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	artifactType, mediaType, err := artifactFormat(header.Filename, content)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	if title == "" || len(title) > 200 {
		writeError(w, http.StatusBadRequest, "title must be between 1 and 200 characters")
		return
	}
	tags := parseTags(r.FormValue("tags"))
	metadata := json.RawMessage(strings.TrimSpace(r.FormValue("metadata")))
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	var metadataObject map[string]any
	if !json.Valid(metadata) || json.Unmarshal(metadata, &metadataObject) != nil || metadataObject == nil {
		writeError(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	hash := sha256.Sum256(content)
	artifact := Artifact{
		ID: uuid.New(), CollectionID: collectionID, Title: title,
		Description: strings.TrimSpace(r.FormValue("description")), Type: artifactType,
		MediaType: mediaType, OriginalFilename: filepath.Base(header.Filename),
		SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(hash[:]), Tags: tags, Metadata: metadata,
	}
	artifact.Slug = uniqueSlug(title, artifact.ID)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	err = s.db.QueryRow(ctx, `
		INSERT INTO artifacts (id, collection_id, slug, title, description, artifact_type, media_type,
		                       original_filename, content, size_bytes, sha256, tags, metadata)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13
		WHERE EXISTS (SELECT 1 FROM collections WHERE id = $2)
		RETURNING created_at`, artifact.ID, artifact.CollectionID, artifact.Slug, artifact.Title,
		artifact.Description, artifact.Type, artifact.MediaType, artifact.OriginalFilename,
		content, artifact.SizeBytes, artifact.SHA256, artifact.Tags, artifact.Metadata).Scan(&artifact.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "collection not found")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	s.addURLs(r, &artifact)
	writeJSON(w, http.StatusCreated, artifact)
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	artifactID, err := uuid.Parse(r.PathValue("artifactID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid artifact id")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	row := s.db.QueryRow(ctx, `
		SELECT a.id, a.collection_id, c.name, a.slug, a.title, a.description, a.artifact_type,
		       a.media_type, a.original_filename, a.size_bytes, a.sha256, a.tags, a.metadata, a.created_at
		FROM artifacts a JOIN collections c ON c.id = a.collection_id WHERE a.id = $1`, artifactID)
	var artifact Artifact
	err = row.Scan(&artifact.ID, &artifact.CollectionID, &artifact.CollectionName, &artifact.Slug, &artifact.Title,
		&artifact.Description, &artifact.Type, &artifact.MediaType, &artifact.OriginalFilename,
		&artifact.SizeBytes, &artifact.SHA256, &artifact.Tags, &artifact.Metadata, &artifact.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	s.addURLs(r, &artifact)
	writeJSON(w, http.StatusOK, artifact)
}

func (s *Server) getArtifactContent(w http.ResponseWriter, r *http.Request) {
	artifactID, err := uuid.Parse(r.PathValue("artifactID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	var content []byte
	var mediaType, filename, hash string
	err = s.db.QueryRow(ctx, "SELECT content, media_type, original_filename, sha256 FROM artifacts WHERE id = $1", artifactID).Scan(&content, &mediaType, &filename, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	w.Header().Set("Content-Type", mediaType+"; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", safeFilename(filename)))
	w.Header().Set("ETag", `"sha256-`+hash+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if strings.HasPrefix(mediaType, "text/html") {
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-forms; default-src 'none'; img-src data: https: http:; style-src 'unsafe-inline' https:; font-src data: https:; script-src 'unsafe-inline' https:; connect-src 'none'; frame-src 'none'")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	artifactID, err := uuid.Parse(r.PathValue("artifactID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid artifact id")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	result, err := s.db.Exec(ctx, "DELETE FROM artifacts WHERE id = $1", artifactID)
	if err != nil {
		writeDBError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "." {
		cleanPath = "index.html"
	}
	if strings.HasPrefix(cleanPath, "..") {
		http.NotFound(w, r)
		return
	}
	requested := filepath.Join(s.frontendDir, cleanPath)
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		http.ServeFile(w, r, requested)
		return
	}
	index := filepath.Join(s.frontendDir, "index.html")
	if _, err := os.Stat(index); errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "frontend is not built; run the frontend development server")
		return
	}
	http.ServeFile(w, r, index)
}

func scanArtifact(row interface{ Scan(...any) error }) (Artifact, error) {
	var artifact Artifact
	err := row.Scan(&artifact.ID, &artifact.CollectionID, &artifact.Slug, &artifact.Title,
		&artifact.Description, &artifact.Type, &artifact.MediaType, &artifact.OriginalFilename,
		&artifact.SizeBytes, &artifact.SHA256, &artifact.Tags, &artifact.Metadata, &artifact.CreatedAt)
	return artifact, err
}

func readArtifact(file multipart.File) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(file, maxArtifactSize+1))
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	if len(content) > maxArtifactSize {
		return nil, fmt.Errorf("artifact must be no larger than 10 MB")
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("artifact cannot be empty")
	}
	return content, nil
}

func artifactFormat(filename string, content []byte) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html", ".htm":
		return "html", "text/html", nil
	case ".md", ".markdown":
		return "markdown", "text/markdown", nil
	default:
		return "", "", fmt.Errorf("only .html, .htm, .md, and .markdown files are supported")
	}
}

func parseTags(value string) []string {
	seen := make(map[string]struct{})
	tags := make([]string, 0)
	for _, raw := range strings.Split(value, ",") {
		tag := strings.TrimSpace(raw)
		if tag == "" || len(tag) > 40 {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
		if len(tags) == 12 {
			break
		}
	}
	return tags
}

func uniqueSlug(value string, id uuid.UUID) string {
	base := strings.Trim(slugInvalid.ReplaceAllString(strings.ToLower(value), "-"), "-")
	if base == "" {
		base = "artifact"
	}
	if len(base) > 64 {
		base = strings.TrimRight(base[:64], "-")
	}
	return base + "-" + id.String()[:8]
}

func safeFilename(value string) string {
	return strings.NewReplacer("\"", "", "\r", "", "\n", "").Replace(filepath.Base(value))
}
