package webpage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

// newConn 构造 connector 具体类型，便于直接调用未导出的 parseList。
func newConn(t *testing.T, config string) *connector {
	t.Helper()
	conn, err := New(config, source.CreateOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return conn.(*connector)
}

func TestParseList(t *testing.T) {
	c := newConn(t, `{"url":"https://example.com/news","selector":"div.article-list > a"}`)
	items, err := c.parseList(readTestdata(t, "list.html"))
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	first := items[0]
	if first.Title != "Third Article" {
		t.Errorf("title = %q", first.Title)
	}
	if first.URL != "https://example.com/news/2024/01/third" {
		t.Errorf("url = %q", first.URL)
	}
	if first.GUID != first.URL {
		t.Errorf("guid = %q, want == url", first.GUID)
	}
	if !first.InferredTime {
		t.Error("published time should be inferred")
	}
	if first.ContentType != "text/html" {
		t.Errorf("content type = %q", first.ContentType)
	}
}

func TestParseListContainers(t *testing.T) {
	c := newConn(t, `{"url":"https://example.com/","selector":"ul.news li"}`)
	items, err := c.parseList(readTestdata(t, "containers.html"))
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	// 第 3 个 li 无锚点，应被跳过
	if len(items) != 3 {
		t.Fatalf("want 3 items (skip li without anchor), got %d", len(items))
	}
	if items[0].Title != "Item One" {
		t.Errorf("title = %q, want collapsed whitespace", items[0].Title)
	}
	if items[0].URL != "https://example.com/items/1" {
		t.Errorf("url = %q", items[0].URL)
	}
	if items[2].URL != "https://example.com/items/3" {
		t.Errorf("url = %q", items[2].URL)
	}
}

func TestParseListSkipsNonHTTP(t *testing.T) {
	c := newConn(t, `{"url":"https://example.com/","selector":"a"}`)
	items, err := c.parseList(readTestdata(t, "edge.html"))
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	// javascript: / mailto: / # 锚点均应被丢弃，只剩 http 链接
	if len(items) != 1 {
		t.Fatalf("want 1 item (only http link), got %d", len(items))
	}
	if items[0].URL != "https://example.com/ok" {
		t.Errorf("url = %q", items[0].URL)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"ok", `{"url":"https://example.com/news","selector":"div.article-list > a"}`, false},
		{"empty url", `{"url":"","selector":"a"}`, true},
		{"no scheme", `{"url":"example.com/news","selector":"a"}`, true},
		{"empty selector", `{"url":"https://example.com/news","selector":""}`, true},
		{"invalid selector", `{"url":"https://example.com/news","selector":"div["}`, true},
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
	body := readTestdata(t, "list.html")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	conn, err := New(`{"url":"`+srv.URL+`/news","selector":"div.article-list > a"}`, source.CreateOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	items, cursor, err := conn.Fetch(ctx, nil, 200)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if len(cursor) == 0 {
		t.Fatal("expected non-empty cursor")
	}
	if items[0].FetchedAt.IsZero() {
		t.Error("FetchedAt should be set")
	}

	// 第二次用游标请求：最新链接未变，应返回空切片并保留游标
	items2, cursor2, err := conn.Fetch(ctx, cursor, 200)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if len(items2) != 0 {
		t.Fatalf("want 0 items on unchanged list, got %d", len(items2))
	}
	if string(cursor2) != string(cursor) {
		t.Error("cursor should be preserved on unchanged list")
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

func TestProbe(t *testing.T) {
	body := readTestdata(t, "list.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	conn, err := New(`{"url":"`+srv.URL+`/news","selector":"div.article-list > a"}`, source.CreateOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer conn.Close()

	prober, ok := conn.(source.Prober)
	if !ok {
		t.Fatal("connector should implement source.Prober")
	}
	info, err := prober.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.Title != "Test News List" {
		t.Errorf("title = %q", info.Title)
	}
}

func TestRunUnsupported(t *testing.T) {
	conn, _ := New(`{"url":"https://example.com/news","selector":"a"}`, source.CreateOptions{})
	if err := conn.Run(context.Background(), func(source.Item) error { return nil }); err != source.ErrPushUnsupported {
		t.Fatalf("Run = %v, want ErrPushUnsupported", err)
	}
}

func TestType(t *testing.T) {
	conn, _ := New(`{"url":"https://example.com/news","selector":"a"}`, source.CreateOptions{})
	if conn.Type() != "webpage" {
		t.Errorf("Type = %q, want webpage", conn.Type())
	}
}
