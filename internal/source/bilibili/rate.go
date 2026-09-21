package bilibili

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// 限速与重试参数（对 B 站风控友好）。
const (
	defaultRateInterval = 500 * time.Millisecond // 相邻请求最小间隔
	maxRetries          = 3                       // 最多重试 3 次
	retryBaseDelay      = 500 * time.Millisecond  // 指数退避基期：500ms/1s
)

// rateLimiter 限制进程内对 B 站请求的最小间隔（并发安全）。
type rateLimiter struct {
	mu   sync.Mutex
	min  time.Duration
	last time.Time
}

func newRateLimiter(min time.Duration) *rateLimiter {
	return &rateLimiter{min: min}
}

// Wait 阻塞直到距上次请求已超过 min；ctx 取消则立即返回。
func (l *rateLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if !l.last.IsZero() {
		if wait := l.min - now.Sub(l.last); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
			now = time.Now()
		}
	}
	l.last = now
	return nil
}

// biliRateLimiter 是包级共享限速器：所有 B 站请求（CLI 与直连 HTTP）共用。
var biliRateLimiter = newRateLimiter(defaultRateInterval)

// retry 执行 fn，最多 attempts 次；仅当 retryable(err) 为 true 时重试，退避按 baseDelay 指数增长。
func retry(ctx context.Context, attempts int, baseDelay time.Duration, retryable func(error) bool, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if !retryable(err) || i == attempts-1 {
			return err
		}
		delay := baseDelay << i
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// cliRetryable 判断 bilibili-cli 返回的错误是否可重试（限流/网络）。
func cliRetryable(err error) bool {
	var ce *cliErr
	if errors.As(err, &ce) {
		return ce.Code == "rate_limited" || ce.Code == "network_error"
	}
	return false
}

// httpStatusError 是直连 B 站接口的非 2xx 错误。
type httpStatusError struct {
	status int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("bilibili: view 接口状态 %d", e.status)
}

// httpRetryable 判断直连 HTTP 请求是否可重试（网络错误 / 412 / 5xx）。
func httpRetryable(err error) bool {
	var se *httpStatusError
	if errors.As(err, &se) {
		return se.status == 412 || se.status >= 500
	}
	var ne net.Error
	return errors.As(err, &ne)
}
