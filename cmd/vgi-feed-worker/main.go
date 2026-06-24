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
	unixPath := flag.String("unix", "", "Serve the AF_UNIX launcher transport on this socket path instead of stdio")
	logFlags := vgi.RegisterLoggingFlags(flag.CommandLine)
	_ = flag.CommandLine.Parse(filterKnownFlags(os.Args[1:], map[string]bool{
		"log-level":  true,
		"log-format": true,
		"log-logger": true,
		"unix":       true,
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
			"source": "vgi-feed",
			"vgi.description_llm": "Fetch and parse RSS, Atom, and JSON feeds into SQL rows. " +
				"The feed input may be an http(s) URL (fetched over HTTP) or a raw feed document " +
				"supplied inline; the format (RSS 2.0, Atom, or JSON Feed) is auto-detected. " +
				"Use feed_items to get one row per entry (title, link, publish/update timestamps, " +
				"author, categories, summary, content) and feed_info for feed-level metadata " +
				"(title, type, language, item count). Use for syndication monitoring, ingesting " +
				"news/blog/podcast feeds, and turning feeds into queryable tables.",
			"vgi.description_md": "# feed\n\n" +
				"Fetch and parse **RSS / Atom / JSON** feeds into DuckDB rows over Apache Arrow.\n\n" +
				"The input is either an `http(s)` URL (fetched over HTTP) or a raw feed document " +
				"supplied inline; the format is auto-detected.\n\n" +
				"Table functions:\n\n" +
				"- `feed_items(input)` — one row per feed entry.\n" +
				"- `feed_info(input)` — one row of feed-level metadata.",
			"vgi.author":             "Query.Farm",
			"vgi.copyright":          "Copyright 2026 Query Farm LLC - https://query.farm",
			"vgi.license":            "MIT",
			"vgi.support_contact":    "https://github.com/Query-farm/vgi-feed/issues",
			"vgi.support_policy_url": "https://github.com/Query-farm/vgi-feed/blob/main/README.md",
		}),
		vgi.WithSchemaComments(map[string]string{
			"main": "RSS / Atom / JSON feed parsing table functions.",
		}),
		vgi.WithSchemaTags(map[string]map[string]string{
			"main": {
				"vgi.description_llm": "Feed parsing table functions: feed_items returns one row " +
					"per feed entry, and feed_info returns one row of feed-level metadata. Both " +
					"accept an http(s) URL or a raw RSS/Atom/JSON feed document and auto-detect " +
					"the feed format.",
				"vgi.description_md": "Table functions for parsing RSS / Atom / JSON feeds into rows.",
			},
		}),
	)
	feedworker.Register(w)

	if *httpMode {
		if err := w.RunHttp("127.0.0.1:0"); err != nil {
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
