package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRefreshStreamSSE 验证 SSE 端点：订阅后触发刷新，能实时收到 source_done 进度事件。
func TestRefreshStreamSSE(t *testing.T) {
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody))
	}))
	defer feedSrv.Close()

	ts := newTestServer(t)

	add := doJSON(t, http.MethodPost, ts.URL+"/api/source",
		`{"name":"test","type":"feed","config":{"url":"`+feedSrv.URL+`"}}`)
	if add.status != http.StatusOK {
		t.Fatalf("add source status = %d, body = %s", add.status, add.body)
	}

	// 订阅 SSE（Do 在收到首帧并 flush 后返回，此时 handler 已订阅进度中心）
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/refresh/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", res.StatusCode)
	}

	type event struct {
		Type     string `json:"type"`
		SourceID uint64 `json:"source_id"`
		Inserted int    `json:"inserted"`
	}
	evCh := make(chan event, 32)
	go func() {
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var e event
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e) == nil {
				evCh <- e
			}
		}
	}()

	// 触发刷新，应实时收到 source_done（含新增条数）
	if r := doJSON(t, http.MethodPost, ts.URL+"/api/refresh", ""); r.status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", r.status, r.body)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-evCh:
			if e.Type == "source_done" {
				if e.Inserted != 2 {
					t.Fatalf("source_done inserted = %d, want 2", e.Inserted)
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for source_done event")
		}
	}
}
