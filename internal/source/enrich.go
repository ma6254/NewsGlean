package source

import "context"

// Enricher 是 Connector 的可选能力：为单条「新」条目回填详情（如视频简介/字幕）。
// 核心层在判定条目为新、写库前调用一次；已存在条目不会触发，从而避免 N+1 重复回填。
type Enricher interface {
	// Enrich 回填条目详情，返回（可能已增强的）条目。
	// 实现应尽力而为：回填失败时返回原条目即可，不阻断采集。
	Enrich(ctx context.Context, item Item) (Item, error)
}
