package httpapi

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	maxJSONPrettySourceBytes = 512 << 10
	maxJSONPreviewBytes      = 512 << 10
	maxJSONPreviewLines      = 5000
	maxJSONLPreviewRecords   = 500
	maxJSONLPreviewBytes     = 512 << 10
	maxCSVPreviewRows        = 1000
	maxCSVPreviewColumns     = 100
	maxCSVPreviewBytes       = 1 << 20
)

var (
	structuredPageTmpl = template.Must(template.New("structured-page").Parse(structuredPageHTML))
	csvTableTmpl       = template.Must(template.New("csv-table").Funcs(template.FuncMap{
		"inc": func(value int) int { return value + 1 },
	}).Parse(csvTableHTML))
)

type structuredPageData struct {
	Title    string
	Filename string
	Kind     string
	Summary  string
	Content  template.HTML
}

func renderJSONPage(content []byte, title, filename string) ([]byte, error) {
	formatted := content
	summary := largeJSONSummary(content)
	mode := "Raw preview"
	notice := "JSON preview truncated to keep this page responsive; the raw API still returns the complete artifact."
	highlight := false
	if len(content) <= maxJSONPrettySourceBytes {
		var value any
		if err := json.Unmarshal(content, &value); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, content, "", "  "); err != nil {
			return nil, fmt.Errorf("format JSON: %w", err)
		}
		formatted = pretty.Bytes()
		summary = jsonSummary(value)
		mode = "Pretty printed"
		notice = ""
		highlight = true
	}

	lines, previewClipped := boundedJSONLines(formatted)
	if previewClipped && notice == "" {
		notice = "JSON preview truncated to keep this page responsive; the raw API still returns the complete artifact."
	}
	var body strings.Builder
	body.WriteString(`<section class="data-card json-card"><header class="card-bar"><span>`)
	body.WriteString(mode)
	body.WriteString(`</span><strong>`)
	fmt.Fprintf(&body, "%d lines", len(lines))
	body.WriteString(`</strong></header>`)
	if notice != "" {
		body.WriteString(`<p class="preview-note">`)
		body.WriteString(template.HTMLEscapeString(notice))
		body.WriteString(`</p>`)
	}
	body.WriteString(`<div class="json-view" role="region" aria-label="JSON content"><div class="json-code">`)
	body.WriteString(renderCollapsibleJSONLines(lines, highlight))
	body.WriteString(`</div></div></section>`)

	return renderStructuredPage(structuredPageData{
		Title: title, Filename: filename, Kind: "JSON", Summary: summary, Content: template.HTML(body.String()),
	})
}

func renderCollapsibleJSONLines(lines []string, highlight bool) string {
	var rendered strings.Builder
	openNodes := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		isOpeningNode := strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "[")
		isClosingNode := strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "]")
		if isOpeningNode {
			rendered.WriteString(`<details class="json-node" open><summary title="Collapse or expand this JSON node">`)
		}
		rendered.WriteString(`<span class="code-line"><span class="line-number" aria-hidden="true">`)
		fmt.Fprintf(&rendered, "%d", index+1)
		rendered.WriteString(`</span><span class="line-content">`)
		if isOpeningNode {
			rendered.WriteString(`<span class="json-fold-marker" aria-hidden="true">▾</span>`)
		}
		if highlight {
			rendered.WriteString(highlightJSONLine(line))
		} else {
			rendered.WriteString(template.HTMLEscapeString(line))
		}
		rendered.WriteString(`</span></span>`)
		if isOpeningNode {
			rendered.WriteString(`</summary><div class="json-node-children">`)
			openNodes++
		}
		if isClosingNode && openNodes > 0 {
			rendered.WriteString(`</div></details>`)
			openNodes--
		}
	}
	for openNodes > 0 {
		rendered.WriteString(`</div></details>`)
		openNodes--
	}
	return rendered.String()
}

func boundedJSONLines(content []byte) ([]string, bool) {
	limit := min(len(content), maxJSONPreviewBytes)
	for limit > 0 && !utf8.Valid(content[:limit]) {
		limit--
	}
	clipped := limit < len(content)
	rawLines := bytes.Split(content[:limit], []byte("\n"))
	if len(rawLines) > maxJSONPreviewLines {
		rawLines = rawLines[:maxJSONPreviewLines]
		clipped = true
	}
	lines := make([]string, len(rawLines))
	for index, line := range rawLines {
		lines[index] = string(line)
	}
	return lines, clipped
}

func largeJSONSummary(content []byte) string {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return "Large JSON object"
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return "Large JSON array"
	}
	return "Large JSON value"
}

func jsonSummary(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		return fmt.Sprintf("%d top-level fields", len(typed))
	case []any:
		return fmt.Sprintf("%d top-level items", len(typed))
	default:
		return "Scalar value"
	}
}

func renderJSONLPage(content []byte, title, filename string) ([]byte, error) {
	remainingBytes := maxJSONLPreviewBytes
	totalRecords := 0
	previewRecords := 0
	contentClipped := false
	var body strings.Builder
	body.WriteString(`<section class="data-card jsonl-card"><header class="card-bar"><span>Record stream</span><strong>Line-delimited JSON</strong></header>`)
	var recordsBody strings.Builder
	for len(content) > 0 {
		lineEnd := bytes.IndexByte(content, '\n')
		line := content
		if lineEnd >= 0 {
			line = content[:lineEnd]
			content = content[lineEnd+1:]
		} else {
			content = nil
		}
		line = trimJSONWhitespace(line)
		if len(line) == 0 {
			return nil, fmt.Errorf("parse JSONL: record %d is empty", totalRecords+1)
		}
		totalRecords++
		if previewRecords >= maxJSONLPreviewRecords || remainingBytes == 0 {
			continue
		}

		formatted := line
		highlight := false
		if len(line) <= remainingBytes {
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, line, "", "  "); err != nil {
				return nil, fmt.Errorf("format JSONL record %d: %w", totalRecords, err)
			}
			formatted = pretty.Bytes()
			highlight = true
		}
		limit := min(len(formatted), remainingBytes)
		for limit > 0 && !utf8.Valid(formatted[:limit]) {
			limit--
		}
		if limit < len(formatted) {
			contentClipped = true
			highlight = false
		}
		formatted = formatted[:limit]
		remainingBytes -= limit
		previewRecords++

		fmt.Fprintf(&recordsBody, `<article class="jsonl-record"><header><span>Record %d</span><strong>%s</strong></header><pre><code>`, totalRecords, jsonValueKind(line))
		lines, linesClipped := boundedJSONLines(formatted)
		if linesClipped {
			contentClipped = true
		}
		for index, recordLine := range lines {
			recordsBody.WriteString(`<span class="code-line"><span class="line-number" aria-hidden="true">`)
			fmt.Fprintf(&recordsBody, "%d", index+1)
			recordsBody.WriteString(`</span><span class="line-content">`)
			if highlight {
				recordsBody.WriteString(highlightJSONLine(recordLine))
			} else {
				recordsBody.WriteString(template.HTMLEscapeString(recordLine))
			}
			recordsBody.WriteString(`</span></span>`)
		}
		recordsBody.WriteString(`</code></pre></article>`)
	}

	notice := jsonlPreviewNotice(totalRecords, previewRecords, contentClipped)
	if notice != "" {
		body.WriteString(`<p class="preview-note">`)
		body.WriteString(template.HTMLEscapeString(notice))
		body.WriteString(`</p>`)
	}
	body.WriteString(`<div class="jsonl-list">`)
	body.WriteString(recordsBody.String())
	body.WriteString(`</div></section>`)
	return renderStructuredPage(structuredPageData{
		Title: title, Filename: filename, Kind: "JSONL",
		Summary: fmt.Sprintf("%d records", totalRecords), Content: template.HTML(body.String()),
	})
}

func jsonValueKind(content []byte) string {
	content = trimJSONWhitespace(content)
	if len(content) == 0 {
		return "value"
	}
	switch content[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

func jsonlPreviewNotice(totalRecords, previewRecords int, contentClipped bool) string {
	parts := make([]string, 0, 2)
	if previewRecords < totalRecords {
		parts = append(parts, fmt.Sprintf("Showing first %d of %d records", previewRecords, totalRecords))
	}
	if contentClipped {
		parts = append(parts, "A long record was shortened")
	}
	return strings.Join(parts, " · ")
}

func renderCSVPage(content []byte, title, filename string) ([]byte, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	headingsRecord, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}

	columnCount := len(headingsRecord)
	previewColumns := min(columnCount, maxCSVPreviewColumns)
	remainingBytes := maxCSVPreviewBytes
	contentClipped := false
	headings := make([]string, previewColumns)
	for index, heading := range headingsRecord[:previewColumns] {
		if index == 0 {
			heading = strings.TrimPrefix(heading, "\ufeff")
		}
		if strings.TrimSpace(heading) == "" {
			heading = fmt.Sprintf("Column %d", index+1)
		}
		headings[index], contentClipped = previewCSVValue(heading, &remainingBytes, contentClipped)
	}

	rows := make([][]string, 0, min(maxCSVPreviewRows, 128))
	totalRows := 0
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("parse CSV: %w", readErr)
		}
		totalRows++
		if len(rows) >= maxCSVPreviewRows || remainingBytes == 0 {
			continue
		}
		row := make([]string, previewColumns)
		for index, value := range record[:previewColumns] {
			row[index], contentClipped = previewCSVValue(value, &remainingBytes, contentClipped)
			if remainingBytes == 0 {
				break
			}
		}
		rows = append(rows, row)
	}
	notice := csvPreviewNotice(totalRows, len(rows), columnCount, previewColumns, contentClipped)

	var table bytes.Buffer
	if err := csvTableTmpl.Execute(&table, struct {
		Headings           []string
		Rows               [][]string
		Notice             string
		ColumnHighlightCSS template.CSS
	}{Headings: headings, Rows: rows, Notice: notice, ColumnHighlightCSS: csvColumnHighlightCSS(previewColumns)}); err != nil {
		return nil, fmt.Errorf("render CSV table: %w", err)
	}

	return renderStructuredPage(structuredPageData{
		Title: title, Filename: filename, Kind: "CSV",
		Summary: fmt.Sprintf("%d data rows · %d columns", totalRows, columnCount),
		Content: template.HTML(table.String()),
	})
}

func csvColumnHighlightCSS(columnCount int) template.CSS {
	var styles strings.Builder
	for columnIndex := 0; columnIndex < columnCount; columnIndex++ {
		childIndex := columnIndex + 2 // Account for the leading row-number column.
		fmt.Fprintf(&styles, ".csv-table:has(tr > :nth-child(%d):hover) tr > :nth-child(%d) { background: #e8e3fb; }\n", childIndex, childIndex)
		fmt.Fprintf(&styles, ".csv-table tr > :nth-child(%d):hover { background: #d5ccf8; }\n", childIndex)
	}
	return template.CSS(styles.String())
}

func previewCSVValue(value string, remainingBytes *int, alreadyClipped bool) (string, bool) {
	if len(value) <= *remainingBytes {
		*remainingBytes -= len(value)
		return value, alreadyClipped
	}
	limit := *remainingBytes
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	*remainingBytes = 0
	return value[:limit] + "…", true
}

func csvPreviewNotice(totalRows, previewRows, totalColumns, previewColumns int, contentClipped bool) string {
	parts := make([]string, 0, 3)
	if previewRows < totalRows {
		parts = append(parts, fmt.Sprintf("Showing first %d of %d data rows", previewRows, totalRows))
	}
	if previewColumns < totalColumns {
		parts = append(parts, fmt.Sprintf("Showing first %d of %d columns", previewColumns, totalColumns))
	}
	if contentClipped {
		parts = append(parts, "Some long cells were shortened")
	}
	return strings.Join(parts, " · ")
}

func highlightJSONLine(line string) string {
	var highlighted strings.Builder
	for index := 0; index < len(line); {
		if line[index] == '"' {
			end := index + 1
			for end < len(line) {
				if line[end] == '\\' {
					end += 2
					continue
				}
				if line[end] == '"' {
					end++
					break
				}
				end++
			}
			className := "json-string"
			remainder := strings.TrimSpace(line[end:])
			if strings.HasPrefix(remainder, ":") {
				className = "json-key"
			}
			fmt.Fprintf(&highlighted, `<span class="%s">%s</span>`, className, template.HTMLEscapeString(line[index:end]))
			index = end
			continue
		}

		end := index
		for end < len(line) && !strings.ContainsRune(" \t{}[],:\"", rune(line[end])) {
			end++
		}
		if end == index {
			highlighted.WriteString(template.HTMLEscapeString(line[index : index+1]))
			index++
			continue
		}
		token := line[index:end]
		className := "json-number"
		if token == "true" || token == "false" {
			className = "json-boolean"
		} else if token == "null" {
			className = "json-null"
		}
		fmt.Fprintf(&highlighted, `<span class="%s">%s</span>`, className, template.HTMLEscapeString(token))
		index = end
	}
	return highlighted.String()
}

func renderStructuredPage(data structuredPageData) ([]byte, error) {
	var page bytes.Buffer
	if err := structuredPageTmpl.Execute(&page, data); err != nil {
		return nil, fmt.Errorf("render structured page: %w", err)
	}
	return page.Bytes(), nil
}

const structuredPageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light">
  <title>{{.Title}} · Artifact Hub</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #24252b; background: #f1efe9; font-synthesis: none; text-rendering: optimizeLegibility; }
    * { box-sizing: border-box; }
    html { min-width: 320px; background: #f1efe9; }
    body { min-height: 100vh; margin: 0; background: radial-gradient(circle at 15% 0%, rgba(111,93,230,.11), transparent 28rem), #f1efe9; }
    .page-shell { width: min(1380px, calc(100% - 40px)); margin: 0 auto; padding: clamp(32px, 6vw, 72px) 0 72px; }
    .artifact-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 32px; margin: 0 4px 24px; }
    .artifact-kind { display: inline-flex; align-items: center; min-height: 24px; margin-bottom: 14px; padding: 0 9px; color: #6257cf; background: rgba(98,87,207,.09); border: 1px solid rgba(98,87,207,.16); border-radius: 6px; font: 700 10px/1 ui-monospace, SFMono-Regular, Menlo, monospace; letter-spacing: .1em; }
    h1 { max-width: 900px; margin: 0; color: #17181d; font-size: clamp(2rem, 5vw, 3.5rem); line-height: 1.04; letter-spacing: -.055em; }
    .filename { margin: 12px 0 0; color: #77736b; font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; overflow-wrap: anywhere; }
    .summary { flex: none; margin: 0 0 4px; padding: 9px 12px; color: #68645c; background: rgba(255,255,255,.52); border: 1px solid rgba(66,59,48,.1); border-radius: 8px; font: 11px/1 ui-monospace, SFMono-Regular, Menlo, monospace; box-shadow: 0 8px 24px rgba(62,52,36,.05); }
    .data-card { overflow: hidden; background: #fff; border: 1px solid rgba(45,40,32,.13); border-radius: 15px; box-shadow: 0 28px 80px rgba(67,55,36,.12); }
    .card-bar { height: 42px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 0 15px; color: #8d8990; background: #191a20; border-bottom: 1px solid rgba(255,255,255,.08); font: 10px/1 ui-monospace, SFMono-Regular, Menlo, monospace; text-transform: uppercase; letter-spacing: .08em; }
    .card-bar strong { color: #656873; font-weight: 500; }
    .json-view { max-height: min(72vh, 900px); margin: 0; overflow: auto; color: #d9dae0; background: #111217; font: 13px/1.7 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; tab-size: 2; }
    .json-view .json-code { display: block; min-width: max-content; padding: 15px 0 20px; }
    .json-node { min-width: max-content; }
    .json-node > summary { display: block; cursor: pointer; list-style: none; }
    .json-node > summary::-webkit-details-marker { display: none; }
    .json-node > summary:hover { background: rgba(111,93,230,.09); }
    .json-node > summary:focus-visible { outline: 2px solid #8d82f5; outline-offset: -2px; }
    .json-node:not([open]) > summary { color: #aeb0ba; background: rgba(111,93,230,.075); }
    .json-fold-marker { display: inline-block; width: 17px; margin-left: -17px; color: #7772b9; transform-origin: 45% 50%; transition: transform .12s ease; }
    .json-node:not([open]) > summary .json-fold-marker { transform: rotate(-90deg); }
    .code-line { display: grid; grid-template-columns: 58px minmax(max-content, 1fr); min-height: 22px; padding-right: 24px; }
    .code-line:hover { background: rgba(255,255,255,.035); }
    .line-number { padding-right: 17px; color: #4f515a; border-right: 1px solid rgba(255,255,255,.055); text-align: right; user-select: none; }
    .line-content { padding-left: 18px; white-space: pre; }
    .json-key { color: #9cdcfe; }
    .json-string { color: #b6d99b; }
    .json-number { color: #e9b982; }
    .json-boolean { color: #c6a7ef; }
    .json-null { color: #7f828d; font-style: italic; }
    .jsonl-list { display: grid; gap: 12px; padding: 14px; background: #111217; }
    .jsonl-record { min-width: 0; overflow: hidden; background: #17181e; border: 1px solid rgba(255,255,255,.075); border-radius: 10px; }
    .jsonl-record > header { height: 36px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 0 13px; color: #a5a7b0; background: #1d1e25; border-bottom: 1px solid rgba(255,255,255,.065); font: 10px/1 ui-monospace, SFMono-Regular, Menlo, monospace; text-transform: uppercase; letter-spacing: .07em; }
    .jsonl-record > header strong { color: #777b87; font-weight: 550; }
    .jsonl-record pre { max-height: 420px; margin: 0; overflow: auto; color: #d9dae0; background: #111217; font: 12px/1.65 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .jsonl-record pre code { display: block; min-width: max-content; padding: 11px 0 13px; }
    .jsonl-record .code-line { grid-template-columns: 44px minmax(max-content, 1fr); min-height: 20px; padding-right: 18px; }
    .jsonl-record .line-number { padding-right: 12px; }
    .jsonl-record .line-content { padding-left: 13px; }
    .preview-note { margin: 0; padding: 10px 15px; color: #716c62; background: #fff9e8; border-bottom: 1px solid #ebe4cf; font: 11px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
    .table-scroll { max-width: 100%; max-height: min(72vh, 900px); overflow: auto; background: #fff; }
    .csv-table { width: 100%; min-width: max-content; border-spacing: 0; border-collapse: separate; color: #32333a; font: 12px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }
    .csv-table th, .csv-table td { max-width: 520px; padding: 11px 14px; overflow-wrap: anywhere; border-right: 1px solid #e8e6e0; border-bottom: 1px solid #e8e6e0; text-align: left; vertical-align: top; white-space: pre-wrap; }
    .csv-table thead th { position: sticky; top: 0; z-index: 2; color: #4a4657; background: #efedf8; border-bottom-color: #d8d4e8; font-weight: 750; letter-spacing: .01em; }
    .csv-table tbody tr:nth-child(even) td, .csv-table tbody tr:nth-child(even) th { background: #faf9f6; }
    .csv-table tbody tr:hover td, .csv-table tbody tr:hover th { background: #f3f1fb; }
    .csv-table .row-number { position: sticky; left: 0; z-index: 1; width: 54px; min-width: 54px; color: #99959c; background: #f7f6f2; border-right-color: #dedbd3; font-weight: 500; text-align: right; user-select: none; }
    .csv-table thead .row-number { z-index: 4; color: #777181; background: #e7e4f1; }
    .csv-table thead th:nth-child(2), .csv-table tbody td:first-child { position: sticky; left: 54px; z-index: 2; background: #fff; box-shadow: 8px 0 14px -12px rgba(41,37,55,.75); }
    .csv-table thead th:nth-child(2) { z-index: 3; background: #efedf8; }
    .csv-table tbody tr:nth-child(even) td:first-child { background: #faf9f6; }
    .csv-table tbody tr:hover td:first-child { background: #f3f1fb; }
    .page-footer { margin-top: 22px; color: #8b867c; font: 10px ui-monospace, SFMono-Regular, Menlo, monospace; text-align: center; text-transform: uppercase; letter-spacing: .09em; }
    @media (max-width: 700px) {
      .page-shell { width: 100%; padding: 0; }
      .artifact-heading { align-items: flex-start; flex-direction: column; gap: 14px; margin: 0; padding: 25px 18px 20px; }
      .artifact-kind { margin-bottom: 10px; }
      h1 { font-size: 2rem; }
      .summary { margin: 0; }
      .data-card { border-right: 0; border-left: 0; border-radius: 0; box-shadow: none; }
      .json-view { font-size: 12px; }
      .jsonl-list { gap: 9px; padding: 10px; }
      .jsonl-record { border-radius: 8px; }
      .code-line { grid-template-columns: 42px minmax(max-content, 1fr); padding-right: 16px; }
      .line-number { padding-right: 11px; }
      .line-content { padding-left: 12px; }
      .page-footer { display: none; }
    }
  </style>
</head>
<body>
  <main class="page-shell">
    <header class="artifact-heading">
      <div><span class="artifact-kind">{{.Kind}}</span><h1>{{.Title}}</h1><p class="filename">{{.Filename}}</p></div>
      <p class="summary">{{.Summary}}</p>
    </header>
    {{.Content}}
    <footer class="page-footer">Immutable artifact · Rendered for inspection</footer>
  </main>
</body>
</html>`

const csvTableHTML = `<section class="data-card csv-card">
  <header class="card-bar"><span>Tabular preview</span><strong>First row used as headings</strong></header>
  {{if .Notice}}<p class="preview-note">{{.Notice}}</p>{{end}}
  <style class="csv-column-highlight-rules">{{.ColumnHighlightCSS}}</style>
  <div class="table-scroll">
    <table class="csv-table">
      <thead><tr><th class="row-number" scope="col">#</th>{{range .Headings}}<th scope="col">{{.}}</th>{{end}}</tr></thead>
      <tbody>{{range $rowIndex, $row := .Rows}}<tr><th class="row-number" scope="row">{{inc $rowIndex}}</th>{{range $row}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody>
    </table>
  </div>
</section>`
