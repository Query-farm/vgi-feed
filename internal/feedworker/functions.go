// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Query-farm/vgi-go/vgi"
	"github.com/Query-farm/vgi-rpc-go/vgirpc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// CatalogName is the VGI catalog name advertised by this worker.
const CatalogName = "feed"

// SampleRSS is a tiny, self-contained RSS 2.0 document used by the catalog
// examples and executable examples. It is parsed directly with NO network
// access, so the examples execute deterministically against an attached worker
// even when no feed server is reachable. It deliberately contains NO single
// quotes so it can be embedded verbatim inside a single-quoted SQL literal.
const SampleRSS = "<rss version=\"2.0\"><channel>" +
	"<title>Query Farm Sample Feed</title>" +
	"<link>https://query.farm/</link>" +
	"<description>A small RSS feed used by vgi-feed examples</description>" +
	"<language>en-us</language>" +
	"<item><title>First Post</title><link>https://query.farm/first</link>" +
	"<category>news</category><category>release</category>" +
	"<description>The first sample entry</description></item>" +
	"<item><title>Second Post</title><link>https://query.farm/second</link>" +
	"<category>news</category>" +
	"<description>The second sample entry</description></item>" +
	"</channel></rss>"

// executableExamples is the VGI509 guaranteed-runnable, catalog-qualified
// example set. Every sql is self-contained, re-runnable against an attached
// feed worker, and parses inline RSS text so it needs no network. We omit
// expected_result deliberately — the linter only needs each query to execute
// cleanly. It is JSON-marshalled from structs so the embedded RSS literal
// (which contains double quotes from version="2.0") is correctly escaped.
var executableExamples = buildExecutableExamples()

func buildExecutableExamples() string {
	type example struct {
		Description string `json:"description"`
		SQL         string `json:"sql"`
	}
	examples := []example{
		{
			Description: "List the items parsed from an inline RSS 2.0 feed, ordered by position.",
			SQL:         "SELECT seq, title, link FROM feed.main.feed_items('" + SampleRSS + "') ORDER BY seq",
		},
		{
			Description: "Count the items in an inline feed.",
			SQL:         "SELECT count(*) AS items FROM feed.main.feed_items('" + SampleRSS + "')",
		},
		{
			Description: "Expand each item's categories into one row per (item, category).",
			SQL:         "SELECT title, UNNEST(categories) AS category FROM feed.main.feed_items('" + SampleRSS + "') ORDER BY seq",
		},
		{
			Description: "Read feed-level metadata: title, detected format, language, and item count.",
			SQL:         "SELECT title, feed_type, language, item_count FROM feed.main.feed_info('" + SampleRSS + "')",
		},
	}
	b, err := json.Marshal(examples)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// IMPORTANT: table-function state is gob-encoded by the SDK between NewState and
// Process (it may cross a process/worker boundary), so state structs must have
// EXPORTED, gob-encodable fields only — no arrow.RecordBatch, no interfaces, no
// channels/funcs, no unexported fields. Each function parses its feed eagerly in
// NewState, stores plain Go slices/structs in an embedded Cursor, and rebuilds
// the Arrow batch in Process.
//
// time.Time is gob-encodable; *time.Time is too (nil round-trips), which is how
// missing dates survive as NULL timestamps.
//
// WHY AN EXPLICIT CURSOR, NOT A bool Done (the HTTP-continuation fix):
//
// Over the stateless HTTP transport the worker keeps NO live state between
// Process ticks — the framework round-trips the producer state through an opaque
// continuation token: after each tick it gob-encodes the LIVE user state, the
// client returns the token, and the worker resumes by gob-decoding it. The HTTP
// server emits at most one data batch per response, so a producer with more to
// emit is always resumed mid-stream from its token. A bare `Done bool` flipped
// *after* the single Emit observes the pre-Emit snapshot on resume, re-emits the
// same rows forever, and pins the worker in an infinite loop (subprocess/unix
// hold live state in memory, so they never hit it). feed_items emits MANY rows,
// so this is mandatory, not cosmetic. The fix: each state embeds Cursor[T]
// carrying the fetched Rows plus the Offset of the next unemitted row; Process
// emits a bounded slice from Offset, advances Offset BEFORE yielding, and
// Finish()es once Offset >= len(Rows). The framework snapshots Offset into the
// token, so HTTP resumes correctly and terminates.

// rowsPerTick bounds how many rows each Process tick emits. Emitting a bounded
// slice and advancing the cursor is what makes the offset observable across the
// HTTP continuation boundary (and scales to large feeds).
const rowsPerTick = 256

// Cursor is the shared streaming cursor embedded by every table-function state:
// the eagerly fetched rows plus the offset of the next unemitted row. Both
// fields are exported so gob round-trips them through the HTTP continuation
// token. The TYPE is exported (Cursor, not cursor) because the SDK counts a
// state struct's exported FIELDS at registration to verify it is gob-encodable.
type Cursor[T any] struct {
	Rows   []T
	Offset int
}

// nextSlice returns the next bounded slice of rows to emit and advances the
// cursor past them. It reports done=true once all rows have been consumed, at
// which point Process should call out.Finish().
func (c *Cursor[T]) nextSlice() (slice []T, done bool) {
	if c.Offset >= len(c.Rows) {
		return nil, true
	}
	end := c.Offset + rowsPerTick
	if end > len(c.Rows) {
		end = len(c.Rows)
	}
	slice = c.Rows[c.Offset:end]
	c.Offset = end
	return slice, false
}

// tsType is plain TIMESTAMP (microsecond, no timezone) so DuckDB sees TIMESTAMP
// rather than TIMESTAMP WITH TIME ZONE.
var tsType = &arrow.TimestampType{Unit: arrow.Microsecond}

// buildTimestampArray builds a TIMESTAMP column; a nil *time.Time becomes NULL.
func buildTimestampArray(n int64, fn func(i int64) *time.Time) arrow.Array {
	b := array.NewTimestampBuilder(memory.DefaultAllocator, tsType)
	defer b.Release()
	b.Reserve(int(n))
	for i := int64(0); i < n; i++ {
		t := fn(i)
		if t == nil {
			b.AppendNull()
			continue
		}
		b.Append(arrow.Timestamp(t.UTC().UnixMicro()))
	}
	return b.NewArray()
}

// buildStringListArray builds a VARCHAR[] column. A nil slice yields an empty
// (non-NULL) list.
func buildStringListArray(n int64, fn func(i int64) []string) arrow.Array {
	b := array.NewListBuilder(memory.DefaultAllocator, arrow.BinaryTypes.String)
	defer b.Release()
	vb := b.ValueBuilder().(*array.StringBuilder)
	for i := int64(0); i < n; i++ {
		b.Append(true)
		for _, s := range fn(i) {
			vb.Append(s)
		}
	}
	return b.NewArray()
}

// isNullArg reports whether positional argument pos is present and NULL.
func isNullArg(args *vgi.Arguments, pos int) bool {
	if args == nil {
		return true
	}
	col, err := args.GetColumn(pos)
	if err != nil {
		return false
	}
	return col.Len() == 0 || col.IsNull(0)
}

// optsFrom builds FetchOptions from the bound common arguments.
func optsFrom(input string, timeoutMS, maxItems int64) FetchOptions {
	return FetchOptions{
		Input:    input,
		Timeout:  time.Duration(timeoutMS) * time.Millisecond,
		MaxItems: clampMaxItems(maxItems),
	}
}

// ---------------------------------------------------------------------------
// feed_items(input) ->
//   (seq BIGINT, guid, title, link, published TIMESTAMP, updated TIMESTAMP,
//    author, categories VARCHAR[], summary, content)
// ---------------------------------------------------------------------------

var itemsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "seq", Type: arrow.PrimitiveTypes.Int64},
	{Name: "guid", Type: arrow.BinaryTypes.String},
	{Name: "title", Type: arrow.BinaryTypes.String},
	{Name: "link", Type: arrow.BinaryTypes.String},
	{Name: "published", Type: tsType, Nullable: true},
	{Name: "updated", Type: tsType, Nullable: true},
	{Name: "author", Type: arrow.BinaryTypes.String},
	{Name: "categories", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	{Name: "summary", Type: arrow.BinaryTypes.String},
	{Name: "content", Type: arrow.BinaryTypes.String},
}, nil)

type itemsArgs struct {
	Input     string `vgi:"pos=0,doc=Feed URL (http/https) or raw feed text (RSS/Atom XML or JSON Feed)"`
	TimeoutMS int64  `vgi:"default=15000,name=timeout_ms,doc=HTTP fetch timeout in milliseconds"`
	MaxItems  int64  `vgi:"default=1000,name=max_items,doc=Maximum number of items to return"`
}

type itemsState struct {
	Cursor[Item]
}

// ItemsFunction returns one row per feed item.
type ItemsFunction struct{}

var _ vgi.TypedTableFunc[itemsState] = (*ItemsFunction)(nil)

func (f *ItemsFunction) Name() string { return "feed_items" }

func (f *ItemsFunction) Metadata() vgi.FunctionMetadata {
	tags := objectTags(
		"Feed Items To Rows",
		"Parse an RSS 2.0, Atom, or JSON Feed into one row per entry. The input is "+
			"either an http(s) URL (fetched over HTTP) or a raw feed document supplied "+
			"inline; the format is auto-detected. Each row carries the item's sequence, "+
			"GUID, title, link, publish and update timestamps, author, categories array, "+
			"summary, and full content. Use it to ingest news/blog/podcast feeds and turn "+
			"syndicated entries into a queryable table.",
		"Parse an RSS/Atom/JSON feed (URL or raw text) into **one row per item** — "+
			"`seq`, `guid`, `title`, `link`, `published`, `updated`, `author`, "+
			"`categories`, `summary`, `content`.",
		keywordsJSON("feed", "rss", "atom", "json feed", "syndication", "items",
			"entries", "parse feed", "feed items", "news", "blog", "podcast",
			"rss reader", "feed reader"),
	)
	tags["vgi.result_columns_md"] = "| Column | Type | Description |\n" +
		"| --- | --- | --- |\n" +
		"| `seq` | BIGINT | 0-based position of the item within the feed |\n" +
		"| `guid` | VARCHAR | Item GUID / id (empty when the feed omits one) |\n" +
		"| `title` | VARCHAR | Item title |\n" +
		"| `link` | VARCHAR | Item link / permalink URL |\n" +
		"| `published` | TIMESTAMP | Publish time (NULL when absent or unparseable) |\n" +
		"| `updated` | TIMESTAMP | Last-updated time (NULL when absent or unparseable) |\n" +
		"| `author` | VARCHAR | Author display name or email (empty when absent) |\n" +
		"| `categories` | VARCHAR[] | Item categories / tags (empty list when none) |\n" +
		"| `summary` | VARCHAR | Item summary / description |\n" +
		"| `content` | VARCHAR | Full item content (empty when not provided) |"
	tags["vgi.executable_examples"] = executableExamples
	return vgi.FunctionMetadata{
		Description: "Parse an RSS/Atom/JSON feed (URL or raw text) into one row per item",
		Stability:   vgi.StabilityVolatile,
		Categories:  []string{"feed", "rss", "atom"},
		Tags:        tags,
		Examples: []vgi.CatalogExample{
			{
				SQL:         "SELECT seq, title, link FROM feed.main.feed_items('" + SampleRSS + "') ORDER BY seq;",
				Description: "List items parsed directly from an inline RSS 2.0 document (no network access).",
			},
			{
				SQL:         "SELECT title, UNNEST(categories) AS category FROM feed.main.feed_items('" + SampleRSS + "', max_items := 20);",
				Description: "Expand each item's categories into one row per (item, category), capped at 20 items.",
			},
		},
	}
}

func (f *ItemsFunction) ArgumentSpecs() []vgi.ArgSpec { return vgi.DeriveArgSpecs(itemsArgs{}) }

func (f *ItemsFunction) OnBind(_ *vgi.BindParams) (*vgi.BindResponse, error) {
	return vgi.BindSchema(itemsSchema)
}

func (f *ItemsFunction) NewState(params *vgi.ProcessParams) (*itemsState, error) {
	var args itemsArgs
	if err := vgi.BindArgs(params.Args, &args); err != nil {
		return nil, err
	}
	if isNullArg(params.Args, 0) {
		return &itemsState{}, nil
	}
	feed, err := ParseFeed(context.Background(), optsFrom(args.Input, args.TimeoutMS, args.MaxItems))
	if err != nil {
		return nil, err
	}
	return &itemsState{Cursor[Item]{Rows: ItemsFrom(feed, clampMaxItems(args.MaxItems))}}, nil
}

func (f *ItemsFunction) Process(_ context.Context, _ *vgi.ProcessParams, state *itemsState, out *vgirpc.OutputCollector) error {
	it, done := state.nextSlice()
	if done {
		return out.Finish()
	}
	n := int64(len(it))
	batch := array.NewRecordBatch(itemsSchema, []arrow.Array{
		vgi.BuildInt64Array(n, func(i int64) int64 { return it[i].Seq }),
		vgi.BuildStringArray(n, func(i int64) string { return it[i].GUID }),
		vgi.BuildStringArray(n, func(i int64) string { return it[i].Title }),
		vgi.BuildStringArray(n, func(i int64) string { return it[i].Link }),
		buildTimestampArray(n, func(i int64) *time.Time { return it[i].Published }),
		buildTimestampArray(n, func(i int64) *time.Time { return it[i].Updated }),
		vgi.BuildStringArray(n, func(i int64) string { return it[i].Author }),
		buildStringListArray(n, func(i int64) []string { return it[i].Categories }),
		vgi.BuildStringArray(n, func(i int64) string { return it[i].Summary }),
		vgi.BuildStringArray(n, func(i int64) string { return it[i].Content }),
	}, n)
	defer batch.Release()
	return out.Emit(batch)
}

// NewItemsFunction builds the registerable table function.
func NewItemsFunction() vgi.TableFunction {
	return vgi.AsTableFunction[itemsState](&ItemsFunction{})
}

// ---------------------------------------------------------------------------
// feed_info(input) ->
//   (title, description, link, feed_type, language, updated TIMESTAMP,
//    item_count INT)
// ---------------------------------------------------------------------------

var infoSchema = arrow.NewSchema([]arrow.Field{
	{Name: "title", Type: arrow.BinaryTypes.String},
	{Name: "description", Type: arrow.BinaryTypes.String},
	{Name: "link", Type: arrow.BinaryTypes.String},
	{Name: "feed_type", Type: arrow.BinaryTypes.String},
	{Name: "language", Type: arrow.BinaryTypes.String},
	{Name: "updated", Type: tsType, Nullable: true},
	{Name: "item_count", Type: arrow.PrimitiveTypes.Int32},
}, nil)

type infoArgs struct {
	Input     string `vgi:"pos=0,doc=Feed URL (http/https) or raw feed text (RSS/Atom XML or JSON Feed)"`
	TimeoutMS int64  `vgi:"default=15000,name=timeout_ms,doc=HTTP fetch timeout in milliseconds"`
}

type infoState struct {
	Cursor[Info]
}

// InfoFunction returns one row of feed-level metadata.
type InfoFunction struct{}

var _ vgi.TypedTableFunc[infoState] = (*InfoFunction)(nil)

func (f *InfoFunction) Name() string { return "feed_info" }

func (f *InfoFunction) Metadata() vgi.FunctionMetadata {
	tags := objectTags(
		"Feed Metadata Summary",
		"Return one row of feed-level metadata for an RSS 2.0, Atom, or JSON Feed: "+
			"the feed title, description/subtitle, home link, detected format "+
			"(rss/atom/json), language, last-updated timestamp, and item count. The "+
			"input is either an http(s) URL (fetched over HTTP) or a raw feed document "+
			"supplied inline; the format is auto-detected. Use it to inspect or classify "+
			"a feed without expanding every entry.",
		"Return **feed-level metadata** (title, type, language, item count) for an "+
			"RSS/Atom/JSON feed as a single row.",
		keywordsJSON("feed", "rss", "atom", "json feed", "syndication", "feed info",
			"feed metadata", "feed type", "feed title", "language", "item count",
			"detect feed format"),
	)
	tags["vgi.result_columns_md"] = "| Column | Type | Description |\n" +
		"| --- | --- | --- |\n" +
		"| `title` | VARCHAR | Feed title |\n" +
		"| `description` | VARCHAR | Feed description / subtitle |\n" +
		"| `link` | VARCHAR | Feed home / site URL |\n" +
		"| `feed_type` | VARCHAR | Detected format: `rss`, `atom`, or `json` |\n" +
		"| `language` | VARCHAR | Feed language code (empty when absent) |\n" +
		"| `updated` | TIMESTAMP | Feed last-updated time (NULL when absent or unparseable) |\n" +
		"| `item_count` | INTEGER | Number of items present in the feed |"
	return vgi.FunctionMetadata{
		Description: "Return feed-level metadata (title, type, language, item count) for an RSS/Atom/JSON feed",
		Stability:   vgi.StabilityVolatile,
		Categories:  []string{"feed", "rss", "atom"},
		Tags:        tags,
		Examples: []vgi.CatalogExample{
			{
				SQL:         "SELECT title, feed_type, language, item_count FROM feed.main.feed_info('" + SampleRSS + "');",
				Description: "Inspect an inline feed's title, detected format, language, and item count (no network access).",
			},
			{
				SQL:         "SELECT feed_type, item_count FROM feed.main.feed_info('<rss version=\"2.0\"><channel><title>Example</title><item><title>Hi</title></item></channel></rss>');",
				Description: "Parse feed-level metadata directly from a raw inline RSS document (no network access).",
			},
		},
	}
}

func (f *InfoFunction) ArgumentSpecs() []vgi.ArgSpec { return vgi.DeriveArgSpecs(infoArgs{}) }

func (f *InfoFunction) OnBind(_ *vgi.BindParams) (*vgi.BindResponse, error) {
	return vgi.BindSchema(infoSchema)
}

func (f *InfoFunction) NewState(params *vgi.ProcessParams) (*infoState, error) {
	var args infoArgs
	if err := vgi.BindArgs(params.Args, &args); err != nil {
		return nil, err
	}
	if isNullArg(params.Args, 0) {
		return &infoState{}, nil
	}
	feed, err := ParseFeed(context.Background(), optsFrom(args.Input, args.TimeoutMS, 0))
	if err != nil {
		return nil, err
	}
	return &infoState{Cursor[Info]{Rows: []Info{InfoFrom(feed)}}}, nil
}

func (f *InfoFunction) Process(_ context.Context, _ *vgi.ProcessParams, state *infoState, out *vgirpc.OutputCollector) error {
	rows, done := state.nextSlice()
	if done {
		return out.Finish()
	}
	n := int64(len(rows))
	batch := array.NewRecordBatch(infoSchema, []arrow.Array{
		vgi.BuildStringArray(n, func(i int64) string { return rows[i].Title }),
		vgi.BuildStringArray(n, func(i int64) string { return rows[i].Description }),
		vgi.BuildStringArray(n, func(i int64) string { return rows[i].Link }),
		vgi.BuildStringArray(n, func(i int64) string { return rows[i].FeedType }),
		vgi.BuildStringArray(n, func(i int64) string { return rows[i].Language }),
		buildTimestampArray(n, func(i int64) *time.Time { return rows[i].Updated }),
		vgi.BuildInt32Array(n, func(i int64) int32 { return rows[i].ItemCount }),
	}, n)
	defer batch.Release()
	return out.Emit(batch)
}

// NewInfoFunction builds the registerable table function.
func NewInfoFunction() vgi.TableFunction {
	return vgi.AsTableFunction[infoState](&InfoFunction{})
}

// Register registers all feed table functions on the worker.
func Register(w *vgi.Worker) {
	w.RegisterTable(NewItemsFunction())
	w.RegisterTable(NewInfoFunction())
}
