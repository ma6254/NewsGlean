package database

// FetchLog 采集历史表（fetch_log）：每次拉取尝试记一条，用于可观测性与排障。
// 时间字段统一用 RFC3339 字符串存储（与其它表一致）。
type FetchLog struct {
	ID        uint64 `gorm:"column:id;unique;primaryKey;autoIncrement"` // 日志ID
	SourceID  uint64 `gorm:"column:source_id;index"`                    // 归属渠道实例ID
	StartedAt string `gorm:"column:started_at"`                         // 采集开始时间（RFC3339）
	ElapsedMS int64  `gorm:"column:elapsed_ms"`                         // 耗时（毫秒）
	Inserted  int    `gorm:"column:inserted"`                           // 新增条目数
	Skipped   int    `gorm:"column:skipped"`                            // 去重跳过数
	Success   bool   `gorm:"column:success"`                            // 是否成功
	Error     string `gorm:"column:error;type:text"`                    // 错误信息（成功为空）
}

// TableName 指定表名。
func (FetchLog) TableName() string { return "fetch_log" }

// CreateFetchLog 新增一条采集日志。
func (d *DB) CreateFetchLog(l *FetchLog) error {
	return d.Create(l).Error
}

// ListFetchLogs 按时间倒序返回采集日志。sourceID 为 0 表示不过滤渠道；
// limit ≤ 0 或超上限时取默认上限 50。
func (d *DB) ListFetchLogs(sourceID uint64, limit int) ([]FetchLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := d.Model(&FetchLog{})
	if sourceID != 0 {
		q = q.Where("source_id = ?", sourceID)
	}
	var list []FetchLog
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// FetchStats 是单个渠道的采集统计（从 fetch_log 聚合而来）。
type FetchStats struct {
	SourceID      uint64 // 渠道实例ID
	Total         int64  // 累计采集次数
	Success       int64  // 累计成功次数
	LastSuccessAt string // 最后成功时间（RFC3339，无成功记录为空）
	LastStartedAt string // 最近一次采集时间（RFC3339）
	LastElapsedMS int64  // 最近一次耗时（毫秒）
}

// FetchStatsBySource 返回所有渠道的采集统计，key 为渠道实例 ID。
// 统计来源：总次数 / 成功次数 / 最后成功时间走聚合；最近一次耗时与时间取各渠道 id 最大的一条。
func (d *DB) FetchStatsBySource() (map[uint64]FetchStats, error) {
	type aggRow struct {
		SourceID      uint64 `gorm:"column:source_id"`
		Total         int64  `gorm:"column:total"`
		Success       int64  `gorm:"column:success"`
		LastSuccessAt string `gorm:"column:last_success_at"`
	}
	var aggs []aggRow
	if err := d.Raw(`
		SELECT source_id,
		       COUNT(*) AS total,
		       SUM(success) AS success,
		       COALESCE(MAX(CASE WHEN success = 1 THEN started_at END), '') AS last_success_at
		FROM fetch_log
		GROUP BY source_id`).Scan(&aggs).Error; err != nil {
		return nil, err
	}

	type lastRow struct {
		SourceID  uint64 `gorm:"column:source_id"`
		StartedAt string `gorm:"column:started_at"`
		ElapsedMS int64  `gorm:"column:elapsed_ms"`
	}
	var lasts []lastRow
	if err := d.Raw(`
		SELECT f.source_id AS source_id, f.started_at AS started_at, f.elapsed_ms AS elapsed_ms
		FROM fetch_log f
		INNER JOIN (SELECT source_id, MAX(id) AS max_id FROM fetch_log GROUP BY source_id) m
		  ON f.id = m.max_id`).Scan(&lasts).Error; err != nil {
		return nil, err
	}

	stats := make(map[uint64]FetchStats, len(aggs))
	for _, a := range aggs {
		stats[a.SourceID] = FetchStats{
			SourceID:      a.SourceID,
			Total:         a.Total,
			Success:       a.Success,
			LastSuccessAt: a.LastSuccessAt,
		}
	}
	for _, l := range lasts {
		st := stats[l.SourceID]
		st.LastStartedAt = l.StartedAt
		st.LastElapsedMS = l.ElapsedMS
		stats[l.SourceID] = st
	}
	return stats, nil
}
