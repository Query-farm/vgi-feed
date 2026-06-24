// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

// Shared helpers for the per-object discovery/description metadata that the
// vgi-lint strict profile (0.23.0) expects on EVERY function and table.
//
// Each function/table surfaces these in its FunctionMetadata.Tags:
//   - vgi.title           (VGI124) — human-friendly display name
//   - vgi.description_llm (VGI112) — concise prose aimed at LLMs
//   - vgi.description_md  (VGI113) — short Markdown description
//   - vgi.keywords        (VGI126) — comma-separated search terms/synonyms
//   - vgi.source_url      (VGI128) — link to the implementing source file
//
// sourceURL(file) builds the canonical GitHub blob URL for a source file so
// every object points at exactly where it is implemented.

// sourceBase is the GitHub blob URL prefix for source files in this repo,
// pinned to main.
const sourceBase = "https://github.com/Query-farm/vgi-feed/blob/main"

// sourceURL builds the implementation vgi.source_url for a repo-relative file,
// e.g. sourceURL("internal/feedworker/functions.go").
func sourceURL(relativePath string) string {
	return sourceBase + "/" + relativePath
}

// objectTags returns the five standard per-object discovery/description tags.
// relativePath is the implementing file relative to the repo root.
func objectTags(title, descriptionLLM, descriptionMD, keywords, relativePath string) map[string]string {
	return map[string]string{
		"vgi.title":           title,
		"vgi.description_llm": descriptionLLM,
		"vgi.description_md":  descriptionMD,
		"vgi.keywords":        keywords,
		"vgi.source_url":      sourceURL(relativePath),
	}
}
