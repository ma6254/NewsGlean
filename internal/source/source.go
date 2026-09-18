// Package source 定义内容来源的采集抽象层。
// 本包是「内容怎么来」与「内容怎么读」之间唯一的接口契约：
// 核心层只认识 Item 与 Connector，不认识任何具体渠道实现。
// 修改本文件的接口属于契约变更，需同步更新 PLAN.md 并说明迁移方式。
package source

import (
	"context"
	"errors"
	"time"
)

// ErrPushUnsupported 表示该渠道只支持拉取模式，不支持 Run 推送模式。
// 核心层捕获此错误后应回退为定时调用 Fetch。
var ErrPushUnsupported = errors.New("push mode not supported")

// Cursor 是拉取进度的不透明游标，由各适配器自行解释：
// RSS 存 ETag/Last-Modified，Telegram 存 update_id，网页爬虫存最后见到的链接与发布时间。
// 核心层只负责持久化与透传，不理解其含义。
type Cursor []byte

// State 是渠道实例的持久化状态，在进程启动时经 Init 回传给适配器。
// 核心层只负责存取，不解析字段含义。
type State struct {
	Cursor Cursor            // 上次拉取的游标
	Extra  map[string]string // 游标之外的附加状态，原样透传
}

// Connector 是一个已配置好的渠道实例。
// 实现方只负责「拿到原始内容并规范化」，不负责去重、过滤、存储。
type Connector interface {
	// Type 返回渠道类型标识，如 "feed"、"webpage"、"telegram"。
	Type() string

	// Validate 在保存配置前校验参数与凭据，尽早报错而不是等到定时任务静默失败。
	Validate(ctx context.Context) error

	// Init 建立长连接或载入游标（例如 Telegram 的 bot 信息、网页源的会话）。
	// 必须可重入：进程重启会再次调用。
	Init(ctx context.Context, state State) error

	// Fetch 拉取一批新条目。游标由本适配器自行解释，
	// 核心层只负责持久化与去重，不理解游标含义。
	// 返回 io.EOF 或空切片表示「暂无新内容」，不算错误。
	Fetch(ctx context.Context, cursor Cursor, limit int) ([]Item, Cursor, error)

	// Run 以推送模式运行，阻塞直到 ctx 取消。
	// 返回 ErrPushUnsupported 表示本渠道只支持拉取，核心层会改为定时调用 Fetch。
	Run(ctx context.Context, emit func(Item) error) error

	// Close 释放适配器持有的资源（长连接、临时文件等）。
	Close() error
}

// Item 是所有渠道都必须收敛到的规范化条目结构。
type Item struct {
	// 身份（按优先级去重：GUID → URL → ContentHash，三者都缺则该条目被丢弃并告警）
	GUID string // 渠道自带的稳定 ID（RSS 的 guid、TG 的 chat_id+message_id）
	URL  string // 条目原始链接

	Title        string    // 标题
	Author       string    // 作者
	PublishedAt  time.Time // 发布时间；缺失时用抓取时间，并置 InferredTime=true
	InferredTime bool      // 发布时间是否由抓取时间推断而来
	Summary      string    // 渠道自带的摘要（如 RSS description），可能为空
	Content      string    // 渠道自带正文；为空则交给正文提取模块去原文取
	ContentType  string    // "text/html" / "text/plain" / "text/markdown"

	SourceID  string            // 归属渠道实例标识
	Tags      []string          // 渠道自带标签（如 Telegram 话题、网页栏目标签）
	Extra     map[string]string // 渠道特有字段，原样透传，不做规范化
	FetchedAt time.Time         // 抓取时间
}
