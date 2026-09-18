package database

import "testing"

// TestFetchLogStats 验证采集日志的写入、倒序列表与聚合统计。
func TestFetchLogStats(t *testing.T) {
	db := newTestDB(t)

	rows := []FetchLog{
		{SourceID: 1, StartedAt: "2023-01-01T00:00:00Z", ElapsedMS: 100, Inserted: 2, Skipped: 0, Success: true},
		{SourceID: 1, StartedAt: "2023-01-01T01:00:00Z", ElapsedMS: 200, Inserted: 0, Skipped: 0, Success: false, Error: "boom"},
		{SourceID: 1, StartedAt: "2023-01-01T02:00:00Z", ElapsedMS: 150, Inserted: 1, Skipped: 1, Success: true},
		{SourceID: 2, StartedAt: "2023-01-01T00:30:00Z", ElapsedMS: 50, Inserted: 0, Skipped: 5, Success: true},
	}
	for i := range rows {
		if err := db.CreateFetchLog(&rows[i]); err != nil {
			t.Fatalf("CreateFetchLog: %v", err)
		}
	}

	stats, err := db.FetchStatsBySource()
	if err != nil {
		t.Fatalf("FetchStatsBySource: %v", err)
	}

	st1, ok := stats[1]
	if !ok {
		t.Fatalf("stats missing source 1: %+v", stats)
	}
	if st1.Total != 3 || st1.Success != 2 {
		t.Fatalf("source 1 total/success = %d/%d, want 3/2", st1.Total, st1.Success)
	}
	if st1.LastSuccessAt != "2023-01-01T02:00:00Z" {
		t.Fatalf("source 1 last_success_at = %q, want 02:00", st1.LastSuccessAt)
	}
	// 最近一次（id 最大）是第三条：elapsed 150、02:00
	if st1.LastElapsedMS != 150 || st1.LastStartedAt != "2023-01-01T02:00:00Z" {
		t.Fatalf("source 1 last elapsed/started = %d/%q, want 150/02:00", st1.LastElapsedMS, st1.LastStartedAt)
	}

	st2, ok := stats[2]
	if !ok {
		t.Fatalf("stats missing source 2: %+v", stats)
	}
	if st2.Total != 1 || st2.Success != 1 || st2.LastSuccessAt != "2023-01-01T00:30:00Z" {
		t.Fatalf("source 2 stats = %+v", st2)
	}

	// ListFetchLogs 按 id 倒序（等价于时间倒序）
	logs, err := db.ListFetchLogs(1, 0)
	if err != nil {
		t.Fatalf("ListFetchLogs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("ListFetchLogs len = %d, want 3", len(logs))
	}
	if logs[0].StartedAt != "2023-01-01T02:00:00Z" || logs[2].StartedAt != "2023-01-01T00:00:00Z" {
		t.Fatalf("ListFetchLogs order wrong: first=%q last=%q", logs[0].StartedAt, logs[2].StartedAt)
	}
}
