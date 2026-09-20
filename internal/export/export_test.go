package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ma6254/news-glean/internal/database"
)

// newTestExportDB 装配一个临时数据库并完成 Install（含 FTS 触发器）。
func newTestExportDB(t *testing.T) *database.DB {
	t.Helper()
	tmp, err := os.CreateTemp("", "newsglean-export-*.db")
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
	return db
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"normal":                "normal",
		"a/b\\c:d*e?f\"g<h>i|j": "a_b_c_d_e_f_g_h_i_j",
		"  spaced  ":            "spaced",
		"trailing.":             "trailing",
		"CON":                   "_CON",
		"com1":                  "_com1",
		"...":                   "",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMonthOf(t *testing.T) {
	if got := monthOf(&database.Entry{PublishedAt: "2023-01-02T15:04:05Z"}); got != "2023-01" {
		t.Errorf("published = %q, want 2023-01", got)
	}
	if got := monthOf(&database.Entry{FetchedAt: "2024-03-04T00:00:00Z"}); got != "2024-03" {
		t.Errorf("fetched = %q, want 2024-03", got)
	}
	if got := monthOf(&database.Entry{}); got != "unknown" {
		t.Errorf("empty = %q, want unknown", got)
	}
}

func TestUniqueBase(t *testing.T) {
	used := map[string]bool{}
	if got := uniqueBase(used, "a"); got != "a" {
		t.Errorf("first = %q, want a", got)
	}
	if got := uniqueBase(used, "a"); got != "a-2" {
		t.Errorf("second = %q, want a-2", got)
	}
	if got := uniqueBase(used, "a"); got != "a-3" {
		t.Errorf("third = %q, want a-3", got)
	}
}

// TestExportMarkdown 端到端验证目录结构、front matter、文件名清洗、
// 无 URL 条目不写 url、正文保留 HTML、Content 空用 Summary 兜底、幂等覆盖。
func TestExportMarkdown(t *testing.T) {
	db := newTestExportDB(t)

	if err := db.CreateSource(&database.Source{Name: "源A", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(&database.Source{Name: "源B/", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	idA, idB := sources[0].ID, sources[1].ID

	entries := []*database.Entry{
		{SourceID: idA, Title: "标题一", URL: "https://example.com/1", PublishedAt: "2023-01-02T15:04:05Z", Content: "<p>正文一</p>", ContentType: "text/html", Tags: `["a","b"]`, Extra: `{"k":"v"}`},
		{SourceID: idA, Title: "标题一", URL: "https://example.com/2", PublishedAt: "2023-02-03T10:00:00Z", Content: "正文二"},
		{SourceID: idA, Title: "摘要条目", URL: "https://example.com/3", PublishedAt: "2023-01-10T00:00:00Z", Summary: "这是摘要"},
		{SourceID: idB, Title: "无链接", GUID: "g-1", PublishedAt: "2023-01-05T00:00:00Z", Content: "正文三"},
	}
	for _, e := range entries {
		if err := db.CreateEntry(e); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	res, err := ExportMarkdown(db, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 4 {
		t.Errorf("Total = %d, want 4", res.Total)
	}
	if res.Sources != 2 {
		t.Errorf("Sources = %d, want 2", res.Sources)
	}
	if res.Dir != dir {
		t.Errorf("Dir = %q, want %q", res.Dir, dir)
	}

	// 目录结构：<源>/<YYYY-MM>/<标题>.md，源名含非法字符被清洗。
	files := []string{
		filepath.Join(dir, "源A", "2023-01", "标题一.md"),
		filepath.Join(dir, "源A", "2023-01", "摘要条目.md"),
		filepath.Join(dir, "源A", "2023-02", "标题一.md"),
		filepath.Join(dir, "源B_", "2023-01", "无链接.md"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected file %s: %v", f, err)
		}
	}

	// front matter 关键字段 + 正文保留 HTML
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"---\n",
		"title: 标题一",
		"url: https://example.com/1",
		"source: 源A",
		"content_type: text/html",
		"tags:",
		"- a",
		"- b",
		"extra:",
		"k: v",
		"<p>正文一</p>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("file %s missing %q\n%s", files[0], want, s)
		}
	}

	// 无 URL 条目不写 url 字段
	data3, err := os.ReadFile(files[3])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data3), "url:") {
		t.Errorf("no-URL entry should not contain url field, got:\n%s", data3)
	}

	// Content 为空时用 Summary 兜底
	data2, err := os.ReadFile(files[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data2), "这是摘要") {
		t.Errorf("summary fallback missing in %s:\n%s", files[1], data2)
	}

	// 幂等：再次导出覆盖，结果一致
	res2, err := ExportMarkdown(db, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Total != 4 || res2.Sources != 2 {
		t.Errorf("second export: %+v, want total=4 sources=2", res2)
	}
}

// TestExportMarkdownFilter 验证过滤参数只导出符合条件的条目。
func TestExportMarkdownFilter(t *testing.T) {
	db := newTestExportDB(t)

	if err := db.CreateSource(&database.Source{Name: "s1", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(&database.Source{Name: "s2", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	idA, idB := sources[0].ID, sources[1].ID

	for _, e := range []*database.Entry{
		{SourceID: idA, Title: "a1", URL: "https://e/1", PublishedAt: "2023-01-01T00:00:00Z", Content: "x"},
		{SourceID: idB, Title: "b1", URL: "https://e/2", PublishedAt: "2023-01-01T00:00:00Z", Content: "x"},
	} {
		if err := db.CreateEntry(e); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	res, err := ExportMarkdown(db, dir, Options{Filter: database.EntryFilter{SourceID: idA}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
	if res.Sources != 1 {
		t.Errorf("Sources = %d, want 1", res.Sources)
	}
	if _, err := os.Stat(filepath.Join(dir, "s1", "2023-01", "a1.md")); err != nil {
		t.Errorf("expected s1/a1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "s2", "2023-01", "b1.md")); err == nil {
		t.Error("s2/b1 should not be exported when filtered to s1")
	}
}
