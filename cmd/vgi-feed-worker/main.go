// Copyright 2026 Query Farm LLC - https://query.farm

// Command vgi-feed-worker is a VGI worker that fetches and parses RSS, Atom, and
// JSON feeds into DuckDB rows. The feed input may be an http(s) URL (fetched
// over HTTP) or a raw feed document supplied inline. It speaks the VGI protocol
// over stdio.
package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/Query-farm/vgi-feed/internal/feedworker"
	"github.com/Query-farm/vgi-go/vgi"
)

func main() {
	// Accept --http for HTTP transport and --unix for the AF_UNIX launcher
	// transport; default is stdio. Unknown launcher flags are tolerated (the
	// VGI extension varies argv to key its worker cache), so we filter to flags
	// we actually define before parsing.
	httpMode := flag.Bool("http", false, "Run as an HTTP server instead of stdio")
	// --http-addr sets the HTTP bind address (only with --http). The default
	// 127.0.0.1:0 (loopback, ephemeral port) is unchanged for dev/CI, which
	// discover the port from the "PORT:<n>" line the SDK prints on startup. The
	// container entrypoint passes 0.0.0.0:$PORT so a published host port and the
	// image HEALTHCHECK can reach the server.
	httpAddr := flag.String("http-addr", "127.0.0.1:0", "Address for the HTTP server to bind (only with --http)")
	unixPath := flag.String("unix", "", "Serve the AF_UNIX launcher transport on this socket path instead of stdio")
	logFlags := vgi.RegisterLoggingFlags(flag.CommandLine)
	_ = flag.CommandLine.Parse(filterKnownFlags(os.Args[1:], map[string]bool{
		"log-level":  true,
		"log-format": true,
		"log-logger": true,
		"unix":       true,
		"http-addr":  true,
	}))
	if err := logFlags.Apply(); err != nil {
		log.Fatalf("logging flags: %v", err)
	}

	sourceURL := "https://github.com/Query-farm/vgi-feed"
	w := vgi.NewWorker(
		vgi.WithCatalogName(feedworker.CatalogName),
		vgi.WithCatalogComment("Fetch and parse RSS / Atom / JSON feeds into rows."),
		vgi.WithCatalogInfo(vgi.CatalogInfo{
			Name:      feedworker.CatalogName,
			SourceURL: &sourceURL,
		}),
		vgi.WithCatalogTags(map[string]string{
			"source":    "vgi-feed",
			"vgi.title": "RSS / Atom / JSON Feed Parser",
			// VGI138: vgi.keywords must be a JSON array of strings, not a
			// comma-separated string.
			"vgi.keywords": `["feed","rss","atom","json feed","syndication","feed parser","news","blog","podcast","feed reader","rss reader","parse feed","feed items","feed metadata"]`,
			"vgi.doc_llm": "Fetch and parse RSS, Atom, and JSON feeds into SQL rows. " +
				"The feed input may be an http(s) URL (fetched over HTTP) or a raw feed document " +
				"supplied inline; the format (RSS 2.0, Atom, or JSON Feed) is auto-detected. " +
				"Use feed_items to get one row per entry (title, link, publish/update timestamps, " +
				"author, categories, summary, content) and feed_info for feed-level metadata " +
				"(title, type, language, item count). Use for syndication monitoring, ingesting " +
				"news/blog/podcast feeds, and turning feeds into queryable tables.",
			"vgi.doc_md": "# RSS, Atom & JSON Feed Parser for DuckDB\n\n" +
				"Parse RSS, Atom, and JSON feeds directly in SQL — turn any syndication feed into " +
				"queryable DuckDB rows over Apache Arrow, with zero glue code and automatic format detection.\n\n" +
				"This VGI extension brings **feed parsing to SQL** for data engineers, analysts, and " +
				"anyone who needs to ingest news, blog, or podcast feeds without writing a custom scraper. " +
				"Point it at an `http(s)` URL and it fetches the feed over HTTP, or hand it a raw RSS / Atom / " +
				"JSON Feed document inline — either way the format is auto-detected, so a single query " +
				"handles RSS 2.0, Atom, and JSON Feed alike. Missing or unparseable publish dates surface as " +
				"`NULL` timestamps, absent text fields as empty strings, and malformed feeds raise a clean, " +
				"actionable error rather than crashing your session.\n\n" +
				"Feed parsing and format auto-detection are powered by [gofeed](https://github.com/mmcdole/gofeed), " +
				"a robust, widely-used Go feed parser (see the [gofeed API documentation](https://pkg.go.dev/github.com/mmcdole/gofeed)). " +
				"It understands the [RSS 2.0 specification](https://www.rssboard.org/rss-specification), the " +
				"[Atom Syndication Format (RFC 4287)](https://datatracker.ietf.org/doc/html/rfc4287), and the " +
				"[JSON Feed format](https://www.jsonfeed.org/), normalizing their differences into one consistent " +
				"row shape. HTTP fetches are bounded by a timeout and a maximum response size so a query can " +
				"never hang or exhaust memory on a hostile feed.\n\n" +
				"## SQL use cases & functions\n\n" +
				"Use `feed_items(input [, timeout_ms, max_items])` to get **one row per feed entry**, with " +
				"`seq`, `guid`, `title`, `link`, `published`, `updated`, `author`, a `categories` list, " +
				"`summary`, and `content` columns — ideal for monitoring news and blog feeds, building a " +
				"podcast episode catalog, deduplicating items by GUID, or `UNNEST`-ing categories for tag " +
				"analytics. Use `feed_info(input [, timeout_ms])` for a **single row of feed-level metadata**: " +
				"`title`, `description`, `link`, detected `feed_type` (rss / atom / json), `language`, last " +
				"`updated` time, and `item_count` — perfect for cataloging or health-checking a set of feeds.\n\n" +
				"New to the data? Browse the `feed_registry` view for a curated set of real, public " +
				"feeds across all three formats, pick a `url`, and hand it to either table function. " +
				"Runnable, network-free example queries are attached to each function and to the `main` " +
				"schema.\n\n" +
				"Source code and issues: [github.com/Query-farm/vgi-feed](https://github.com/Query-farm/vgi-feed).",
			"vgi.author":             "Query.Farm",
			"vgi.copyright":          "Copyright 2026 Query Farm LLC - https://query.farm",
			"vgi.license":            "MIT",
			"vgi.support_contact":    "https://github.com/Query-farm/vgi-feed/issues",
			"vgi.support_policy_url": "https://github.com/Query-farm/vgi-feed/blob/main/README.md",
			// VGI152/VGI920: analyst-task suite so `vgi-lint simulate` can measure
			// how well agents use this worker. Every task runs against the inline
			// SampleRSS document, so it executes with no network access.
			"vgi.agent_test_tasks": feedworker.AgentTestTasks,
		}),
		vgi.WithSchemaComments(map[string]string{
			"main": "RSS / Atom / JSON feed parsing table functions.",
		}),
		vgi.WithSchemaTags(map[string]map[string]string{
			"main": {
				"vgi.title": "Feed Parsing Functions",
				// VGI138: vgi.keywords must be a JSON array of strings.
				"vgi.keywords": `["feed","rss","atom","json feed","syndication","feed_items","feed_info","parse feed","feed items","feed metadata","news","blog","podcast"]`,
				// VGI413: ordered category registry for this schema; each function
				// declares a vgi.category naming one of these entries.
				"vgi.categories": `[{"name":"Feed Items","description":"Expand a feed's entries into one row per item (title, link, dates, author, categories, summary, content) for querying, filtering, and analytics."},{"name":"Feed Metadata","description":"Summarize a feed's top-level attributes — title, description, detected format, language, last-updated time, and item count — as a single row."},{"name":"Feed Sources","description":"Browse a curated registry of well-known public RSS / Atom / JSON feeds to discover feed URLs to parse."}]`,
				// VGI123 classifying tags (BARE keys: domain/category/topic) for faceting.
				"domain":   "data-integration",
				"category": "parsing",
				"topic":    "feed-syndication",
				// VGI139: source_url belongs only on the catalog object (set via
				// CatalogInfo.SourceURL); no per-schema source_url here.
				"vgi.doc_llm": "Feed parsing table functions: feed_items returns one row " +
					"per feed entry, and feed_info returns one row of feed-level metadata. Both " +
					"accept an http(s) URL or a raw RSS/Atom/JSON feed document and auto-detect " +
					"the feed format.",
				"vgi.doc_md": "## Feed Parsing Functions\n\n" +
					"Table functions for turning **RSS 2.0 / Atom / JSON Feed** documents into " +
					"DuckDB rows over Apache Arrow.\n\n" +
					"### Functions\n\n" +
					"- **`feed_items(input [, timeout_ms, max_items])`** — one row per feed " +
					"entry, with sequence, GUID, title, link, publish/update timestamps, " +
					"author, categories array, summary, and content.\n" +
					"- **`feed_info(input [, timeout_ms])`** — a single row of feed-level " +
					"metadata: title, description, link, detected format, language, " +
					"last-updated time, and item count.\n\n" +
					"### Usage\n\n" +
					"`input` is either an `http(s)` URL (fetched over HTTP) or a raw feed " +
					"document supplied inline; the format is auto-detected.\n\n" +
					"### Notes\n\n" +
					"Missing/unparseable dates surface as `NULL` timestamps and absent " +
					"text fields as empty strings; malformed feeds raise a clean error.",
				// VGI506/VGI515 representative example queries for the schema, as a
				// JSON list of {description, sql} so each carries a human-readable
				// description. Each parses an inline RSS document so it runs without
				// network access.
				"vgi.example_queries": feedworker.SchemaExampleQueries,
			},
		}),
	)
	feedworker.Register(w)

	if *httpMode {
		if err := w.RunHttp(*httpAddr); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *unixPath != "" {
		// AF_UNIX launcher transport: serve on the given socket path. The SDK
		// prints "UNIX:<path>" once listening; idleTimeout=0 disables the
		// self-shutdown timer (the launcher/CI owns the process lifecycle).
		if err := w.RunUnix(*unixPath, 0); err != nil {
			log.Fatal(err)
		}
		return
	}
	w.RunStdio()
}

// filterKnownFlags drops argv tokens for flags this binary doesn't define, so
// launcher-injected differentiation flags don't abort flag parsing. Flags named
// in valueFlags consume the following token as their value.
func filterKnownFlags(args []string, valueFlags map[string]bool) []string {
	defined := map[string]bool{}
	flag.CommandLine.VisitAll(func(f *flag.Flag) { defined[f.Name] = true })
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		hasInlineValue := strings.ContainsRune(name, '=')
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		if !defined[name] {
			continue
		}
		out = append(out, a)
		if valueFlags[name] && !hasInlineValue && i+1 < len(args) {
			i++
			out = append(out, args[i])
		}
	}
	return out
}
