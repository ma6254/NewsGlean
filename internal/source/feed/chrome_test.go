package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ma6254/news-glean/internal/source"
)

// TestFetchChrome 是 chromedp 集成测试：需要本机已安装 Chrome/Chromium。
// 默认跳过；设置 NEWSGLEAN_CHROME_TEST=1 时运行（无头模式）。
func TestFetchChrome(t *testing.T) {
	if os.Getenv("NEWSGLEAN_CHROME_TEST") == "" {
		t.Skip("set NEWSGLEAN_CHROME_TEST=1 to run chromedp integration test")
	}

	feedBody := readTestdata(t, "rss2.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feedBody)
	}))
	defer srv.Close()

	conn, err := New(`{"url":"`+srv.URL+`","fetch_mode":"chromedp"}`, source.CreateOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer conn.Close()

	items, _, err := conn.Fetch(context.Background(), nil, 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].Title, "Post") {
		t.Fatalf("unexpected title %q", items[0].Title)
	}
}
