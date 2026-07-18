package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const maxArtifactSize = 10 << 20

var (
	slugInvalid      = regexp.MustCompile(`[^a-z0-9]+`)
	hexColor         = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	markdownParser   = goldmark.New(goldmark.WithExtensions(extension.GFM))
	markdownPageTmpl = template.Must(template.New("markdown-page").Parse(markdownPageHTML))
)

type artifactContent struct {
	content   []byte
	mediaType string
	filename  string
	title     string
	hash      string
}

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
	mux.HandleFunc("GET /a/{artifactID}/{slug}", s.getArtifactContent)
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
	var artifact artifactContent
	err = s.db.QueryRow(ctx, "SELECT content, media_type, original_filename, title, sha256 FROM artifacts WHERE id = $1", artifactID).Scan(
		&artifact.content, &artifact.mediaType, &artifact.filename, &artifact.title, &artifact.hash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeArtifactContent(w, r, artifact)
}

func writeArtifactContent(w http.ResponseWriter, r *http.Request, artifact artifactContent) {
	etag := `"sha256-` + artifact.hash + `"`
	content := artifact.content
	mediaType := artifact.mediaType
	responseFilename := safeFilename(artifact.filename)
	cacheControl := "public, max-age=31536000, immutable"
	if strings.HasPrefix(r.URL.Path, "/a/") && strings.HasPrefix(mediaType, "text/markdown") {
		var rendered bytes.Buffer
		if err := markdownParser.Convert(content, &rendered); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to render markdown")
			return
		}
		var page bytes.Buffer
		if err := markdownPageTmpl.Execute(&page, struct {
			Title    string
			Filename string
			Content  template.HTML
		}{
			Title:    artifact.title,
			Filename: safeFilename(artifact.filename),
			Content:  template.HTML(rendered.String()),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to render markdown page")
			return
		}
		content = page.Bytes()
		renderedHash := sha256.Sum256(content)
		etag = `"sha256-` + hex.EncodeToString(renderedHash[:]) + `"`
		mediaType = "text/html"
		cacheControl = "no-cache"
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data: https: http:; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	} else if strings.HasPrefix(r.URL.Path, "/a/") && strings.HasPrefix(mediaType, "application/json") {
		etag = structuredPageETag(artifact.hash, artifact.mediaType, artifact.title, artifact.filename)
		mediaType = "text/html"
		responseFilename = renderedHTMLFilename(artifact.filename)
		cacheControl = "no-cache"
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			setArtifactRepresentationHeaders(w, mediaType, responseFilename, etag, cacheControl)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		page, err := renderJSONPage(content, artifact.title, safeFilename(artifact.filename))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to render JSON page")
			return
		}
		content = page
	} else if strings.HasPrefix(r.URL.Path, "/a/") && strings.HasPrefix(mediaType, "text/csv") {
		etag = structuredPageETag(artifact.hash, artifact.mediaType, artifact.title, artifact.filename)
		mediaType = "text/html"
		responseFilename = renderedHTMLFilename(artifact.filename)
		cacheControl = "no-cache"
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			setArtifactRepresentationHeaders(w, mediaType, responseFilename, etag, cacheControl)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		page, err := renderCSVPage(content, artifact.title, safeFilename(artifact.filename))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to render CSV page")
			return
		}
		content = page
	}
	setArtifactRepresentationHeaders(w, mediaType, responseFilename, etag, cacheControl)
	if strings.HasPrefix(artifact.mediaType, "text/html") {
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-forms; default-src 'none'; img-src data: https: http:; style-src 'unsafe-inline' https:; font-src data: https:; script-src 'unsafe-inline' https:; connect-src 'none'; frame-src 'none'")
	}
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func setArtifactRepresentationHeaders(w http.ResponseWriter, mediaType, filename, etag, cacheControl string) {
	w.Header().Set("Content-Type", mediaType+"; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheControl)
}

func structuredPageETag(hash, mediaType, title, filename string) string {
	digest := sha256.Sum256([]byte("structured-page-v1:" + mediaType + ":" + hash + ":" + title + ":" + filename))
	return `"sha256-` + hex.EncodeToString(digest[:]) + `"`
}

const markdownPageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <title>{{.Title}} · Artifact Hub</title>
  <link rel="stylesheet" href="/markdown-assets/katex/katex.min.css">
  <script defer src="/markdown-assets/mermaid.min.js"></script>
  <script defer src="/markdown-assets/katex/katex.min.js"></script>
  <script defer src="/markdown-assets/katex/auto-render.min.js"></script>
  <script defer src="/markdown-assets/enhance.js"></script>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #383733; background: #f3f0e8; font-synthesis: none; text-rendering: optimizeLegibility; }
    * { box-sizing: border-box; }
    html { min-width: 320px; background: #f3f0e8; }
    body { min-height: 100vh; margin: 0; background: #f3f0e8; }
    a { color: #6257d7; text-underline-offset: .18em; text-decoration-thickness: .08em; }
    a:hover { color: #4e43bd; }
    .page-shell { width: min(980px, calc(100% - 40px)); margin: 0 auto; padding: 48px 0 72px; }
    .page-shell.has-reader-toc { width: min(1180px, calc(100% - 40px)); display: grid; grid-template-columns: 180px minmax(0, 820px); justify-content: center; align-items: start; gap: 36px; }
    .reader-content { min-width: 0; }
    .reader-toc { position: sticky; top: 30px; max-height: calc(100vh - 60px); overflow-y: auto; padding: 12px 8px 14px 0; color: #6f6b63; scrollbar-width: thin; }
    .reader-toc[hidden] { display: none; }
    .reader-toc-title { display: block; margin: 0 0 10px 12px; color: #999388; font: 650 10px/1.2 ui-monospace, SFMono-Regular, Menlo, monospace; text-transform: uppercase; letter-spacing: .09em; }
    .reader-toc ol { margin: 0; padding: 0; list-style: none; border-left: 1px solid #d9d4c9; }
    .reader-toc li { margin: 0; padding: 0; }
    .reader-toc a { display: block; margin-left: -1px; padding: 5px 8px 5px calc(12px + (var(--toc-depth, 0) * 11px)); overflow: hidden; color: #777269; border-left: 2px solid transparent; font-size: 12px; line-height: 1.45; text-decoration: none; text-overflow: ellipsis; white-space: nowrap; transition: color .15s ease, border-color .15s ease, background .15s ease; }
    .reader-toc a:hover { color: #2454ba; background: rgba(37,99,235,.055); }
    .reader-toc a[aria-current="location"] { color: #1d4ed8; background: rgba(37,99,235,.07); border-left-color: #2563eb; font-weight: 650; }
    .markdown-body { --reader-font-size: 16px; --reader-line-height: 1.75; --reader-font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; width: min(820px, 100%); margin: 0 auto; padding: clamp(36px, 6vw, 68px); color: #35363d; background: #fffdf8; border: 1px solid rgba(91,80,61,.12); border-radius: 16px; box-shadow: 0 24px 72px rgba(78,68,50,.11); font-family: var(--reader-font-family); font-size: var(--reader-font-size); line-height: var(--reader-line-height); overflow-wrap: break-word; }
    .markdown-body[data-reader-font="sans"] { --reader-font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    .markdown-body[data-reader-font="pingfang"] { --reader-font-family: "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif; }
    .markdown-body[data-reader-font="songti"] { --reader-font-family: "Songti SC", STSong, SimSun, "Noto Serif CJK SC", serif; }
    .markdown-body[data-reader-font="kaiti"] { --reader-font-family: "Kaiti SC", STKaiti, KaiTi, "Noto Serif CJK SC", serif; }
    .markdown-body[data-reader-font="helvetica"] { --reader-font-family: "Helvetica Neue", Helvetica, Arial, "PingFang SC", sans-serif; }
    .markdown-body[data-reader-font="arial"] { --reader-font-family: Arial, "Helvetica Neue", "PingFang SC", sans-serif; }
    .markdown-body[data-reader-font="verdana"] { --reader-font-family: Verdana, Geneva, "PingFang SC", sans-serif; }
    .markdown-body[data-reader-font="georgia"] { --reader-font-family: Georgia, "Songti SC", STSong, serif; }
    .markdown-body[data-reader-font="times"] { --reader-font-family: "Times New Roman", Times, "Songti SC", STSong, serif; }
    .markdown-body[data-reader-font="mono"] { --reader-font-family: Menlo, SFMono-Regular, Consolas, "Liberation Mono", monospace; }
    .markdown-body > :first-child { margin-top: 0; }
    .markdown-body > :last-child { margin-bottom: 0; }
    .markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4, .markdown-body h5, .markdown-body h6 { color: #15161b; font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; line-height: 1.2; letter-spacing: -.035em; scroll-margin-top: 24px; }
    .markdown-body h1 { margin: 0 0 1em; font-size: clamp(2.2rem, 6vw, 3.4rem); font-weight: 720; }
    .markdown-body h2 { margin: 2.2em 0 .75em; padding-bottom: .42em; font-size: 1.75rem; font-weight: 680; border-bottom: 1px solid #e2e0da; }
    .markdown-body h3 { margin: 1.8em 0 .65em; font-size: 1.3rem; font-weight: 680; }
    .markdown-body h4, .markdown-body h5, .markdown-body h6 { margin: 1.6em 0 .55em; font-size: 1.05rem; }
    .markdown-body p, .markdown-body ul, .markdown-body ol, .markdown-body blockquote, .markdown-body pre, .markdown-body table { margin-top: 0; margin-bottom: 1.25em; }
    .markdown-body ul, .markdown-body ol { padding-left: 1.45em; }
    .markdown-body li { margin: .35em 0; padding-left: .18em; }
    .markdown-body li::marker { color: #7569e5; }
    .markdown-body blockquote { margin-left: 0; padding: .25em 0 .25em 1.2em; color: #666770; border-left: 3px solid #8c82f0; }
    .markdown-body blockquote > :last-child { margin-bottom: 0; }
    .markdown-body strong { color: #202126; font-weight: 720; }
    .markdown-body hr { height: 1px; margin: 2.8em 0; background: #dddcd6; border: 0; }
    .markdown-body code { padding: .18em .42em; color: #5146c2; background: #efedf9; border: 1px solid #e3e0f3; border-radius: 5px; font: .84em/1.55 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .markdown-body pre { overflow: auto; padding: 1.15em 1.25em; color: #e6e7ec; background: #18191f; border: 1px solid #2b2c34; border-radius: 10px; box-shadow: inset 0 1px 0 rgba(255,255,255,.04); }
    .markdown-body pre code { padding: 0; color: inherit; background: none; border: 0; font-size: .86em; }
    .markdown-body .code-block { overflow: hidden; margin: 0 0 1.25em; background: #18191f; border: 1px solid #2b2c34; border-radius: 10px; box-shadow: inset 0 1px 0 rgba(255,255,255,.04); }
    .markdown-body .code-block-toolbar { min-height: 38px; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 0 8px 0 14px; color: #8c8f9c; background: #202127; border-bottom: 1px solid #30313a; font: 10px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; text-transform: uppercase; letter-spacing: .08em; }
    .markdown-body .code-copy-button { min-height: 28px; display: inline-flex; align-items: center; gap: 6px; padding: 0 9px; color: #b6b8c1; background: transparent; border: 1px solid transparent; border-radius: 6px; font: inherit; text-transform: none; letter-spacing: 0; cursor: pointer; }
    .markdown-body .code-copy-button:hover { color: #f2f3f6; background: rgba(255,255,255,.055); border-color: rgba(255,255,255,.08); }
    .markdown-body .code-copy-button.copied { color: #75d6a4; }
    .markdown-body .code-block pre { margin: 0; border: 0; border-radius: 0; box-shadow: none; }
    .markdown-body table { width: 100%; display: block; overflow-x: auto; border-spacing: 0; border-collapse: collapse; font-family: Inter, ui-sans-serif, sans-serif; font-size: .9em; }
    .markdown-body th, .markdown-body td { padding: .7em .85em; text-align: left; border: 1px solid #dcdbd5; }
    .markdown-body th { color: #222329; background: #f0efeb; font-weight: 680; }
    .markdown-body tr:nth-child(even) td { background: #f7f6f2; }
    .markdown-body img { max-width: 100%; height: auto; display: block; margin: 1.6em auto; border-radius: 10px; box-shadow: 0 14px 36px rgba(0,0,0,.13); }
    .markdown-body input[type="checkbox"] { margin-right: .55em; accent-color: #7065df; }
    .markdown-body .mermaid-diagram { overflow-x: auto; margin: 1.8em 0; padding: 1.25em; color: #202126; background: #f7f6f2; border: 1px solid #dcdbd5; border-radius: 12px; text-align: center; }
    .markdown-body .mermaid-diagram svg { max-width: 100%; height: auto; }
    .markdown-body .katex-display { overflow-x: auto; overflow-y: hidden; margin: 1.6em 0; padding: .35em 0; }
    .page-footer { margin: 24px auto 0; color: #817c72; font: 10px ui-monospace, SFMono-Regular, Menlo, monospace; text-align: center; text-transform: uppercase; letter-spacing: .08em; }
    .reader-settings { position: fixed; right: max(18px, env(safe-area-inset-right)); bottom: max(18px, env(safe-area-inset-bottom)); z-index: 30; display: flex; align-items: flex-end; flex-direction: column; gap: 10px; color: #e9eaf0; font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    .settings-pill { min-height: 42px; display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 0 17px; color: #fff; background: #2563eb; border: 1px solid rgba(255,255,255,.22); border-radius: 999px; box-shadow: 0 14px 38px rgba(37,99,235,.34), inset 0 1px 0 rgba(255,255,255,.16); font: 600 12px/1 Inter, ui-sans-serif, sans-serif; letter-spacing: .02em; cursor: pointer; transition: background .15s ease, border-color .15s ease, box-shadow .15s ease, transform .15s ease; }
    .settings-icon { color: #fff; font-size: 13px; line-height: 1; }
    .settings-pill:hover, .settings-pill[aria-expanded="true"] { color: #fff; background: #1d4ed8; border-color: rgba(255,255,255,.32); box-shadow: 0 16px 42px rgba(37,99,235,.42), inset 0 1px 0 rgba(255,255,255,.18); transform: translateY(-1px); }
    .settings-pill:focus-visible { outline: 3px solid rgba(96,165,250,.4); outline-offset: 3px; }
    .settings-panel { width: min(320px, calc(100vw - 28px)); padding: 17px; color: #e9eaf0; background: rgba(18,19,25,.96); border: 1px solid rgba(255,255,255,.12); border-radius: 15px; box-shadow: 0 24px 70px rgba(0,0,0,.5); backdrop-filter: blur(22px); }
    .settings-panel[hidden] { display: none; }
    .settings-panel header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 17px; }
    .settings-panel header span { display: block; margin-bottom: 4px; color: #777b89; font: 9px ui-monospace, SFMono-Regular, Menlo, monospace; text-transform: uppercase; letter-spacing: .11em; }
    .settings-panel header strong { display: block; color: #f1f2f5; font-size: 15px; font-weight: 650; letter-spacing: -.02em; }
    .settings-close { width: 30px; height: 30px; display: grid; place-items: center; padding: 0; color: #898c98; background: transparent; border: 1px solid transparent; border-radius: 8px; font-size: 18px; cursor: pointer; }
    .settings-close:hover { color: #f1f2f5; background: rgba(255,255,255,.05); border-color: rgba(255,255,255,.08); }
    .reader-setting { display: block; margin-top: 14px; }
    .reader-setting > span, .range-heading span { color: #9295a2; font-size: 11px; font-weight: 550; }
    .reader-select { width: 100%; height: 40px; margin-top: 8px; padding: 0 11px; color: #e7e8ed; background: #202128; border: 1px solid rgba(255,255,255,.1); border-radius: 9px; outline: 0; font: 12px Inter, ui-sans-serif, sans-serif; }
    .reader-select:focus { border-color: rgba(149,137,255,.55); box-shadow: 0 0 0 3px rgba(120,107,242,.1); }
    .range-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
    .range-heading output { color: #a9a3ef; font: 10px ui-monospace, SFMono-Regular, Menlo, monospace; }
    .reader-range { width: 100%; margin: 11px 0 0; accent-color: #887cf4; cursor: pointer; }
    .settings-reset { width: 100%; min-height: 36px; margin-top: 18px; color: #9da0ab; background: rgba(255,255,255,.025); border: 1px solid rgba(255,255,255,.08); border-radius: 9px; font: 11px Inter, ui-sans-serif, sans-serif; cursor: pointer; }
    .settings-reset:hover { color: #e8e9ee; background: rgba(255,255,255,.05); }
    @media (max-width: 1100px) {
      .page-shell.has-reader-toc { width: min(980px, calc(100% - 40px)); display: block; }
      .reader-toc { display: none; }
    }
    @media (max-width: 700px) {
      .page-shell { width: 100%; padding: 0; }
      .markdown-body { min-height: 100dvh; padding: 30px 20px 48px; border: 0; border-radius: 0; box-shadow: none; font-size: 16px; }
      .markdown-body h1 { font-size: 2.25rem; }
      .page-footer { display: none; }
      .reader-settings { right: max(12px, env(safe-area-inset-right)); bottom: max(12px, env(safe-area-inset-bottom)); }
      .settings-panel { width: min(320px, calc(100vw - 24px)); }
    }
    @media (prefers-reduced-motion: reduce) { * { scroll-behavior: auto !important; } }
  </style>
</head>
<body>
  <main class="page-shell">
    <nav class="reader-toc" data-reader-toc aria-label="On this page" hidden>
      <span class="reader-toc-title">On this page</span>
      <ol data-reader-toc-list></ol>
    </nav>
    <div class="reader-content">
      <article class="markdown-body" data-code-copy="enabled" data-math-render="enabled">{{.Content}}</article>
      <footer class="page-footer">Immutable artifact · Rendered for reading</footer>
    </div>
  </main>
  <div class="reader-settings" data-reader-settings>
    <section class="settings-panel" id="reader-settings-panel" role="dialog" aria-labelledby="reader-settings-title" hidden>
      <header>
        <div><span>Reader</span><strong id="reader-settings-title">Typography</strong></div>
        <button class="settings-close" type="button" data-reader-close aria-label="Close settings">×</button>
      </header>
      <label class="reader-setting">
        <span>Font family</span>
        <select class="reader-select" data-reader-font>
          <option value="sans">System UI</option>
          <option value="pingfang">PingFang SC</option>
          <option value="songti">Songti SC</option>
          <option value="kaiti">Kaiti SC</option>
          <option value="helvetica">Helvetica Neue</option>
          <option value="arial">Arial</option>
          <option value="verdana">Verdana</option>
          <option value="georgia">Georgia</option>
          <option value="times">Times New Roman</option>
          <option value="mono">Menlo</option>
        </select>
      </label>
      <label class="reader-setting">
        <span class="range-heading"><span>Text size</span><output data-reader-size-output>16 px</output></span>
        <input class="reader-range" type="range" min="14" max="22" step="1" value="16" data-reader-size>
      </label>
      <label class="reader-setting">
        <span class="range-heading"><span>Line height</span><output data-reader-leading-output>1.75</output></span>
        <input class="reader-range" type="range" min="1.4" max="2.1" step="0.05" value="1.75" data-reader-leading>
      </label>
      <button class="settings-reset" type="button" data-reader-reset>Reset defaults</button>
    </section>
    <button class="settings-pill" type="button" aria-controls="reader-settings-panel" aria-expanded="false"><span class="settings-icon" aria-hidden="true">⚙</span><span>settings</span></button>
  </div>
</body>
</html>`

func matchesETag(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
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
	case ".json":
		if !utf8.Valid(content) {
			return "", "", fmt.Errorf("JSON artifact must be UTF-8 encoded")
		}
		if !json.Valid(content) {
			return "", "", fmt.Errorf("JSON artifact must contain valid JSON")
		}
		return "json", "application/json", nil
	case ".csv":
		if !utf8.Valid(content) {
			return "", "", fmt.Errorf("CSV artifact must be UTF-8 encoded")
		}
		reader := csv.NewReader(bytes.NewReader(content))
		reader.ReuseRecord = true
		recordCount := 0
		for {
			if _, err := reader.Read(); err == io.EOF {
				break
			} else if err != nil {
				return "", "", fmt.Errorf("CSV artifact must contain valid CSV: %w", err)
			}
			recordCount++
		}
		if recordCount == 0 {
			return "", "", fmt.Errorf("CSV artifact must contain at least one record")
		}
		return "csv", "text/csv", nil
	default:
		return "", "", fmt.Errorf("only HTML, Markdown, JSON, and CSV files are supported")
	}
}

func renderedHTMLFilename(filename string) string {
	filename = safeFilename(filename)
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" || base == "." {
		base = "artifact"
	}
	return base + ".html"
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
