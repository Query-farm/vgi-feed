<p align="center">
  <img src="https://query.farm/media-kit/products/vgi/vgi-logo.png" alt="Vector Gateway Interface (VGI)" width="320">
</p>

<p align="center"><em>A <a href="https://query.farm">Query.Farm</a> VGI worker for DuckDB.</em></p>

# Parse RSS, Atom & JSON Feeds into Rows in DuckDB

> **vgi-feed** · a [Query.Farm](https://query.farm) VGI worker · powered by gofeed

[![CI](https://github.com/Query-farm/vgi-feed/actions/workflows/ci.yml/badge.svg)](https://github.com/Query-farm/vgi-feed/actions/workflows/ci.yml)

A [VGI](https://query.farm) worker, written in **Go**, that fetches and parses
**RSS**, **Atom**, and **JSON Feed** documents into DuckDB/SQL rows. The feed
format is auto-detected. Input may be an **http(s) URL** (fetched over HTTP) or a
**raw feed document** supplied inline (RSS/Atom XML or JSON Feed text).

Built on the [`vgi-go`](https://github.com/Query-farm/vgi-go) SDK; it speaks the
VGI protocol over stdio. Catalog name: `feed`. Universal feed parsing is provided
by [`gofeed`](https://github.com/mmcdole/gofeed).

```sql
INSTALL vgi FROM community; LOAD vgi;

ATTACH 'feed' AS feed (TYPE vgi, LOCATION 'github-auto://Query-farm/vgi-feed@v0.5.0');

-- One row per feed item (input is a URL → fetched over HTTP).
SELECT seq, title, link, published, author, categories
FROM feed.feed_items('https://news.ycombinator.com/rss');

-- Feed-level metadata (one row); feed_type is 'rss' | 'atom' | 'json'.
SELECT title, feed_type, language, item_count
FROM feed.feed_info('https://news.ycombinator.com/rss');

-- Raw feed text input (not a URL) is parsed directly.
SELECT title FROM feed.feed_items('<?xml version="1.0"?><rss version="2.0">...</rss>');

-- Named options: HTTP timeout and an item cap.
SELECT * FROM feed.feed_items('https://example.com/feed.xml',
  timeout_ms := 5000, max_items := 50);

-- categories is a VARCHAR[] — UNNEST to one row per category.
SELECT title, UNNEST(categories) AS category
FROM feed.feed_items('https://example.com/feed.xml');
```

## Quick start

Every [GitHub release](https://github.com/Query-farm/vgi-feed/releases) ships a prebuilt worker binary. The `vgi` extension downloads the one for your platform, checks its SHA-256 and caches it, so there is nothing to install besides DuckDB:

```sql
INSTALL vgi FROM community;
LOAD vgi;

ATTACH 'feed' AS feed (TYPE vgi, LOCATION 'github-auto://Query-farm/vgi-feed@v0.5.0');

-- List the items parsed from an inline RSS 2.0 feed, ordered by position.
SELECT seq, title, link FROM feed.main.feed_items('<rss version="2.0"><channel><title>Query Farm Sample Feed</title><link>https://query.farm/</link><description>A small RSS feed used by vgi-feed examples</description><language>en-us</language><item><title>First Post</title><link>https://query.farm/first</link><category>news</category><category>release</category><description>The first sample entry</description></item><item><title>Second Post</title><link>https://query.farm/second</link><category>news</category><description>The second sample entry</description></item></channel></rss>') ORDER BY seq;

-- Count the items in an inline feed.
SELECT count(*) AS items FROM feed.main.feed_items('<rss version="2.0"><channel><title>Query Farm Sample Feed</title><link>https://query.farm/</link><description>A small RSS feed used by vgi-feed examples</description><language>en-us</language><item><title>First Post</title><link>https://query.farm/first</link><category>news</category><category>release</category><description>The first sample entry</description></item><item><title>Second Post</title><link>https://query.farm/second</link><category>news</category><description>The second sample entry</description></item></channel></rss>');
```

Prebuilt binaries cover macOS (Apple silicon), Linux (x86-64 and arm64) and Windows (x86-64). `github-auto://` needs an explicit release tag; change `@v0.5.0` to move to a newer release.

### Other ways to run it

- **Docker** (the image bundles the worker; needs a container runtime):

  ```sql
  ATTACH 'feed' AS feed (TYPE vgi, LOCATION 'docker://ghcr.io/query-farm/vgi-feed:0.5.0');
  ```

- **Manual download.** Download the archive for your platform from the [releases page](https://github.com/Query-farm/vgi-feed/releases), verify it against its `.sha256` file (a cosign `.cosign.bundle` is published alongside), unpack it, and set `LOCATION` to the path of the extracted worker executable.

### Build from source

To build the worker yourself, see [Building](#building).

## Functions

Both are **table functions** (each parses one feed and returns rows). The first
argument is always `input` — either an `http://`/`https://` URL to fetch, or a
raw feed document (RSS/Atom XML or JSON Feed text).

| Function | Returns | Description |
| --- | --- | --- |
| `feed_items(input)` | `seq BIGINT, guid, title, link, published TIMESTAMP, updated TIMESTAMP, author, categories VARCHAR[], summary, content` | One row per feed item, in feed order (`seq` starts at 0). |
| `feed_info(input)` | `title, description, link, feed_type, language, updated TIMESTAMP, item_count INT` | Feed-level metadata (one row). `feed_type` is `rss` / `atom` / `json`. |

### Named options

| Option | Applies to | Default | Meaning |
| --- | --- | --- | --- |
| `timeout_ms` | both | `15000` | HTTP fetch timeout in milliseconds (ignored for raw-text input). |
| `max_items` | `feed_items` | `1000` | Maximum number of items returned. |

### URL vs. raw text

The `input` argument is classified by its leading character (after trimming
whitespace and an optional UTF-8 BOM):

- starts with `<`, `{`, or `[` → treated as a **raw feed document** and parsed
  directly (no network access);
- starts with `http://` or `https://` → **fetched over HTTP** and then parsed;
- anything else → a clear error.

### Fetching feed URLs

Pass a feed URL directly to either function. The worker performs the HTTP GET;
you do not need to download the document first or load DuckDB's `httpfs` extension
to fetch the feed.

```sql
-- Fetch an RSS feed and return its latest entries in feed order.
SELECT seq, title, link, published
FROM feed.feed_items('https://hnrss.org/frontpage',
  timeout_ms := 5000, max_items := 25)
ORDER BY seq;

-- Inspect a remote feed's metadata.
SELECT title, feed_type, language, item_count
FROM feed.feed_info('https://www.nasa.gov/feed/');

-- Atom and JSON Feed URLs work through the same function.
SELECT title, link FROM feed.feed_items('https://daringfireball.net/feeds/main');
SELECT title, link FROM feed.feed_items('https://www.jsonfeed.org/feed.json');

-- Store a fetched feed in a local DuckDB table.
CREATE TABLE headlines AS
SELECT * FROM feed.feed_items('https://hnrss.org/frontpage', max_items := 0);

-- DuckDB's built-in COPY exports the fetched rows to Parquet, CSV, or JSON.
COPY (
  SELECT * FROM feed.feed_items('https://hnrss.org/frontpage', max_items := 0)
) TO 'headlines.parquet' (FORMAT parquet);

-- Discover more URLs, then pass a chosen URL to either function.
SELECT name, url, feed_type FROM feed.main.feed_registry ORDER BY name;
```

The URL must point to a feed document. HTML pages are not searched for feed links.
Each call accepts one URL; the worker does not crawl links within feed entries.
`feed_items` returns at most 1,000 items by default; `max_items := 0` raises that
to the one-million-item safety ceiling.

### Behaviour

- **Format auto-detection** — RSS, Atom, and JSON Feed are detected by `gofeed`;
  `feed_info.feed_type` reports which.
- **Dates** — `published` / `updated` come from the feed's parsed dates; a
  missing or unparseable date is emitted as a **NULL TIMESTAMP**.
- **Categories** — a non-nil `VARCHAR[]` (empty list when the item has none).
- **Robustness** — HTTP errors/timeouts, unreachable hosts, and malformed feeds
  become clear DuckDB errors (with a short body excerpt for non-2xx responses);
  the worker never crashes or hangs. Response bodies are bounded (64 MiB) and the
  item count is capped.

## COPY feed documents

COPY formats are available in **v0.5.0 and later**. Attach the released worker
in a new DuckDB session:

```sql
INSTALL vgi FROM community; LOAD vgi;
ATTACH 'feed' AS feed (TYPE vgi, LOCATION 'github-auto://Query-farm/vgi-feed@v0.5.0');
```

Format names use the attachment alias: attaching as `news` makes them
`news.auto`, `news.rss`, `news.atom`, and `news.jsonfeed`.

| Format | Direction | Meaning |
| --- | --- | --- |
| `feed.auto` | `COPY FROM` | Auto-detect and import RSS, Atom, or JSON Feed from an HTTP(S) URL or local file. |
| `feed.rss` | `COPY TO` | Write an RSS 2.0 document. |
| `feed.atom` | `COPY TO` | Write an Atom 1.0 document. |
| `feed.jsonfeed` | `COPY TO` | Write a JSON Feed 1.1 document. |

### Import from a URL or file

```sql
-- Create the complete item schema without fetching a feed.
CREATE TABLE items AS SELECT * FROM feed.feed_items(NULL);

COPY items FROM 'https://hnrss.org/frontpage'
  (FORMAT 'feed.auto', timeout_ms 5000);

COPY items FROM '/absolute/path/feed.xml' (FORMAT 'feed.auto');
COPY items FROM '/absolute/path/feed.json' (FORMAT 'feed.auto');

-- A subset of columns is supported; names determine the mapping.
CREATE TABLE titles (link VARCHAR, title VARCHAR, published TIMESTAMP);
COPY titles FROM 'https://www.nasa.gov/feed/'
  (FORMAT 'feed.auto', max_items 50);
```

`COPY FROM` appends to an existing table. Destination columns must match
`feed_items` column names and types; subsets and reordered columns are supported.
Use `BIGINT` for `seq`, `TIMESTAMP` for dates, `VARCHAR[]` for categories, and
`VARCHAR` for other fields. Unknown columns, duplicate names, and incompatible
types raise errors. A `COPY items (title, link) FROM ...` column list can populate
selected columns while DuckDB supplies defaults for the rest.

Import options are `timeout_ms` (default `15000`, positive) and `max_items`
(default `0`, meaning all items). An explicit positive `max_items` caps the import.
The allowed range is 0–1,000,000. Without an explicit cap, a larger feed raises an
error instead of silently truncating. Source documents over 64 MiB are rejected.

### Generate feeds from SQL rows

```sql
COPY (SELECT * FROM items ORDER BY published DESC, seq)
TO '/absolute/path/headlines.xml'
  (FORMAT 'feed.rss', title 'Headlines', link 'https://example.com',
   description 'Recent headlines', language 'en');

COPY (SELECT * FROM items ORDER BY seq)
TO '/absolute/path/headlines.atom'
  (FORMAT 'feed.atom', title 'Headlines', link 'https://example.com',
   author 'Editorial team');

COPY (SELECT title, link, published, content FROM items ORDER BY published DESC)
TO '/absolute/path/headlines.json'
  (FORMAT 'feed.jsonfeed', title 'Headlines', link 'https://example.com');

-- Inspect available COPY formats and their options.
SELECT format_name, direction, option_name, option_description
FROM vgi_copy_formats() WHERE catalog_name = 'feed';
```

All writers require `title` and an absolute HTTP(S) `link` for the feed's home
page. RSS also requires `description`. Optional `language` and `author` describe
the feed. Source columns use the same names/types as `feed_items`, and subsets
are allowed. Each item needs a title, summary, or content; Atom requires a title.
Use SQL aliases and casts to map other tables to these columns.

The writers preserve query order; use `ORDER BY` for a defined order. `seq` is
not serialized: importing a document assigns new positions starting at zero.
Missing GUIDs use the item's link or a deterministic hash of its contents.
Atom hashes opaque GUIDs into URNs and supplies its required `updated` timestamp
from `updated`, then `published`, then export time. Its feed author defaults to
the feed title. The parser may use Atom's `updated` as `published` when the latter
is absent. RSS has no equivalent of the item's `updated` field. JSON Feed uses
`content_html` for `content`, falling back to `content_text` from summary/title.
This exports the normalized item columns, not a lossless copy of feed extensions
or fields such as enclosures that `feed_items` does not expose.

Files are read and written **on the worker's filesystem**. With Docker or a
remote HTTP worker, use paths accessible to that worker (and a mounted directory
for Docker). HTTP(S) is supported for imports; exports require local file paths.
Exports buffer rows in shared execution storage, enforce a 64 MiB serialized-input
and output limit plus a one-million-item limit, and replace the destination only
after encoding succeeds. Empty exports create a valid feed with no items.

## Building

```sh
make build        # builds vgi-feed-worker + mockserver
make test-unit    # Go unit/integration tests (in-process httptest feed server)
make test-sql     # haybarn SQL end-to-end against a local mock feed server
make test         # both
```

`make test-sql` needs Haybarn 2.0's `haybarn-unittest` on `PATH` (the worker
speaks VGI protocol `vgi.v3` / 3.3, which only a compatible Haybarn 2.0 `vgi` extension
accepts):

```sh
uv tool install --prerelease allow 'haybarn-unittest>=2.0.0rc11,<2.1'
export PATH="$HOME/.local/bin:$PATH"
```

## Licensing

This worker is licensed **MIT** (see [`LICENSE`](LICENSE)).

Dependencies:

- [`gofeed`](https://github.com/mmcdole/gofeed) — universal RSS/Atom/JSON feed
  parser, **MIT License** (Copyright (c) 2016 mmcdole; verified from its
  repository `LICENSE`).
- The VGI SDK stack — [`vgi-go`](https://github.com/Query-farm/vgi-go) (the
  worker SDK, MIT here), [`vgi-rpc-go`](https://github.com/Query-farm/vgi-rpc-go),
  and [`arrow-go`](https://github.com/apache/arrow-go) (Apache-2.0).

---

## Authorship & License

Written by [Query.Farm](https://query.farm).

Copyright 2026 Query Farm LLC - https://query.farm

