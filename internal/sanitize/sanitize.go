// Package sanitize 提供条目 HTML 的白名单净化（BUG-001）。
//
// 订阅源（feed/webpage 及未来渠道）的正文是不可信输入：恶意或被劫持的源
// 可在正文中夹带 <script>、on* 事件属性、javascript: URL 等载荷，前端详情页
// 以 innerHTML 渲染即构成存储型 XSS。本包在入库前统一做白名单净化。
//
// 策略取 bluemonday UGCPolicy（面向富文本/UGC 的白名单），并满足本项目约束：
//   - 保留 <img src>（图墙封面三级兜底与正文图片查看器 PicViewer 依赖，
//     见 view-modes.md D1），保留标题/段落/列表/表格/引用/链接等阅读结构；
//   - 剥除 script/style（含内容）、iframe/svg/object 等嵌入元素与全部表单元素，
//     on* 事件属性与 style 属性一律不在白名单内；
//   - URL 仅允许 http/https/mailto 与相对地址，javascript:/data: 等被丢弃；
//   - 链接自动补 rel="nofollow"（UGCPolicy 默认行为，对渲染无影响）。
package sanitize

import "github.com/microcosm-cc/bluemonday"

// policy 是条目净化策略。bluemonday Policy 构建后为只读、可并发使用，
// 包级变量随进程初始化一次，Sanitize 调用无锁开销。
var policy = bluemonday.UGCPolicy()

// HTML 净化一段 HTML 片段，返回白名单内的安全子集。
// 空串原样返回；未许可的标签被剥除（script/style 连内容一起丢弃），
// 其余未许可元素仅剥标签、保留已转义的文本；事件属性与 style 属性被移除。
// 注意：入参应为 HTML 片段；纯文本中的 < > & 会被转义，
// 调用方需按 ContentType（text/html）区分，不要对纯文本调用。
func HTML(s string) string {
	if s == "" {
		return ""
	}
	return policy.Sanitize(s)
}
