package log

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ma6254/news-glean/log/ws"
)

// asyncQueue 有界环形队列：满时覆盖最旧条目并计数（慢 Sink 异步化）。
// Push 永不阻塞调用方；Pop 在队列空时阻塞。
type asyncQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	buf     []Record
	head    int
	size    int
	cap     int
	dropped int64
	closed  bool
}

func newAsyncQueue(capacity int) *asyncQueue {
	q := &asyncQueue{cap: capacity, buf: make([]Record, capacity)}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push 入队；满时覆盖最旧并递增 dropped。
func (q *asyncQueue) Push(rec Record) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	if q.size < q.cap {
		q.buf[(q.head+q.size)%q.cap] = rec
		q.size++
	} else {
		q.buf[q.head] = rec
		q.head = (q.head + 1) % q.cap
		q.dropped++
	}
	q.cond.Signal()
	q.mu.Unlock()
}

// Pop 出队；队列空时阻塞等待。Close 后且队列空时返回 ok=false。
func (q *asyncQueue) Pop() (Record, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.size == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.size == 0 {
		return Record{}, false
	}
	rec := q.buf[q.head]
	q.head = (q.head + 1) % q.cap
	q.size--
	return rec, true
}

// Close 关闭队列并唤醒所有等待的 Pop。
func (q *asyncQueue) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// Dropped 返回因队列满而被覆盖丢弃的记录数。
func (q *asyncQueue) Dropped() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}

// Size 返回当前队列长度。
func (q *asyncQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size
}

// WSSink WebSocket 输出端：慢 Sink 异步化（内部队列），
// JSON 事件流推送到网页，含环形缓冲历史补发与自动重连。
type WSSink struct {
	addr   string
	ring   int
	queue  *asyncQueue
	hub    *ws.Hub
	server *http.Server
	ln     net.Listener

	mu     sync.Mutex
	opened bool
	closed bool
}

// NewWSSink 创建 WebSocket 输出端。addr 如 ":8080" 或 "127.0.0.1:0"；
// ringSize 为网页新连接的历史补发条数。
func NewWSSink(addr string, ringSize int) *WSSink {
	return &WSSink{
		addr:  addr,
		ring:  ringSize,
		queue: newAsyncQueue(1000),
	}
}

// Addr 返回实际监听地址（Open 成功后有效，用于端口 0 场景）。
func (s *WSSink) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Open 启动 HTTP 服务（内置页面 / + WebSocket /ws）。幂等：
// 同一实例注册到多个通道时只会启动一次。
func (s *WSSink) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.opened {
		return nil
	}
	s.hub = ws.NewHub(s.ring)
	mux := http.NewServeMux()
	mux.Handle("/", s.hub)
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.server = &http.Server{Addr: s.addr, Handler: mux}
	go func() { _ = s.server.Serve(ln) }()
	go s.consume()
	s.opened = true
	return nil
}

// consume 队列 → hub 广播（序列化后的 JSON 行）。
func (s *WSSink) consume() {
	for {
		rec, ok := s.queue.Pop()
		if !ok {
			return
		}
		s.hub.Broadcast(json.RawMessage(FormatJSONLine(rec)))
	}
}

// Write 入队（满时覆盖最旧，永不阻塞调用方）。
func (s *WSSink) Write(rec Record) {
	s.queue.Push(rec)
}

// Close 关闭队列与 HTTP 服务，并报告丢弃计数。
func (s *WSSink) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	server := s.server
	s.mu.Unlock()

	if dropped := s.queue.Dropped(); dropped > 0 {
		fmt.Fprintf(os.Stderr, "log: ws sink dropped %d records (queue full)\n", dropped)
	}
	s.queue.Close()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
	return nil
}
