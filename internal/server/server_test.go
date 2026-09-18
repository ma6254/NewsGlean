package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
	sched := scheduler.New(cfg, db, a)
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

// TestSourceFetchLogAndStats 验证采集日志可观测性：刷新后渠道列表带统计，日志端点可读。
func TestSourceFetchLogAndStats(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}
	var created struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(add.body, &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}

	if ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}

	// 渠道列表应带采集统计
	list := doJSON(t, http.MethodGet, ts.URL+"/api/source", "")
	if list.status != http.StatusOK {
		t.Fatalf("list sources status = %d", list.status)
	}
	var lr struct {
		Items []struct {
			ID            uint64  `json:"id"`
			FetchCount    int64   `json:"fetch_count"`
			SuccessCount  int64   `json:"success_count"`
			SuccessRate   float64 `json:"success_rate"`
			LastElapsedMS int64   `json:"last_elapsed_ms"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.body, &lr); err != nil {
		t.Fatalf("unmarshal source list: %v", err)
	}
	found := false
	for _, it := range lr.Items {
		if it.ID == created.ID {
			found = true
			if it.FetchCount < 1 || it.SuccessCount < 1 || it.SuccessRate <= 0 {
				t.Fatalf("stats not populated for source: %+v", it)
			}
		}
	}
	if !found {
		t.Fatal("created source not found in list")
	}

	// 采集日志端点应可读且首条为成功
	logs := doJSON(t, http.MethodGet, ts.URL+"/api/source/"+strconv.FormatUint(created.ID, 10)+"/logs", "")
	if logs.status != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", logs.status, logs.body)
	}
	var logResp struct {
		Total int `json:"total"`
		Items []struct {
			Success bool `json:"success"`
		} `json:"items"`
	}
	if err := json.Unmarshal(logs.body, &logResp); err != nil {
		t.Fatalf("unmarshal logs: %v", err)
	}
	if logResp.Total < 1 || !logResp.Items[0].Success {
		t.Fatalf("fetch log not recorded correctly: %+v", logResp)
	}
}

// TestEntryStateAPI 验证已读 / 收藏 / 归档三个状态端点的读写与列表过滤。
func TestEntryStateAPI(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}
	if ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}

	list := doJSON(t, http.MethodGet, ts.URL+"/api/entry/list", "")
	var lr struct {
		Total int `json:"total"`
		Items []struct {
			ID    uint64 `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.body, &lr); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if lr.Total != 2 {
		t.Fatalf("total = %d, want 2", lr.Total)
	}
	first := lr.Items[0]
	second := lr.Items[1]

	// 标记第一条已读 + 收藏 + 归档
	set := doJSON(t, http.MethodPut, ts.URL+"/api/entry/"+strconv.FormatUint(first.ID, 10)+"/read", `{"read":true}`)
	if set.status != http.StatusOK {
		t.Fatalf("set read status = %d, body = %s", set.status, set.body)
	}
	var dto struct {
		Read     bool `json:"read"`
		Favorite bool `json:"favorite"`
		Archive  bool `json:"archive"`
	}
	if err := json.Unmarshal(set.body, &dto); err != nil {
		t.Fatalf("unmarshal set read: %v", err)
	}
	if !dto.Read {
		t.Fatal("read should be true after set")
	}

	if set := doJSON(t, http.MethodPut, ts.URL+"/api/entry/"+strconv.FormatUint(first.ID, 10)+"/favorite", `{"favorite":true}`); set.status != http.StatusOK {
		t.Fatalf("set favorite status = %d, body = %s", set.status, set.body)
	}
	if set := doJSON(t, http.MethodPut, ts.URL+"/api/entry/"+strconv.FormatUint(first.ID, 10)+"/archive", `{"archive":true}`); set.status != http.StatusOK {
		t.Fatalf("set archive status = %d, body = %s", set.status, set.body)
	}

	// 过滤：已读 1 条、收藏 1 条、归档 1 条、未归档（收件箱）1 条
	checkTotal := func(q string, want int) {
		t.Helper()
		r := doJSON(t, http.MethodGet, ts.URL+"/api/entry/list"+q, "")
		var res struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(r.body, &res); err != nil {
			t.Fatalf("unmarshal %s: %v", q, err)
		}
		if res.Total != want {
			t.Fatalf("%s total = %d, want %d", q, res.Total, want)
		}
	}
	checkTotal("?read=true", 1)
	checkTotal("?favorite=true", 1)
	checkTotal("?archive=true", 1)
	checkTotal("?archive=false", 1) // 第二条未归档

	// 归档的第二条不应出现在收件箱；确认剩余那条是 second
	arch := doJSON(t, http.MethodGet, ts.URL+"/api/entry/list?archive=false", "")
	var archRes struct {
		Items []struct {
			ID uint64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(arch.body, &archRes); err != nil {
		t.Fatalf("unmarshal archive=false: %v", err)
	}
	if len(archRes.Items) != 1 || archRes.Items[0].ID != second.ID {
		t.Fatalf("inbox should contain only second entry, got %+v", archRes.Items)
	}

	// 取消已读后 read=false 过滤应为 0 条
	if set := doJSON(t, http.MethodPut, ts.URL+"/api/entry/"+strconv.FormatUint(first.ID, 10)+"/read", `{"read":false}`); set.status != http.StatusOK {
		t.Fatalf("unset read status = %d, body = %s", set.status, set.body)
	}
	checkTotal("?read=true", 0)
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
