package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/source"
)

// TestIngestItemSanitizesHTML 验证 BUG-001 修复：text/html 条目经 ingestItem
// 入库后，Content/Summary 不含 on* 事件属性、script/iframe 标签等 XSS 载荷，
// 而合法阅读结构（<img src>、链接、段落）保留。
func TestIngestItemSanitizesHTML(t *testing.T) {
	a := newTestApp(t)

	item := source.Item{
		GUID:        "guid-xss-1",
		URL:         "https://example.com/xss-1",
		Title:       "恶意条目",
		ContentType: "text/html",
		Content: `<p>正常段落</p>` +
			`<img src="x.png" onerror="fetch('/api/source',{method:'DELETE'})">` +
			`<script>alert(1)</script>` +
			`<a href="javascript:alert(1)">链接</a>`,
		Summary: `摘要<img src=x onerror=alert(1)>`,
	}

	id, ok, err := a.ingestItem(1, item)
	if err != nil {
		t.Fatalf("ingestItem: %v", err)
	}
	if !ok {
		t.Fatal("ingestItem 未入库（ok=false），期望入库")
	}

	entry, err := a.db.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}

	for _, field := range []struct{ name, val string }{
		{"Content", entry.Content},
		{"Summary", entry.Summary},
	} {
		lower := strings.ToLower(field.val)
		for _, bad := range []string{"<script", "onerror", "onload", "javascript:", "<iframe", "style="} {
			if strings.Contains(lower, bad) {
				t.Fatalf("入库 %s 仍含危险片段 %q: %s", field.name, bad, field.val)
			}
		}
	}
	for _, want := range []string{"<p>正常段落</p>", `src="x.png"`, "链接"} {
		if !strings.Contains(entry.Content, want) {
			t.Errorf("入库 Content 丢失合法片段 %q: %s", want, entry.Content)
		}
	}
}

// TestIngestItemKeepsPlainText 验证 text/plain 条目不做净化（原文保留，
// 前端以 <pre> 文本渲染、React 转义，无 XSS 面）。
func TestIngestItemKeepsPlainText(t *testing.T) {
	a := newTestApp(t)

	raw := `代码示例: a < b && c > d`
	item := source.Item{
		GUID:        "guid-plain-1",
		URL:         "https://example.com/plain-1",
		Title:       "纯文本条目",
		ContentType: "text/plain",
		Content:     raw,
	}

	id, ok, err := a.ingestItem(2, item)
	if err != nil {
		t.Fatalf("ingestItem: %v", err)
	}
	if !ok {
		t.Fatal("ingestItem 未入库（ok=false），期望入库")
	}
	entry, err := a.db.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Content != raw {
		t.Fatalf("text/plain Content 被改写: %q", entry.Content)
	}
}

// TestIngestItemSanitizedContentHashDeterministic 验证净化发生在内容指纹
// 计算之前：同一载荷（净化后等价）重复采集会命中 ContentHash 去重。
func TestIngestItemSanitizedContentHashDeterministic(t *testing.T) {
	a := newTestApp(t)

	makeItem := func(guid string) source.Item {
		return source.Item{
			GUID:        guid,
			URL:         "https://example.com/" + guid,
			Title:       "同内容条目",
			ContentType: "text/html",
			Content:     `<p>同一段正文</p><img src=x onerror=alert(1)>`,
		}
	}

	if _, ok, err := a.ingestItem(3, makeItem("guid-hash-a")); err != nil || !ok {
		t.Fatalf("首次入库失败: ok=%v err=%v", ok, err)
	}
	// 不同 GUID/URL、仅事件属性差异：净化后内容一致 → ContentHash 去重跳过
	_, ok, err := a.ingestItem(3, makeItem("guid-hash-b"))
	if err != nil {
		t.Fatalf("二次入库出错: %v", err)
	}
	if ok {
		t.Fatal("净化后等价内容未被 ContentHash 去重（ok=true），期望跳过")
	}
}

// TestIngestItemDedupByGUIDAfterSanitize 常规回归：GUID 去重不受净化影响。
func TestIngestItemDedupByGUIDAfterSanitize(t *testing.T) {
	a := newTestApp(t)
	item := source.Item{
		GUID:        "guid-dup-1",
		URL:         "https://example.com/dup",
		Title:       "去重条目",
		ContentType: "text/html",
		Content:     "<p>正文</p>",
	}
	if _, ok, err := a.ingestItem(4, item); err != nil || !ok {
		t.Fatalf("首次入库失败: ok=%v err=%v", ok, err)
	}
	if _, ok, err := a.ingestItem(4, item); err != nil || ok {
		t.Fatalf("GUID 重复未去重: ok=%v err=%v", ok, err)
	}
}

// TestRefreshOneSanitizesFeedContent 端到端回归（BUG-001 复现路径）：
// 构造含 onerror/<script>/javascript:/iframe 载荷的 RSS 源，经真实 HTTP 采集
// 入库后，Content/Summary 均已净化，合法阅读结构（段落、<img src>、链接文本）保留。
func TestRefreshOneSanitizesFeedContent(t *testing.T) {
	// 绕过环境代理，保证对 httptest 回环地址直连
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")

	payload := `<p>正文段落</p>` +
		`<img src="pic.png" onerror="fetch('/api/source',{method:'DELETE'})">` +
		`<script>alert(1)</script>` +
		`<a href="javascript:alert(2)">链接</a>` +
		`<iframe srcdoc="<script>alert(3)</script>"></iframe>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>malicious</title>
<item><title>触发条目</title><link>https://evil.example/post-1</link><guid>evil-1</guid>
<description><![CDATA[%s]]></description></item>
</channel></rss>`, payload)
	}))
	defer srv.Close()

	a := newTestApp(t)
	cfg, err := json.Marshal(map[string]string{"url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	src, err := a.AddSource("malicious", database.SourceTypeFeed, string(cfg), 60, true)
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	res, err := a.RefreshSource(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if res.Inserted != 1 || len(res.Errors) > 0 {
		t.Fatalf("refresh result = %+v, want 1 inserted / 0 errors", res)
	}

	list, _, err := a.db.ListEntries(1, 10, database.EntryFilter{SourceID: src.ID})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("entries = %d, want 1", len(list))
	}
	e := list[0]
	for _, field := range []struct{ name, val string }{
		{"Content", e.Content},
		{"Summary", e.Summary},
	} {
		lower := strings.ToLower(field.val)
		for _, bad := range []string{"<script", "onerror", "onload", "javascript:", "<iframe", "srcdoc", "style="} {
			if strings.Contains(lower, bad) {
				t.Fatalf("入库 %s 仍含危险片段 %q: %s", field.name, bad, field.val)
			}
		}
	}
	for _, want := range []string{"<p>正文段落</p>", `src="pic.png"`, "链接"} {
		if !strings.Contains(e.Content, want) {
			t.Errorf("入库 Content 丢失合法片段 %q: %s", want, e.Content)
		}
	}
}
