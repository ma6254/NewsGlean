package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ma6254/news-glean/internal/database"
)

// TestExportJSON 验证 JSON 导出的单文件结构、tags/extra 展开、无 URL 省略、幂等覆盖。
func TestExportJSON(t *testing.T) {
	db := newTestExportDB(t)

	if err := db.CreateSource(&database.Source{Name: "源A", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(&database.Source{Name: "源B", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	idA, idB := sources[0].ID, sources[1].ID

	for _, e := range []*database.Entry{
		{SourceID: idA, Title: "标题一", URL: "https://example.com/1", PublishedAt: "2023-01-02T15:04:05Z", Content: "正文", Tags: `["a","b"]`, Extra: `{"k":"v"}`},
		{SourceID: idB, Title: "无链接", GUID: "g-1", PublishedAt: "2023-01-05T00:00:00Z", Content: "正文三"},
	} {
		if err := db.CreateEntry(e); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	res, err := ExportJSON(db, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Sources != 2 {
		t.Errorf("result = %+v, want total=2 sources=2", res)
	}

	data, err := os.ReadFile(filepath.Join(dir, "entries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var items []EntryJSON
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshal entries.json: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	var withURL, noURL *EntryJSON
	for i := range items {
		if items[i].Title == "标题一" {
			withURL = &items[i]
		}
		if items[i].Title == "无链接" {
			noURL = &items[i]
		}
	}
	if withURL == nil || noURL == nil {
		t.Fatalf("entries not found: %+v", items)
	}
	if withURL.Source != "源A" || withURL.URL != "https://example.com/1" {
		t.Errorf("withURL fields: %+v", withURL)
	}
	if len(withURL.Tags) != 2 || withURL.Tags[0] != "a" || withURL.Tags[1] != "b" {
		t.Errorf("tags = %v, want [a b]", withURL.Tags)
	}
	if withURL.Extra["k"] != "v" {
		t.Errorf("extra = %v, want k=v", withURL.Extra)
	}
	if noURL.URL != "" {
		t.Errorf("no-URL entry should have empty url, got %q", noURL.URL)
	}

	// 幂等覆盖
	if _, err := ExportJSON(db, dir, Options{}); err != nil {
		t.Fatal(err)
	}
}

// TestExportJSONFilter 验证过滤参数只导出符合条件的条目。
func TestExportJSONFilter(t *testing.T) {
	db := newTestExportDB(t)

	if err := db.CreateSource(&database.Source{Name: "s1", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(&database.Source{Name: "s2", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	sources, _ := db.ListSources()
	idA, idB := sources[0].ID, sources[1].ID
	for _, e := range []*database.Entry{
		{SourceID: idA, Title: "a1", URL: "https://e/1", Content: "x"},
		{SourceID: idB, Title: "b1", URL: "https://e/2", Content: "x"},
	} {
		if err := db.CreateEntry(e); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	res, err := ExportJSON(db, dir, Options{Filter: database.EntryFilter{SourceID: idA}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
	data, err := os.ReadFile(filepath.Join(dir, "entries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var items []EntryJSON
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "a1" {
		t.Errorf("filtered items = %+v, want [a1]", items)
	}
}
