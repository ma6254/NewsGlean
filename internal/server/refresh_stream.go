package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleRefreshStream 处理 GET /api/refresh/stream（SSE 实时推送采集进度）。
// 事件为单行 `data: <json>\n\n`，字段见 app.ProgressEvent；心跳为注释行 `: ping`，
// 前端用 EventSource 订阅即可。
//
// @Summary      订阅采集进度（SSE）
// @Description  以 Server-Sent Events 实时推送采集进度（渠道开始/完成/失败），供前端显示进度与新内容高亮
// @Tags         refresh
// @Produce      text/event-stream
// @Success      200  {object}  app.ProgressEvent
// @Router       /refresh/stream [get]
func (s *Server) handleRefreshStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 关闭 nginx 等反代缓冲

	ch := s.app.SubscribeProgress()
	defer s.app.UnsubscribeProgress(ch)

	// 立即写一个注释事件，建立连接并触发 flush。
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownCh:
			// 服务关停：立即退出，让 Shutdown 能及时完成而不是等满超时。
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
