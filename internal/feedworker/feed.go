// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"time"

	"github.com/mmcdole/gofeed"
)

// Item is a single feed entry flattened to the columns the worker exposes. All
// fields are exported and gob-encodable: these structs are stored directly in
// table-function state, which the SDK gob-encodes between NewState and Process.
//
// Published / Updated are *time.Time so a missing or unparseable date stays nil
// and is emitted as a NULL TIMESTAMP rather than a zero value.
type Item struct {
	Seq        int64
	GUID       string
	Title      string
	Link       string
	Published  *time.Time
	Updated    *time.Time
	Author     string
	Categories []string
	Summary    string
	Content    string
}

// Info is feed-level metadata (one row).
type Info struct {
	Title       string
	Description string
	Link        string
	FeedType    string
	Language    string
	Updated     *time.Time
	ItemCount   int32
}

// authorName extracts a display name from gofeed's author shapes, preferring the
// singular Author and falling back to the first entry of Authors.
func authorName(primary *gofeed.Person, all []*gofeed.Person) string {
	if primary != nil && primary.Name != "" {
		return primary.Name
	}
	if primary != nil && primary.Email != "" {
		return primary.Email
	}
	for _, p := range all {
		if p == nil {
			continue
		}
		if p.Name != "" {
			return p.Name
		}
		if p.Email != "" {
			return p.Email
		}
	}
	return ""
}

// ItemsFrom flattens a parsed feed into Items, capping the count at maxItems
// (<= 0 means no cap). Categories is normalised to a non-nil slice so the Arrow
// list column is an empty list (not NULL) when a feed item has no categories.
func ItemsFrom(feed *gofeed.Feed, maxItems int) []Item {
	n := len(feed.Items)
	if maxItems > 0 && n > maxItems {
		n = maxItems
	}
	out := make([]Item, 0, n)
	for i := 0; i < n; i++ {
		it := feed.Items[i]
		cats := it.Categories
		if cats == nil {
			cats = []string{}
		}
		out = append(out, Item{
			Seq:        int64(i),
			GUID:       it.GUID,
			Title:      it.Title,
			Link:       it.Link,
			Published:  it.PublishedParsed,
			Updated:    it.UpdatedParsed,
			Author:     authorName(it.Author, it.Authors),
			Categories: cats,
			Summary:    it.Description,
			Content:    it.Content,
		})
	}
	return out
}

// InfoFrom builds feed-level metadata from a parsed feed.
func InfoFrom(feed *gofeed.Feed) Info {
	return Info{
		Title:       feed.Title,
		Description: feed.Description,
		Link:        feed.Link,
		FeedType:    feed.FeedType,
		Language:    feed.Language,
		Updated:     feed.UpdatedParsed,
		ItemCount:   int32(len(feed.Items)),
	}
}
