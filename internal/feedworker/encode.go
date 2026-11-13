// Copyright 2026 Query Farm LLC - https://query.farm

package feedworker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"time"
)

type feedOutput struct {
	ctx       context.Context
	w         io.Writer
	remaining int
}

func (w *feedOutput) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > w.remaining {
		return 0, fmt.Errorf("feed COPY: output exceeds the 64 MiB limit")
	}
	n, err := w.w.Write(p)
	w.remaining -= n
	return n, err
}

func encodeFeed(ctx context.Context, dst io.Writer, format string, opts copyWriteArgs, items []Item) error {
	for i, item := range items {
		if item.Title == "" && item.Summary == "" && item.Content == "" {
			return fmt.Errorf("feed COPY: item %d needs title, summary, or content", i)
		}
		if format == "atom" && item.Title == "" {
			return fmt.Errorf("feed COPY: Atom item %d needs a title", i)
		}
	}
	w := &feedOutput{ctx: ctx, w: dst, remaining: maxBodyBytes}
	now := time.Now().UTC()
	switch format {
	case "rss":
		return encodeXML(w, makeRSS(opts, items))
	case "atom":
		return encodeXML(w, makeAtom(opts, items, now))
	case "jsonfeed":
		return json.NewEncoder(w).Encode(makeJSONFeed(opts, items))
	default:
		return fmt.Errorf("feed COPY: unsupported output format %q", format)
	}
}

func encodeXML(w io.Writer, doc any) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	return xml.NewEncoder(w).Encode(doc)
}

// itemID preserves supplied IDs and otherwise derives a stable ID from the
// link or item contents. Atom IDs must be absolute IRIs, so opaque IDs are hashed.
func itemID(item Item, atom bool) string {
	id := item.GUID
	if id == "" {
		id = item.Link
	}
	if id != "" {
		u, err := url.Parse(id)
		if !atom || (err == nil && u.IsAbs()) {
			return id
		}
	} else {
		// seq is query position, not identity: exclude it from the fallback ID.
		item.Seq = 0
		b, _ := json.Marshal(item)
		id = string(b)
	}
	return fmt.Sprintf("urn:sha256:%x", sha256.Sum256([]byte(id)))
}

func dateString(t *time.Time, layout string) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(layout)
}

type rssDocument struct {
	XMLName   xml.Name   `xml:"rss"`
	Version   string     `xml:"version,attr"`
	ContentNS string     `xml:"xmlns:content,attr"`
	DCNS      string     `xml:"xmlns:dc,attr"`
	Channel   rssChannel `xml:"channel"`
}
type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language,omitempty"`
	Author      string    `xml:"dc:creator,omitempty"`
	Items       []rssItem `xml:"item"`
}
type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}
type rssItem struct {
	GUID       rssGUID  `xml:"guid"`
	Title      string   `xml:"title,omitempty"`
	Link       string   `xml:"link,omitempty"`
	Published  string   `xml:"pubDate,omitempty"`
	Author     string   `xml:"dc:creator,omitempty"`
	Categories []string `xml:"category"`
	Summary    string   `xml:"description,omitempty"`
	Content    string   `xml:"content:encoded,omitempty"`
}

func makeRSS(opts copyWriteArgs, items []Item) rssDocument {
	doc := rssDocument{Version: "2.0", ContentNS: "http://purl.org/rss/1.0/modules/content/", DCNS: "http://purl.org/dc/elements/1.1/",
		Channel: rssChannel{Title: opts.Title, Link: opts.Link, Description: opts.Description, Language: opts.Language, Author: opts.Author}}
	for _, item := range items {
		summary := item.Summary
		if summary == "" && item.Title == "" {
			summary = item.Content
		}
		doc.Channel.Items = append(doc.Channel.Items, rssItem{
			GUID: rssGUID{IsPermaLink: "false", Value: itemID(item, false)}, Title: item.Title,
			Link: item.Link, Published: dateString(item.Published, time.RFC1123Z), Author: item.Author,
			Categories: item.Categories, Summary: summary, Content: item.Content,
		})
	}
	return doc
}

type atomDocument struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Language string      `xml:"xml:lang,attr,omitempty"`
	ID       string      `xml:"id"`
	Title    string      `xml:"title"`
	Link     atomLink    `xml:"link"`
	Subtitle string      `xml:"subtitle,omitempty"`
	Updated  string      `xml:"updated"`
	Author   atomAuthor  `xml:"author"`
	Entries  []atomEntry `xml:"entry"`
}
type atomLink struct {
	Href string `xml:"href,attr"`
}
type atomAuthor struct {
	Name string `xml:"name"`
}
type atomCategory struct {
	Term string `xml:"term,attr"`
}
type atomText struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}
type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Link       *atomLink      `xml:"link,omitempty"`
	Published  string         `xml:"published,omitempty"`
	Updated    string         `xml:"updated"`
	Author     *atomAuthor    `xml:"author,omitempty"`
	Categories []atomCategory `xml:"category"`
	Summary    *atomText      `xml:"summary,omitempty"`
	Content    *atomText      `xml:"content,omitempty"`
}

func makeAtom(opts copyWriteArgs, items []Item, now time.Time) atomDocument {
	author := opts.Author
	if author == "" {
		author = opts.Title
	}
	doc := atomDocument{Language: opts.Language, ID: opts.Link, Title: opts.Title,
		Link: atomLink{opts.Link}, Subtitle: opts.Description, Author: atomAuthor{author}}
	var latest time.Time
	for _, item := range items {
		updated := item.Updated
		if updated == nil {
			updated = item.Published
		}
		if updated == nil {
			updated = &now
		}
		if updated.After(latest) {
			latest = *updated
		}
		entry := atomEntry{ID: itemID(item, true), Title: item.Title,
			Published: dateString(item.Published, time.RFC3339Nano), Updated: dateString(updated, time.RFC3339Nano)}
		if item.Link != "" {
			entry.Link = &atomLink{item.Link}
		}
		if item.Author != "" {
			entry.Author = &atomAuthor{item.Author}
		}
		if item.Summary != "" {
			entry.Summary = &atomText{Type: "html", Value: item.Summary}
		}
		content := item.Content
		if content == "" && item.Link == "" {
			content = item.Summary
			if content == "" {
				content = item.Title
			}
		}
		if content != "" {
			entry.Content = &atomText{Type: "html", Value: content}
		}
		for _, category := range item.Categories {
			entry.Categories = append(entry.Categories, atomCategory{category})
		}
		doc.Entries = append(doc.Entries, entry)
	}
	if latest.IsZero() {
		latest = now
	}
	doc.Updated = dateString(&latest, time.RFC3339Nano)
	return doc
}

type jsonAuthor struct {
	Name string `json:"name"`
}
type jsonFeed struct {
	Version     string       `json:"version"`
	Title       string       `json:"title"`
	HomePageURL string       `json:"home_page_url"`
	Description string       `json:"description,omitempty"`
	Language    string       `json:"language,omitempty"`
	Authors     []jsonAuthor `json:"authors,omitempty"`
	Items       []jsonItem   `json:"items"`
}
type jsonItem struct {
	ID          string       `json:"id"`
	URL         string       `json:"url,omitempty"`
	Title       string       `json:"title,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	ContentHTML string       `json:"content_html,omitempty"`
	ContentText string       `json:"content_text,omitempty"`
	Published   string       `json:"date_published,omitempty"`
	Updated     string       `json:"date_modified,omitempty"`
	Authors     []jsonAuthor `json:"authors,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
}

func makeJSONFeed(opts copyWriteArgs, items []Item) jsonFeed {
	doc := jsonFeed{Version: "https://jsonfeed.org/version/1.1", Title: opts.Title,
		HomePageURL: opts.Link, Description: opts.Description, Language: opts.Language, Items: make([]jsonItem, 0, len(items))}
	if opts.Author != "" {
		doc.Authors = []jsonAuthor{{opts.Author}}
	}
	for _, item := range items {
		entry := jsonItem{ID: itemID(item, false), URL: item.Link, Title: item.Title,
			Summary: item.Summary, ContentHTML: item.Content, Published: dateString(item.Published, time.RFC3339Nano),
			Updated: dateString(item.Updated, time.RFC3339Nano), Tags: item.Categories}
		if item.Author != "" {
			entry.Authors = []jsonAuthor{{item.Author}}
		}
		if entry.ContentHTML == "" {
			entry.ContentText = item.Summary
			if entry.ContentText == "" {
				entry.ContentText = item.Title
			}
		}
		doc.Items = append(doc.Items, entry)
	}
	return doc
}
