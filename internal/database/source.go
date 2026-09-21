package database

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// 渠道类型标识常量。与 source 包的注册表一致，这里用于落库与展示。
const (
	SourceTypeFeed     = "feed"     // RSS / Atom / JSON Feed
	SourceTypeWebpage  = "webpage"  // 网页列表页爬取
	SourceTypeBilibili = "bilibili" // B 站个人数据（观看历史 / 收藏夹）
	SourceTypeTelegram = "telegram" // Telegram 频道/群
	SourceTypeWebhook  = "webhook"  // 通用入站
)

var (
	// ErrorSourceNotFound 渠道实例不存在（或已软删除）。
	ErrorSourceNotFound = errors.New("source not found")
)

// Source 渠道实例表（sources）。
type Source struct {
	ID          uint64 `gorm:"column:id;unique;primaryKey;autoIncrement"` // 渠道实例ID
	Name        string `gorm:"column:name"`                               // 显示名
	Type        string `gorm:"column:type"`                               // 渠道类型标识
	Config      string `gorm:"column:config;type:text"`                   // 渠道配置 JSON，核心层不解析
	Interval    int    `gorm:"column:interval"`                           // 刷新间隔（秒）
	Enabled     bool   `gorm:"column:enabled"`                            // 是否启用
	FailCount   int    `gorm:"column:fail_count"`                         // 连续失败次数
	LastError   string `gorm:"column:last_error;type:text"`               // 最近一次错误
	CreatedAt   string `gorm:"column:created_at"`                         // 创建时间
	UpdatedAt   string `gorm:"column:updated_at"`                         // 更新时间
	Deleted     bool   `gorm:"column:deleted"`                            // 删除标记，软删除
	LastEntryAt string `gorm:"-"`                                         // 非持久化：该渠道最新条目的发布时间，由聚合查询填充
}

// TableName 指定表名。
func (Source) TableName() string { return "sources" }

// CreateSource 新增渠道实例。
func (d *DB) CreateSource(s *Source) error {
	now := nowString()
	s.CreatedAt = now
	s.UpdatedAt = now
	return d.Create(s).Error
}

// ListSources 返回所有未删除的渠道实例，按 ID 升序。
func (d *DB) ListSources() ([]Source, error) {
	var list []Source
	err := d.Where("deleted = ?", false).Order("id ASC").Find(&list).Error
	return list, err
}

// GetSource 按 ID 返回未删除的渠道实例。
func (d *DB) GetSource(id uint64) (*Source, error) {
	var s Source
	err := d.Where("id = ? AND deleted = ?", id, false).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrorSourceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSource 更新渠道实例（非零值字段）。
func (d *DB) UpdateSource(s *Source) error {
	s.UpdatedAt = nowString()
	return d.Model(&Source{}).Where("id = ? AND deleted = ?", s.ID, false).
		Updates(map[string]any{
			"name":       s.Name,
			"type":       s.Type,
			"config":     s.Config,
			"interval":   s.Interval,
			"enabled":    s.Enabled,
			"fail_count": s.FailCount,
			"last_error": s.LastError,
			"updated_at": s.UpdatedAt,
		}).Error
}

// UpdateSourceHealth 仅更新健康度字段（失败计数与最近错误）。
func (d *DB) UpdateSourceHealth(id uint64, failCount int, lastError string) error {
	return d.Model(&Source{}).Where("id = ? AND deleted = ?", id, false).
		Updates(map[string]any{
			"fail_count": failCount,
			"last_error": lastError,
			"updated_at": nowString(),
		}).Error
}

// DeleteSource 软删除渠道实例。
func (d *DB) DeleteSource(id uint64) error {
	res := d.Model(&Source{}).Where("id = ? AND deleted = ?", id, false).
		Updates(map[string]any{"deleted": true, "updated_at": nowString()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrorSourceNotFound
	}
	return nil
}

// nowString 返回当前 UTC 时间的 RFC3339 字符串，全项目统一时间格式。
func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
