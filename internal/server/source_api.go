package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ma6254/news-glean/internal/database"
)

// SourceDTO 是渠道实例的对外表示（config 展开为 JSON 对象）。
type SourceDTO struct {
	ID            uint64          `json:"id"`              // 渠道实例ID
	Name          string          `json:"name"`            // 显示名
	Type          string          `json:"type"`            // 渠道类型标识
	Config        json.RawMessage `json:"config"`          // 渠道配置 JSON 对象
	Interval      int             `json:"interval"`        // 刷新间隔（秒）
	Enabled       bool            `json:"enabled"`         // 是否启用
	FailCount     int             `json:"fail_count"`      // 连续失败次数
	LastError     string          `json:"last_error"`      // 最近一次错误
	LastEntryAt   string          `json:"last_entry_at"`   // 最新条目的发布时间
	FetchCount    int64           `json:"fetch_count"`     // 累计采集次数
	SuccessCount  int64           `json:"success_count"`   // 累计成功次数
	SuccessRate   float64         `json:"success_rate"`    // 成功率（0~1）
	LastSuccessAt string          `json:"last_success_at"` // 最后成功时间（RFC3339）
	LastFetchAt   string          `json:"last_fetch_at"`   // 最近一次采集时间（RFC3339）
	LastElapsedMS int64           `json:"last_elapsed_ms"` // 最近一次耗时（毫秒）
	CreatedAt     string          `json:"created_at"`      // 创建时间
	UpdatedAt     string          `json:"updated_at"`      // 更新时间
}

// SourceListResponse 是渠道列表的响应体。
type SourceListResponse struct {
	Items []SourceDTO `json:"items"` // 渠道列表
	Total int         `json:"total"` // 总数
}

// SourceRequest 是创建/更新渠道的请求体。
type SourceRequest struct {
	Name     string          `json:"name"`     // 显示名
	Type     string          `json:"type"`     // 渠道类型标识
	Config   json.RawMessage `json:"config"`   // 渠道配置 JSON 对象
	Interval int             `json:"interval"` // 刷新间隔（秒）
	Enabled  *bool           `json:"enabled"`  // 是否启用
}

func toSourceDTO(s *database.Source) SourceDTO {
	cfg := json.RawMessage(s.Config)
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	return SourceDTO{
		ID:          s.ID,
		Name:        s.Name,
		Type:        s.Type,
		Config:      cfg,
		Interval:    s.Interval,
		Enabled:     s.Enabled,
		FailCount:   s.FailCount,
		LastError:   s.LastError,
		LastEntryAt: s.LastEntryAt,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

// applyFetchStats 把采集统计写入渠道 DTO，并计算成功率。
func applyFetchStats(dto *SourceDTO, st database.FetchStats) {
	dto.FetchCount = st.Total
	dto.SuccessCount = st.Success
	dto.LastSuccessAt = st.LastSuccessAt
	dto.LastFetchAt = st.LastStartedAt
	dto.LastElapsedMS = st.LastElapsedMS
	if st.Total > 0 {
		dto.SuccessRate = float64(st.Success) / float64(st.Total)
	}
}

// handleSourceCreate 处理 POST /api/source。
//
// @Summary      新增渠道实例
// @Description  校验配置后入库（走适配器 Validate，失败不入库）
// @Tags         source
// @Accept       json
// @Produce      json
// @Param        req  body      SourceRequest  true  "渠道参数"
// @Success      200  {object}  SourceDTO
// @Failure      400  {object}  ErrorResponse
// @Router       /source [post]
func (s *Server) handleSourceCreate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSourceRequest(w, r)
	if !ok {
		return
	}
	src, err := s.app.AddSource(req.Name, req.Type, string(req.Config), req.Interval, *req.Enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toSourceDTO(src))
}

// handleSourceList 处理 GET /api/source。
//
// @Summary      列出渠道实例
// @Tags         source
// @Produce      json
// @Success      200  {object}  SourceListResponse
// @Router       /source [get]
func (s *Server) handleSourceList(w http.ResponseWriter, _ *http.Request) {
	list, err := s.db.ListSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	latest, err := s.db.LatestPublishTimes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := s.db.FetchStatsBySource()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dtos := make([]SourceDTO, 0, len(list))
	for i := range list {
		list[i].LastEntryAt = latest[list[i].ID]
		dto := toSourceDTO(&list[i])
		applyFetchStats(&dto, stats[list[i].ID])
		dtos = append(dtos, dto)
	}
	writeJSON(w, http.StatusOK, SourceListResponse{Items: dtos, Total: len(dtos)})
}

// handleSourceGet 处理 GET /api/source/{id}。
//
// @Summary      获取单个渠道实例
// @Tags         source
// @Produce      json
// @Param        id  path      int  true  "渠道实例ID"
// @Success      200  {object}  SourceDTO
// @Failure      404  {object}  ErrorResponse
// @Router       /source/{id} [get]
func (s *Server) handleSourceGet(w http.ResponseWriter, r *http.Request) {
	src, ok := s.lookupSource(w, r)
	if !ok {
		return
	}
	latest, err := s.db.LatestPublishTime(src.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := s.db.FetchStatsBySource()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	src.LastEntryAt = latest
	dto := toSourceDTO(src)
	applyFetchStats(&dto, stats[src.ID])
	writeJSON(w, http.StatusOK, dto)
}

// handleSourceUpdate 处理 PUT /api/source/{id}。
//
// @Summary      更新渠道实例
// @Tags         source
// @Accept       json
// @Produce      json
// @Param        id   path      int            true  "渠道实例ID"
// @Param        req  body      SourceRequest  true  "渠道参数"
// @Success      200  {object}  SourceDTO
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /source/{id} [put]
func (s *Server) handleSourceUpdate(w http.ResponseWriter, r *http.Request) {
	src, ok := s.lookupSource(w, r)
	if !ok {
		return
	}
	req, ok := decodeSourceRequest(w, r)
	if !ok {
		return
	}

	// 仅覆盖请求中给出的字段
	src.Name = req.Name
	if req.Type != "" {
		src.Type = req.Type
	}
	if len(req.Config) > 0 {
		src.Config = string(req.Config)
	}
	if req.Interval > 0 {
		src.Interval = req.Interval
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	if err := s.db.UpdateSource(src); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toSourceDTO(src))
}

// handleSourceDelete 处理 DELETE /api/source/{id}（软删除）。
//
// @Summary      删除渠道实例（软删除）
// @Tags         source
// @Produce      json
// @Param        id  path      int  true  "渠道实例ID"
// @Success      200  {object}  map[string]bool
// @Failure      404  {object}  ErrorResponse
// @Router       /source/{id} [delete]
func (s *Server) handleSourceDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteSource(id); err != nil {
		if errors.Is(err, database.ErrorSourceNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// FetchLogDTO 是采集日志的对外表示。
type FetchLogDTO struct {
	ID        uint64 `json:"id"`         // 日志ID
	SourceID  uint64 `json:"source_id"`  // 归属渠道实例ID
	StartedAt string `json:"started_at"` // 采集开始时间（RFC3339）
	ElapsedMS int64  `json:"elapsed_ms"` // 耗时（毫秒）
	Inserted  int    `json:"inserted"`   // 新增条目数
	Skipped   int    `json:"skipped"`    // 去重跳过数
	Success   bool   `json:"success"`    // 是否成功
	Error     string `json:"error"`      // 错误信息（成功为空）
}

// FetchLogListResponse 是采集日志列表的响应体。
type FetchLogListResponse struct {
	Items []FetchLogDTO `json:"items"` // 日志列表（按时间倒序）
	Total int           `json:"total"` // 返回条数
}

func toFetchLogDTO(l *database.FetchLog) FetchLogDTO {
	return FetchLogDTO{
		ID:        l.ID,
		SourceID:  l.SourceID,
		StartedAt: l.StartedAt,
		ElapsedMS: l.ElapsedMS,
		Inserted:  l.Inserted,
		Skipped:   l.Skipped,
		Success:   l.Success,
		Error:     l.Error,
	}
}

// handleSourceLogs 处理 GET /api/source/{id}/logs（查看某渠道的采集历史）。
//
// @Summary      列出渠道采集日志
// @Description  按时间倒序返回指定渠道最近的采集日志，用于排障
// @Tags         source
// @Produce      json
// @Param        id     path      int  true  "渠道实例ID"
// @Param        limit  query     int  false "返回条数上限（默认 50，最大 200）"
// @Success      200    {object}  FetchLogListResponse
// @Failure      404    {object}  ErrorResponse
// @Router       /source/{id}/logs [get]
func (s *Server) handleSourceLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, err := s.db.GetSource(id); err != nil {
		if errors.Is(err, database.ErrorSourceNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := s.db.ListFetchLogs(id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]FetchLogDTO, 0, len(logs))
	for i := range logs {
		items = append(items, toFetchLogDTO(&logs[i]))
	}
	writeJSON(w, http.StatusOK, FetchLogListResponse{Items: items, Total: len(items)})
}

// SourceProbeRequest 是探测渠道元信息的请求体（保存前「自动获取」显示名等）。
type SourceProbeRequest struct {
	Type   string          `json:"type"`   // 渠道类型标识
	Config json.RawMessage `json:"config"` // 渠道配置 JSON 对象
}

// handleSourceProbe 处理 POST /api/source/probe。
//
// @Summary      探测渠道元信息
// @Description  在保存前探测渠道元信息（如 feed 标题），供「自动获取显示名」使用
// @Tags         source
// @Accept       json
// @Produce      json
// @Param        req  body      SourceProbeRequest  true  "渠道类型与配置"
// @Success      200  {object}  source.ProbeInfo
// @Failure      400  {object}  ErrorResponse
// @Router       /source/probe [post]
func (s *Server) handleSourceProbe(w http.ResponseWriter, r *http.Request) {
	var req SourceProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage("{}")
	}
	info, err := s.app.ProbeSource(r.Context(), req.Type, string(req.Config))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// handleSourceEnvCheck 处理 GET /api/source/env-check（检测渠道运行环境）。
//
// @Summary      检测渠道运行环境
// @Description  检测渠道的外部依赖（如 bilibili-cli）是否安装、是否登录，返回用户信息与安装提示
// @Tags         source
// @Produce      json
// @Param        type      query     string  true   "渠道类型标识"
// @Param        bili_path query     string  false  "可选：bilibili-cli 可执行文件路径覆盖"
// @Success      200       {object}  source.EnvCheck
// @Failure      400       {object}  ErrorResponse
// @Router       /source/env-check [get]
func (s *Server) handleSourceEnvCheck(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	if typ == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	// 可选：bili_path 覆盖（前端手动填执行文件路径时，用该路径重新检测）
	cfgJSON := "{}"
	if p := strings.TrimSpace(r.URL.Query().Get("bili_path")); p != "" {
		b, _ := json.Marshal(map[string]string{"bili_path": p})
		cfgJSON = string(b)
	}
	check, err := s.app.CheckEnv(r.Context(), typ, cfgJSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, check)
}

// handleRefresh 处理 POST /api/refresh（手动触发一轮采集）。
//
// @Summary      手动触发一轮采集
// @Description  遍历所有启用的渠道拉取新内容并写库（三层去重）
// @Tags         refresh
// @Produce      json
// @Success      200  {object}  app.RefreshResult
// @Router       /refresh [post]
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	result, err := s.sched.Refresh(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// decodeSourceRequest 解析并校验渠道请求体。config 展开为 JSON 对象。
func decodeSourceRequest(w http.ResponseWriter, r *http.Request) (SourceRequest, bool) {
	var req SourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return req, false
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return req, false
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return req, false
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage("{}")
	}
	if req.Interval <= 0 {
		req.Interval = 1800
	}
	if req.Enabled == nil {
		t := true
		req.Enabled = &t
	}
	return req, true
}

// lookupSource 按路径 {id} 查找渠道实例，失败时写出错误响应。
func (s *Server) lookupSource(w http.ResponseWriter, r *http.Request) (*database.Source, bool) {
	id, ok := parseID(w, r)
	if !ok {
		return nil, false
	}
	src, err := s.db.GetSource(id)
	if err != nil {
		if errors.Is(err, database.ErrorSourceNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return src, true
}

// parseID 解析路径参数 {id} 为 uint64。
func parseID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
