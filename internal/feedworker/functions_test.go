// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"net/http/httptest"
	"testing"

	"github.com/Query-farm/vgi-feed/internal/mockfeed"
	"github.com/Query-farm/vgi-go/vgi"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// strCol builds a 1-row string array (optionally NULL) for use as a positional
// argument value.
func strCol(v string, null bool) arrow.Array {
	b := array.NewStringBuilder(memory.DefaultAllocator)
	defer b.Release()
	if null {
		b.AppendNull()
	} else {
		b.Append(v)
	}
	return b.NewArray()
}

func argsWith(positional ...arrow.Array) *vgi.Arguments {
	return &vgi.Arguments{
		Positional: positional,
		Named:      map[string]arrow.Array{},
	}
}

func startServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(mockfeed.NewHandler())
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestItemsNewStateFromURL(t *testing.T) {
	base := startServer(t)
	f := &ItemsFunction{}
	st, err := f.NewState(&vgi.ProcessParams{
		Args: argsWith(strCol(base+"/rss", false)),
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if len(st.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(st.Items))
	}
	if st.Done {
		t.Error("state should not be marked done before Process")
	}
	if st.Items[0].Title != "First RSS Post" {
		t.Errorf("item title wrong: %q", st.Items[0].Title)
	}
}

func TestItemsNewStateFromRawText(t *testing.T) {
	f := &ItemsFunction{}
	st, err := f.NewState(&vgi.ProcessParams{
		Args: argsWith(strCol(mockfeed.JSON, false)),
	})
	if err != nil {
		t.Fatalf("NewState (raw JSON): %v", err)
	}
	if len(st.Items) != 2 || st.Items[0].Title != "First JSON Item" {
		t.Fatalf("raw JSON feed not parsed via NewState: %+v", st.Items)
	}
}

func TestItemsNullArgNoRows(t *testing.T) {
	f := &ItemsFunction{}
	st, err := f.NewState(&vgi.ProcessParams{
		Args: argsWith(strCol("", true)),
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if len(st.Items) != 0 {
		t.Errorf("NULL input should yield no items, got %d", len(st.Items))
	}
}

func TestItemsNewStateError(t *testing.T) {
	base := startServer(t)
	f := &ItemsFunction{}
	_, err := f.NewState(&vgi.ProcessParams{
		Args: argsWith(strCol(base+"/malformed", false)),
	})
	if err == nil {
		t.Fatal("expected an error parsing the malformed feed")
	}
}

func TestInfoNewStateFromURL(t *testing.T) {
	base := startServer(t)
	f := &InfoFunction{}
	st, err := f.NewState(&vgi.ProcessParams{
		Args: argsWith(strCol(base+"/atom", false)),
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if !st.HasInfo {
		t.Fatal("expected HasInfo true")
	}
	if st.Info.FeedType != "atom" {
		t.Errorf("feed_type = %q, want atom", st.Info.FeedType)
	}
	if st.Info.ItemCount != 2 {
		t.Errorf("item_count = %d, want 2", st.Info.ItemCount)
	}
}

func TestInfoNullArgNoRows(t *testing.T) {
	f := &InfoFunction{}
	st, err := f.NewState(&vgi.ProcessParams{
		Args: argsWith(strCol("", true)),
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if st.HasInfo {
		t.Error("NULL input should yield no info row")
	}
}

func TestRegisterDoesNotPanic(t *testing.T) {
	// Registration validates the gob-encodability of the state structs; if a
	// state field were non-encodable, the SDK panics here.
	w := vgi.NewWorker(vgi.WithCatalogName(CatalogName))
	Register(w)
}
