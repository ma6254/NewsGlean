package database

import (
	"errors"

	"gorm.io/gorm"
)

// EntryState 条目阅读状态表（entry_state），与 entries 一对一。
// 承载已读、收藏、归档、稍后再阅四类状态；阶段 4 起全部接线。
type EntryState struct {
	EntryID   uint64 `gorm:"column:entry_id;unique;primaryKey"` // 条目ID
	Read      bool   `gorm:"column:read"`                       // 已读
	Favorite  bool   `gorm:"column:favorite"`                   // 收藏
	Archive   bool   `gorm:"column:archive"`                    // 归档
	ReadLater bool   `gorm:"column:read_later;index"`           // 稍后再阅
	UpdatedAt string `gorm:"column:updated_at"`                 // 更新时间
}

// TableName 指定表名。
func (EntryState) TableName() string { return "entry_state" }

// GetEntryState 返回条目状态；未记录时返回零值状态（全部 false），不报错。
func (d *DB) GetEntryState(entryID uint64) (EntryState, error) {
	var st EntryState
	err := d.Where("entry_id = ?", entryID).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return EntryState{}, nil
	}
	if err != nil {
		return EntryState{}, err
	}
	return st, nil
}

// GetEntryStates 批量返回条目状态，key 为条目ID。未记录的条目不出现在结果中。
func (d *DB) GetEntryStates(entryIDs []uint64) (map[uint64]EntryState, error) {
	m := make(map[uint64]EntryState, len(entryIDs))
	if len(entryIDs) == 0 {
		return m, nil
	}
	var states []EntryState
	if err := d.Where("entry_id IN ?", entryIDs).Find(&states).Error; err != nil {
		return nil, err
	}
	for _, st := range states {
		m[st.EntryID] = st
	}
	return m, nil
}

// SetReadLater 设置/取消条目的「稍后再阅」，并刷新 entry_state.updated_at
// （用于按加入时间排序）。无状态行且要取消时不做任何写入。返回更新后的状态。
func (d *DB) SetReadLater(entryID uint64, readLater bool) (EntryState, error) {
	var st EntryState
	err := d.Where("entry_id = ?", entryID).First(&st).Error
	if err == nil {
		if st.ReadLater == readLater {
			return st, nil
		}
		st.ReadLater = readLater
		st.UpdatedAt = nowString()
		return st, d.Model(&EntryState{}).Where("entry_id = ?", entryID).
			Updates(map[string]any{"read_later": readLater, "updated_at": st.UpdatedAt}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return EntryState{}, err
	}
	if !readLater {
		return EntryState{}, nil
	}
	st = EntryState{EntryID: entryID, ReadLater: true, UpdatedAt: nowString()}
	return st, d.Create(&st).Error
}

// setStateFlag 是设置布尔阅读状态（已读 / 收藏 / 归档）的通用实现：
// 设置/取消某状态并刷新 entry_state.updated_at。无状态行且要取消时不做任何写入。
// 返回更新后的状态。column 必须是 EntryState 上的布尔列名（read/favorite/archive）。
func (d *DB) setStateFlag(entryID uint64, column string, value bool) (EntryState, error) {
	var st EntryState
	err := d.Where("entry_id = ?", entryID).First(&st).Error
	if err == nil {
		if stateFlag(&st, column) == value {
			return st, nil
		}
		setStateFlagValue(&st, column, value)
		st.UpdatedAt = nowString()
		return st, d.Model(&EntryState{}).Where("entry_id = ?", entryID).
			Updates(map[string]any{column: value, "updated_at": st.UpdatedAt}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return EntryState{}, err
	}
	if !value {
		return EntryState{}, nil
	}
	st = EntryState{EntryID: entryID, UpdatedAt: nowString()}
	setStateFlagValue(&st, column, true)
	return st, d.Create(&st).Error
}

// SetRead 设置/取消条目的「已读」状态。
func (d *DB) SetRead(entryID uint64, read bool) (EntryState, error) {
	return d.setStateFlag(entryID, "read", read)
}

// SetFavorite 设置/取消条目的「收藏」状态。
func (d *DB) SetFavorite(entryID uint64, favorite bool) (EntryState, error) {
	return d.setStateFlag(entryID, "favorite", favorite)
}

// SetArchive 设置/取消条目的「归档」状态。
func (d *DB) SetArchive(entryID uint64, archive bool) (EntryState, error) {
	return d.setStateFlag(entryID, "archive", archive)
}

// stateFlag 读取 EntryState 上指定布尔列的值。
func stateFlag(st *EntryState, column string) bool {
	switch column {
	case "read":
		return st.Read
	case "favorite":
		return st.Favorite
	case "archive":
		return st.Archive
	}
	return false
}

// setStateFlagValue 写入 EntryState 上指定布尔列的值。
func setStateFlagValue(st *EntryState, column string, value bool) {
	switch column {
	case "read":
		st.Read = value
	case "favorite":
		st.Favorite = value
	case "archive":
		st.Archive = value
	}
}

// ListReadLater 分页返回标记为「稍后再阅」的未删除条目，
// 按加入稍后读时间（entry_state.updated_at）倒序。
func (d *DB) ListReadLater(page, pageSize int) ([]Entry, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	q := d.Model(&Entry{}).
		Joins("JOIN entry_state ON entry_state.entry_id = entries.id").
		Where("entries.deleted = ? AND entry_state.read_later = ?", false, true)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Entry
	err := q.Order("entry_state.updated_at DESC, entries.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// migrateReadLater 把旧版 entries.read_later 列迁移到 entry_state 表。
// 仅在旧列仍存在时执行（全新安装无此列，直接跳过）；INSERT OR IGNORE 幂等。
// 旧列由 GORM 的 AutoMigrate 保留（它不删列），迁移后不再被代码读取，无害。
func (d *DB) migrateReadLater() error {
	var count int64
	if err := d.Raw("SELECT COUNT(*) FROM pragma_table_info('entries') WHERE name = 'read_later'").Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	return d.Exec(`
		INSERT OR IGNORE INTO entry_state (entry_id, read_later, updated_at)
		SELECT id, read_later, updated_at FROM entries WHERE read_later = 1
	`).Error
}
