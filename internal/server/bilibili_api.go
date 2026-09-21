package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/source/bilibili"
)

// handleBilibiliLogin 处理 POST /api/bilibili/login：手动写入 SESSDATA/bili_jct 凭证。
// 注意：SESSDATA/bili_jct 属敏感信息，仅存在于请求体，绝不写日志、绝不回显。
//
// @Summary      手动登录 bilibili
// @Description  把 SESSDATA/bili_jct 写入 bilibili-cli 凭证文件（~/.bilibili-cli/credential.json），并回读校验登录态
// @Tags         bilibili
// @Accept       json
// @Produce      json
// @Param        req  body      object  true  "{sessdata, bili_jct, buvid3?}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /bilibili/login [post]
func (s *Server) handleBilibiliLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sessdata string `json:"sessdata"`
		BiliJct  string `json:"bili_jct"`
		Buvid3   string `json:"buvid3"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Sessdata) == "" {
		writeError(w, http.StatusBadRequest, "sessdata is required")
		return
	}
	if strings.TrimSpace(req.BiliJct) == "" {
		writeError(w, http.StatusBadRequest, "bili_jct is required")
		return
	}
	if _, err := bilibili.WriteCredential(req.Sessdata, req.BiliJct, req.Buvid3); err != nil {
		writeError(w, http.StatusInternalServerError, "写入凭证失败: "+err.Error())
		return
	}
	// 回读校验并返回最新登录态与用户信息。
	check, err := s.app.CheckEnv(r.Context(), "bilibili", "{}")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authed": check.Authed, "user": check.User})
}

// handleBilibiliFavorites 处理 GET /api/bilibili/favorites：列出当前登录用户的收藏夹。
//
// @Summary      列出收藏夹
// @Description  调用 bili favorites 返回当前登录用户的收藏夹列表（需已登录）
// @Tags         bilibili
// @Produce      json
// @Param        bili_path  query     string  false  "可选：bilibili-cli 可执行文件路径覆盖"
// @Success      200        {object}  map[string]any
// @Failure      400        {object}  ErrorResponse
// @Router       /bilibili/favorites [get]
func (s *Server) handleBilibiliFavorites(w http.ResponseWriter, r *http.Request) {
	folders, err := bilibili.ListFavorites(r.Context(), r.URL.Query().Get("bili_path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": folders})
}

// handleBilibiliBackfill 处理 POST /api/bilibili/backfill：对指定 bilibili 源中摘要为空的条目补拉简介。
//
// @Summary      回填简介
// @Description  对指定 bilibili 源中摘要为空的条目，逐条调用 bili video 补拉简介
// @Tags         bilibili
// @Accept       json
// @Produce      json
// @Param        req  body      object  true  "{source_id}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /bilibili/backfill [post]
func (s *Server) handleBilibiliBackfill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceID uint64 `json:"source_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.SourceID == 0 {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return
	}
	src, err := s.db.GetSource(req.SourceID)
	if err != nil {
		if errors.Is(err, database.ErrorSourceNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if src.Type != database.SourceTypeBilibili {
		writeError(w, http.StatusBadRequest, "仅支持 bilibili 渠道")
		return
	}
	var cfg bilibili.Config
	_ = json.Unmarshal([]byte(src.Config), &cfg)

	entries, err := s.db.ListEntriesForBackfill(req.SourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	count := 0
	for _, e := range entries {
		d, derr := bilibili.GetVideoDetail(r.Context(), e.GUID, cfg.BiliPath, false)
		if derr != nil || d.Description == "" {
			continue // 单条失败/无简介，跳过
		}
		extraMap := map[string]string{}
		if e.Extra != "" {
			_ = json.Unmarshal([]byte(e.Extra), &extraMap)
		}
		extraMap["bili_view"] = strconv.Itoa(d.Stats.View)
		extraMap["bili_like"] = strconv.Itoa(d.Stats.Like)
		if cover, cerr := bilibili.GetVideoCover(r.Context(), e.GUID); cerr == nil && cover != "" {
			extraMap["cover"] = cover
		}
		extraJSON, _ := json.Marshal(extraMap)
		if err := s.db.UpdateEntryEnrich(e.ID, d.Description, string(extraJSON)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		count++
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backfilled": count, "total": len(entries)})
}
