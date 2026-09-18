package database

import (
	"errors"

	"gorm.io/gorm"
)

// SourceCursor 拉取游标表（source_cursors）。
// 游标是不透明字节串，由各渠道适配器自行解释：RSS 存 ETag/Last-Modified，
// Telegram 存 update_id，网页爬虫存最后见到的链接与发布时间。
// 核心层只负责持久化与透传，不理解其含义。
type SourceCursor struct {
	SourceID  uint64 `gorm:"column:source_id;unique;primaryKey"` // 渠道实例ID
	Cursor    string `gorm:"column:cursor;type:text"`            // 游标字节串（UTF-8 文本化存储）
	UpdatedAt string `gorm:"column:updated_at"`                  // 更新时间
}

// TableName 指定表名。
func (SourceCursor) TableName() string { return "source_cursors" }

// GetCursor 返回指定渠道的持久化游标；尚未记录时返回 (nil, nil)。
func (d *DB) GetCursor(sourceID uint64) ([]byte, error) {
	var c SourceCursor
	err := d.Where("source_id = ?", sourceID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(c.Cursor), nil
}

// SaveCursor 持久化指定渠道的游标（upsert）。
// cursor 为空时删除该行，避免残留过期空游标。
func (d *DB) SaveCursor(sourceID uint64, cursor []byte) error {
	if len(cursor) == 0 {
		return d.Where("source_id = ?", sourceID).Delete(&SourceCursor{}).Error
	}

	var existing SourceCursor
	err := d.Where("source_id = ?", sourceID).First(&existing).Error
	if err == nil {
		return d.Model(&SourceCursor{}).Where("source_id = ?", sourceID).
			Updates(map[string]any{"cursor": string(cursor), "updated_at": nowString()}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return d.Create(&SourceCursor{
		SourceID:  sourceID,
		Cursor:    string(cursor),
		UpdatedAt: nowString(),
	}).Error
}
