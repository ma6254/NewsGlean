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
	mux.HandleFunc("GET /api/source/{id}/logs", s.handleSourceLogs)
	mux.HandleFunc("PUT /api/source/{id}", s.handleSourceUpdate)
	mux.HandleFunc("DELETE /api/source/{id}", s.handleSourceDelete)

	// 渠道元信息探测（保存前「自动获取显示名」）
	mux.HandleFunc("POST /api/source/probe", s.handleSourceProbe)

	// 渠道运行环境检测（外部依赖是否安装/登录）
	mux.HandleFunc("GET /api/source/env-check", s.handleSourceEnvCheck)

	// bilibili 手动登录（写 SESSDATA/bili_jct 到 bilibili-cli 凭证文件）
	mux.HandleFunc("POST /api/bilibili/login", s.handleBilibiliLogin)

	// bilibili 收藏夹列表（需登录）
	mux.HandleFunc("GET /api/bilibili/favorites", s.handleBilibiliFavorites)

	// bilibili 简介回填（对已有空简介条目补拉）
	mux.HandleFunc("POST /api/bilibili/backfill", s.handleBilibiliBackfill)

	// 手动触发采集
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)

	// 采集进度实时推送（SSE）
	mux.HandleFunc("GET /api/refresh/stream", s.handleRefreshStream)

	// 全文检索
	mux.HandleFunc("GET /api/search", s.handleSearch)

	// 导出
	mux.HandleFunc("POST /api/export/markdown", s.handleExportMarkdown)
	mux.HandleFunc("POST /api/export/json", s.handleExportJSON)
	mux.HandleFunc("POST /api/export/epub", s.handleExportEPUB)
	mux.HandleFunc("GET /api/export/download", s.handleExportDownload)

	// 条目读取
	mux.HandleFunc("GET /api/entry/list", s.handleEntryList)
	mux.HandleFunc("GET /api/entry/read-later", s.handleReadLaterList)
	mux.HandleFunc("GET /api/entry/{id}", s.handleEntryGet)
	mux.HandleFunc("PUT /api/entry/{id}/read", s.handleEntrySetRead)
	mux.HandleFunc("PUT /api/entry/{id}/favorite", s.handleEntrySetFavorite)
	mux.HandleFunc("PUT /api/entry/{id}/archive", s.handleEntrySetArchive)
	mux.HandleFunc("PUT /api/entry/{id}/read-later", s.handleEntrySetReadLater)

	// 系统信息（版本 / 构建时间 / 运行状态 / 操作系统信息）
	mux.HandleFunc("GET /api/sys/info", s.handleSysInfo)
	mux.HandleFunc("GET /api/sys/state", s.handleSysState)
	mux.HandleFunc("GET /api/os/info", s.handleOsInfo)
	mux.HandleFunc("GET /api/os/state", s.handleOsState)

	// Swagger UI
	mux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Web 前端（按 web.mode 接入：embed / proxy / dir / gz / off）
	s.mountWeb(mux)

	return mux
}
