package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ma6254/news-glean/internal/app"
	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/scheduler"

	// 注册 feed 渠道（app 的采集依赖注册表）
	_ "github.com/ma6254/news-glean/internal/source/feed"
)

// rssBody 是集成测试用的本地 RSS 样本。
const rssBody = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>IT</title>
    <item>
      <title>Post One</title>
      <link>https://example.com/one</link>
      <guid>https://example.com/one</guid>
      <pubDate>Mon, 02 Jan 2023 15:04:05 +0000</pubDate>
      <description>one</description>
    </item>
    <item>
      <title>Post Two</title>
      <link>https://example.com/two</link>
      <guid>https://example.com/two</guid>
      <pubDate>Tue, 03 Jan 2023 10:00:00 GMT</pubDate>
      <description>two</description>
    </item>
  </channel>
</rss>`

// newTestServer 装配一套完整的内存应用并返回可请求的 HTTP 服务。
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	tmp, err := os.CreateTemp("", "newsglean-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	t.Cleanup(func() { _ = os.Remove(tmp.Name()) })

	db, err := database.Open("sqlite", tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Install(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	a := app.New(cfg, db)
	sched := scheduler.New(a)
	srv := New(cfg, db, a, sched)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

type resp struct {
	status int
	body   []byte
}

func doJSON(t *testing.T, method, url, body string) resp {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp{status: res.StatusCode, body: data}
}

func TestSourceAddRefreshEntryList(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	// 1. 新增 feed 渠道
	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}

	// 2. 手动触发一轮采集
	ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", "")
	if ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}
	var refreshResult struct {
		Inserted int `json:"inserted"`
	}
	if err := json.Unmarshal(ref.body, &refreshResult); err != nil {
		t.Fatalf("unmarshal refresh result: %v", err)
	}
	if refreshResult.Inserted != 2 {
		t.Fatalf("inserted = %d, want 2", refreshResult.Inserted)
	}

	// 3. 读取条目列表
	list := doJSON(t, http.MethodGet, ts.URL+"/api/entry/list", "")
	if list.status != http.StatusOK {
		t.Fatalf("entry list status = %d, body = %s", list.status, list.body)
	}
	var listResult struct {
		Total int `json:"total"`
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.body, &listResult); err != nil {
		t.Fatalf("unmarshal entry list: %v", err)
	}
	if listResult.Total != 2 {
		t.Fatalf("total = %d, want 2", listResult.Total)
	}

	// 4. 再次刷新应去重，不重复入库
	ref2 := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", "")
	if ref2.status != http.StatusOK {
		t.Fatalf("second refresh status = %d, body = %s", ref2.status, ref2.body)
	}
	if err := json.Unmarshal(ref2.body, &refreshResult); err != nil {
		t.Fatalf("unmarshal second refresh: %v", err)
	}
	if refreshResult.Inserted != 0 {
		t.Fatalf("second refresh inserted = %d, want 0 (dedup)", refreshResult.Inserted)
	}
}

func TestSourceValidation(t *testing.T) {
	ts := newTestServer(t)

	// 无 url 的 feed 配置应被 Validate 拒绝
	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"bad","type":"feed","config":{"url":""}}`)
	if add.status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid feed, got %d body=%s", add.status, add.body)
	}
}

func TestSourceProbe(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	// 合法 feed 地址应探测出标题（rssBody 的 <title>IT</title>）
	probe := doJSON(t, http.MethodPost, ts.URL+"/api/source/probe",
		`{"type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if probe.status != http.StatusOK {
		t.Fatalf("probe status = %d, body = %s", probe.status, probe.body)
	}
	var info struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(probe.body, &info); err != nil {
		t.Fatalf("unmarshal probe result: %v", err)
	}
	if info.Title != "IT" {
		t.Fatalf("title = %q, want IT", info.Title)
	}

	// 未知渠道类型应报 400
	bad := doJSON(t, http.MethodPost, ts.URL+"/api/source/probe",
		`{"type":"nope","config":{}}`)
	if bad.status != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown type, got %d body=%s", bad.status, bad.body)
	}
}

// TestRefreshPersistsCursor 验证游标跨轮次持久化：第二轮采集应带上上一轮的
// ETag 条件请求头，服务端命中 304 后不重复入库。
func TestRefreshPersistsCursor(t *testing.T) {
	const etag = `"cursor-etag-1"`
	var hits int
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}

	var refreshResult struct {
		Inserted int `json:"inserted"`
	}
	first := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", "")
	if err := json.Unmarshal(first.body, &refreshResult); err != nil {
		t.Fatalf("unmarshal first refresh: %v", err)
	}
	if refreshResult.Inserted != 2 {
		t.Fatalf("first refresh inserted = %d, want 2", refreshResult.Inserted)
	}

	// 第二轮应带上持久化的 ETag，命中 304，不重复入库
	second := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", "")
	if err := json.Unmarshal(second.body, &refreshResult); err != nil {
		t.Fatalf("unmarshal second refresh: %v", err)
	}
	if refreshResult.Inserted != 0 {
		t.Fatalf("second refresh inserted = %d, want 0 (304)", refreshResult.Inserted)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 2 (first 200 + second 304)", hits)
	}
}

// TestWebEmbedServesIndex 验证默认（embed 模式）下 / 能返回内嵌前端页面。
func TestWebEmbedServesIndex(t *testing.T) {
	ts := newTestServer(t)
	res := doJSON(t, http.MethodGet, ts.URL+"/", "")
	if res.status != http.StatusOK {
		t.Fatalf("GET / = %d, body=%s", res.status, res.body)
	}
	if !bytes.Contains(res.body, []byte("NewsGlean")) {
		t.Fatalf("GET / body should contain NewsGlean, got %s", res.body)
	}
}
