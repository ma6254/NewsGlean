package log

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestAsyncQueueOrderAndOverflow(t *testing.T) {
	q := newAsyncQueue(3)
	q.Push(Record{Message: "a"})
	q.Push(Record{Message: "b"})
	q.Push(Record{Message: "c"})
	q.Push(Record{Message: "d"}) // 满：覆盖最旧 a
	if q.Dropped() != 1 {
		t.Errorf("dropped = %d, want 1", q.Dropped())
	}
	want := []string{"b", "c", "d"}
	for _, w := range want {
		rec, ok := q.Pop()
		if !ok || rec.Message != w {
			t.Fatalf("pop = %q/%v, want %q", rec.Message, ok, w)
		}
	}
	// 空队列 Close 后 Pop 返回 ok=false
	q.Close()
	if _, ok := q.Pop(); ok {
		t.Error("pop after close should return ok=false")
	}
}

func TestAsyncQueuePopBlocksUntilPush(t *testing.T) {
	q := newAsyncQueue(2)
	done := make(chan string, 1)
	go func() {
		rec, ok := q.Pop()
		if ok {
			done <- rec.Message
		}
	}()
	time.Sleep(50 * time.Millisecond) // 让 Pop 先阻塞
	q.Push(Record{Message: "wake"})
	select {
	case m := <-done:
		if m != "wake" {
			t.Errorf("got %q", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pop did not wake up")
	}
	q.Close()
}

func TestWSSinkEndToEnd(t *testing.T) {
	core := NewCore()
	sink := NewWSSink("127.0.0.1:0", 10)
	if err := sink.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	core.AddSink(ChannelAccess, sink)
	core.AddSink(ChannelError, sink)
	defer core.Close()

	l := NewLogger(core)
	l.Info("hello ws", "k", "v")
	l.Error("boom ws")

	addr := sink.Addr()
	if addr == "" {
		t.Fatal("no addr")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// 应收到两条 JSON 事件：hello ws（access）+ boom ws（error）
	msgs := make([]string, 0, 2)
	for len(msgs) < 2 {
		rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, data, err := conn.Read(rctx)
		rcancel()
		if err != nil {
			t.Fatalf("read: %v (got %v)", err, msgs)
		}
		msgs = append(msgs, string(data))
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, `"msg":"hello ws"`) || !strings.Contains(joined, `"channel":"access"`) {
		t.Errorf("missing access event: %v", msgs)
	}
	if !strings.Contains(joined, `"msg":"boom ws"`) || !strings.Contains(joined, `"channel":"error"`) {
		t.Errorf("missing error event: %v", msgs)
	}
	if !strings.Contains(joined, `"k":"v"`) {
		t.Errorf("missing field: %v", msgs)
	}
}

func TestWSSinkReplayOnConnect(t *testing.T) {
	core := NewCore()
	sink := NewWSSink("127.0.0.1:0", 5)
	if err := sink.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	core.AddSink(ChannelAccess, sink)
	defer core.Close()

	l := NewLogger(core)
	for i := 1; i <= 3; i++ {
		l.Info("before connect")
	}
	// 等队列消费完
	deadline := time.Now().Add(2 * time.Second)
	for sink.queue.Size() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+sink.Addr()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	count := 0
	for count < 3 {
		rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, data, err := conn.Read(rctx)
		rcancel()
		if err != nil {
			t.Fatalf("read: %v (got %d)", err, count)
		}
		if strings.Contains(string(data), `"msg":"before connect"`) {
			count++
		}
	}
}
