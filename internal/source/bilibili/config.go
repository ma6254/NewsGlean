// Package bilibili 实现「B 站个人数据」渠道的拉取适配器。
// 通过子进程调用 bilibili-cli（命令名 `bili`）取数，把 B 站内容规范化成 source.Item。
// 与 RSSHub 互补：RSSHub 覆盖公开路由（UP 主投稿/热门/排行），本渠道覆盖需要登录的
// 个人数据（观看历史 / 收藏夹）与后续的字幕全文回填。
package bilibili

import (
	"encoding/json"
	"fmt"

	"github.com/ma6254/news-glean/internal/source"
)

// 渠道模式。
const (
	ModeHistory   = "history"   // 我的观看历史
	ModeFavorites = "favorites" // 我的收藏夹（需 fav_id）
)

// Config 是 bilibili 渠道的配置。
type Config struct {
	Mode        string `json:"mode"`         // 采集模式：history | favorites
	FavID       string `json:"fav_id"`       // favorites 模式的收藏夹 ID
	BiliPath    string `json:"bili_path"`    // 可选：bili 可执行文件路径覆盖
	FetchDetail bool   `json:"fetch_detail"` // 逐条回填视频简介（N+1 调用，较慢）
}

// New 根据配置 JSON 构造 bilibili 渠道实例。
// 不在此处校验 bili 是否安装：未安装时允许保存，由 CheckEnv 报「未就绪」、Fetch 报明确错误。
func New(configJSON string, _ source.CreateOptions) (source.Connector, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("bilibili: invalid config: %w", err)
	}
	return &connector{
		mode:        cfg.Mode,
		favID:       cfg.FavID,
		cmd:         resolveBili(cfg.BiliPath),
		fetchDetail: cfg.FetchDetail,
	}, nil
}
