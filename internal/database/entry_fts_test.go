package database

import "testing"

// ftsMatchCount 统计索引中命中给定 MATCH 查询的行数。
// 判断「某行是否在索引中」只能用 MATCH 命中唯一 token；外部内容表下
// `WHERE rowid=?` 会回退到内容表 entries（而非索引），不能反映索引成员关系。
func ftsMatchCount(t *testing.T, db *DB, query string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM entries_fts WHERE entries_fts MATCH ?", query).Scan(&n).Error; err != nil {
		t.Fatalf("fts match %q: %v", query, err)
	}
	return n
}

func TestInstallFTSCreatesTableAndTriggers(t *testing.T) {
	db := newTestDB(t)

	exists, err := db.ftsTableExists()
	if err != nil {
		t.Fatalf("ftsTableExists: %v", err)
	}
	if !exists {
		t.Fatal("entries_fts should exist after Install")
	}

	// 三个同步触发器都应已在 sqlite_master 登记
	for _, tr := range ftsTriggerDDL {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", tr.name).Scan(&n).Error; err != nil {
			t.Fatalf("check trigger %s: %v", tr.name, err)
		}
		if n != 1 {
			t.Fatalf("trigger %s should exist", tr.name)
		}
	}

	// 幂等：再次 Install 不报错
	if err := db.Install(); err != nil {
		t.Fatalf("second Install: %v", err)
	}
}

func TestEntryInsertSyncsToFTS(t *testing.T) {
	db := newTestDB(t)

	e := &Entry{SourceID: 1, Title: "alpha news", Summary: "summary", Content: "body text"}
	if err := db.CreateEntry(e); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	if ftsMatchCount(t, db, "alpha") != 1 {
		t.Fatal("inserted title token should be searchable")
	}
}

func TestEntryUpdateSyncsToFTS(t *testing.T) {
	db := newTestDB(t)

	e := &Entry{SourceID: 1, Title: "alpha"}
	if err := db.CreateEntry(e); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	// 更新标题：旧词应从索引移除，新词应加入
	if err := db.Model(&Entry{}).Where("id = ?", e.ID).
		Updates(map[string]any{"title": "beta", "updated_at": nowString()}).Error; err != nil {
		t.Fatalf("update entry: %v", err)
	}

	if ftsMatchCount(t, db, "alpha") != 0 {
		t.Fatal("old token should be removed from index after update")
	}
	if ftsMatchCount(t, db, "beta") != 1 {
		t.Fatal("new token should be indexed after update")
	}
}

func TestEntrySoftDeleteRemovesFromFTS(t *testing.T) {
	db := newTestDB(t)

	e := &Entry{SourceID: 1, Title: "alpha"}
	if err := db.CreateEntry(e); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if ftsMatchCount(t, db, "alpha") != 1 {
		t.Fatal("entry should be indexed before soft delete")
	}

	// 软删除：置 deleted=1（项目无物理删除，AFTER DELETE 触发器不会触发）
	if err := db.Model(&Entry{}).Where("id = ?", e.ID).
		Updates(map[string]any{"deleted": true, "updated_at": nowString()}).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if ftsMatchCount(t, db, "alpha") != 0 {
		t.Fatal("soft-deleted entry token should not be searchable")
	}
}

func TestFTSBackfill(t *testing.T) {
	db := newRawDB(t)

	// 手工造 entries 表（不含 FTS），模拟阶段 5 之前已采集的存量数据
	if err := db.Exec(`CREATE TABLE entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT, summary TEXT, content TEXT, author TEXT, deleted INTEGER
	)`).Error; err != nil {
		t.Fatalf("create entries: %v", err)
	}
	if err := db.Exec(`INSERT INTO entries (title, summary, content, author, deleted) VALUES
		('keep me', '', '', '', 0),
		('drop me', '', '', '', 1)`).Error; err != nil {
		t.Fatalf("insert entries: %v", err)
	}

	// 直接装 FTS（不经 AutoMigrate，聚焦回填逻辑）
	if err := db.installFTS(); err != nil {
		t.Fatalf("installFTS: %v", err)
	}

	// 未删除的存量条目应回填进索引，已软删的不应回填
	if ftsMatchCount(t, db, "keep") != 1 {
		t.Fatal("non-deleted existing entry should be backfilled")
	}
	if ftsMatchCount(t, db, "drop") != 0 {
		t.Fatal("soft-deleted existing entry should not be backfilled")
	}
}
