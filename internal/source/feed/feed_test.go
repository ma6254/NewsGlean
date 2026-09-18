package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ma6254/news-glean/internal/source"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return data
}

func TestParseRSS(t *testing.T) {
	items, err := parseFeed(readTestdata(t, "rss2.xml"))
	if err != nil {
		t.Fatalf("parse RSS: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}

	first := items[0]
	if first.Title != "First Post" {
		t.Errorf("title = %q, want %q", first.Title, "First Post")
	}
	if first.GUID != "https://example.com/post/1" {
		t.Errorf("guid = %q", first.GUID)
	}
	if first.Author != "Alice" {
		t.Errorf("author = %q, want Alice", first.Author)
	}
	if first.Content != "<p>Full content of first post</p>" {
		t.Errorf("content = %q", first.Content)
	}
	if first.ContentType != "text/html" {
		t.Errorf("content type = %q", first.ContentType)
	}
	if len(first.Tags) != 1 || first.Tags[0] != "tech" {
		t.Errorf("tags = %v", first.Tags)
	}
	want, _ := time.Parse(time.RFC1123, "Mon, 02 Jan 2023 15:04:05 +0000")
	if !first.PublishedAt.Equal(want) {
		t.Errorf("published = %v, want %v", first.PublishedAt, want)
	}
	if first.InferredTime {
		t.Error("published time should not be inferred")
	}

	// 第二条无 content:encoded 与 dc:creator，正文回退到 description，作者为空
	second := items[1]
	if second.GUID != "guid-2" {
		t.Errorf("second guid = %q", second.GUID)
	}
	if second.Content != "Summary of second" {
		t.Errorf("second content = %q", second.Content)
	}
	if second.Author != "" {
		t.Errorf("second author = %q, want empty", second.Author)
	}
}

func TestParseAtom(t *testing.T) {
	items, err := parseFeed(readTestdata(t, "atom.xml"))
	if err != nil {
		t.Fatalf("parse Atom: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	it := items[0]
	if it.Title != "Atom Entry" {
		t.Errorf("title = %q", it.Title)
	}
	if it.GUID != "urn:uuid:entry-1" {
		t.Errorf("guid = %q", it.GUID)
	}
	if it.URL != "https://example.com/atom/1" {
		t.Errorf("url = %q", it.URL)
	}
	if it.Author != "Bob" {
		t.Errorf("author = %q", it.Author)
	}
	if it.Content != "<p>Atom content</p>" {
		t.Errorf("content = %q", it.Content)
	}
	if len(it.Tags) != 1 || it.Tags[0] != "science" {
		t.Errorf("tags = %v", it.Tags)
	}
}

func TestParseJSONFeed(t *testing.T) {
	items, err := parseFeed(readTestdata(t, "jsonfeed.json"))
	if err != nil {
		t.Fatalf("parse JSON Feed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	it := items[0]
	if it.Title != "JSON Entry" {
		t.Errorf("title = %q", it.Title)
	}
	if it.GUID != "jf-1" {
		t.Errorf("guid = %q", it.GUID)
	}
	if it.Content != "<p>JSON content</p>" {
		t.Errorf("content = %q", it.Content)
	}
	if it.ContentType != "text/html" {
		t.Errorf("content type = %q", it.ContentType)
	}
	if it.Author != "Carol" {
		t.Errorf("author = %q", it.Author)
	}
	if len(it.Tags) != 2 {
		t.Errorf("tags = %v", it.Tags)
	}
}

func TestParseMalformed(t *testing.T) {
	if _, err := parseFeed(readTestdata(t, "malformed.xml")); err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := parseFeed([]byte("   ")); err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"ok", `{"url":"https://example.com/feed.xml"}`, false},
		{"empty", `{"url":""}`, true},
		{"no scheme", `{"url":"example.com/feed.xml"}`, true},
		{"invalid json", `{`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := New(tc.config, source.CreateOptions{})
			if tc.name == "invalid json" {
				if err == nil {
					t.Fatal("expected error for invalid config json")
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := conn.Validate(context.Background()); (err != nil) != tc.wantErr {
				t.Fatalf("Validate = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	feedBody := readTestdata(t, "rss2.xml")
	etag := `"abc123"`
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", etag)
		_, _ = w.Write(feedBody)
	}))
	defer srv.Close()

	conn, err := New(`{"url":"`+srv.URL+`"}`, source.CreateOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	items, cursor, err := conn.Fetch(ctx, nil, 0)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if len(cursor) == 0 {
		t.Fatal("expected non-empty cursor with ETag")
	}
	if items[0].FetchedAt.IsZero() {
		t.Error("FetchedAt should be set")
	}

	// 第二次用 ETag 游标请求，应命中 304 返回空切片与原游标
	items2, cursor2, err := conn.Fetch(ctx, cursor, 0)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if len(items2) != 0 {
		t.Fatalf("want 0 items on 304, got %d", len(items2))
	}
	if string(cursor2) != string(cursor) {
		t.Error("cursor should be preserved on 304")
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

func TestRunUnsupported(t *testing.T) {
	conn, _ := New(`{"url":"https://example.com/feed.xml"}`, source.CreateOptions{})
	if err := conn.Run(context.Background(), func(source.Item) error { return nil }); err != source.ErrPushUnsupported {
		t.Fatalf("Run = %v, want ErrPushUnsupported", err)
	}
}

func TestType(t *testing.T) {
	conn, _ := New(`{"url":"https://example.com/feed.xml"}`, source.CreateOptions{})
	if conn.Type() != "feed" {
		t.Errorf("Type = %q, want feed", conn.Type())
	}
}

func TestFetchMode(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"default http", `{"url":"https://example.com/feed.xml"}`, false},
		{"http", `{"url":"https://example.com/feed.xml","fetch_mode":"http"}`, false},
		{"chromedp", `{"url":"https://example.com/feed.xml","fetch_mode":"chromedp"}`, false},
		{"chromedp headed", `{"url":"https://example.com/feed.xml","fetch_mode":"chromedp_headed"}`, false},
		{"invalid", `{"url":"https://example.com/feed.xml","fetch_mode":"nope"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := New(tc.config, source.CreateOptions{})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error for invalid fetch_mode")
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if conn == nil {
				t.Fatal("nil connector")
			}
		})
	}
}
