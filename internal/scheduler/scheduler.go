// Package scheduler 负责采集调度。M1 只提供手动触发入口；
// 定时后台调度（拉取型）与常驻入站服务（推送型）在 M3 落地。
package scheduler

import (
	"context"

	"github.com/ma6254/news-glean/internal/app"
)

// Scheduler 是采集调度器。
type Scheduler struct {
	app *app.App
}

// New 构造调度器。
func New(a *app.App) *Scheduler {
	return &Scheduler{app: a}
}

// Refresh 手动触发一轮采集（所有启用的渠道）。
func (s *Scheduler) Refresh(ctx context.Context) (*app.RefreshResult, error) {
	return s.app.RefreshAll(ctx)
}

// RefreshSource 手动触发单个渠道的采集。
func (s *Scheduler) RefreshSource(ctx context.Context, id uint64) (*app.RefreshResult, error) {
	return s.app.RefreshSource(ctx, id)
}
