package app

import "sync"

// 采集进度事件类型。
const (
	EventSourceStarted = "source_started" // 单个渠道开始采集
	EventSourceDone    = "source_done"    // 单个渠道采集成功
	EventSourceFailed  = "source_failed"  // 单个渠道采集失败
)

// ProgressEvent 是采集进度事件，经 server 的 SSE 端点推送给前端。
type ProgressEvent struct {
	Type        string   `json:"type"`                   // 事件类型（见 Event* 常量）
	SourceID    uint64   `json:"source_id,omitempty"`    // 渠道ID（source_* 事件）
	SourceName  string   `json:"source_name,omitempty"`  // 渠道显示名
	Inserted    int      `json:"inserted,omitempty"`     // 新增条目数
	InsertedIDs []uint64 `json:"inserted_ids,omitempty"` // 新增条目ID（供前端高亮）
	Skipped     int      `json:"skipped,omitempty"`      // 去重跳过数
	ElapsedMS   int64    `json:"elapsed_ms,omitempty"`   // 耗时（毫秒）
	Error       string   `json:"error,omitempty"`        // 错误信息
}

// progressHub 是采集进度的发布-订阅中心：app 发布，server 的 SSE 端点订阅。
// publish 是非阻塞的：订阅者消费不及时就丢弃事件，绝不拖慢采集主流程。
type progressHub struct {
	mu   sync.Mutex
	subs map[chan ProgressEvent]struct{}
}

func newProgressHub() *progressHub {
	return &progressHub{subs: make(map[chan ProgressEvent]struct{})}
}

func (h *progressHub) subscribe() chan ProgressEvent {
	ch := make(chan ProgressEvent, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *progressHub) unsubscribe(ch chan ProgressEvent) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *progressHub) publish(ev ProgressEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default: // 订阅者消费太慢，丢弃该事件
		}
	}
}
