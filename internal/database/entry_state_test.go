package database

import (
	"os"
	"testing"
)

// newRawDB 打开一个未执行 Install 的临时数据库，供迁移测试手工构造旧表结构。
func newRawDB(t *testing.T) *DB {
	t.Helper()
	tmp, err := os.CreateTemp("", "newsglean-raw-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	t.Cleanup(func() { _ = os.Remove(tmp.Name()) })

	db, err := Open("sqlite", tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestEntryStateReadLater(t *testing.T) {
	db := newTestDB(t)

	ids := make([]uint64, 0, 3)
	for _, title := range []string{"a", "b", "c"} {
		e := &Entry{SourceID: 1, Title: title, URL: "https://example.com/" + title}
		if err := db.CreateEntry(e); err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
		ids = append(ids, e.ID)
	}

	// 标记 1、3 为稍后再阅
	if _, err := db.SetReadLater(ids[0], true); err != nil {
		t.Fatalf("SetReadLater true: %v", err)
	}
	if _, err := db.SetReadLater(ids[2], true); err != nil {
		t.Fatalf("SetReadLater true: %v", err)
	}

	// 批量状态查询
	states, err := db.GetEntryStates(ids)
	if err != nil {
		t.Fatalf("GetEntryStates: %v", err)
	}
	if !states[ids[0]].ReadLater || states[ids[1]].ReadLater || !states[ids[2]].ReadLater {
		t.Fatalf("states = %+v", states)
	}

	// 列表按加入时间倒序：3 后加入，应在前（同秒时按 id 倒序兜底）
	list, total, err := db.ListReadLater(1, 50)
	if err != nil {
		t.Fatalf("ListReadLater: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(list) != 2 || list[0].ID != ids[2] || list[1].ID != ids[0] {
		t.Fatalf("order wrong: got %v, %v", list[0].ID, list[1].ID)
	}

	// 取消 3 的稍后再阅
	st, err := db.SetReadLater(ids[2], false)
	if err != nil {
		t.Fatalf("SetReadLater false: %v", err)
	}
	if st.ReadLater {
		t.Fatal("state should be false after cancel")
	}
	list, total, err = db.ListReadLater(1, 50)
	if err != nil {
		t.Fatalf("ListReadLater after cancel: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != ids[0] {
		t.Fatalf("after cancel: total=%d list=%v", total, list)
	}

	// 重复标记幂等：再标一次 true 不报错
	if _, err := db.SetReadLater(ids[0], true); err != nil {
		t.Fatalf("SetReadLater idempotent: %v", err)
	}
}

func TestMigrateReadLater(t *testing.T) {
	db := newRawDB(t)

	// 手工造旧版 entries 表（带 read_later 列）与两条数据
	if err := db.Exec(`CREATE TABLE entries (id INTEGER PRIMARY KEY AUTOINCREMENT, read_later INTEGER, updated_at TEXT)`).Error; err != nil {
		t.Fatalf("create old entries: %v", err)
	}
	if err := db.Exec(`INSERT INTO entries (read_later, updated_at) VALUES (1, '2026-01-01T00:00:00Z'), (0, '2026-01-02T00:00:00Z')`).Error; err != nil {
		t.Fatalf("insert old entries: %v", err)
	}
	// entry_state 表用 AutoMigrate 建立（与 Install 中一致）
	if err := db.AutoMigrate(&EntryState{}); err != nil {
		t.Fatalf("migrate entry_state: %v", err)
	}

	if err := db.migrateReadLater(); err != nil {
		t.Fatalf("migrateReadLater: %v", err)
	}

	st, err := db.GetEntryState(1)
	if err != nil {
		t.Fatalf("GetEntryState(1): %v", err)
	}
	if !st.ReadLater {
		t.Fatal("entry 1 should be read_later after migration")
	}
	st2, err := db.GetEntryState(2)
	if err != nil {
		t.Fatalf("GetEntryState(2): %v", err)
	}
	if st2.ReadLater {
		t.Fatal("entry 2 should not be read_later")
	}
}
