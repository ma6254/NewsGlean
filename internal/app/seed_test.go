package app

import (
	"os"
	"testing"

	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"

	// 注册 feed 渠道（SeedDefaults 依赖 source 注册表）
	_ "github.com/ma6254/news-glean/internal/source/feed"
)

// newTestApp 装配一个临时数据库的 App。
func newTestApp(t *testing.T) *App {
	t.Helper()
	tmp, err := os.CreateTemp("", "newsglean-seed-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	t.Cleanup(func() { _ = os.Remove(tmp.Name()) })

	db, err := database.Open("sqlite", tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Install(); err != nil {
		t.Fatal(err)
	}
	return New(config.Default(), db)
}

func TestSeedDefaults(t *testing.T) {
	a := newTestApp(t)

	n, err := a.SeedDefaults()
	if err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if n != len(defaultSources) {
		t.Fatalf("seed count = %d, want %d", n, len(defaultSources))
	}

	list, err := a.db.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(list) != len(defaultSources) {
		t.Fatalf("sources after seed = %d, want %d", len(list), len(defaultSources))
	}

	// 再次 seed 应跳过（幂等）
	n, err = a.SeedDefaults()
	if err != nil {
		t.Fatalf("SeedDefaults again: %v", err)
	}
	if n != 0 {
		t.Fatalf("second seed count = %d, want 0", n)
	}
}
