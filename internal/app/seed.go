// Package app 是应用服务层：编排采集、三层去重、写库。
package app

import (
	"encoding/json"

	"github.com/ma6254/news-glean/internal/database"
)

// defaultSource 是首次安装时预置的默认渠道描述。
type defaultSource struct {
	Name     string // 显示名
	URL      string // feed 地址
	Interval int    // 刷新间隔（秒）
}

// defaultSources 是开箱即用的默认订阅源。
// 仅在全新数据库（sources 表为空）时写入一次，不会覆盖用户已有渠道。
var defaultSources = []defaultSource{
	{Name: "阮一峰的网络日志", URL: "https://feeds.feedburner.com/ruanyifeng", Interval: 3600},
	{Name: "少数派", URL: "https://sspai.com/feed", Interval: 3600},
	{Name: "Hacker News", URL: "https://news.ycombinator.com/rss", Interval: 1800},
}

// SeedDefaults 在数据库尚无任何渠道时写入内置默认渠道，返回本次写入的数量。
// 已有渠道时直接返回 0（幂等）：用户自建的渠道不会被默认渠道污染。
func (a *App) SeedDefaults() (int, error) {
	sources, err := a.db.ListSources()
	if err != nil {
		return 0, err
	}
	if len(sources) > 0 {
		return 0, nil
	}

	added := 0
	for _, ds := range defaultSources {
		cfg, err := json.Marshal(map[string]string{"url": ds.URL})
		if err != nil {
			return added, err
		}
		if _, err := a.AddSource(ds.Name, database.SourceTypeFeed, string(cfg), ds.Interval, true); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}
