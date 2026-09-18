package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dial 建立测试客户端连接。
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func readText(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func TestHubRingReplay(t *testing.T) {
	h := NewHub(3)
	srv := httptest.NewServer(h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// 先广播 5 条（环形缓冲只保留最后 3 条）
	for i := 1; i <= 5; i++ {
		h.Broadcast(json.RawMessage(`{"n":` + string(rune('0'+i)) + `}`))
	}
	_ = h // noop

	conn := dial(t, url)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// 新连接应先收到历史（n=3,4,5），再收到实时消息
	got := make([]string, 0, 4)
	for i := 0; i < 3; i++ {
		got = append(got, readText(t, conn))
	}
	want := []string{`{"n":3}`, `{"n":4}`, `{"n":5}`}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Fatalf("replay[%d] = %q, want contains %q (all: %v)", i, got[i], want[i], got)
		}
	}

	// 实时广播
	h.Broadcast(json.RawMessage(`{"n":6}`))
	live := readText(t, conn)
	if !strings.Contains(live, `{"n":6}`) {
		t.Errorf("live = %q, want n=6", live)
	}
}

func TestHubPageServed(t *testing.T) {
	h := NewHub(10)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	// 页面应包含 websocket 连接逻辑
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), "WebSocket") && !strings.Contains(string(buf[:n]), "ws") {
		t.Errorf("page missing ws client code")
	}
	if resp.Header.Get("Content-Type") == "" {
		t.Error("missing content-type")
	}
}

func TestHubClientCountAndRemove(t *testing.T) {
	h := NewHub(10)
	srv := httptest.NewServer(h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	conn := dial(t, url)
	defer conn.Close(websocket.StatusNormalClosure, "")
	// 等待注册完成
	deadline := time.Now().Add(2 * time.Second)
	for h.ClientCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.ClientCount() != 1 {
		t.Fatalf("client count = %d, want 1", h.ClientCount())
	}

	conn.Close(websocket.StatusNormalClosure, "test")
	deadline = time.Now().Add(2 * time.Second)
	for h.ClientCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.ClientCount() != 0 {
		t.Errorf("client count = %d, want 0 after close", h.ClientCount())
	}
}

// 并发广播不 panic、不泄漏（-race 下验证）。
func TestHubConcurrentBroadcast(t *testing.T) {
	h := NewHub(100)
	srv := httptest.NewServer(h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	conn := dial(t, url)
	defer conn.Close(websocket.StatusNormalClosure, "")
	// 消费端并行读
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	var bwg sync.WaitGroup
	for i := 0; i < 8; i++ {
		bwg.Add(1)
		go func() {
			defer bwg.Done()
			for j := 0; j < 50; j++ {
				h.Broadcast(json.RawMessage(`{"x":1}`))
			}
		}()
	}
	bwg.Wait()
	conn.Close(websocket.StatusNormalClosure, "done")
	wg.Wait()
}
