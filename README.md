<p align="center">
  <img src="https://raw.githubusercontent.com/Query-farm/vgi/main/docs/vgi-logo.png" alt="Vector Gateway Interface (VGI)" width="320">
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

-- LOCATION is the path to the compiled worker binary.
ATTACH 'feed' AS feed (TYPE vgi, LOCATION '/path/to/vgi-feed-worker');

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

## Building

```sh
make build        # builds vgi-feed-worker + mockserver
make test-unit    # Go unit/integration tests (in-process httptest feed server)
make test-sql     # haybarn SQL end-to-end against a local mock feed server
make test         # both
```

`make test-sql` needs `haybarn-unittest` on `PATH`:

```sh
uv tool install haybarn-unittest
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

