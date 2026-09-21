package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ma6254/news-glean/internal/source"
)

// connector 实现 source.Connector。
type connector struct {
	mode        string
	favID       string
	cmd         biliCmd
	fetchDetail bool
}

func (c *connector) Type() string { return "bilibili" }

// Validate 校验配置字段。不校验 bili 是否安装：环境可用性交给 CheckEnv，未装时允许保存并优雅降级。
func (c *connector) Validate(_ context.Context) error {
	switch c.mode {
	case ModeHistory:
		return nil
	case ModeFavorites:
		if strings.TrimSpace(c.favID) == "" {
			return errors.New("bilibili: favorites 模式需要 fav_id")
		}
		return nil
	default:
		return fmt.Errorf("bilibili: 无效 mode %q（want history|favorites）", c.mode)
	}
}

// Init 无操作（无长连接、无游标需预热）。
func (c *connector) Init(_ context.Context, _ source.State) error { return nil }

// Fetch 拉取一批条目。history/favorites 均按「取最新一页 + 三层去重」语义，无持久游标。
func (c *connector) Fetch(ctx context.Context, _ source.Cursor, limit int) ([]source.Item, source.Cursor, error) {
	switch c.mode {
	case ModeHistory:
		return c.fetchHistory(ctx, limit)
	case ModeFavorites:
		return c.fetchFavorites(ctx, limit)
	default:
		return nil, nil, fmt.Errorf("bilibili: 无效 mode %q", c.mode)
	}
}

// Probe 实现 source.Prober：history 返回固定标题；favorites 回读收藏夹列表取名字。
func (c *connector) Probe(ctx context.Context) (source.ProbeInfo, error) {
	switch c.mode {
	case ModeHistory:
		return source.ProbeInfo{Title: "B 站观看历史"}, nil
	case ModeFavorites:
		return c.probeFavorites(ctx)
	default:
		return source.ProbeInfo{}, fmt.Errorf("bilibili: 无效 mode %q", c.mode)
	}
}

// Run 返回 ErrPushUnsupported：bilibili 渠道只支持拉取模式。
func (c *connector) Run(_ context.Context, _ func(source.Item) error) error {
	return source.ErrPushUnsupported
}

func (c *connector) Close() error { return nil }

// fetchHistory 拉取观看历史（page 1，最近 limit 条，上限 100）。
func (c *connector) fetchHistory(ctx context.Context, limit int) ([]source.Item, source.Cursor, error) {
	n := limit
	if n <= 0 || n > 100 {
		n = 100
	}
	data, err := runBili(ctx, c.cmd, "history", "--page", "1", "--max", strconv.Itoa(n))
	if err != nil {
		return nil, nil, err
	}
	var resp struct {
		Items []historyItem `json:"items"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, nil, fmt.Errorf("bilibili: 解析 history 输出: %w", err)
	}
	return mapItems(resp.Items, mapHistoryItem), nil, nil
}

// fetchFavorites 拉取收藏夹第 1 页（最新）。
func (c *connector) fetchFavorites(ctx context.Context, _ int) ([]source.Item, source.Cursor, error) {
	data, err := runBili(ctx, c.cmd, "favorites", c.favID, "--page", "1")
	if err != nil {
		return nil, nil, err
	}
	var resp struct {
		Items []favoriteItem `json:"items"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, nil, fmt.Errorf("bilibili: 解析 favorites 输出: %w", err)
	}
	return mapItems(resp.Items, mapFavoriteItem), nil, nil
}

// probeFavorites 列出收藏夹并按 fav_id 匹配标题。
func (c *connector) probeFavorites(ctx context.Context) (source.ProbeInfo, error) {
	data, err := runBili(ctx, c.cmd, "favorites")
	if err != nil {
		return source.ProbeInfo{}, err
	}
	var folders []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &folders); err != nil {
		return source.ProbeInfo{}, fmt.Errorf("bilibili: 解析收藏夹列表: %w", err)
	}
	for _, f := range folders {
		if strconv.Itoa(f.ID) == c.favID {
			return source.ProbeInfo{Title: strings.TrimSpace(f.Title)}, nil
		}
	}
	return source.ProbeInfo{}, nil
}

// Enrich 实现 source.Enricher：为单条「新」条目回填视频简介（Summary）与部分统计（Extra）。
// 仅在 fetch_detail 开启时生效；回填失败返回原条目、不阻断采集。
func (c *connector) Enrich(ctx context.Context, item source.Item) (source.Item, error) {
	if !c.fetchDetail {
		return item, nil
	}
	bvid := item.GUID
	if bvid == "" {
		return item, nil
	}
	d, err := getVideoDetail(ctx, c.cmd, bvid, false)
	if err != nil {
		return item, nil
	}
	if d.Description != "" {
		item.Summary = d.Description
	}
	if item.Extra == nil {
		item.Extra = map[string]string{}
	}
	item.Extra["bili_view"] = strconv.Itoa(d.Stats.View)
	item.Extra["bili_like"] = strconv.Itoa(d.Stats.Like)
	if cover, cerr := GetVideoCover(ctx, bvid); cerr == nil && cover != "" {
		item.Extra["cover"] = cover
	}
	return item, nil
}

// mapItems 把 CLI 条目列表映射为规范化 Item 列表，并补齐 FetchedAt。
func mapItems[T any](raw []T, conv func(T) source.Item) []source.Item {
	now := time.Now().UTC()
	items := make([]source.Item, 0, len(raw))
	for _, r := range raw {
		item := conv(r)
		if item.FetchedAt.IsZero() {
			item.FetchedAt = now
		}
		items = append(items, item)
	}
	return items
}
