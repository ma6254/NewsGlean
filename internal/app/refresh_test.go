package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ma6254/news-glean/internal/database"
)

// TestRefreshOneContextCanceledNotRecorded 验证：停机/断连导致的 context.Canceled
// 不计入健康度（fail_count/last_error 不变），也不落 fetch_log。
func TestRefreshOneContextCanceledNotRecorded(t *testing.T) {
	a := newTestApp(t)

	src, err := a.AddSource("test", database.SourceTypeFeed, `{"url":"http://127.0.0.1:1"}`, 1, true)
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟 Ctrl+C / 客户端断连：请求 context 已被取消

	_, _, err = a.refreshOne(ctx, *src)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("refreshOne error = %v, want context.Canceled", err)
	}

	fresh, err := a.db.GetSource(src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if fresh.FailCount != 0 {
		t.Fatalf("fail_count = %d, want 0 (取消不应计为失败)", fresh.FailCount)
	}
	if fresh.LastError != "" {
		t.Fatalf("last_error = %q, want empty (取消不应写入最近错误)", fresh.LastError)
	}

	logs, err := a.db.ListFetchLogs(src.ID, 10)
	if err != nil {
		t.Fatalf("ListFetchLogs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("fetch_log rows = %d, want 0 (取消不应落采集日志)", len(logs))
	}
}
