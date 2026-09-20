package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ma6254/news-glean/internal/database"
)

// ExportEPUB 把满足条件的条目按源导出为 EPUB（每源一本，按月份分章）。
// 容器用标准库 archive/zip 自写，无外部依赖；重复导出覆盖同名文件（幂等）。
func ExportEPUB(db *database.DB, dir string, opts Options) (*Result, error) {
	if dir == "" {
		dir = "./export-epub"
	}

	sources, err := db.ListSources()
	if err != nil {
		return nil, err
	}
	nameByID := make(map[uint64]string, len(sources))
	for i := range sources {
		nameByID[sources[i].ID] = sources[i].Name
	}

	entries, err := db.ListAllEntries(opts.Filter)
	if err != nil {
		return nil, err
	}

	// 按源分组（entries 已按 source_id 升序、published_at 降序，组内顺序确定）。
	bySource := make(map[uint64][]*database.Entry)
	for i := range entries {
		e := &entries[i]
		bySource[e.SourceID] = append(bySource[e.SourceID], e)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for sourceID, list := range bySource {
		srcName := nameByID[sourceID]
		if srcName == "" {
			srcName = fmt.Sprintf("source-%d", sourceID)
		}
		base := sanitizeName(srcName)
		if base == "" {
			base = fmt.Sprintf("source-%d", sourceID)
		}
		if err := writeEPUB(filepath.Join(dir, base+".epub"), sourceID, srcName, list); err != nil {
			return nil, err
		}
	}

	return &Result{Total: len(entries), Sources: len(bySource), Dir: dir}, nil
}

// monthChapter 是 EPUB 的一个章节（按月份分组）。
type monthChapter struct {
	Month   string            // 月份（YYYY-MM 或 unknown）
	Entries []*database.Entry // 该月条目，保持来源顺序
}

// groupByMonth 把同源条目按月份分组，保持月份首次出现的顺序
// （entries 已按 published_at 降序，故章节从新到旧）。
func groupByMonth(entries []*database.Entry) []monthChapter {
	var order []string
	byMonth := make(map[string][]*database.Entry)
	for _, e := range entries {
		m := monthOf(e)
		if _, ok := byMonth[m]; !ok {
			order = append(order, m)
		}
		byMonth[m] = append(byMonth[m], e)
	}
	chapters := make([]monthChapter, 0, len(order))
	for _, m := range order {
		chapters = append(chapters, monthChapter{Month: m, Entries: byMonth[m]})
	}
	return chapters
}

// writeEPUB 把同一源的一批条目写成一个 EPUB 文件（书名=源名，章节按月份）。
func writeEPUB(path string, sourceID uint64, title string, entries []*database.Entry) error {
	chapters := groupByMonth(entries)
	chapterFiles := make([]string, len(chapters))
	for i, ch := range chapters {
		chapterFiles[i] = fmt.Sprintf("chapter-%s.xhtml", ch.Month)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)

	// 1) mimetype 必须是第一个条目，且不压缩（EPUB 规范要求）。
	mh := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mw, err := w.CreateHeader(mh)
	if err != nil {
		return err
	}
	if _, err := mw.Write([]byte("application/epub+zip")); err != nil {
		return err
	}

	uid := fmt.Sprintf("news-glean-source-%d", sourceID)

	// 2) META-INF/container.xml 指向 OPF。
	container := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`
	if err := writeZipFile(w, "META-INF/container.xml", []byte(container)); err != nil {
		return err
	}

	// 3) OEBPS/content.opf
	if err := writeZipFile(w, "OEBPS/content.opf", []byte(buildOPF(title, uid, chapters, chapterFiles))); err != nil {
		return err
	}

	// 4) OEBPS/toc.ncx（EPUB2 兼容目录）
	if err := writeZipFile(w, "OEBPS/toc.ncx", []byte(buildNCX(title, uid, chapters, chapterFiles))); err != nil {
		return err
	}

	// 5) OEBPS/nav.xhtml（EPUB3 导航）
	if err := writeZipFile(w, "OEBPS/nav.xhtml", []byte(buildNav(title, chapters, chapterFiles))); err != nil {
		return err
	}

	// 6) 各月份章节
	for i, ch := range chapters {
		if err := writeZipFile(w, "OEBPS/"+chapterFiles[i], []byte(buildChapter(ch))); err != nil {
			return err
		}
	}

	return w.Close()
}

// writeZipFile 往 zip 里写入一个普通文件（deflate 压缩）。
func writeZipFile(w *zip.Writer, name string, data []byte) error {
	fw, err := w.Create(name)
	if err != nil {
		return err
	}
	_, err = fw.Write(data)
	return err
}

// buildOPF 生成 EPUB 的包清单（metadata + manifest + spine）。
func buildOPF(title, uid string, chapters []monthChapter, chapterFiles []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">` + "\n")
	b.WriteString(`  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">` + "\n")
	fmt.Fprintf(&b, "    <dc:identifier id=\"bookid\">%s</dc:identifier>\n", xmlEscape(uid))
	fmt.Fprintf(&b, "    <dc:title>%s</dc:title>\n", xmlEscape(title))
	b.WriteString("    <dc:language>zh</dc:language>\n")
	b.WriteString(`  </metadata>` + "\n")
	b.WriteString(`  <manifest>` + "\n")
	b.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	b.WriteString(`    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + "\n")
	for i, cf := range chapterFiles {
		fmt.Fprintf(&b, "    <item id=\"ch%d\" href=\"%s\" media-type=\"application/xhtml+xml\"/>\n", i+1, xmlEscape(cf))
	}
	b.WriteString(`  </manifest>` + "\n")
	b.WriteString(`  <spine toc="ncx">` + "\n")
	for i := range chapterFiles {
		fmt.Fprintf(&b, "    <itemref idref=\"ch%d\"/>\n", i+1)
	}
	b.WriteString(`  </spine>` + "\n")
	b.WriteString(`</package>` + "\n")
	return b.String()
}

// buildNCX 生成 EPUB2 兼容的目录（navMap 一项对应一个月份章节）。
func buildNCX(title, uid string, chapters []monthChapter, chapterFiles []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">` + "\n")
	b.WriteString(`  <head>` + "\n")
	fmt.Fprintf(&b, "    <meta name=\"dtb:uid\" content=\"%s\"/>\n", xmlEscape(uid))
	b.WriteString(`  </head>` + "\n")
	fmt.Fprintf(&b, "  <docTitle><text>%s</text></docTitle>\n", xmlEscape(title))
	b.WriteString(`  <navMap>` + "\n")
	for i, cf := range chapterFiles {
		fmt.Fprintf(&b,
			"    <navPoint id=\"ch%d\" playOrder=\"%d\"><navLabel><text>%s</text></navLabel><content src=\"%s\"/></navPoint>\n",
			i+1, i+1, xmlEscape(chapters[i].Month), xmlEscape(cf))
	}
	b.WriteString(`  </navMap>` + "\n")
	b.WriteString(`</ncx>` + "\n")
	return b.String()
}

// buildNav 生成 EPUB3 的导航文档（XHTML nav）。
func buildNav(title string, chapters []monthChapter, chapterFiles []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">` + "\n")
	fmt.Fprintf(&b, "<head><title>%s</title></head>\n", xmlEscape(title))
	b.WriteString(`<body>` + "\n")
	b.WriteString(`<nav epub:type="toc" id="toc">` + "\n")
	fmt.Fprintf(&b, "<h1>%s</h1>\n", xmlEscape(title))
	b.WriteString(`<ol>` + "\n")
	for i, cf := range chapterFiles {
		fmt.Fprintf(&b, "<li><a href=\"%s\">%s</a></li>\n", xmlEscape(cf), xmlEscape(chapters[i].Month))
	}
	b.WriteString(`</ol>` + "\n")
	b.WriteString(`</nav>` + "\n")
	b.WriteString(`</body>` + "\n")
	b.WriteString(`</html>` + "\n")
	return b.String()
}

// buildChapter 生成一个月份章节的 XHTML：该月条目按序排列，每条一个 <article>。
func buildChapter(ch monthChapter) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml">` + "\n")
	fmt.Fprintf(&b, "<head><title>%s</title></head>\n", xmlEscape(ch.Month))
	b.WriteString(`<body>` + "\n")
	fmt.Fprintf(&b, "<h1>%s</h1>\n", xmlEscape(ch.Month))
	for _, e := range ch.Entries {
		b.WriteString(`<article>` + "\n")
		fmt.Fprintf(&b, "<h2>%s</h2>\n", xmlEscape(e.Title))
		var meta []string
		if e.Author != "" {
			meta = append(meta, xmlEscape(e.Author))
		}
		if e.PublishedAt != "" {
			meta = append(meta, xmlEscape(e.PublishedAt))
		}
		if len(meta) > 0 {
			fmt.Fprintf(&b, "<p class=\"meta\">%s</p>\n", strings.Join(meta, " · "))
		}
		if e.URL != "" {
			fmt.Fprintf(&b, "<p class=\"meta\"><a href=\"%s\">%s</a></p>\n", xmlEscape(e.URL), xmlEscape(e.URL))
		}
		if body := bodyToXHTML(e); body != "" {
			b.WriteString(body + "\n")
		}
		b.WriteString(`</article>` + "\n")
	}
	b.WriteString(`</body>` + "\n")
	b.WriteString(`</html>` + "\n")
	return b.String()
}

// bodyToXHTML 把条目正文转为可嵌入章节的 XHTML 片段。
// Content 为空时用 Summary 兜底；HTML 正文做最小 XHTML 化，纯文本转义后按 <p> 包裹。
func bodyToXHTML(e *database.Entry) string {
	body := e.Content
	if body == "" {
		body = e.Summary
	}
	if body == "" {
		return ""
	}
	if e.ContentType == "text/html" || looksLikeHTML(body) {
		return htmlToXHTML(body)
	}
	return textToXHTML(body)
}

// htmlTagRe 粗略匹配 HTML 标签，用于判断文本是否含 HTML 结构。
var htmlTagRe = regexp.MustCompile(`<[a-zA-Z][^>]*>`)

// looksLikeHTML 粗略判断文本是否含 HTML 标签。
func looksLikeHTML(s string) bool {
	return htmlTagRe.MatchString(s)
}

// htmlToXHTML 把 HTML 正文做最小 XHTML 化：void 元素补自闭合、裸 & 转义。
// 属「尽力而为」：不做完整良构校验，非良构正文可能无法被严格阅读器解析；
// 完整清洗留到阶段 12 正文提取后增强。
func htmlToXHTML(s string) string {
	return selfCloseVoid(escapeBareAmp(s))
}

// textToXHTML 把纯文本转义后按 <p> 包裹，换行转 <br/>。
func textToXHTML(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	b.WriteString("<p>")
	for i, line := range lines {
		if i > 0 {
			b.WriteString("<br/>")
		}
		b.WriteString(xmlEscape(line))
	}
	b.WriteString("</p>")
	return b.String()
}

// entityRe 匹配合法的 HTML 实体引用（&amp;、&#123;、&#x1F; 等），用于保护已有实体。
var entityRe = regexp.MustCompile(`^(#x?[0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);`)

// escapeBareAmp 把裸 & 转义为 &amp;，保留已有实体引用不变。
func escapeBareAmp(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '&' {
			if m := entityRe.FindString(s[i+1:]); m != "" {
				b.WriteByte('&')
				b.WriteString(m)
				i += 1 + len(m)
				continue
			}
			b.WriteString("&amp;")
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// voidTagRe 匹配 HTML void 元素（无闭合标签），用于补自闭合斜杠。
var voidTagRe = regexp.MustCompile(`<(br|hr|img|input|meta|link|source|area|base|col|embed|param|track|wbr)([^>]*?)/?>`)

// selfCloseVoid 把 void 元素补成自闭合（<br> → <br/>、<img ...> → <img .../>），
// 已自闭合的不变。非 void 元素（div/p 等）不处理，依赖原 HTML 已闭合。
func selfCloseVoid(s string) string {
	return voidTagRe.ReplaceAllString(s, "<$1$2/>")
}

// xmlEscape 转义文本字段中的 XML 特殊字符。
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
