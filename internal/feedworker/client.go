// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// FetchOptions carries the parameters shared by both table functions.
type FetchOptions struct {
	// Input is either an http(s):// URL to fetch, or a raw feed document
	// (RSS/Atom XML or JSON Feed text) to parse directly.
	Input string
	// Timeout bounds the HTTP fetch. Zero uses defaultTimeout.
	Timeout time.Duration
	// MaxItems caps the number of items materialised. <= 0 means no cap (a hard
	// safety cap of hardMaxItems still applies).
	MaxItems int
}

const (
	defaultTimeout = 15 * time.Second
	// maxBodyBytes bounds how much we read from an HTTP response so a hostile or
	// runaway server can't exhaust memory.
	maxBodyBytes = 64 << 20 // 64 MiB
	// hardMaxItems is the absolute ceiling on materialised items regardless of
	// the caller's max_items, so a feed with millions of entries can't blow up.
	hardMaxItems = 1_000_000
)

// parser is shared; gofeed.Parser is safe for sequential reuse and auto-detects
// RSS / Atom / JSON Feed.
var feedHTTPClient = &http.Client{}

// looksLikeRawFeed reports whether s is a raw feed document rather than a URL.
// A document that (after trimming whitespace and a BOM) starts with '<' (XML:
// RSS/Atom) or '{'/'[' (JSON Feed) is treated as raw text; everything else is
// treated as a URL to fetch.
func looksLikeRawFeed(s string) bool {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "\ufeff") // UTF-8 BOM
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	switch t[0] {
	case '<', '{', '[':
		return true
	default:
		return false
	}
}

// looksLikeURL reports whether s is an http(s) URL.
func looksLikeURL(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://")
}

// ParseFeed resolves the input (URL → fetch, raw text → parse directly) and
// returns the parsed feed. All error paths return a clear, wrapped error and
// never panic.
func ParseFeed(ctx context.Context, opts FetchOptions) (*gofeed.Feed, error) {
	input := strings.TrimSpace(opts.Input)
	if input == "" {
		return nil, fmt.Errorf("feed: input is required (a URL or raw feed text)")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	var doc string
	switch {
	case looksLikeRawFeed(opts.Input):
		// Raw feed document supplied inline; parse it directly.
		doc = opts.Input
	case looksLikeURL(input):
		body, err := fetchURL(ctx, input, timeout)
		if err != nil {
			return nil, err
		}
		doc = body
	default:
		return nil, fmt.Errorf("feed: input %q is neither an http(s) URL nor raw feed text (expected it to start with http://, https://, '<', or '{')", snippet(input))
	}

	fp := gofeed.NewParser()
	feed, err := fp.ParseString(doc)
	if err != nil {
		return nil, fmt.Errorf("feed: parse failed: %w", err)
	}
	return feed, nil
}

// fetchURL performs a bounded GET and returns the response body as a string.
func fetchURL(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("feed: build request for %s: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/json, application/xml, text/xml, */*")
	req.Header.Set("User-Agent", "vgi-feed/1.0 (+https://query.farm)")

	resp, err := feedHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("feed: GET %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("feed: read response from %s: %w", rawURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("feed: %s returned HTTP %d: %s", rawURL, resp.StatusCode, snippet(string(body)))
	}
	return string(body), nil
}

// snippet returns a short, single-line excerpt for error messages.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "(empty)"
	}
	return s
}

// clampMaxItems normalises a caller-supplied max_items to the [0, hardMaxItems]
// range used by the parser layer (0 → hardMaxItems).
func clampMaxItems(max int64) int {
	if max <= 0 || max > hardMaxItems {
		return hardMaxItems
	}
	return int(max)
}
