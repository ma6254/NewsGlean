package fetch

import (
	"context"
	"sync"
	"time"
)

// Limiter 是全局请求限速器：保证相邻两次放行（Wait 返回）之间至少间隔 interval。
// 用于「礼貌限速」——避免多路采集在同一瞬间齐发请求。interval ≤ 0 表示不限速。
// 并发调用会被串行化：一次只放行一个调用方，其余排队等待。
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time // 上次放行时间；零值表示从未放行，首个调用方立即放行
}

// NewLimiter 构造限速器。interval ≤ 0 时 Wait 立即返回（不限速）。
func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{interval: interval}
}

// Wait 阻塞直到距上次放行至少 interval，或 ctx 取消。
// 返回 ctx.Err() 表示在等待期间被取消，未消费任何放行配额（不影响后续调用方）。
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interval <= 0 {
		return nil
	}
	wait := time.Until(l.last.Add(l.interval))
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.last = time.Now()
	return nil
}
