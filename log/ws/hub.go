// Package ws 提供 WebSocket 日志实时推送：连接管理、环形缓冲历史补发、广播。
// 内置只读网页（index.html），浏览器打开即可实时查看日志。
package ws

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

//go:embed index.html
var indexHTML []byte

// Hub 管理 WebSocket 客户端连接，维护最近日志的环形缓冲。
// 新连接先补发历史（按时间顺序），再进入实时广播。并发安全。
type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	ring    []json.RawMessage
	pos     int
	full    bool
	maxRing int
}

// NewHub 创建环形缓冲大小为 maxRing 的 Hub。
func NewHub(maxRing int) *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]struct{}),
		maxRing: maxRing,
	}
}

// Broadcast 广播一条日志（已序列化的 JSON 行），并写入环形缓冲。
// 写失败（客户端断开）的连接会被移除。
func (h *Hub) Broadcast(msg json.RawMessage) {
	h.mu.Lock()
	if len(h.ring) < h.maxRing {
		h.ring = append(h.ring, msg)
	} else {
		h.ring[h.pos] = msg
		h.pos = (h.pos + 1) % h.maxRing
		h.full = true
	}
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := c.Write(ctx, websocket.MessageText, msg)
		cancel()
		if err != nil {
			h.Remove(c)
		}
	}
}

// history 返回环形缓冲中按时间顺序排列的历史（需持有读锁）。
func (h *Hub) history() []json.RawMessage {
	if !h.full {
		out := make([]json.RawMessage, len(h.ring))
		copy(out, h.ring)
		return out
	}
	out := make([]json.RawMessage, 0, h.maxRing)
	out = append(out, h.ring[h.pos:]...)
	out = append(out, h.ring[:h.pos]...)
	return out
}

// Add 注册新连接并补发历史。历史补发失败则拒绝该连接。
func (h *Hub) Add(conn *websocket.Conn) {
	h.mu.RLock()
	history := h.history()
	h.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, m := range history {
		if err := conn.Write(ctx, websocket.MessageText, m); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "history failed")
			return
		}
	}

	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

// Remove 移除并关闭连接（幂等）。
func (h *Hub) Remove(conn *websocket.Conn) {
	h.mu.Lock()
	_, ok := h.clients[conn]
	delete(h.clients, conn)
	h.mu.Unlock()
	if ok {
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}
}

// ClientCount 当前连接数。
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ServeHTTP 提供 /（内置页面）与 /ws（WebSocket 升级）。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/", "/index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	case "/ws":
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			return
		}
		h.Add(conn)
		// 读循环：仅用于检测断开（网页端是只读的，不处理消息）。
		ctx := r.Context()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				break
			}
		}
		h.Remove(conn)
	default:
		http.NotFound(w, r)
	}
}
