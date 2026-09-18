package source

import (
	"context"
	"errors"
)

// ErrProbeUnsupported 表示该渠道类型不支持「保存前探测元信息」。
// 核心层捕获此错误后应向调用方说明该能力不可用。
var ErrProbeUnsupported = errors.New("probe not supported")

// ProbeInfo 是渠道在保存前可探测到的元信息（供前端「自动获取」等交互使用）。
type ProbeInfo struct {
	Title string `json:"title"` // 渠道标题（如 feed 的 <title>），可能为空
}

// Prober 是 Connector 的可选能力：探测渠道元信息（如 feed 标题）。
// 实现方只需在能低成本获取元信息时实现；不实现则核心层返回 ErrProbeUnsupported。
type Prober interface {
	// Probe 探测渠道元信息。实现应复用与 Fetch 一致的抓取逻辑与超时约束。
	Probe(ctx context.Context) (ProbeInfo, error)
}
