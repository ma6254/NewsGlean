package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// rssBodyZH 是含中文标题/描述的本地 RSS 样本，用于验证中文检索端到端。
const rssBodyZH = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>中文源</title>
    <item>
      <title>苹果发布新手机</title>
      <link>https://example.com/zh1</link>
      <guid>zh1</guid>
      <pubDate>Mon, 02 Jan 2023 15:04:05 +0000</pubDate>
      <description>苹果公司发布了新一代手机</description>
    </item>
    <item>
      <title>香蕉价格上涨</title>
      <link>https://example.com/zh2</link>
      <guid>zh2</guid>
      <pubDate>Tue, 03 Jan 2023 10:00:00 GMT</pubDate>
      <description>今日香蕉价格大涨</description>
    </item>
  </channel>
</rss>`

func TestSearchAPI(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBodyZH))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"zh","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}
	if ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}

	searchTotal := func(kw string) (int, int) {
		t.Helper()
		r := doJSON(t, http.MethodGet, ts.URL+"/api/search?q="+url.QueryEscape(kw), "")
		var res struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(r.body, &res); err != nil {
			t.Fatalf("unmarshal search %q: %v", kw, err)
		}
		return r.status, res.Total
	}

	// 中文双字关键词命中（标题 + 描述）
	if st, n := searchTotal("苹果"); st != http.StatusOK || n != 1 {
		t.Fatalf("search 苹果: status=%d total=%d, want 200/1", st, n)
	}
	if st, n := searchTotal("发布"); st != http.StatusOK || n != 1 {
		t.Fatalf("search 发布: status=%d total=%d, want 200/1", st, n)
	}
	if st, n := searchTotal("香蕉"); st != http.StatusOK || n != 1 {
		t.Fatalf("search 香蕉: status=%d total=%d, want 200/1", st, n)
	}
	if st, n := searchTotal("葡萄"); st != http.StatusOK || n != 0 {
		t.Fatalf("search 葡萄: status=%d total=%d, want 200/0", st, n)
	}

	// 缺 q → 400
	if r := doJSON(t, http.MethodGet, ts.URL+"/api/search", ""); r.status != http.StatusBadRequest {
		t.Fatalf("missing q: status=%d, want 400", r.status)
	}
}
