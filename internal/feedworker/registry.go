// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"encoding/json"
	"strings"

	"github.com/Query-farm/vgi-go/vgi"
)

// exampleQueriesJSON serialises example queries as the JSON array of
// {"description","sql"} objects that vgi.example_queries requires (VGI502/503).
func exampleQueriesJSON(pairs ...[2]string) string {
	type ex struct {
		Description string `json:"description"`
		SQL         string `json:"sql"`
	}
	out := make([]ex, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, ex{Description: p[0], SQL: p[1]})
	}
	b, err := json.Marshal(out)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// A curated, browsable registry of well-known public feeds.
//
// WHY THIS EXISTS (VGI146): a worker whose only entry points are table
// functions forces an agent to already KNOW a feed URL (and the function's
// arguments) before it can retrieve any data. This view is a zero-argument,
// credential-free browsable relation — `SELECT * FROM feed.main.feed_registry`
// returns a sensible default slice of real, public RSS / Atom / JSON feeds an
// agent can discover and then hand straight to feed_items / feed_info. It is a
// pure VALUES list (no network access), so it always scans.

// feedSource is one row of the feed_registry view: a human label, the feed URL,
// its format, a broad topic, and a one-line description.
type feedSource struct {
	Name        string
	URL         string
	FeedType    string
	Category    string
	Description string
}

// feedRegistry is the curated list surfaced by the feed_registry view. Every
// URL is a real, stable, publicly reachable feed, chosen to span all three
// supported formats (rss / atom / json) and several topics.
var feedRegistry = []feedSource{
	{"Hacker News Front Page", "https://hnrss.org/frontpage", "rss", "Technology", "Front-page stories from Hacker News."},
	{"NASA Breaking News", "https://www.nasa.gov/feed/", "rss", "Science", "Breaking news and mission updates from NASA."},
	{"BBC News World", "https://feeds.bbci.co.uk/news/world/rss.xml", "rss", "News", "Top world news headlines from the BBC."},
	{"The Verge", "https://www.theverge.com/rss/index.xml", "atom", "Technology", "Technology, science, and culture reporting from The Verge."},
	{"GitHub Blog", "https://github.blog/feed/", "rss", "Technology", "Product, engineering, and community posts from GitHub."},
	{"Daring Fireball", "https://daringfireball.net/feeds/main", "atom", "Technology", "John Gruber's commentary on Apple and technology."},
	{"xkcd", "https://xkcd.com/rss.xml", "rss", "Comics", "The xkcd webcomic of romance, sarcasm, math, and language."},
	{"Krebs on Security", "https://krebsonsecurity.com/feed/", "rss", "Security", "In-depth security news and investigation from Brian Krebs."},
	{"JSON Feed", "https://www.jsonfeed.org/feed.json", "json", "Technology", "The reference JSON Feed published by the format's authors."},
	{"Query Farm", "https://query.farm/index.xml", "atom", "Technology", "Posts and release notes from Query Farm."},
}

// sqlQuote renders a Go string as a single-quoted SQL literal.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// registryViewDefinition builds the VALUES-backed SELECT that defines the
// feed_registry view. It has NO external dependency (pure literals), so it
// scans without network access or credentials.
func registryViewDefinition() string {
	var b strings.Builder
	b.WriteString("SELECT * FROM (VALUES\n")
	for i, s := range feedRegistry {
		b.WriteString("  (")
		b.WriteString(sqlQuote(s.Name))
		b.WriteString(", ")
		b.WriteString(sqlQuote(s.URL))
		b.WriteString(", ")
		b.WriteString(sqlQuote(s.FeedType))
		b.WriteString(", ")
		b.WriteString(sqlQuote(s.Category))
		b.WriteString(", ")
		b.WriteString(sqlQuote(s.Description))
		b.WriteString(")")
		if i < len(feedRegistry)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(") AS t(name, url, feed_type, category, description)")
	return b.String()
}

// RegisterRegistry registers the curated feed_registry view on the worker.
// It is a browsable, credential-free discovery relation (see the WHY note above)
// that satisfies VGI146 and gives agents a starting set of real feeds.
func RegisterRegistry(w *vgi.Worker) {
	w.RegisterCatalogView("main", vgi.CatalogView{
		Name: "feed_registry",
		Comment: "Curated registry of well-known public RSS / Atom / JSON feeds " +
			"(name, url, feed_type, category, description) to browse and feed into " +
			"feed_items / feed_info.",
		Definition: registryViewDefinition(),
		ColumnComments: map[string]string{
			"name":        "Human-friendly feed name.",
			"url":         "Feed URL to pass to feed_items(input) or feed_info(input).",
			"feed_type":   "Feed format: rss, atom, or json.",
			"category":    "Broad topic such as Technology, News, Science, Security, or Comics.",
			"description": "One-line description of what the feed publishes.",
		},
		Tags: map[string]string{
			"vgi.title": "Curated Feed Registry",
			// VGI413: name one of the schema's vgi.categories entries.
			"vgi.category": "Feed Sources",
			// VGI123 classifying tags (bare keys) for faceting, matching the schema.
			"domain": "data-integration",
			"topic":  "feed-syndication",
			"vgi.keywords": keywordsJSON("feed registry", "feed catalog", "feed directory",
				"well-known feeds", "public feeds", "rss sources", "atom sources",
				"json feed", "discover feeds", "feed urls", "example feeds"),
			"vgi.doc_llm": "A browsable, zero-argument catalog of well-known public feeds. " +
				"Query it first to discover a real feed URL (across rss, atom, and json " +
				"formats and topics like Technology, News, Science, Security, and Comics), " +
				"then pass a chosen url into feed_items(input) or feed_info(input). It is a " +
				"static list, so it returns instantly with no network access or credentials.",
			"vgi.doc_md": "## Curated Feed Registry\n\n" +
				"A browsable, zero-argument view listing well-known public **RSS / Atom / " +
				"JSON** feeds — a starting point for discovery when you do not already have a " +
				"feed URL in hand.\n\n" +
				"### Columns\n\n" +
				"- **`name`** — human-friendly feed name.\n" +
				"- **`url`** — the feed URL to hand to `feed_items(input)` or `feed_info(input)`.\n" +
				"- **`feed_type`** — the feed format (`rss`, `atom`, or `json`).\n" +
				"- **`category`** — a broad topic (Technology, News, Science, Security, Comics).\n" +
				"- **`description`** — a one-line summary of the feed.\n\n" +
				"### Notes\n\n" +
				"The registry is a static list, so it returns instantly with no network " +
				"access. Pick a `url` here, then parse it with the feed table functions.",
			// VGI502/503/504/505/511: object-level examples (JSON array of
			// {description, sql}) that call this view with fully catalog-qualified
			// references.
			"vgi.example_queries": exampleQueriesJSON(
				[2]string{
					"Browse every feed in the registry with its URL, format, and topic.",
					"SELECT name, url, feed_type, category FROM feed.main.feed_registry ORDER BY name",
				},
				[2]string{
					"Find the feed URLs published in JSON Feed format.",
					"SELECT name, url FROM feed.main.feed_registry WHERE feed_type = 'json' ORDER BY name",
				},
			),
		},
	})
}
