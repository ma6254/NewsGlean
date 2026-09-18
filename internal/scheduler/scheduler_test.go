package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
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

// newTestScheduler 装配临时数据库与调度器，并把扫描粒度调到毫秒级、关闭全局限速以加速测试。
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
	cfg.Fetch.RateLimit = "" // 调度测试关闭全局限速，限速器本身由 fetch 包测试覆盖
	a := app.New(cfg, db)
	s := New(cfg, db, a)
	s.scanInterval = 50 * time.Millisecond
	return s, db
}

// waitFutureNextRun 等待指定渠道某一轮采集结束且已登记「未来的下一次到期时间」，返回剩余时长。
func waitFutureNextRun(t *testing.T, s *Scheduler, id uint64, timeout time.Duration) time.Duration {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		nr, running := s.nextRun[id], s.running[id]
		s.mu.Unlock()
		remain := time.Until(nr)
		if !running && remain > 0 {
			return remain
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for future nextRun of source %d (running=%v, remain=%v)", id, running, remain)
		}
		time.Sleep(20 * time.Millisecond)
	}
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

// TestSchedulerHealthDegradation 验证阶段 2 核心验收：连续失败降频，恢复后回升。
func TestSchedulerHealthDegradation(t *testing.T) {
	s, db := newTestScheduler(t)

	var failing atomic.Bool
	failing.Store(true)
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testRSS))
	}))
	defer feedSrv.Close()

	src, err := s.app.AddSource("flaky", database.SourceTypeFeed, `{"url":"`+feedSrv.URL+`"}`, 1, true)
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	id := src.ID

	s.Start(context.Background())
	defer s.Stop()

	// 1. 首轮采集失败：FailCount 增加，下一次到期被降频（基础 1s → 2s，显著超过 1s）
	deadline := time.Now().Add(5 * time.Second)
	for {
		fresh, err := db.GetSource(id)
		if err != nil {
			t.Fatalf("GetSource: %v", err)
		}
		if fresh.FailCount >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for first failure")
		}
		time.Sleep(20 * time.Millisecond)
	}
	remain := waitFutureNextRun(t, s, id, 2*time.Second)
	if remain <= 1500*time.Millisecond {
		t.Fatalf("next run not degraded: remain=%v, want > 1.5s (base 1s)", remain)
	}

	// 2. 恢复：feed 转正常，下一次到期到达后采集成功，FailCount 归零、间隔回落基准
	failing.Store(false)
	deadline = time.Now().Add(5 * time.Second)
	for {
		fresh, err := db.GetSource(id)
		if err != nil {
			t.Fatalf("GetSource: %v", err)
		}
		if fresh.FailCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for recovery, fail_count=%d", fresh.FailCount)
		}
		time.Sleep(20 * time.Millisecond)
	}
	remain = waitFutureNextRun(t, s, id, 2*time.Second)
	if remain > time.Second {
		t.Fatalf("next run not recovered: remain=%v, want <= 1s (base interval)", remain)
	}
}

// TestEffectiveInterval 验证有效间隔计算：基础回退 + 指数退避封顶 + 不缩短超长基础间隔。
func TestEffectiveInterval(t *testing.T) {
	cfg := config.Default()
	cfg.Fetch.Interval = "45m"
	s := New(cfg, nil, nil)

	// 基础间隔：渠道 interval 优先，无效时回退全局默认
	if got := s.effectiveInterval(120, 0); got != 120*time.Second {
		t.Fatalf("effectiveInterval(120, 0) = %v, want 2m", got)
	}
	if got := s.effectiveInterval(0, 0); got != 45*time.Minute {
		t.Fatalf("effectiveInterval(0, 0) = %v, want 45m", got)
	}
	if got := s.effectiveInterval(-3, 0); got != 45*time.Minute {
		t.Fatalf("effectiveInterval(-3, 0) = %v, want 45m", got)
	}

	// 连续失败指数退避：每次失败间隔翻倍，封顶 maxBackoff
	backoff := []struct {
		fail int
		want time.Duration
	}{
		{0, 30 * time.Minute},
		{1, 1 * time.Hour},
		{2, 2 * time.Hour},
		{3, 4 * time.Hour},
		{4, 8 * time.Hour},
		{5, 16 * time.Hour},
		{6, 24 * time.Hour}, // 30m*2^6=32h 被 maxBackoff 封顶为 24h
		{7, 24 * time.Hour},
		{100, 24 * time.Hour},
	}
	for _, c := range backoff {
		if got := s.effectiveInterval(1800, c.fail); got != c.want {
			t.Fatalf("effectiveInterval(1800, %d) = %v, want %v", c.fail, got, c.want)
		}
	}

	// 基础间隔已超过退避上限时，降频不应把间隔拉回更短的值
	huge := int(7 * 24 * 60 * 60) // 7 天
	if got := s.effectiveInterval(huge, 6); got != time.Duration(huge)*time.Second {
		t.Fatalf("effectiveInterval(huge, 6) = %v, want %v", got, time.Duration(huge)*time.Second)
	}
}
