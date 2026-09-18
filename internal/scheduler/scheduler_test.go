package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ma6254/news-glean/internal/app"
	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"

	// 注册 feed 渠道（采集依赖 source 注册表）
	_ "github.com/ma6254/news-glean/internal/source/feed"
)

// testRSS 是调度测试用的本地 RSS 样本，含两条条目。
const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item>
      <title>One</title>
      <link>https://example.com/one</link>
      <guid>https://example.com/one</guid>
      <pubDate>Mon, 02 Jan 2023 15:04:05 +0000</pubDate>
      <description>one</description>
    </item>
    <item>
      <title>Two</title>
      <link>https://example.com/two</link>
      <guid>https://example.com/two</guid>
      <pubDate>Tue, 03 Jan 2023 10:00:00 GMT</pubDate>
      <description>two</description>
    </item>
  </channel>
</rss>`

// newTestScheduler 装配临时数据库与调度器，并把扫描粒度调到毫秒级以加速测试。
func newTestScheduler(t *testing.T) (*Scheduler, *database.DB) {
	t.Helper()
	tmp, err := os.CreateTemp("", "newsglean-sched-*.db")
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

	cfg := config.Default()
	a := app.New(cfg, db)
	s := New(cfg, db, a)
	s.scanInterval = 50 * time.Millisecond
	return s, db
}

// TestSchedulerAutoRefresh 验证核心验收：挂上服务后自动按间隔刷新，无需手动触发。
func TestSchedulerAutoRefresh(t *testing.T) {
	s, db := newTestScheduler(t)

	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	// 添加一个启用、interval=1s 的 feed 渠道
	if _, err := s.app.AddSource("test", database.SourceTypeFeed, `{"url":"`+feedSrv.URL+`"}`, 1, true); err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	s.Start(context.Background())
	defer s.Stop()

	// 不调用任何手动刷新，轮询等待条目自动入库
	deadline := time.Now().Add(5 * time.Second)
	var total int64
	var err error
	for time.Now().Before(deadline) {
		_, total, err = db.ListEntries(1, 200, 0)
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if total >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if total != 2 {
		t.Fatalf("entries after auto refresh = %d, want 2", total)
	}
}

// TestSchedulerDisabledSourceNotFetched 验证禁用的渠道不会被后台调度。
func TestSchedulerDisabledSourceNotFetched(t *testing.T) {
	s, _ := newTestScheduler(t)

	var hits int
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	// 添加一个禁用的 feed 渠道
	if _, err := s.app.AddSource("disabled", database.SourceTypeFeed, `{"url":"`+feedSrv.URL+`"}`, 1, false); err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	s.Start(context.Background())
	defer s.Stop()

	// 覆盖多个扫描周期，禁用渠道不应被采集
	time.Sleep(300 * time.Millisecond)
	if hits != 0 {
		t.Fatalf("disabled source was fetched %d times, want 0", hits)
	}
}

// TestEffectiveInterval 验证渠道 interval 无效时回退到全局默认间隔。
func TestEffectiveInterval(t *testing.T) {
	cfg := config.Default()
	cfg.Fetch.Interval = "45m"
	s := New(cfg, nil, nil)

	if got := s.effectiveInterval(120); got != 120*time.Second {
		t.Fatalf("effectiveInterval(120) = %v, want 2m", got)
	}
	if got := s.effectiveInterval(0); got != 45*time.Minute {
		t.Fatalf("effectiveInterval(0) = %v, want 45m", got)
	}
	if got := s.effectiveInterval(-3); got != 45*time.Minute {
		t.Fatalf("effectiveInterval(-3) = %v, want 45m", got)
	}
}
