// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Query-farm/vgi-go/vgi"
	"github.com/Query-farm/vgi-rpc-go/vgirpc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

type copyReadArgs struct {
	TimeoutMS int64 `vgi:"default=15000,name=timeout_ms,doc=HTTP timeout in milliseconds"`
	MaxItems  int64 `vgi:"default=0,name=max_items,doc=Maximum items; zero imports all items up to the safety limit"`
}

type copyWriteArgs struct {
	Title       string `vgi:"doc=Required feed title"`
	Link        string `vgi:"doc=Required absolute HTTP or HTTPS home-page URL"`
	Description string `vgi:"default=,doc=Feed description; required for RSS"`
	Language    string `vgi:"default=,doc=Feed language code"`
	Author      string `vgi:"default=,doc=Feed author name; defaults to the feed title for Atom"`
}

type feedCopyReader struct{}
type feedCopyWriter struct{ format string }

var (
	_            vgi.CopyFromFunction = (*feedCopyReader)(nil)
	_            vgi.CopyToFunction   = (*feedCopyWriter)(nil)
	copyShardKey                      = []byte("feed_copy_items")
)

func (f *feedCopyReader) Name() string           { return "read_feed_copy" }
func (f *feedCopyReader) CopyFromFormat() string { return "auto" }
func (f *feedCopyReader) ArgumentSpecs() []vgi.ArgSpec {
	return vgi.DeriveArgSpecs(copyReadArgs{})
}
func (f *feedCopyReader) Metadata() vgi.FunctionMetadata {
	return copyMetadata("auto", false)
}
func (f *feedCopyReader) CopyFromComment() string {
	return "Import feed items from an HTTP(S) URL or a worker-local file; columns map by name."
}

func (f *feedCopyWriter) Name() string         { return "write_" + f.format + "_copy" }
func (f *feedCopyWriter) CopyToFormat() string { return f.format }
func (f *feedCopyWriter) ArgumentSpecs() []vgi.ArgSpec {
	return vgi.DeriveArgSpecs(copyWriteArgs{})
}
func (f *feedCopyWriter) Metadata() vgi.FunctionMetadata {
	return copyMetadata(f.format, true)
}
func (f *feedCopyWriter) CopyToComment() string {
	return "Export feed items to a worker-local file, preserving query order."
}

func copyMetadata(format string, write bool) vgi.FunctionMetadata {
	description := "Import RSS, Atom, or JSON Feed items from a URL or file using COPY FROM"
	doc := "Import " + format + " feed items using COPY FROM an HTTP(S) URL or worker-local file. " +
		"Target columns must be a subset of feed_items columns with matching names and types. " +
		"timeout_ms defaults to 15000; max_items defaults to 0 (all items). " +
		"Documents over 64 MiB or over one million items are rejected."
	if write {
		description = "Export SQL rows to a " + format + " feed document using COPY TO"
		doc = "Export " + format + " using COPY TO a worker-local file. " +
			"Source columns are matched by feed_items column names and types. " +
			"title and link options are required; RSS also requires description. " +
			"Optional language and author describe the feed. Use ORDER BY to choose item order. " +
			"Missing item IDs are generated deterministically; Atom dates default to the export time."
	}
	md := "## Import feed items\n\n" + doc + "\n\n" +
		"### Column mapping\n\n" +
		"The destination table chooses the output schema. Use any subset of the item columns, " +
		"in any order, with matching types. Dates are plain `TIMESTAMP`; categories are `VARCHAR[]`. " +
		"Unknown columns and incompatible types are errors.\n\n" +
		"### Example\n\n```sql\n" +
		"CREATE TABLE headlines (title VARCHAR, link VARCHAR);\n" +
		"COPY headlines FROM 'https://hnrss.org/frontpage' (FORMAT 'feed.auto', timeout_ms 5000);\n```\n\n" +
		"Local file paths refer to the worker's filesystem, including for remote workers."
	if write {
		md = "## Generate a " + format + " feed\n\n" + doc + "\n\n" +
			"### Input and output\n\n" +
			"Alias and cast source columns to the item schema; subsets are supported. " +
			"This is a sink with no result columns. DuckDB reports the COPY row count. " +
			"Every item needs title, summary, or content; Atom requires a title. " +
			"Empty input produces a valid empty feed.\n\n" +
			"### Example\n\n```sql\n" +
			"COPY (SELECT 'First post' AS title, 'https://example.com/1' AS link)\n" +
			"TO '/tmp/posts.feed' (FORMAT 'feed." + format + "', title 'Posts', " +
			"link 'https://example.com', description 'Recent posts');\n```\n\n" +
			"### Limits\n\n" +
			"Only worker-local destinations are supported. Buffered input and encoded output " +
			"are limited to 64 MiB, and exports to one million items. " +
			"The destination is replaced after successful encoding."
	}
	tags := objectTags("COPY "+format+" feed", doc, md,
		keywordsJSON("feed", "copy", "import", "export", format))
	tags["vgi.category"] = "Feed Items"
	if write {
		// The COPY TO adapter binds an empty output schema: it is a sink.
		tags["vgi.result_columns_schema"] = "[]"
	} else {
		tags["vgi.result_dynamic_columns_md"] = "The COPY destination selects any subset and order of these columns.\n\n" +
			"| Name | Type | Description |\n| --- | --- | --- |\n" +
			"| seq | BIGINT | Zero-based position in the feed. |\n" +
			"| guid | VARCHAR | Item ID, or empty when absent. |\n" +
			"| title | VARCHAR | Item title. |\n| link | VARCHAR | Item URL. |\n" +
			"| published | TIMESTAMP | Publish time in UTC; NULL when unavailable. |\n" +
			"| updated | TIMESTAMP | Update time in UTC; NULL when unavailable. |\n" +
			"| author | VARCHAR | Author name or email. |\n" +
			"| categories | VARCHAR[] | Category names; empty list when absent. |\n" +
			"| summary | VARCHAR | Item summary. |\n| content | VARCHAR | Full item content. |"
	}
	return vgi.FunctionMetadata{
		Description: description, Stability: vgi.StabilityVolatile,
		Categories: []string{"feed", "copy"}, Tags: tags, SinkOrderDependent: write,
	}
}

// Read emits one batch: the SDK's COPY reader adapter finishes after this call.
func (f *feedCopyReader) Read(ctx context.Context, params *vgi.ProcessParams, path string, schema *arrow.Schema, out *vgirpc.OutputCollector) error {
	items, err := f.readItems(ctx, params.Args, path, schema)
	if err != nil {
		return err
	}
	batch := itemsRecord(items, schema)
	// Emit takes ownership, including when it rewraps the batch's schema.
	return out.Emit(batch)
}

func (f *feedCopyReader) readItems(ctx context.Context, args *vgi.Arguments, path string, schema *arrow.Schema) ([]Item, error) {
	if err := validateItemSchema(schema); err != nil {
		return nil, err
	}
	var opts copyReadArgs
	if err := vgi.BindArgs(args, &opts); err != nil {
		return nil, err
	}
	if opts.TimeoutMS <= 0 || opts.TimeoutMS > math.MaxInt64/int64(time.Millisecond) {
		return nil, fmt.Errorf("feed COPY: timeout_ms must be a positive, representable duration")
	}
	if opts.MaxItems < 0 || opts.MaxItems > hardMaxItems {
		return nil, fmt.Errorf("feed COPY: max_items must be between 0 and %d", hardMaxItems)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input := strings.TrimSpace(path)
	if !looksLikeURL(input) {
		if err := localFeedPath(input); err != nil {
			return nil, err
		}
		file, err := os.Open(input)
		if err != nil {
			return nil, fmt.Errorf("feed COPY: open source: %w", err)
		}
		defer func() { _ = file.Close() }()
		body, err := readFeedBytes(file)
		if err != nil {
			return nil, fmt.Errorf("feed COPY: read source: %w", err)
		}
		input = string(body)
		if !looksLikeRawFeed(input) {
			return nil, fmt.Errorf("feed COPY: source file does not contain a feed document")
		}
	}
	feed, err := ParseFeed(ctx, FetchOptions{Input: input, Timeout: time.Duration(opts.TimeoutMS) * time.Millisecond})
	if err != nil {
		return nil, err
	}
	if len(feed.Items) > hardMaxItems && opts.MaxItems == 0 {
		return nil, fmt.Errorf("feed COPY: feed exceeds the %d item limit", hardMaxItems)
	}
	return ItemsFrom(feed, int(opts.MaxItems)), ctx.Err()
}

// validateItemSchema checks COPY column names and types against feed_items.
func validateItemSchema(schema *arrow.Schema) error {
	if schema == nil || schema.NumFields() == 0 {
		return fmt.Errorf("feed COPY: at least one item column is required")
	}
	seen := make(map[string]bool)
	for _, field := range schema.Fields() {
		name := strings.ToLower(field.Name)
		matches := itemsSchema.FieldIndices(name)
		if len(matches) != 1 {
			return fmt.Errorf("feed COPY: unknown item column %q; use feed_items column names", field.Name)
		}
		if seen[name] {
			return fmt.Errorf("feed COPY: duplicate item column %q", field.Name)
		}
		seen[name] = true
		want := itemsSchema.Field(matches[0]).Type
		if !arrow.TypeEqual(field.Type, want) {
			return fmt.Errorf("feed COPY: column %q has type %s; expected %s", field.Name, field.Type, want)
		}
	}
	return nil
}

// itemsRecord builds a batch in the validated COPY target's column order.
func itemsRecord(items []Item, schema *arrow.Schema) arrow.RecordBatch {
	n := int64(len(items))
	cols := make([]arrow.Array, schema.NumFields())
	for c, field := range schema.Fields() {
		switch strings.ToLower(field.Name) {
		case "seq":
			cols[c] = vgi.BuildInt64Array(n, func(i int64) int64 { return items[i].Seq })
		case "published", "updated":
			cols[c] = buildTimestampArray(n, func(i int64) *time.Time {
				if strings.EqualFold(field.Name, "published") {
					return items[i].Published
				}
				return items[i].Updated
			})
		case "categories":
			cols[c] = buildStringListArray(n, func(i int64) []string { return items[i].Categories })
		default:
			cols[c] = vgi.BuildStringArray(n, func(i int64) string {
				switch strings.ToLower(field.Name) {
				case "guid":
					return items[i].GUID
				case "title":
					return items[i].Title
				case "link":
					return items[i].Link
				case "author":
					return items[i].Author
				case "summary":
					return items[i].Summary
				default:
					return items[i].Content
				}
			})
		}
	}
	batch := array.NewRecordBatch(schema, cols, n)
	for _, col := range cols {
		col.Release()
	}
	return batch
}

func itemsFromRecord(batch arrow.RecordBatch) ([]Item, error) {
	if err := validateItemSchema(batch.Schema()); err != nil {
		return nil, err
	}
	items := make([]Item, int(batch.NumRows()))
	for c, field := range batch.Schema().Fields() {
		col := batch.Column(c)
		for r := range items {
			if col.IsNull(r) {
				continue
			}
			item := &items[r]
			switch strings.ToLower(field.Name) {
			case "seq":
				item.Seq = col.(*array.Int64).Value(r)
			case "guid":
				item.GUID = strings.Clone(col.(*array.String).Value(r))
			case "title":
				item.Title = strings.Clone(col.(*array.String).Value(r))
			case "link":
				item.Link = strings.Clone(col.(*array.String).Value(r))
			case "author":
				item.Author = strings.Clone(col.(*array.String).Value(r))
			case "summary":
				item.Summary = strings.Clone(col.(*array.String).Value(r))
			case "content":
				item.Content = strings.Clone(col.(*array.String).Value(r))
			case "published", "updated":
				t := time.UnixMicro(int64(col.(*array.Timestamp).Value(r))).UTC()
				if strings.EqualFold(field.Name, "published") {
					item.Published = &t
				} else {
					item.Updated = &t
				}
			case "categories":
				list := col.(*array.List)
				start, end := list.ValueOffsets(r)
				values := list.ListValues().(*array.String)
				for j := start; j < end; j++ {
					if !values.IsNull(int(j)) {
						item.Categories = append(item.Categories, strings.Clone(values.Value(int(j))))
					}
				}
			}
		}
	}
	return items, nil
}

func localFeedPath(path string) error {
	if path == "" || strings.Contains(path, "://") {
		return fmt.Errorf("feed COPY: expected a worker-local file path, got %q", path)
	}
	return nil
}

func (f *feedCopyWriter) options(args *vgi.Arguments) (copyWriteArgs, error) {
	var opts copyWriteArgs
	if err := vgi.BindArgs(args, &opts); err != nil {
		return opts, err
	}
	u, parseErr := url.Parse(opts.Link)
	if strings.TrimSpace(opts.Title) == "" || parseErr != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return opts, fmt.Errorf("feed COPY: title and an absolute HTTP(S) link are required")
	}
	if f.format == "rss" && strings.TrimSpace(opts.Description) == "" {
		return opts, fmt.Errorf("feed COPY: description is required for RSS")
	}
	return opts, nil
}

func (f *feedCopyWriter) Write(ctx context.Context, params *vgi.ProcessParams, batch arrow.RecordBatch) error {
	if _, err := f.options(params.Args); err != nil {
		return err
	}
	if err := validateItemSchema(batch.Schema()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := vgi.SerializeRecordBatch(batch)
	if err != nil {
		return err
	}
	if len(data) > maxBodyBytes {
		return fmt.Errorf("feed COPY: batch exceeds the 64 MiB export limit")
	}
	_, err = params.Storage.StateAppend(copyShardKey, data)
	return err
}

// Close can run in a different worker from Write, so all buffered rows live in
// execution-scoped storage. Publish only a completely encoded document.
func (f *feedCopyWriter) Close(ctx context.Context, params *vgi.ProcessParams) error {
	opts, err := f.options(params.Args)
	if err != nil {
		return err
	}
	if params.CopyTo == nil {
		return fmt.Errorf("feed COPY: missing destination")
	}
	path := params.CopyTo.FilePath
	if err := localFeedPath(path); err != nil {
		return err
	}
	if err := validateItemSchema(params.InputSchema); err != nil {
		return err
	}
	var items []Item
	var size int
	for after := int64(-1); ; {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := params.Storage.StateLogScan(copyShardKey, after, 1)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			break
		}
		entry := entries[0]
		after = entry.ID
		size += len(entry.Value)
		if size > maxBodyBytes {
			return fmt.Errorf("feed COPY: buffered input exceeds the 64 MiB export limit")
		}
		batch, err := vgi.DeserializeRecordBatch(entry.Value)
		if err != nil {
			return err
		}
		rows, err := itemsFromRecord(batch)
		batch.Release()
		if err != nil {
			return err
		}
		if len(items)+len(rows) > hardMaxItems {
			return fmt.Errorf("feed COPY: export exceeds the %d item limit", hardMaxItems)
		}
		items = append(items, rows...)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".vgi-feed-*")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if err := encodeFeed(ctx, file, f.format, opts, items); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func registerCopyFormats(w *vgi.Worker) {
	w.RegisterCopyFrom(&feedCopyReader{})
	for _, format := range []string{"rss", "atom", "jsonfeed"} {
		w.RegisterCopyTo(&feedCopyWriter{format: format})
	}
}
