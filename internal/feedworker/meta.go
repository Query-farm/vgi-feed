// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

// Shared helpers for the per-object discovery/description metadata that the
// vgi-lint strict profile expects on EVERY function and table.
//
// Each function/table surfaces these in its FunctionMetadata.Tags:
//   - vgi.title      (VGI124) — human-friendly display name
//   - vgi.doc_llm    (VGI112) — Markdown narrative aimed at LLMs/agents
//   - vgi.doc_md     (VGI113) — Markdown narrative for human docs
//   - vgi.keywords   (VGI126/VGI138) — a JSON array of search terms/synonyms
//
// vgi.source_url is deliberately NOT set per object: VGI139 wants source_url
// only on the catalog object (it is set there via CatalogInfo.SourceURL), so
// per-object source_url tags are omitted.

import "encoding/json"

// keywordsJSON serialises the given keywords as a JSON array string, which is
// the form VGI138 requires for vgi.keywords (e.g. ["feed","rss"]). Comma-
// separated strings are rejected.
func keywordsJSON(keywords ...string) string {
	b, err := json.Marshal(keywords)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// objectTags returns the four standard per-object discovery/description tags.
func objectTags(title, docLLM, docMD, keywords string) map[string]string {
	return map[string]string{
		"vgi.title":    title,
		"vgi.doc_llm":  docLLM,
		"vgi.doc_md":   docMD,
		"vgi.keywords": keywords,
	}
}
