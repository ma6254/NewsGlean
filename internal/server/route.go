package server

import (
	"net/http"

	_ "github.com/ma6254/news-glean/docs" // swag init 生成的文档包
	httpSwagger "github.com/swaggo/http-swagger"
)

// routes 注册全部 HTTP 路由（方法 + 路径模式）。
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// 渠道 CRUD
	mux.HandleFunc("POST /api/source", s.handleSourceCreate)
	mux.HandleFunc("GET /api/source", s.handleSourceList)
	mux.HandleFunc("GET /api/source/{id}", s.handleSourceGet)
	mux.HandleFunc("PUT /api/source/{id}", s.handleSourceUpdate)
	mux.HandleFunc("DELETE /api/source/{id}", s.handleSourceDelete)

	// 手动触发采集
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)

	// 条目读取
	mux.HandleFunc("GET /api/entry/list", s.handleEntryList)
	mux.HandleFunc("GET /api/entry/{id}", s.handleEntryGet)

	// Swagger UI
	mux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Web 前端（按 web.mode 接入：embed / proxy / dir / gz / off）
	s.mountWeb(mux)

	return mux
}
