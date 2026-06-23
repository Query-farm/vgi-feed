// Copyright 2026 Query Farm LLC - https://query.farm

// Package mockfeed provides an in-memory feed server used by both the Go unit
// tests (as an httptest.Server) and the standalone mockserver binary (for the
// haybarn SQL E2E). It serves a fixed RSS 2.0 feed at /rss, an Atom feed at
// /atom, and a JSON Feed at /json, plus a /malformed endpoint that returns
// garbage for negative tests.
package mockfeed

import "net/http"

// The three feeds share the same logical content (2-3 items each, with titles,
// links, dates, categories, and an author) so tests can make consistent
// assertions across formats. The literals below are valid, well-formed feeds.

// RSS is a valid RSS 2.0 document with two items.
const RSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>VGI Mock RSS</title>
    <link>https://example.com/rss</link>
    <description>A mock RSS 2.0 feed for vgi-feed tests</description>
    <language>en-us</language>
    <lastBuildDate>Mon, 02 Jan 2006 15:04:05 GMT</lastBuildDate>
    <item>
      <title>First RSS Post</title>
      <link>https://example.com/rss/1</link>
      <guid>https://example.com/rss/1</guid>
      <author>alice@example.com (Alice)</author>
      <category>news</category>
      <category>tech</category>
      <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
      <description>Summary of the first RSS post.</description>
    </item>
    <item>
      <title>Second RSS Post</title>
      <link>https://example.com/rss/2</link>
      <guid>https://example.com/rss/2</guid>
      <category>updates</category>
      <pubDate>Tue, 03 Jan 2006 15:04:05 GMT</pubDate>
      <description>Summary of the second RSS post.</description>
    </item>
  </channel>
</rss>`

// Atom is a valid Atom 1.0 document with two entries.
const Atom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>VGI Mock Atom</title>
  <subtitle>A mock Atom feed for vgi-feed tests</subtitle>
  <link href="https://example.com/atom"/>
  <updated>2006-01-02T15:04:05Z</updated>
  <id>urn:uuid:vgi-mock-atom</id>
  <entry>
    <title>First Atom Entry</title>
    <link href="https://example.com/atom/1"/>
    <id>https://example.com/atom/1</id>
    <author><name>Bob</name></author>
    <category term="news"/>
    <category term="tech"/>
    <updated>2006-01-02T15:04:05Z</updated>
    <published>2006-01-02T15:04:05Z</published>
    <summary>Summary of the first Atom entry.</summary>
    <content type="text">Body of the first Atom entry.</content>
  </entry>
  <entry>
    <title>Second Atom Entry</title>
    <link href="https://example.com/atom/2"/>
    <id>https://example.com/atom/2</id>
    <category term="updates"/>
    <updated>2006-01-03T15:04:05Z</updated>
    <published>2006-01-03T15:04:05Z</published>
    <summary>Summary of the second Atom entry.</summary>
  </entry>
</feed>`

// JSON is a valid JSON Feed 1.1 document with two items.
const JSON = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "VGI Mock JSON",
  "home_page_url": "https://example.com/json",
  "feed_url": "https://example.com/json/feed.json",
  "description": "A mock JSON Feed for vgi-feed tests",
  "language": "en-us",
  "authors": [{"name": "Carol"}],
  "items": [
    {
      "id": "https://example.com/json/1",
      "url": "https://example.com/json/1",
      "title": "First JSON Item",
      "summary": "Summary of the first JSON item.",
      "content_text": "Body of the first JSON item.",
      "date_published": "2006-01-02T15:04:05Z",
      "date_modified": "2006-01-02T15:04:05Z",
      "tags": ["news", "tech"],
      "authors": [{"name": "Carol"}]
    },
    {
      "id": "https://example.com/json/2",
      "url": "https://example.com/json/2",
      "title": "Second JSON Item",
      "summary": "Summary of the second JSON item.",
      "date_published": "2006-01-03T15:04:05Z",
      "tags": ["updates"]
    }
  ]
}`

// Malformed is intentionally invalid (neither well-formed XML nor JSON) so a
// negative test can assert the worker surfaces a parse error.
const Malformed = `<rss><channel><title>broken</title><item><title>oops`

// NewHandler returns the feed HTTP handler serving /rss, /atom, /json, and
// /malformed.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rss", serve("application/rss+xml; charset=utf-8", RSS))
	mux.HandleFunc("/atom", serve("application/atom+xml; charset=utf-8", Atom))
	mux.HandleFunc("/json", serve("application/feed+json; charset=utf-8", JSON))
	mux.HandleFunc("/malformed", serve("application/rss+xml; charset=utf-8", Malformed))
	return mux
}

// serve writes a fixed body with the given content type.
func serve(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}
}
