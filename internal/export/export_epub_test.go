package export

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ma6254/news-glean/internal/database"
)

// TestExportEPUB 验证 EPUB 的 zip 结构：mimetype 首项未压缩、清单文件齐全、
// 章节按月生成、正文最小 XHTML 化（<br> → <br/>）。
func TestExportEPUB(t *testing.T) {
	db := newTestExportDB(t)

	if err := db.CreateSource(&database.Source{Name: "源A", Type: database.SourceTypeFeed}); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	idA := sources[0].ID

	for _, e := range []*database.Entry{
		{SourceID: idA, Title: "标题一", URL: "https://example.com/1", PublishedAt: "2023-01-02T15:04:05Z", Content: "<p>正文一</p><br>换行", ContentType: "text/html"},
		{SourceID: idA, Title: "标题二", URL: "https://example.com/2", PublishedAt: "2023-02-03T10:00:00Z", Content: "纯文本正文"},
	} {
		if err := db.CreateEntry(e); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	res, err := ExportEPUB(db, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Sources != 1 {
		t.Errorf("result = %+v, want total=2 sources=1", res)
	}

	r, err := zip.OpenReader(filepath.Join(dir, "源A.epub"))
	if err != nil {
		t.Fatalf("open epub: %v", err)
	}
	defer r.Close()

	names := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		names[f.Name] = f
	}

	// mimetype 必须第一个条目且未压缩
	if len(r.File) == 0 || r.File[0].Name != "mimetype" {
		t.Fatalf("first entry = %v, want mimetype", r.File)
	}
	if r.File[0].Method != zip.Store {
		t.Errorf("mimetype method = %d, want Store", r.File[0].Method)
	}
	if got := string(readZipFile(t, r.File[0])); got != "application/epub+zip" {
		t.Errorf("mimetype content = %q", got)
	}

	// 关键清单文件齐全
	for _, want := range []string{"META-INF/container.xml", "OEBPS/content.opf", "OEBPS/toc.ncx", "OEBPS/nav.xhtml"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing %s in epub", want)
		}
	}
	// 两个月份章节
	if _, ok := names["OEBPS/chapter-2023-01.xhtml"]; !ok {
		t.Error("missing chapter-2023-01.xhtml")
	}
	if _, ok := names["OEBPS/chapter-2023-02.xhtml"]; !ok {
		t.Error("missing chapter-2023-02.xhtml")
	}

	// opf 的 manifest/spine 引用章节与 nav/ncx
	opf := string(readZipFile(t, names["OEBPS/content.opf"]))
	for _, want := range []string{"chapter-2023-01.xhtml", "chapter-2023-02.xhtml", "toc.ncx", "nav.xhtml"} {
		if !strings.Contains(opf, want) {
			t.Errorf("opf missing reference to %s", want)
		}
	}

	// 正文 XHTML 化：void 元素自闭合、HTML 保留、纯文本转义包裹
	ch1 := string(readZipFile(t, names["OEBPS/chapter-2023-01.xhtml"]))
	for _, want := range []string{"<p>正文一</p>", "<br/>", "<h2>标题一</h2>"} {
		if !strings.Contains(ch1, want) {
			t.Errorf("chapter-2023-01 missing %q\n%s", want, ch1)
		}
	}
	ch2 := string(readZipFile(t, names["OEBPS/chapter-2023-02.xhtml"]))
	if !strings.Contains(ch2, "<p>纯文本正文</p>") {
		t.Errorf("chapter-2023-02 should wrap plain text in <p>\n%s", ch2)
	}
}

// readZipFile 读取 zip 中单个文件的内容。
func readZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestEPUBBodyXHTML 直接验证正文 XHTML 化的边界情况。
func TestEPUBBodyXHTML(t *testing.T) {
	// HTML：void 自闭合 + 裸 & 转义，已有实体保留
	html := `a & b <br> <img src="x"> &amp; &#123;`
	got := htmlToXHTML(html)
	for _, want := range []string{"a &amp; b", "<br/>", `<img src="x"/>`, "&amp;", "&#123;"} {
		if !strings.Contains(got, want) {
			t.Errorf("htmlToXHTML missing %q in %q", want, got)
		}
	}

	// 纯文本：转义后 <p> 包裹，换行转 <br/>
	txt := bodyToXHTML(&database.Entry{Content: "第一行\n第二行 < 第三行"})
	if !strings.Contains(txt, "<p>第一行<br/>第二行 &lt; 第三行</p>") {
		t.Errorf("textToXHTML = %q", txt)
	}

	// Content 空用 Summary 兜底
	fallback := bodyToXHTML(&database.Entry{Summary: "摘要", ContentType: "text/plain"})
	if !strings.Contains(fallback, "<p>摘要</p>") {
		t.Errorf("summary fallback = %q", fallback)
	}
}
