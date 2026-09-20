package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportMarkdownAPI 验证导出端点的端到端链路：
// 加源 → 采集 → 导出，返回统计正确且文件真实落盘。
func TestExportMarkdownAPI(t *testing.T) {
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

	exp := doJSON(t, http.MethodPost, ts.URL+"/api/export/markdown", "")
	if exp.status != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exp.status, exp.body)
	}
	var res struct {
		Total   int    `json:"total"`
		Sources int    `json:"sources"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(exp.body, &res); err != nil {
		t.Fatalf("unmarshal export result: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	if res.Sources != 1 {
		t.Fatalf("sources = %d, want 1", res.Sources)
	}
	if res.Dir == "" {
		t.Fatal("dir should be non-empty")
	}

	// rssBody 两条条目发布时间均为 2023-01
	f := filepath.Join(res.Dir, "test", "2023-01", "Post One.md")
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("expected exported file %s: %v", f, err)
	}
}

// TestExportJSONAPI 验证 JSON 导出端点的端到端链路。
func TestExportJSONAPI(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	if add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`); add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}
	if ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}

	exp := doJSON(t, http.MethodPost, ts.URL+"/api/export/json", "")
	if exp.status != http.StatusOK {
		t.Fatalf("export json status = %d, body = %s", exp.status, exp.body)
	}
	var res struct {
		Total   int    `json:"total"`
		Sources int    `json:"sources"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(exp.body, &res); err != nil {
		t.Fatalf("unmarshal export json result: %v", err)
	}
	if res.Total != 2 || res.Sources != 1 {
		t.Fatalf("result = %+v, want total=2 sources=1", res)
	}

	data, err := os.ReadFile(filepath.Join(res.Dir, "entries.json"))
	if err != nil {
		t.Fatalf("read entries.json: %v", err)
	}
	var items []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshal entries.json: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
}

// TestExportEPUBAPI 验证 EPUB 导出端点的端到端链路：zip 结构合法。
func TestExportEPUBAPI(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	if add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`); add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}
	if ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}

	exp := doJSON(t, http.MethodPost, ts.URL+"/api/export/epub", "")
	if exp.status != http.StatusOK {
		t.Fatalf("export epub status = %d, body = %s", exp.status, exp.body)
	}
	var res struct {
		Total   int    `json:"total"`
		Sources int    `json:"sources"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(exp.body, &res); err != nil {
		t.Fatalf("unmarshal export epub result: %v", err)
	}
	if res.Total != 2 || res.Sources != 1 {
		t.Fatalf("result = %+v, want total=2 sources=1", res)
	}

	r, err := zip.OpenReader(filepath.Join(res.Dir, "test.epub"))
	if err != nil {
		t.Fatalf("open epub: %v", err)
	}
	defer r.Close()
	if len(r.File) == 0 || r.File[0].Name != "mimetype" {
		t.Fatalf("first entry = %v, want mimetype", r.File)
	}
	if r.File[0].Method != zip.Store {
		t.Errorf("mimetype method = %d, want Store", r.File[0].Method)
	}
}

// TestExportDownloadAPI 验证下载端点：json→单文件、markdown→zip、epub→单文件、非法 format→400。
func TestExportDownloadAPI(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	if add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`); add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}
	if ref := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); ref.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", ref.status, ref.body)
	}

	get := func(url string) (*http.Response, []byte) {
		t.Helper()
		res, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res, data
	}

	// JSON：单文件，Content-Type application/json
	res, data := get(ts.URL + "/api/export/download?format=json")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("json status = %d, body = %s", res.StatusCode, data)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("json content-type = %q, want application/json", ct)
	}
	var items []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshal downloaded json: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	// Markdown：打包 zip，含 .md 文件
	res, data = get(ts.URL + "/api/export/download?format=markdown")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("markdown status = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("markdown content-type = %q, want application/zip", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("markdown zip: %v", err)
	}
	foundMD := false
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".md") {
			foundMD = true
		}
	}
	if !foundMD {
		t.Error("markdown zip should contain .md files")
	}

	// EPUB：单源 → 单文件，Content-Type application/epub+zip
	res, data = get(ts.URL + "/api/export/download?format=epub")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("epub status = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/epub+zip" {
		t.Errorf("epub content-type = %q, want application/epub+zip", ct)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "test.epub") {
		t.Errorf("epub content-disposition = %q, want test.epub", cd)
	}
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("epub body should be a zip: %v", err)
	}

	// 非法 format → 400
	res, data = get(ts.URL + "/api/export/download?format=nope")
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid format status = %d, want 400 (body=%s)", res.StatusCode, data)
	}
}
