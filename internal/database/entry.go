package database

import (
	"errors"

	"gorm.io/gorm"
)

// Entry 条目表（entries）。
// 时间字段统一用 RFC3339 字符串存储（与 source 表一致）。
type Entry struct {
	ID          uint64 `gorm:"column:id;unique;primaryKey;autoIncrement"` // 条目ID
	SourceID    uint64 `gorm:"column:source_id;index"`                    // 归属渠道实例ID
	GUID        string `gorm:"column:guid;index"`                         // 渠道自带稳定ID（去重第一层）
	URL         string `gorm:"column:url"`                                // 规范化后的链接（去重第二层）
	ContentHash string `gorm:"column:content_hash;index"`                 // 内容指纹（去重第三层）
	Title       string `gorm:"column:title"`                              // 标题
	Author      string `gorm:"column:author"`                             // 作者
	PublishedAt string `gorm:"column:published_at"`                       // 发布时间（RFC3339）
	Summary     string `gorm:"column:summary;type:text"`                  // 摘要
	Content     string `gorm:"column:content;type:text"`                  // 正文
	ContentType string `gorm:"column:content_type"`                       // 内容类型
	Tags        string `gorm:"column:tags;type:text"`                     // 标签（JSON 数组字符串）
	Extra       string `gorm:"column:extra;type:text"`                    // 渠道特有字段（JSON 字符串）
	FetchedAt   string `gorm:"column:fetched_at"`                         // 抓取时间（RFC3339）
	CreatedAt   string `gorm:"column:created_at"`                         // 创建时间
	UpdatedAt   string `gorm:"column:updated_at"`                         // 更新时间
	Deleted     bool   `gorm:"column:deleted"`                            // 删除标记，软删除
	ReadLater   bool   `gorm:"column:read_later;index"`                   // 稍后再阅标记
}

// TableName 指定表名。
func (Entry) TableName() string { return "entries" }

// CreateEntry 新增条目。
func (d *DB) CreateEntry(e *Entry) error {
	now := nowString()
	e.CreatedAt = now
	e.UpdatedAt = now
	return d.Create(e).Error
}

// GetEntry 按 ID 返回未删除的条目。
func (d *DB) GetEntry(id uint64) (*Entry, error) {
	var e Entry
	err := d.Where("id = ? AND deleted = ?", id, false).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListEntries 分页返回未删除的条目，按发布时间倒序、ID 倒序。
// sourceID 为 0 表示不过滤渠道。返回条目列表与总数。
func (d *DB) ListEntries(page, pageSize int, sourceID uint64) ([]Entry, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	q := d.Model(&Entry{}).Where("deleted = ?", false)
	if sourceID != 0 {
		q = q.Where("source_id = ?", sourceID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Entry
	err := q.Order("published_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// EntryExistsByGUID 判断某渠道下是否已存在指定 GUID 的条目（去重第一层，按渠道隔离）。
func (d *DB) EntryExistsByGUID(sourceID uint64, guid string) (bool, error) {
	var count int64
	err := d.Model(&Entry{}).
		Where("source_id = ? AND guid = ? AND deleted = ?", sourceID, guid, false).
		Count(&count).Error
	return count > 0, err
}

// EntryExistsByURL 判断是否已存在指定链接的条目（去重第二层，全局）。
func (d *DB) EntryExistsByURL(url string) (bool, error) {
	var count int64
	err := d.Model(&Entry{}).
		Where("url = ? AND deleted = ?", url, false).
		Count(&count).Error
	return count > 0, err
}

// EntryExistsByHash 判断是否已存在指定内容指纹的条目（去重第三层，全局跨渠道）。
func (d *DB) EntryExistsByHash(hash string) (bool, error) {
	var count int64
	err := d.Model(&Entry{}).
		Where("content_hash = ? AND deleted = ?", hash, false).
		Count(&count).Error
	return count > 0, err
}

// LatestPublishTime 返回指定渠道最新一条未删除条目的发布时间（RFC3339 字符串）。
// 无条目时返回空字符串。published_at 统一为 UTC RFC3339，字符串字典序即时间序。
func (d *DB) LatestPublishTime(sourceID uint64) (string, error) {
	var latest string
	err := d.Model(&Entry{}).
		Select("COALESCE(MAX(published_at), '')").
		Where("source_id = ? AND deleted = ?", sourceID, false).
		Scan(&latest).Error
	return latest, err
}

// LatestPublishTimes 返回所有渠道的最新条目发布时间，key 为渠道实例 ID。
// 没有任何未删除条目的渠道不会出现在结果中。
func (d *DB) LatestPublishTimes() (map[uint64]string, error) {
	type row struct {
		SourceID uint64 `gorm:"column:source_id"`
		Latest   string `gorm:"column:latest"`
	}
	var rows []row
	err := d.Model(&Entry{}).
		Select("source_id, MAX(published_at) AS latest").
		Where("deleted = ?", false).
		Group("source_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]string, len(rows))
	for _, r := range rows {
		m[r.SourceID] = r.Latest
	}
	return m, nil
}

// SetReadLater 设置或取消条目的「稍后再阅」标记，并刷新 updated_at（用于按加入时间排序）。
// 返回更新后的完整条目；条目不存在或已删除时返回 gorm.ErrRecordNotFound。
func (d *DB) SetReadLater(id uint64, readLater bool) (*Entry, error) {
	res := d.Model(&Entry{}).
		Where("id = ? AND deleted = ?", id, false).
		Updates(map[string]any{"read_later": readLater, "updated_at": nowString()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return d.GetEntry(id)
}

// ListReadLater 分页返回标记为「稍后再阅」的未删除条目，按加入时间倒序（updated_at DESC）。
func (d *DB) ListReadLater(page, pageSize int) ([]Entry, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	q := d.Model(&Entry{}).Where("deleted = ? AND read_later = ?", false, true)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Entry
	err := q.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}
