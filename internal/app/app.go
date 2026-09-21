// Package app 是应用服务层：编排采集、三层去重、写库。
// 它只依赖 source 契约与 database/filter 核心层，不认识任何具体渠道实现。
package app

import (
	"context"
	"encoding/json"
	"errors"
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
	hub *progressHub // 采集进度发布-订阅中心（供 SSE 推送）
}

// New 构造 App。
func New(cfg *config.Config, db *database.DB) *App {
	return &App{cfg: cfg, db: db, hub: newProgressHub()}
}

// SubscribeProgress 返回采集进度事件订阅通道。通道带缓冲，消费不及时会丢事件（不阻塞采集）。
// 调用方不再需要时应调用 UnsubscribeProgress 释放。
func (a *App) SubscribeProgress() chan ProgressEvent {
	return a.hub.subscribe()
}

// UnsubscribeProgress 释放一个进度订阅。
func (a *App) UnsubscribeProgress(ch chan ProgressEvent) {
	a.hub.unsubscribe(ch)
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

// ProbeSource 在保存前探测渠道元信息（如 feed 标题）。
// 仅当渠道类型实现了 source.Prober 时可用，否则返回 source.ErrProbeUnsupported。
func (a *App) ProbeSource(ctx context.Context, typ, cfgJSON string) (source.ProbeInfo, error) {
	conn, err := source.Create(typ, cfgJSON, source.CreateOptions{Proxy: a.cfg.Fetch.Proxy, ChromePath: a.cfg.Fetch.ChromePath})
	if err != nil {
		return source.ProbeInfo{}, err
	}
	defer conn.Close()
	if err := conn.Validate(ctx); err != nil {
		return source.ProbeInfo{}, err
	}
	prober, ok := conn.(source.Prober)
	if !ok {
		return source.ProbeInfo{}, source.ErrProbeUnsupported
	}
	return prober.Probe(ctx)
}

// CheckEnv 检测渠道运行环境（如外部 CLI 是否安装/登录）。
// cfgJSON 为可选配置（如 bili_path 覆盖），留空用 "{}"。
// 仅当渠道类型实现了 source.EnvChecker 时可用，否则返回 source.ErrEnvCheckUnsupported。
func (a *App) CheckEnv(ctx context.Context, typ, cfgJSON string) (source.EnvCheck, error) {
	if cfgJSON == "" {
		cfgJSON = "{}"
	}
	conn, err := source.Create(typ, cfgJSON, source.CreateOptions{Proxy: a.cfg.Fetch.Proxy, ChromePath: a.cfg.Fetch.ChromePath})
	if err != nil {
		return source.EnvCheck{}, err
	}
	defer conn.Close()
	checker, ok := conn.(source.EnvChecker)
	if !ok {
		return source.EnvCheck{}, source.ErrEnvCheckUnsupported
	}
	return checker.CheckEnv(ctx)
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
// 无论成功与否，都会在结束时记录一条采集日志（fetch_log）并发布进度事件（SSE）。
func (a *App) refreshOne(ctx context.Context, s database.Source) (ids []uint64, skipped int, err error) {
	startedAt := time.Now()
	a.hub.publish(ProgressEvent{Type: EventSourceStarted, SourceID: s.ID, SourceName: s.Name})
	defer func() {
		// 关闭服务 / 客户端断连导致的取消（context.Canceled）不算失败：
		// 不写健康度、不落采集日志、也不发失败事件，静默放弃本轮。
		if errors.Is(err, context.Canceled) {
			return
		}
		elapsed := time.Since(startedAt).Milliseconds()
		if err != nil {
			a.hub.publish(ProgressEvent{Type: EventSourceFailed, SourceID: s.ID, SourceName: s.Name, Error: err.Error(), ElapsedMS: elapsed})
		} else {
			a.hub.publish(ProgressEvent{Type: EventSourceDone, SourceID: s.ID, SourceName: s.Name, Inserted: len(ids), InsertedIDs: ids, Skipped: skipped, ElapsedMS: elapsed})
		}
		a.recordFetchLog(s.ID, startedAt, len(ids), skipped, err)
	}()

	conn, err := source.Create(s.Type, s.Config, source.CreateOptions{Proxy: a.cfg.Fetch.Proxy, ChromePath: a.cfg.Fetch.ChromePath})
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()

	// 载入上次持久化的游标，回传给适配器（Init），并在 Fetch 时透传。
	cursor, err := a.db.GetCursor(s.ID)
	if err != nil {
		return nil, 0, err
	}
	if err := conn.Init(ctx, source.State{Cursor: source.Cursor(cursor)}); err != nil {
		return nil, 0, err
	}
	items, newCursor, err := conn.Fetch(ctx, source.Cursor(cursor), defaultFetchLimit)
	if err != nil {
		// 主动取消（停机 / 客户端断连）非渠道故障：不递增失败计数、不写最近错误。
		if !errors.Is(err, context.Canceled) {
			_ = a.db.UpdateSourceHealth(s.ID, s.FailCount+1, err.Error())
		}
		return nil, 0, err
	}

	// 仅对「新」条目回填详情（如视频简介/字幕），已存在条目跳过，避免重复 N+1 调用。
	enricher, _ := conn.(source.Enricher)
	ids = []uint64{}
	for _, item := range items {
		item.SourceID = strconv.FormatUint(s.ID, 10)
		if enricher != nil && item.GUID != "" {
			exists, existErr := a.db.EntryExistsByGUID(s.ID, item.GUID)
			if existErr != nil {
				return ids, skipped, existErr
			}
			if !exists {
				if enriched, enrichErr := enricher.Enrich(ctx, item); enrichErr == nil {
					item = enriched
				}
			}
		}
		id, ok, ingestErr := a.ingestItem(s.ID, item)
		if ingestErr != nil {
			return ids, skipped, ingestErr
		}
		if ok {
			ids = append(ids, id)
		} else {
			skipped++
		}
	}
	// 全部条目写库成功后推进游标；中途失败则保留旧游标，下一轮重取兜底。
	if err := a.db.SaveCursor(s.ID, []byte(newCursor)); err != nil {
		return ids, skipped, err
	}
	_ = a.db.UpdateSourceHealth(s.ID, 0, "")
	return ids, skipped, nil
}

// recordFetchLog 记录一条采集日志（fetch_log）。写日志失败只记错误、不阻断主流程。
func (a *App) recordFetchLog(sourceID uint64, startedAt time.Time, inserted, skipped int, fetchErr error) {
	success := fetchErr == nil
	errMsg := ""
	if fetchErr != nil {
		errMsg = fetchErr.Error()
	}
	logEntry := &database.FetchLog{
		SourceID:  sourceID,
		StartedAt: startedAt.UTC().Format(time.RFC3339),
		ElapsedMS: time.Since(startedAt).Milliseconds(),
		Inserted:  inserted,
		Skipped:   skipped,
		Success:   success,
		Error:     errMsg,
	}
	if err := a.db.CreateFetchLog(logEntry); err != nil {
		logger.Error("record fetch log failed", "source_id", sourceID, "error", err)
	}
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
