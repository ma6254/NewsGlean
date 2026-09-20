package server

// 阶段 6 —— 全文检索 HTTP 端点（GET /api/search）。

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ma6254/news-glean/internal/database"
)

// handleSearch 处理 GET /api/search（全文检索，命中 title/summary/content/author）。
//
// @Summary      全文检索
// @Description  检索条目标题/摘要/正文/作者；中文按 bigram 分词，多词按 AND 匹配
// @Tags         entry
// @Produce      json
// @Param        q          query     string  true   "关键词"
// @Param        page       query     int     false  "页码（从1开始）"
// @Param        page_size  query     int     false  "每页数量"
// @Param        source_id  query     int     false  "按渠道过滤"
// @Param        read       query     bool    false  "按已读状态过滤"
// @Param        favorite   query     bool    false  "按收藏状态过滤"
// @Param        archive    query     bool    false  "按归档状态过滤"
// @Success      200        {object}  EntryListResponse
// @Failure      400        {object}  ErrorResponse
// @Router       /search [get]
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kw := strings.TrimSpace(q.Get("q"))
	if kw == "" {
		writeError(w, http.StatusBadRequest, "missing query parameter q")
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	sourceID, _ := strconv.ParseUint(q.Get("source_id"), 10, 64)

	filter := database.EntryFilter{SourceID: sourceID}
	var err error
	if filter.Read, err = parseOptionalBool(q, "read"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid read filter")
		return
	}
	if filter.Favorite, err = parseOptionalBool(q, "favorite"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite filter")
		return
	}
	if filter.Archive, err = parseOptionalBool(q, "archive"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid archive filter")
		return
	}

	list, total, err := s.db.SearchEntries(kw, filter, page, pageSize)
	if err != nil {
		if errors.Is(err, database.ErrEmptyQuery) {
			writeError(w, http.StatusBadRequest, "empty search query")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := s.entryDTOs(list)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, EntryListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}
