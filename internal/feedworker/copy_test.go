// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Query-farm/vgi-go/vgi"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func copyArgs(t *testing.T, values map[string]any) *vgi.Arguments {
	t.Helper()
	args := argsWith()
	for name, value := range values {
		switch v := value.(type) {
		case string:
			args.Named[name] = strCol(v, false)
		case int64:
			args.Named[name] = vgi.BuildInt64Array(1, func(int64) int64 { return v })
		default:
			t.Fatalf("unsupported test option %T", value)
		}
	}
	t.Cleanup(func() {
		for _, col := range args.Named {
			col.Release()
		}
	})
	return args
}

func TestCopyFromURLs(t *testing.T) {
	base := newServer(t)
	reader := &feedCopyReader{}
	for _, path := range []string{"rss", "atom", "json"} {
		t.Run(path, func(t *testing.T) {
			items, err := reader.readItems(context.Background(), argsWith(), base+"/"+path, itemsSchema)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 2 || items[0].Title == "" {
				t.Fatalf("unexpected items: %+v", items)
			}
		})
	}
}

func TestCopyFromFileAndExplicitCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feed.xml")
	doc := `<rss version="2.0"><channel><title>Many</title>`
	for i := 0; i < 1100; i++ {
		doc += fmt.Sprintf("<item><title>Post %d</title></item>", i)
	}
	doc += "</channel></rss>"
	if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &feedCopyReader{}
	items, err := reader.readItems(context.Background(), argsWith(), path, itemsSchema)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1100 {
		t.Fatalf("default COPY truncated to %d items", len(items))
	}
	items, err = reader.readItems(context.Background(), copyArgs(t, map[string]any{"max_items": int64(3)}), path, itemsSchema)
	if err != nil || len(items) != 3 {
		t.Fatalf("explicit cap: %d items, %v", len(items), err)
	}
}

func TestCopyFromErrors(t *testing.T) {
	base := newServer(t)
	reader := &feedCopyReader{}
	for _, tc := range []struct {
		name, path, want string
		opts             map[string]any
	}{
		{"missing", filepath.Join(t.TempDir(), "missing"), "open source", nil},
		{"http", base + "/nope", "404", nil},
		{"malformed", base + "/malformed", "parse failed", nil},
		{"scheme", "s3://bucket/feed", "worker-local", nil},
		{"max", base + "/rss", "max_items", map[string]any{"max_items": int64(-1)}},
		{"timeout", base + "/rss", "timeout_ms", map[string]any{"timeout_ms": int64(0)}},
		{"overflow", base + "/rss", "timeout_ms", map[string]any{"timeout_ms": int64(1<<63 - 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reader.readItems(context.Background(), copyArgs(t, tc.opts), tc.path, itemsSchema)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.readItems(ctx, argsWith(), base+"/rss", itemsSchema); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	_, err := reader.readItems(context.Background(), copyArgs(t, map[string]any{"timeout_ms": int64(10)}), srv.URL, itemsSchema)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestCopyRejectsOversizedDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.xml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxBodyBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = (&feedCopyReader{}).readItems(context.Background(), argsWith(), path, itemsSchema)
	if err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("got %v", err)
	}
}

func TestCopyColumnMapping(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	items := []Item{{Title: "A", Categories: []string{"one", "two"}, Published: &when}, {Title: "B"}}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "categories", Type: arrow.ListOf(arrow.BinaryTypes.String)},
		{Name: "TITLE", Type: arrow.BinaryTypes.String},
		{Name: "published", Type: tsType},
	}, nil)
	if err := validateItemSchema(schema); err != nil {
		t.Fatal(err)
	}
	batch := itemsRecord(items, schema)
	defer batch.Release()
	if batch.Column(1).(*array.String).Value(1) != "B" || !batch.Column(2).IsNull(1) {
		t.Fatal("incorrect projection or NULL date")
	}
	round, err := itemsFromRecord(batch)
	if err != nil {
		t.Fatal(err)
	}
	if round[0].Title != "A" || !reflect.DeepEqual(round[0].Categories, items[0].Categories) || !round[0].Published.Equal(when) {
		t.Fatalf("round trip: %+v", round)
	}
	for _, fields := range [][]arrow.Field{
		{{Name: "unknown", Type: arrow.BinaryTypes.String}},
		{{Name: "title", Type: arrow.PrimitiveTypes.Int64}},
		{{Name: "title", Type: arrow.BinaryTypes.String}, {Name: "TITLE", Type: arrow.BinaryTypes.String}},
		{{Name: "published", Type: arrow.FixedWidthTypes.Timestamp_us}},
	} {
		if err := validateItemSchema(arrow.NewSchema(fields, nil)); err == nil {
			t.Fatalf("accepted invalid schema %v", fields)
		}
	}
}

func testCopyStorage(t *testing.T) *vgi.ExecutionStorage {
	t.Helper()
	back, err := vgi.NewSQLiteStorage(vgi.SQLiteStorageOptions{Path: filepath.Join(t.TempDir(), "state.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = back.Close() })
	storage := vgi.NewExecutionStorage()
	storage.SetBackend(back)
	if err := storage.SetExecutionID([]byte("copy-test")); err != nil {
		t.Fatal(err)
	}
	return storage
}

func TestCopyExportRoundTrip(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	items := []Item{
		{GUID: "https://example.com/1", Title: "A & <B> \"C\" café", Link: "https://example.com/1?a=1&b=2", Published: &when, Updated: &when, Author: "Alice & Bob", Categories: []string{"news", "<tech>"}, Summary: "Summary & more", Content: "<p>Body & more</p>"},
		{Title: "No date or ID"},
	}
	for _, format := range []string{"rss", "atom", "jsonfeed"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out")
			params := &vgi.ProcessParams{Args: copyArgs(t, map[string]any{"title": "Export & test", "link": "https://example.com", "description": "Some <news>"}), Storage: testCopyStorage(t), InputSchema: itemsSchema, CopyTo: &vgi.CopyToContext{FilePath: path}}
			for _, item := range items {
				batch := itemsRecord([]Item{item}, itemsSchema)
				err := (&feedCopyWriter{format: format}).Write(context.Background(), params, batch)
				batch.Release()
				if err != nil {
					t.Fatal(err)
				}
			}
			// A fresh writer has no in-memory state from Write.
			if err := (&feedCopyWriter{format: format}).Close(context.Background(), params); err != nil {
				t.Fatal(err)
			}
			doc, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			feed, err := ParseFeed(context.Background(), FetchOptions{Input: string(doc)})
			if err != nil {
				t.Fatalf("%v\n%s", err, doc)
			}
			if feed.Title != "Export & test" || len(feed.Items) != 2 {
				t.Fatalf("feed: %+v", feed)
			}
			got := ItemsFrom(feed, 0)
			for i := range items {
				if got[i].Title != items[i].Title {
					t.Errorf("title %q != %q", got[i].Title, items[i].Title)
				}
				if got[i].GUID == "" {
					t.Error("missing generated ID")
				}
			}
			if got[0].GUID != items[0].GUID || got[0].Content != items[0].Content || got[0].Summary != items[0].Summary || got[0].Author != items[0].Author || !reflect.DeepEqual(got[0].Categories, items[0].Categories) || !got[0].Published.Equal(when) {
				t.Fatalf("fields lost: %+v", got[0])
			}
			// gofeed uses Atom updated as published when published is absent.
			if format != "atom" && got[1].Published != nil {
				t.Fatal("missing published date should stay absent")
			}
			if format == "atom" && got[1].Updated == nil {
				t.Fatal("Atom updated is required")
			}
		})
	}
}

func TestCopyEmptyExportAndAtomicFailure(t *testing.T) {
	for _, format := range []string{"rss", "atom", "jsonfeed"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out")
			params := &vgi.ProcessParams{Args: copyArgs(t, map[string]any{"title": "Empty", "link": "https://example.com", "description": "Empty feed"}), Storage: testCopyStorage(t), InputSchema: itemsSchema, CopyTo: &vgi.CopyToContext{FilePath: path}}
			writer := &feedCopyWriter{format: format}
			if err := writer.Close(context.Background(), params); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			feed, err := ParseFeed(context.Background(), FetchOptions{Input: string(before)})
			if err != nil || len(feed.Items) != 0 {
				t.Fatalf("empty feed: %v, %s", err, before)
			}
			batch := itemsRecord([]Item{{}}, itemsSchema)
			defer batch.Release()
			if err := writer.Write(context.Background(), params, batch); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(context.Background(), params); err == nil {
				t.Fatal("accepted empty item")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed export changed destination")
			}
			leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".vgi-feed-*"))
			if len(leftovers) != 0 {
				t.Fatalf("temporary files left: %v", leftovers)
			}
		})
	}
}

func TestCopyWriterRejectsBadOptions(t *testing.T) {
	for _, opts := range []map[string]any{
		{}, {"title": "T"}, {"title": "T", "link": "https://"}, {"title": "T", "link": "file:///tmp/out"},
		{"title": "T", "link": "https://example.com"},
	} {
		if _, err := (&feedCopyWriter{format: "rss"}).options(copyArgs(t, opts)); err == nil {
			t.Fatalf("accepted %v", opts)
		}
	}
}

func TestFeedOutputLimitAndCancellation(t *testing.T) {
	var buf bytes.Buffer
	w := &feedOutput{ctx: context.Background(), w: &buf, remaining: 3}
	if _, err := w.Write([]byte("1234")); err == nil || buf.Len() != 0 {
		t.Fatal("size limit was not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := encodeFeed(ctx, &buf, "rss", copyWriteArgs{}, nil); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func TestCopyLocalFileCannotRedirectToURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feed.txt")
	if err := os.WriteFile(path, []byte("https://example.com/feed"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (&feedCopyReader{}).readItems(context.Background(), argsWith(), path, itemsSchema)
	if err == nil || !strings.Contains(err.Error(), "does not contain a feed") {
		t.Fatalf("got %v", err)
	}
}
