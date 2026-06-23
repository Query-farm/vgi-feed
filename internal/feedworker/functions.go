// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"context"
	"time"

	"github.com/Query-farm/vgi-go/vgi"
	"github.com/Query-farm/vgi-rpc-go/vgirpc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// CatalogName is the VGI catalog name advertised by this worker.
const CatalogName = "feed"

// IMPORTANT: table-function state is gob-encoded by the SDK between NewState and
// Process (it may cross a process/worker boundary), so state structs must have
// EXPORTED, gob-encodable fields only — no arrow.RecordBatch, no interfaces, no
// channels/funcs, no unexported fields. Each function parses its feed eagerly in
// NewState, stores plain Go slices/structs, sets Done in Process, and rebuilds
// the Arrow batch there.
//
// time.Time is gob-encodable; *time.Time is too (nil round-trips), which is how
// missing dates survive as NULL timestamps.

// emitState carries the shared "have we emitted yet" flag. Exported so gob can
// round-trip it.
type emitState struct {
	Done bool
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
	emitState
	Items []Item
}

// ItemsFunction returns one row per feed item.
type ItemsFunction struct{}

var _ vgi.TypedTableFunc[itemsState] = (*ItemsFunction)(nil)

func (f *ItemsFunction) Name() string { return "feed_items" }

func (f *ItemsFunction) Metadata() vgi.FunctionMetadata {
	return vgi.FunctionMetadata{
		Description: "Parse an RSS/Atom/JSON feed (URL or raw text) into one row per item",
		Stability:   vgi.StabilityVolatile,
		Categories:  []string{"feed", "rss", "atom"},
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
	return &itemsState{Items: ItemsFrom(feed, clampMaxItems(args.MaxItems))}, nil
}

func (f *ItemsFunction) Process(_ context.Context, _ *vgi.ProcessParams, state *itemsState, out *vgirpc.OutputCollector) error {
	if state.Done {
		return out.Finish()
	}
	state.Done = true
	it := state.Items
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
	emitState
	HasInfo bool
	Info    Info
}

// InfoFunction returns one row of feed-level metadata.
type InfoFunction struct{}

var _ vgi.TypedTableFunc[infoState] = (*InfoFunction)(nil)

func (f *InfoFunction) Name() string { return "feed_info" }

func (f *InfoFunction) Metadata() vgi.FunctionMetadata {
	return vgi.FunctionMetadata{
		Description: "Return feed-level metadata (title, type, language, item count) for an RSS/Atom/JSON feed",
		Stability:   vgi.StabilityVolatile,
		Categories:  []string{"feed", "rss", "atom"},
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
	return &infoState{HasInfo: true, Info: InfoFrom(feed)}, nil
}

func (f *InfoFunction) Process(_ context.Context, _ *vgi.ProcessParams, state *infoState, out *vgirpc.OutputCollector) error {
	if state.Done {
		return out.Finish()
	}
	state.Done = true
	// A NULL input yields zero rows.
	var rows []Info
	if state.HasInfo {
		rows = []Info{state.Info}
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
