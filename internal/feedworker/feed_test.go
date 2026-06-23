// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Query-farm/vgi-feed/internal/mockfeed"
)

// newServer starts an in-process feed server and returns its base URL.
func newServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(mockfeed.NewHandler())
	t.Cleanup(srv.Close)
	return srv.URL
}

func parse(t *testing.T, input string) []Item {
	t.Helper()
	feed, err := ParseFeed(context.Background(), FetchOptions{Input: input})
	if err != nil {
		t.Fatalf("ParseFeed(%.40q): %v", input, err)
	}
	return ItemsFrom(feed, 1000)
}

func TestParseRSSFromURL(t *testing.T) {
	base := newServer(t)
	items := parse(t, base+"/rss")
	if len(items) != 2 {
		t.Fatalf("expected 2 RSS items, got %d", len(items))
	}
	first := items[0]
	if first.Seq != 0 {
		t.Errorf("first seq should be 0, got %d", first.Seq)
	}
	if first.Title != "First RSS Post" {
		t.Errorf("title wrong: %q", first.Title)
	}
	if first.Link != "https://example.com/rss/1" {
		t.Errorf("link wrong: %q", first.Link)
	}
	if first.Published == nil {
		t.Error("first RSS item should have a parsed published date")
	} else if first.Published.Year() != 2006 {
		t.Errorf("published year wrong: %v", first.Published)
	}
	if len(first.Categories) != 2 || first.Categories[0] != "news" || first.Categories[1] != "tech" {
		t.Errorf("categories wrong: %#v", first.Categories)
	}
	if !strings.Contains(first.Author, "Alice") {
		t.Errorf("author should mention Alice, got %q", first.Author)
	}
	if first.Summary == "" {
		t.Error("summary should be populated")
	}
}

func TestParseAtomFromURL(t *testing.T) {
	base := newServer(t)
	items := parse(t, base+"/atom")
	if len(items) != 2 {
		t.Fatalf("expected 2 Atom entries, got %d", len(items))
	}
	first := items[0]
	if first.Title != "First Atom Entry" {
		t.Errorf("title wrong: %q", first.Title)
	}
	if first.Author != "Bob" {
		t.Errorf("author should be Bob, got %q", first.Author)
	}
	if first.Content != "Body of the first Atom entry." {
		t.Errorf("content wrong: %q", first.Content)
	}
	if len(first.Categories) != 2 {
		t.Errorf("expected 2 categories, got %#v", first.Categories)
	}
	if first.Updated == nil {
		t.Error("atom entry should have a parsed updated date")
	}
}

func TestParseJSONFeedFromURL(t *testing.T) {
	base := newServer(t)
	items := parse(t, base+"/json")
	if len(items) != 2 {
		t.Fatalf("expected 2 JSON items, got %d", len(items))
	}
	first := items[0]
	if first.Title != "First JSON Item" {
		t.Errorf("title wrong: %q", first.Title)
	}
	if first.Author != "Carol" {
		t.Errorf("author should be Carol, got %q", first.Author)
	}
	if len(first.Categories) != 2 || first.Categories[0] != "news" {
		t.Errorf("categories (tags) wrong: %#v", first.Categories)
	}
	if first.Published == nil {
		t.Error("json item should have a parsed published date")
	}
}

func TestFeedTypeDetection(t *testing.T) {
	base := newServer(t)
	cases := []struct {
		path string
		want string
	}{
		{"/rss", "rss"},
		{"/atom", "atom"},
		{"/json", "json"},
	}
	for _, c := range cases {
		feed, err := ParseFeed(context.Background(), FetchOptions{Input: base + c.path})
		if err != nil {
			t.Fatalf("ParseFeed %s: %v", c.path, err)
		}
		info := InfoFrom(feed)
		if info.FeedType != c.want {
			t.Errorf("%s: feed_type = %q, want %q", c.path, info.FeedType, c.want)
		}
		if info.ItemCount != 2 {
			t.Errorf("%s: item_count = %d, want 2", c.path, info.ItemCount)
		}
		if info.Title == "" {
			t.Errorf("%s: title should not be empty", c.path)
		}
	}
}

func TestRawTextInputParsedDirectly(t *testing.T) {
	// Raw RSS XML, no server involved.
	items := parse(t, mockfeed.RSS)
	if len(items) != 2 || items[0].Title != "First RSS Post" {
		t.Fatalf("raw RSS not parsed: %d items", len(items))
	}

	// Raw JSON Feed.
	jitems := parse(t, mockfeed.JSON)
	if len(jitems) != 2 || jitems[0].Title != "First JSON Item" {
		t.Fatalf("raw JSON feed not parsed: %d items", len(jitems))
	}

	// Raw Atom with a leading BOM + whitespace still detected as raw text.
	bom := "\ufeff  \n" + mockfeed.Atom
	feed, err := ParseFeed(context.Background(), FetchOptions{Input: bom})
	if err != nil {
		t.Fatalf("raw Atom with BOM not parsed: %v", err)
	}
	if feed.FeedType != "atom" {
		t.Errorf("BOM-prefixed Atom feed_type = %q", feed.FeedType)
	}
}

func TestRawInputIsNotFetchedOverHTTP(t *testing.T) {
	// Raw feed text that happens to be long must NOT be treated as a URL.
	if !looksLikeRawFeed(mockfeed.RSS) {
		t.Error("RSS document should be detected as raw text")
	}
	if looksLikeRawFeed("https://example.com/feed.xml") {
		t.Error("a URL should not be detected as raw text")
	}
	if !looksLikeURL("http://example.com") || !looksLikeURL("https://example.com") {
		t.Error("http(s) inputs should be detected as URLs")
	}
}

func TestMissingDateYieldsNil(t *testing.T) {
	// An RSS item with no pubDate must leave Published nil (→ NULL TIMESTAMP).
	const noDate = `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>
		<item><title>No Date</title><link>https://x/1</link></item></channel></rss>`
	items := parse(t, noDate)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Published != nil {
		t.Errorf("missing pubDate should yield nil Published, got %v", items[0].Published)
	}
	if items[0].Updated != nil {
		t.Errorf("missing updated should yield nil Updated, got %v", items[0].Updated)
	}
	// Categories must be a non-nil empty slice, not nil.
	if items[0].Categories == nil {
		t.Error("categories should be non-nil empty slice")
	}
}

func TestMaxItemsCaps(t *testing.T) {
	base := newServer(t)
	feed, err := ParseFeed(context.Background(), FetchOptions{Input: base + "/rss"})
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if got := ItemsFrom(feed, 1); len(got) != 1 {
		t.Errorf("max_items=1 should cap to 1 item, got %d", len(got))
	}
	if got := ItemsFrom(feed, 0); len(got) != 2 {
		t.Errorf("max_items<=0 should not cap, got %d", len(got))
	}
}

func TestHTTP404Errors(t *testing.T) {
	base := newServer(t)
	_, err := ParseFeed(context.Background(), FetchOptions{Input: base + "/nope"})
	if err == nil {
		t.Fatal("expected an error for a 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention HTTP 404, got: %v", err)
	}
}

func TestHTTP500Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	_, err := ParseFeed(context.Background(), FetchOptions{Input: srv.URL})
	if err == nil {
		t.Fatal("expected an error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP 500, got: %v", err)
	}
}

func TestMalformedFeedErrors(t *testing.T) {
	// Over HTTP.
	base := newServer(t)
	if _, err := ParseFeed(context.Background(), FetchOptions{Input: base + "/malformed"}); err == nil {
		t.Error("expected a parse error for the malformed feed endpoint")
	}
	// Raw text.
	if _, err := ParseFeed(context.Background(), FetchOptions{Input: mockfeed.Malformed}); err == nil {
		t.Error("expected a parse error for raw malformed feed text")
	}
}

func TestUnreachableHostErrors(t *testing.T) {
	// 127.0.0.1:1 is reserved/closed; the dial fails fast.
	_, err := ParseFeed(context.Background(), FetchOptions{Input: "http://127.0.0.1:1/feed.xml"})
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
}

func TestEmptyInputErrors(t *testing.T) {
	if _, err := ParseFeed(context.Background(), FetchOptions{Input: ""}); err == nil {
		t.Error("expected an error for empty input")
	}
	if _, err := ParseFeed(context.Background(), FetchOptions{Input: "   "}); err == nil {
		t.Error("expected an error for whitespace-only input")
	}
}

func TestNonURLNonFeedInputErrors(t *testing.T) {
	// Plain text that is neither a URL nor a feed document.
	if _, err := ParseFeed(context.Background(), FetchOptions{Input: "just some words"}); err == nil {
		t.Error("expected an error for input that is neither a URL nor feed text")
	}
}
