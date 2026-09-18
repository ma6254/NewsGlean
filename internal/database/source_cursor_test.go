package database

import (
	"os"
	"testing"
)

// newTestDB 装配一个临时数据库并完成 Install。
func newTestDB(t *testing.T) *DB {
	t.Helper()
	tmp, err := os.CreateTemp("", "newsglean-db-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	t.Cleanup(func() { _ = os.Remove(tmp.Name()) })

	db, err := Open("sqlite", tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Install(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSourceCursorLifecycle(t *testing.T) {
	db := newTestDB(t)

	// 尚未记录 → 空游标
	c, err := db.GetCursor(1)
	if err != nil {
		t.Fatalf("GetCursor empty: %v", err)
	}
	if len(c) != 0 {
		t.Fatalf("expected empty cursor, got %q", c)
	}

	// 首次保存 → 创建
	if err := db.SaveCursor(1, []byte(`{"etag":"\"a\""}`)); err != nil {
		t.Fatalf("SaveCursor create: %v", err)
	}
	c, err = db.GetCursor(1)
	if err != nil {
		t.Fatalf("GetCursor after create: %v", err)
	}
	if string(c) != `{"etag":"\"a\""}` {
		t.Fatalf("cursor = %q", c)
	}

	// 再次保存 → 更新（不产生重复行）
	if err := db.SaveCursor(1, []byte(`{"etag":"\"b\""}`)); err != nil {
		t.Fatalf("SaveCursor update: %v", err)
	}
	c, err = db.GetCursor(1)
	if err != nil {
		t.Fatalf("GetCursor after update: %v", err)
	}
	if string(c) != `{"etag":"\"b\""}` {
		t.Fatalf("cursor = %q", c)
	}
	var count int64
	if err := db.Model(&SourceCursor{}).Where("source_id = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("count cursor rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("cursor row count = %d, want 1", count)
	}

	// 保存空游标 → 删除该行
	if err := db.SaveCursor(1, nil); err != nil {
		t.Fatalf("SaveCursor delete: %v", err)
	}
	c, err = db.GetCursor(1)
	if err != nil {
		t.Fatalf("GetCursor after delete: %v", err)
	}
	if len(c) != 0 {
		t.Fatalf("expected empty cursor after delete, got %q", c)
	}
}
