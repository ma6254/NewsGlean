package database

import (
	"errors"
	"testing"
)

// insertSearchEntry 创建一条带中文内容的条目，便于搜索测试。
func insertSearchEntry(t *testing.T, db *DB, title, content string) uint64 {
	t.Helper()
	e := &Entry{SourceID: 1, Title: title, Content: content, URL: "https://example.com/" + title}
	if err := db.CreateEntry(e); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	return e.ID
}

func TestSearchEntriesChineseBigram(t *testing.T) {
	db := newTestDB(t)
	insertSearchEntry(t, db, "苹果公司发布新品", "苹果发布新一代 iPhone")
	insertSearchEntry(t, db, "香蕉价格", "今日香蕉涨价")

	// 双字关键词命中标题/正文
	if _, total, err := db.SearchEntries("苹果", EntryFilter{}, 1, 50); err != nil || total != 1 {
		t.Fatalf("search 苹果: total=%d err=%v, want 1", total, err)
	}
	if _, total, err := db.SearchEntries("发布", EntryFilter{}, 1, 50); err != nil || total != 1 {
		t.Fatalf("search 发布: total=%d err=%v, want 1", total, err)
	}
	if _, total, err := db.SearchEntries("香蕉", EntryFilter{}, 1, 50); err != nil || total != 1 {
		t.Fatalf("search 香蕉: total=%d err=%v, want 1", total, err)
	}
	// 不存在的词
	if _, total, err := db.SearchEntries("葡萄", EntryFilter{}, 1, 50); err != nil || total != 0 {
		t.Fatalf("search 葡萄: total=%d err=%v, want 0", total, err)
	}
}

func TestSearchEntriesASCIITerm(t *testing.T) {
	db := newTestDB(t)
	insertSearchEntry(t, db, "iPhone 发布", "内容")

	if _, total, err := db.SearchEntries("iPhone", EntryFilter{}, 1, 50); err != nil || total != 1 {
		t.Fatalf("search iPhone: total=%d err=%v, want 1", total, err)
	}
}

func TestSearchEntriesMultiTermAND(t *testing.T) {
	db := newTestDB(t)
	insertSearchEntry(t, db, "苹果 发布", "苹果和香蕉")
	insertSearchEntry(t, db, "香蕉", "只有香蕉")

	// "苹果 香蕉" 两词需同时命中同一条目（AND）
	if _, total, err := db.SearchEntries("苹果 香蕉", EntryFilter{}, 1, 50); err != nil || total != 1 {
		t.Fatalf("search AND: total=%d err=%v, want 1", total, err)
	}
}

func TestSearchEntriesExcludesSoftDeleted(t *testing.T) {
	db := newTestDB(t)
	id := insertSearchEntry(t, db, "测试关键词", "正文")
	if err := db.Model(&Entry{}).Where("id = ?", id).
		Updates(map[string]any{"deleted": true, "updated_at": nowString()}).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, total, err := db.SearchEntries("关键词", EntryFilter{}, 1, 50); err != nil || total != 0 {
		t.Fatalf("search after soft delete: total=%d err=%v, want 0", total, err)
	}
}

func TestSearchEntriesEmptyQuery(t *testing.T) {
	db := newTestDB(t)
	_, _, err := db.SearchEntries("   ", EntryFilter{}, 1, 50)
	if !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("expected ErrEmptyQuery, got %v", err)
	}
}

func TestSearchEntriesPagination(t *testing.T) {
	db := newTestDB(t)
	for _, title := range []string{"苹果新闻一", "苹果新闻二", "苹果新闻三"} {
		insertSearchEntry(t, db, title, "")
	}
	// 每页 2 条，共 3 条命中
	if _, total, err := db.SearchEntries("苹果", EntryFilter{}, 1, 2); err != nil || total != 3 {
		t.Fatalf("page1: total=%d err=%v, want 3", total, err)
	}
	list, _, err := db.SearchEntries("苹果", EntryFilter{}, 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("page2 items=%d, want 1", len(list))
	}
}
