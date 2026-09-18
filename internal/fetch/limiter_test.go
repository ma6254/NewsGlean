package fetch

import (
	"context"
	"testing"
	"time"
)

// TestLimiterSpacing 验证相邻两次放行之间至少间隔 interval，且首个调用方立即放行。
func TestLimiterSpacing(t *testing.T) {
	const interval = 50 * time.Millisecond
	l := NewLimiter(interval)

	// 首次放行应立即返回（last 为零值）
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait: %v", err)
	}
	if first := time.Since(start); first > interval/2 {
		t.Fatalf("first Wait took %v, want immediate", first)
	}

	// 第二次放行应至少间隔 interval
	start = time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("second Wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < interval-5*time.Millisecond {
		t.Fatalf("second Wait elapsed %v, want >= %v", elapsed, interval)
	}
}

// TestLimiterContextCancel 验证等待期间 ctx 取消会立即返回且不消费放行配额。
func TestLimiterContextCancel(t *testing.T) {
	l := NewLimiter(time.Second)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := l.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("Wait err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Wait should return on ctx cancel, took %v", elapsed)
	}
}

// TestLimiterDisabled 验证 interval ≤ 0 时不限速。
func TestLimiterDisabled(t *testing.T) {
	l := NewLimiter(0)
	start := time.Now()
	for i := 0; i < 10; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Fatalf("disabled limiter should not delay, took %v", elapsed)
	}
}
