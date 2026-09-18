// Package server 是 HTTP 层：路由注册与各实体的 API handler。
// 它是唯一对外的门，Web 界面、外部脚本、CLI 子命令都走同一套 /api。
package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"github.com/ma6254/news-glean/internal/app"
	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/scheduler"
)

// Server 是 HTTP 服务。
type Server struct {
	cfg   *config.Config
	db    *database.DB
	app   *app.App
	sched *scheduler.Scheduler
	mux   *http.ServeMux
	http  *http.Server
}

// New 构造 Server 并注册路由。
func New(cfg *config.Config, db *database.DB, a *app.App, sched *scheduler.Scheduler) *Server {
	srv := &Server{cfg: cfg, db: db, app: a, sched: sched}
	srv.mux = srv.routes()
	return srv
}

// Handler 返回路由处理器（供测试与 Run 使用）。
func (s *Server) Handler() http.Handler { return s.mux }

// Run 监听并服务 HTTP 请求，阻塞直到进程退出。
func (s *Server) Run() error {
	addr := s.cfg.Server.HTTPAddr
	if addr == "" {
		addr = "127.0.0.1:28080"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.http = &http.Server{Addr: addr, Handler: s.mux}
	return s.http.Serve(ln)
}

// Stop 优雅关闭 HTTP 服务。
func (s *Server) Stop(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// writeJSON 以 JSON 写出响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ErrorResponse 是统一的错误响应体。
type ErrorResponse struct {
	Error string `json:"error"` // 错误信息
}

// writeError 以统一格式写出错误响应。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
