package app

import (
	"testing"
	"time"
)

// TestProgressHubPublishSubscribe 验证发布-订阅能收到事件。
func TestProgressHubPublishSubscribe(t *testing.T) {
	a := New(nil, nil) // 仅测进度中心，cfg/db 置空
	ch := a.SubscribeProgress()
	defer a.UnsubscribeProgress(ch)

	a.hub.publish(ProgressEvent{Type: EventSourceStarted, SourceID: 7, SourceName: "x"})
	select {
	case ev := <-ch:
		if ev.Type != EventSourceStarted || ev.SourceID != 7 || ev.SourceName != "x" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}

// TestProgressHubPublishNonBlocking 验证 publish 非阻塞：订阅者消费不及时时丢弃事件而非阻塞。
func TestProgressHubPublishNonBlocking(t *testing.T) {
	a := New(nil, nil)
	ch := a.SubscribeProgress()
	defer a.UnsubscribeProgress(ch)

	// 通道容量 64，灌 200 条：前 64 条进缓冲，其余丢弃，publish 始终不阻塞。
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			a.hub.publish(ProgressEvent{Type: EventSourceDone, SourceID: uint64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked when subscriber is full")
	}

	if got := len(ch); got != 64 {
		t.Fatalf("buffered events = %d, want 64", got)
	}
}
