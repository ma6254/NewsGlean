package server

import (
	"net/http"
	"time"

	"github.com/ma6254/news-glean/log"
)

// httpLogger 是 HTTP 层的日志器，带 http tag。
var httpLogger = log.WithTag("http")

// statusRecorder 包装 ResponseWriter 以捕获响应状态码（未显式 WriteHeader 时按 200 计）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 转发到底层 writer（若支持），保证反代/流式响应不被中间件破坏。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// loggingMiddleware 记录每个 HTTP 请求：方法、路径、状态码与耗时。
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		httpLogger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Microsecond).String(),
		)
	})
}
