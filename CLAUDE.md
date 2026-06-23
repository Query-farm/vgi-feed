# CLAUDE.md — vgi-feed

Contributor/agent notes. User-facing docs live in `README.md`; this is the
"how it's built and where the sharp edges are" companion. It follows the same
Go SDK conventions as the reference Go workers `vgi-grpc` / `vgi-scim`.

## What this is

A [VGI](https://query.farm) worker (Go) that fetches and parses **RSS / Atom /
JSON** feeds into DuckDB rows. Built on the
[`vgi-go`](https://github.com/Query-farm/vgi-go) SDK over stdio. Catalog name:
`feed`. Feed parsing/auto-detection is delegated to
[`gofeed`](https://github.com/mmcdole/gofeed) (MIT).

## Layout

```
cmd/vgi-feed-worker/main.go   stdio entry point; assembles the worker + catalog
cmd/mockserver/main.go        standalone mock feed server for the SQL E2E
internal/feedworker/
  client.go                   input resolution (URL-vs-raw) + bounded HTTP fetch + ParseFeed
  feed.go                     Item/Info structs + flatten (ItemsFrom / InfoFrom)
  functions.go                the two VGI table functions + Register(w)
  *_test.go                   httptest unit/integration tests
internal/mockfeed/server.go   in-memory feed handler (shared by tests + mockserver)
test/sql/*.test               haybarn-unittest sqllogictest — authoritative E2E
Makefile                      build / test-unit / test-sql / lint
```

To add a function: implement the parse/flatten in `feed.go` (or `client.go`),
wrap it as a `vgi.TypedTableFunc` in `functions.go`, register it in `Register(w)`.

## The Go SDK worker pattern (read first)

A worker is `main()` assembling a `*vgi.Worker` and registering functions:

```go
w := vgi.NewWorker(
    vgi.WithCatalogName("feed"),
    vgi.WithCatalogComment("..."),
)
feedworker.Register(w)   // w.RegisterTable(NewXxxFunction()) for each fn
w.RunStdio()             // or w.RunHttp("127.0.0.1:0") behind a --http flag
```

A **table function** is a `vgi.TypedTableFunc[S]` (generic over a *state* type)
wrapped with `vgi.AsTableFunction[S](impl)`. Required methods:

- `Name() string`
- `Metadata() vgi.FunctionMetadata`
- `ArgumentSpecs() []vgi.ArgSpec` — `vgi.DeriveArgSpecs(argsStruct{})`.
- `OnBind(*vgi.BindParams) (*vgi.BindResponse, error)` — `vgi.BindSchema(schema)`.
- `NewState(*vgi.ProcessParams) (*S, error)` — `vgi.BindArgs(params.Args, &args)`;
  **do the network I/O / parsing here**.
- `Process(ctx, *vgi.ProcessParams, *S, *vgirpc.OutputCollector) error` —
  `out.Emit(batch)`, then `out.Finish()`.

**Argument struct tags** (`vgi.DeriveArgSpecs` / `vgi.BindArgs`):

```go
type itemsArgs struct {
    Input     string `vgi:"pos=0,doc=Feed URL or raw feed text"`
    TimeoutMS int64  `vgi:"default=15000,name=timeout_ms,doc=HTTP timeout (ms)"`
    MaxItems  int64  `vgi:"default=1000,name=max_items,doc=Max items"`
}
```

- `pos=N` → positional. `input` is positional 0.
- A field **without** `pos` but **with** `default=` becomes a **named optional**
  argument (DuckDB `name := value`). `name=` sets the wire name.
- Go type → Arrow type is inferred (`string`→varchar, `int64`→bigint,
  `int32`→int, `bool`→bool).

Build arrays in `Process` with `vgi.BuildStringArray`, `vgi.BuildInt64Array`,
`vgi.BuildInt32Array`. There are **no SDK helpers for TIMESTAMP or LIST**, so
`functions.go` has local builders: `buildTimestampArray` (nil `*time.Time` →
NULL) and `buildStringListArray` (nil slice → empty list). Then
`array.NewRecordBatch(schema, []arrow.Array{...}, n)`.

## Sharp edges (learned the hard way)

1. **Table-function state is `gob`-encoded by the SDK** between `NewState` and
   `Process` (it may cross a process/worker boundary). So **`S` must have
   exported, gob-encodable fields only** — no `arrow.Record`, no interfaces, no
   channels/funcs, no unexported fields. The SDK **panics at registration**
   otherwise (`TestRegisterDoesNotPanic` guards this). The pattern every function
   uses: parse the feed eagerly in `NewState`, store the rows as plain exported
   Go slices/structs (`Items []Item`, `Info Info` + `HasInfo bool`) plus a
   `Done bool`, and **rebuild the Arrow batch in `Process`**. The `Item`/`Info`
   structs in `feed.go` are deliberately all-exported; `*time.Time` is
   gob-encodable and nil round-trips, which is how missing dates survive as NULL.

2. **URL vs. raw-text input.** `input` is classified by its first non-space byte
   (after stripping a UTF-8 BOM): `<`/`{`/`[` → a raw feed document parsed
   directly with no network access; `http://`/`https://` → fetched then parsed;
   anything else → a clear error. See `looksLikeRawFeed` / `looksLikeURL` /
   `ParseFeed` in `client.go`. gofeed's `ParseString` auto-detects RSS/Atom/JSON,
   so a single code path handles all three formats.

3. **Missing dates → NULL TIMESTAMP.** gofeed exposes `PublishedParsed` /
   `UpdatedParsed` as `*time.Time` (nil when absent/unparseable); we carry the
   pointer straight through and `buildTimestampArray` emits NULL for nil. The
   column type is plain `TIMESTAMP` (`&arrow.TimestampType{Unit: Microsecond}`,
   **no** timezone — `FixedWidthTypes.Timestamp_us` carries `UTC` and would map
   to `TIMESTAMP WITH TIME ZONE`).

4. **Bounded + never hangs.** The HTTP fetch has a `timeout_ms` deadline, reads
   at most 64 MiB, and non-2xx → a clean error with a short body excerpt.
   `max_items` (and a hard `hardMaxItems` ceiling) caps materialised rows.
   Malformed feeds → a `gofeed` parse error wrapped as a clean DuckDB error. No
   panics on any error path.

5. **`haybarn-unittest` silently SKIPS `require vgi`.** Under haybarn the
   extension is not autoloaded for `require`, so a `.test` using `require vgi`
   is skipped (looks green but ran nothing). Use an explicit `statement ok` /
   `LOAD vgi;` instead — every `.test` here does.

6. **Source files must not contain a literal BOM** mid-file (Go rejects it). The
   BOM-stripping code uses the `"﻿"` escape, not a literal BOM character.

## Mock-feed E2E (how `make test-sql` works)

Mirrors `vgi-scim`'s start/stop pattern:

1. `make build` compiles `vgi-feed-worker` **and** `mockserver`.
2. `mockserver --addr 127.0.0.1:0` binds a free port and prints `PORT:<n>`; the
   Makefile captures it. It serves the same in-memory fixtures as the unit tests
   via `internal/mockfeed.NewHandler`: a known RSS 2.0 feed at `/rss`, an Atom
   feed at `/atom`, a JSON Feed at `/json`, and a `/malformed` endpoint.
3. The Makefile exports `VGI_FEED_WORKER` (the worker binary, used as the ATTACH
   `LOCATION`) and `VGI_FEED_TEST_URL` (`http://127.0.0.1:<n>`, the base URL; the
   `.test` files append `/rss`, `/atom`, `/json`, `/malformed`), both read by the
   `.test` files.
4. `haybarn-unittest --test-dir . "test/sql/*"` runs the suite; the haybarn exit
   status is captured and returned, and a shell `trap` kills the mock server.

`cmd/mockserver` and the Go tests share `internal/mockfeed.NewHandler`.

## Test inventory

- **Go (`make test-unit`)** — `internal/feedworker/feed_test.go` +
  `functions_test.go` spin up the feed server in-process via `httptest.Server`
  and assert: RSS/Atom/JSON parsing (titles, links, parsed dates, categories
  array, author, content), `feed_type` detection, raw-text input parsed directly
  (incl. a BOM prefix), `max_items` cap, missing date → nil/NULL, and errors
  (404, 500, malformed, unreachable host, empty input, non-URL-non-feed input),
  plus the VGI `NewState` data paths and NULL→no-rows.
- **SQL (`make test-sql`)** — `test/sql/feed_items.test` (count, ordered
  title+link, date EXTRACT, `UNNEST(categories)`, atom author/content, json
  titles, `max_items`, raw-text input, malformed `statement error`) and
  `test/sql/feed_info.test` (`feed_type` = rss/atom/json, item_count, title +
  language, raw-text input, malformed `statement error`).

## Conventions

- Source files start with `// Copyright 2026 Query Farm LLC - https://query.farm`.
- `gofmt`, `go vet`, and `go test ./...` must be clean before committing.
