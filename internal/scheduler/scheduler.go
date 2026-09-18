// Package scheduler 负责采集调度。
// 阶段 1 落地定时后台调度：ticker 扫描 + 按渠道 interval 决定到期时间 + 全局并发上限，
// 挂上服务即自动按间隔刷新，无需手动触发。
// 阶段 2 落地礼貌限速 + 健康度自动降频：全局请求最小间隔限速；连续失败按指数退避降频，成功即回升。
// 手动触发入口（Refresh / RefreshSource）保留，且不受后台限速影响（用户显式动作即刻执行）。
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/ma6254/news-glean/internal/app"
	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/fetch"
	"github.com/ma6254/news-glean/log"
)

const (
	// scanInterval 是调度扫描粒度：调度器每隔这么长扫描一次「哪些渠道到期了」。
	// 它只决定调度精度，不决定每个渠道的实际刷新频率（那由渠道自身的 interval 决定）。
	scanInterval = 10 * time.Second

	// fallbackInterval 是全局默认刷新间隔的兜底值，配置缺失或解析失败时使用。
	fallbackInterval = 30 * time.Minute

	// maxBackoff 是连续失败退避的上限：再失败也不会把间隔拉得比这更长。
	maxBackoff = 24 * time.Hour

	// maxBackoffExp 是退避指数封顶：failCount 超过该值后间隔不再翻倍（2^maxBackoffExp）。
	// 例如基础 30m、封顶 6 次后：30m → 1h → 2h → 4h → 8h → 16h → 32h(封顶 24h)。
	maxBackoffExp = 6
)

// logger 是调度器的日志器，带 scheduler tag。
var logger = log.WithTag("scheduler")

// Scheduler 是采集调度器：负责定时后台刷新（阶段 1）与手动触发入口。
type Scheduler struct {
	app *app.App
	db  *database.DB

	scanInterval    time.Duration  // 调度扫描粒度
	defaultInterval time.Duration  // 全局默认刷新间隔（fetch.interval）
	concurrency     int            // 全局并发采集上限（fetch.concurrency）
	rate            *fetch.Limiter // 全局请求间隔限速器（fetch.rate_limit）

	mu      sync.Mutex
	nextRun map[uint64]time.Time // 每个渠道下一次可运行的时间
	running map[uint64]bool      // 正在采集中的渠道，防止同一渠道并发重入
	stopped bool                 // 是否已停止，阻止 Stop 之后再有新的 Add

	sem    chan struct{}      // 全局并发上限信号量
	cancel context.CancelFunc // 调度循环的取消函数；nil 表示未启动
	wg     sync.WaitGroup     // 等待调度循环与在途采集结束
}

// New 构造调度器。
func New(cfg *config.Config, db *database.DB, a *app.App) *Scheduler {
	concurrency := cfg.Fetch.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	defInterval := parseDuration(cfg.Fetch.Interval)
	if defInterval <= 0 {
		defInterval = fallbackInterval
	}
	return &Scheduler{
		app:             a,
		db:              db,
		scanInterval:    scanInterval,
		defaultInterval: defInterval,
		concurrency:     concurrency,
		rate:            fetch.NewLimiter(parseDuration(cfg.Fetch.RateLimit)),
		nextRun:         map[uint64]time.Time{},
		running:         map[uint64]bool{},
		sem:             make(chan struct{}, concurrency),
	}
}

// Start 启动定时后台调度，幂等：已在运行则直接返回。
// ctx 取消时调度循环与在途采集随之停止（配合 Stop 等待在途采集收敛）。
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.stopped = false
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop(loopCtx)
	logger.Info("scheduler started", "scan_interval", s.scanInterval.String(), "concurrency", s.concurrency)
}

// Stop 停止调度并等待在途采集结束。幂等，未启动时直接返回。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	s.cancel = nil
	s.stopped = true
	s.mu.Unlock()

	cancel()
	s.wg.Wait()
	logger.Info("scheduler stopped")
}

// Refresh 手动触发一轮采集（所有启用的渠道）。
func (s *Scheduler) Refresh(ctx context.Context) (*app.RefreshResult, error) {
	return s.app.RefreshAll(ctx)
}

// RefreshSource 手动触发单个渠道的采集。
func (s *Scheduler) RefreshSource(ctx context.Context, id uint64) (*app.RefreshResult, error) {
	return s.app.RefreshSource(ctx, id)
}

// loop 是调度循环：先立即扫描一轮，之后按 scanInterval 周期扫描。
func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	s.scheduleDue(ctx) // 启动即调度一轮，让已到期（含新加/改动的）渠道尽快拉新
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scheduleDue(ctx)
		}
	}
}

// scheduleDue 扫描所有启用的渠道，把「已到期且不在采集中的」交给后台采集。
// 到期判定：now >= nextRun[id]；nextRun 初值为零值，因此启动后首次扫描会立刻调度。
func (s *Scheduler) scheduleDue(ctx context.Context) {
	sources, err := s.db.ListSources()
	if err != nil {
		logger.Error("scheduler list sources failed", "error", err)
		return
	}
	now := time.Now()
	for i := range sources {
		src := sources[i]
		if !src.Enabled {
			continue
		}

		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return
		}
		if s.running[src.ID] || now.Before(s.nextRun[src.ID]) {
			s.mu.Unlock()
			continue
		}
		s.running[src.ID] = true
		s.wg.Add(1)
		s.mu.Unlock()

		go s.runOne(ctx, src.ID, src.Name, src.Interval)
	}
}

// runOne 采集单个渠道：拿并发令牌 → 等待全局限速放行 → 拉取 → 按健康度登记下一次到期时间。
// 连续失败按指数退避降频（effectiveInterval），成功则回落基准间隔。
func (s *Scheduler) runOne(ctx context.Context, id uint64, name string, intervalSec int) {
	defer s.wg.Done()

	// 全局并发上限：拿不到令牌就等待；ctx 取消时放弃本轮（释放 running 标记）。
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		s.clearRunning(id)
		return
	}
	defer func() { <-s.sem }()

	// 全局请求间隔限速：保证相邻两次采集起点至少间隔 fetch.rate_limit，避免齐发。
	if err := s.rate.Wait(ctx); err != nil {
		s.clearRunning(id)
		return
	}

	start := time.Now()
	_, refreshErr := s.app.RefreshSource(ctx, id) // 失败已由 app 层计入 FailCount 与日志

	// 重读渠道最新状态，按连续失败次数计算下一次到期（降频）；成功则 FailCount 归零、回升基准。
	base := intervalSec
	failCount := 0
	if fresh, err := s.db.GetSource(id); err == nil {
		base = fresh.Interval
		failCount = fresh.FailCount
	} else if refreshErr != nil {
		// 渠道在采集期间被删除或库异常，取不到健康度，仅记录不降频。
		logger.Debug("scheduled refresh source lookup failed", "id", id, "name", name, "error", err)
	}
	interval := s.effectiveInterval(base, failCount)

	elapsed := time.Since(start)
	switch {
	case refreshErr != nil && ctx.Err() != nil:
		// 停机取消导致的失败不算故障，降为 debug 避免刷错误日志。
		logger.Debug("scheduled refresh cancelled", "id", id, "name", name)
	case failCount > 0:
		logger.Warn("scheduled refresh failed", "id", id, "name", name, "fail_count", failCount, "next_interval", interval.String(), "elapsed", elapsed.String())
	default:
		logger.Info("scheduled refresh ok", "id", id, "name", name, "next_interval", interval.String(), "elapsed", elapsed.String())
	}

	// 完成后再登记下一次到期时间，避免拉取耗时超过 interval 时同渠道并发重入。
	s.mu.Lock()
	s.nextRun[id] = time.Now().Add(interval)
	delete(s.running, id)
	s.mu.Unlock()
}

// clearRunning 清除指定渠道的「采集中」标记（用于放弃本轮时）。
func (s *Scheduler) clearRunning(id uint64) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
}

// effectiveInterval 返回渠道的有效刷新间隔：
//   - intervalSec ≤ 0 时回退到全局默认间隔；
//   - 连续失败 failCount > 0 时按 2^failCount 指数退避，封顶 maxBackoff（且不低于基础间隔）。
func (s *Scheduler) effectiveInterval(intervalSec, failCount int) time.Duration {
	base := s.defaultInterval
	if intervalSec > 0 {
		base = time.Duration(intervalSec) * time.Second
	}
	if failCount <= 0 {
		return base
	}

	shift := failCount
	if shift > maxBackoffExp {
		shift = maxBackoffExp
	}
	cap_ := maxBackoff
	if base > cap_ {
		cap_ = base // 基础间隔已超过退避上限时，降频不应把间隔拉回更短的值
	}
	factor := time.Duration(int64(1) << uint(shift))
	if base > cap_/factor {
		return cap_
	}
	return base * factor
}

// parseDuration 解析时长字符串（如 "30m"、"20s"），失败返回 0。
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}
