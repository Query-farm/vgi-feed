// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"bytes"
	"encoding/gob"
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
	if len(st.Rows) != 2 {
		t.Fatalf("expected 2 items, got %d", len(st.Rows))
	}
	if st.Offset != 0 {
		t.Error("cursor offset should be 0 before Process")
	}
	if st.Rows[0].Title != "First RSS Post" {
		t.Errorf("item title wrong: %q", st.Rows[0].Title)
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
	if len(st.Rows) != 2 || st.Rows[0].Title != "First JSON Item" {
		t.Fatalf("raw JSON feed not parsed via NewState: %+v", st.Rows)
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
	if len(st.Rows) != 0 {
		t.Errorf("NULL input should yield no items, got %d", len(st.Rows))
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
	if len(st.Rows) != 1 {
		t.Fatal("expected one info row")
	}
	if st.Rows[0].FeedType != "atom" {
		t.Errorf("feed_type = %q, want atom", st.Rows[0].FeedType)
	}
	if st.Rows[0].ItemCount != 2 {
		t.Errorf("item_count = %d, want 2", st.Rows[0].ItemCount)
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
	if len(st.Rows) != 0 {
		t.Error("NULL input should yield no info row")
	}
}

func TestRegisterDoesNotPanic(t *testing.T) {
	// Registration validates the gob-encodability of the state structs; if a
	// state field were non-encodable, the SDK panics here.
	w := vgi.NewWorker(vgi.WithCatalogName(CatalogName))
	Register(w)
}

// TestCursorSurvivesContinuation proves the streaming cursor round-trips through
// a gob snapshot between Process ticks — the exact path the stateless HTTP
// transport takes when it resumes a producer from its continuation token. A
// multi-row producer that advances Offset BEFORE yielding emits each row exactly
// once and terminates (the bug a bare Done flag re-emitted forever over HTTP).
func TestCursorSurvivesContinuation(t *testing.T) {
	const total = 1000 // > rowsPerTick (256), so it spans several continuations
	rows := make([]Item, total)
	for i := range rows {
		rows[i] = Item{Seq: int64(i)}
	}
	st := &itemsState{Cursor[Item]{Rows: rows}}

	emitted := 0
	for tick := 0; tick < total+5; tick++ {
		slice, done := st.nextSlice()
		if done {
			break
		}
		emitted += len(slice)
		// Simulate the HTTP continuation boundary: gob-encode then decode the
		// LIVE state, and resume from the snapshot (never the in-memory state).
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(st); err != nil {
			t.Fatalf("gob encode: %v", err)
		}
		var resumed itemsState
		if err := gob.NewDecoder(&buf).Decode(&resumed); err != nil {
			t.Fatalf("gob decode: %v", err)
		}
		st = &resumed
	}
	if emitted != total {
		t.Fatalf("cursor emitted %d rows across continuations, want %d", emitted, total)
	}
	if _, done := st.nextSlice(); !done {
		t.Fatal("cursor did not report done after draining all rows")
	}
}
