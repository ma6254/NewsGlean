// Package app 是应用服务层：编排采集、三层去重、写库。
// 它只依赖 source 契约与 database/filter 核心层，不认识任何具体渠道实现。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/filter"
	"github.com/ma6254/news-glean/internal/source"
	"github.com/ma6254/news-glean/log"
)

const defaultFetchLimit = 200 // 单轮单渠道拉取条数上限

// logger 是应用服务层的日志器，带 app tag。
var logger = log.WithTag("app")

// App 是应用服务层。
type App struct {
	cfg *config.Config
	db  *database.DB
}

// New 构造 App。
func New(cfg *config.Config, db *database.DB) *App {
	return &App{cfg: cfg, db: db}
}

// RefreshResult 一轮采集的汇总结果。
type RefreshResult struct {
	Sources     int      `json:"sources"`      // 处理的渠道数
	Inserted    int      `json:"inserted"`     // 新入库条目数
	InsertedIDs []uint64 `json:"inserted_ids"` // 本轮新增条目的 ID（供前端高亮新内容）
	Skipped     int      `json:"skipped"`      // 去重跳过的条目数
	Errors      []string `json:"errors"`       // 错误信息
}

// AddSource 校验并新增渠道实例。校验走适配器的 Validate，失败则不入库。
func (a *App) AddSource(name, typ, cfgJSON string, interval int, enabled bool) (*database.Source, error) {
	conn, err := source.Create(typ, cfgJSON, source.CreateOptions{Proxy: a.cfg.Fetch.Proxy, ChromePath: a.cfg.Fetch.ChromePath})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := conn.Validate(context.Background()); err != nil {
		return nil, err
	}
	s := &database.Source{
		Name:     name,
		Type:     typ,
		Config:   cfgJSON,
		Interval: interval,
		Enabled:  enabled,
	}
	if err := a.db.CreateSource(s); err != nil {
		return nil, err
	}
	logger.Info("source created", "id", s.ID, "name", name, "type", typ)
	return s, nil
}

// RefreshAll 手动触发一轮采集，遍历所有启用的渠道。
func (a *App) RefreshAll(ctx context.Context) (*RefreshResult, error) {
	sources, err := a.db.ListSources()
	if err != nil {
		return nil, err
	}
	result := &RefreshResult{Errors: []string{}, InsertedIDs: []uint64{}}
	for i := range sources {
		if !sources[i].Enabled {
			continue
		}
		result.Sources++
		ids, skip, err := a.refreshOne(ctx, sources[i])
		result.Inserted += len(ids)
		result.InsertedIDs = append(result.InsertedIDs, ids...)
		result.Skipped += skip
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("source %d (%s): %v", sources[i].ID, sources[i].Name, err))
			logger.Error("refresh source failed", "id", sources[i].ID, "name", sources[i].Name, "error", err)
		}
	}
	logger.Info("refresh finished", "sources", result.Sources, "inserted", result.Inserted, "skipped", result.Skipped, "errors", len(result.Errors))
	return result, nil
}

// RefreshSource 手动触发单个渠道的采集（无论其是否启用）。
func (a *App) RefreshSource(ctx context.Context, id uint64) (*RefreshResult, error) {
	s, err := a.db.GetSource(id)
	if err != nil {
		return nil, err
	}
	result := &RefreshResult{Sources: 1, Errors: []string{}, InsertedIDs: []uint64{}}
	ids, skip, err := a.refreshOne(ctx, *s)
	result.Inserted = len(ids)
	result.InsertedIDs = ids
	result.Skipped = skip
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("source %d (%s): %v", s.ID, s.Name, err))
		logger.Error("refresh source failed", "id", s.ID, "name", s.Name, "error", err)
	}
	logger.Info("refresh source finished", "id", s.ID, "name", s.Name, "inserted", len(ids), "skipped", skip)
	return result, nil
}

// refreshOne 拉取单个渠道并写库，返回 (新增条目ID列表, 去重跳过数, 错误)。
func (a *App) refreshOne(ctx context.Context, s database.Source) ([]uint64, int, error) {
	conn, err := source.Create(s.Type, s.Config, source.CreateOptions{Proxy: a.cfg.Fetch.Proxy, ChromePath: a.cfg.Fetch.ChromePath})
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()

	if err := conn.Init(ctx, source.State{}); err != nil {
		return nil, 0, err
	}
	items, _, err := conn.Fetch(ctx, nil, defaultFetchLimit)
	if err != nil {
		_ = a.db.UpdateSourceHealth(s.ID, s.FailCount+1, err.Error())
		return nil, 0, err
	}

	insertedIDs := []uint64{}
	skipped := 0
	for _, item := range items {
		item.SourceID = strconv.FormatUint(s.ID, 10)
		id, ok, err := a.ingestItem(s.ID, item)
		if err != nil {
			return insertedIDs, skipped, err
		}
		if ok {
			insertedIDs = append(insertedIDs, id)
		} else {
			skipped++
		}
	}
	_ = a.db.UpdateSourceHealth(s.ID, 0, "")
	return insertedIDs, skipped, nil
}

// ingestItem 对单条 Item 做三层去重（GUID → URL → ContentHash）并写库，
// 返回 (新条目ID, 是否入库, 错误)；跳过时 ID 为 0。
func (a *App) ingestItem(sourceID uint64, item source.Item) (uint64, bool, error) {
	guid := item.GUID
	normURL := filter.NormalizeURL(item.URL)

	// 内容指纹仅在存在可指纹化的内容时计算（标题或正文任一非空）
	var hash string
	if item.Title != "" || item.Content != "" {
		hash = filter.ContentHash(item.Title, item.Content)
	}

	if guid != "" {
		exists, err := a.db.EntryExistsByGUID(sourceID, guid)
		if err != nil {
			return 0, false, err
		}
		if exists {
			return 0, false, nil
		}
	}
	if normURL != "" {
		exists, err := a.db.EntryExistsByURL(normURL)
		if err != nil {
			return 0, false, err
		}
		if exists {
			return 0, false, nil
		}
	}
	if hash != "" {
		exists, err := a.db.EntryExistsByHash(hash)
		if err != nil {
			return 0, false, err
		}
		if exists {
			return 0, false, nil
		}
	}

	// 三者都缺：无任何身份可去重，丢弃并告警
	if guid == "" && normURL == "" && hash == "" {
		logger.Warn("dropping item with no identity", "title", item.Title)
		return 0, false, nil
	}

	entry := entryFromItem(item, sourceID, normURL, hash)
	if err := a.db.CreateEntry(entry); err != nil {
		return 0, false, err
	}
	return entry.ID, true, nil
}

// entryFromItem 把规范化条目映射为数据库条目。
func entryFromItem(item source.Item, sourceID uint64, normURL, hash string) *database.Entry {
	published := item.PublishedAt
	if published.IsZero() {
		published = item.FetchedAt // 发布时间缺失时用抓取时间
	}
	tags := item.Tags
	if tags == nil {
		tags = []string{}
	}
	extra := item.Extra
	if extra == nil {
		extra = map[string]string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	extraJSON, _ := json.Marshal(extra)
	return &database.Entry{
		SourceID:    sourceID,
		GUID:        item.GUID,
		URL:         normURL,
		ContentHash: hash,
		Title:       item.Title,
		Author:      item.Author,
		PublishedAt: published.UTC().Format(time.RFC3339),
		Summary:     item.Summary,
		Content:     item.Content,
		ContentType: item.ContentType,
		Tags:        string(tagsJSON),
		Extra:       string(extraJSON),
		FetchedAt:   item.FetchedAt.UTC().Format(time.RFC3339),
	}
}
