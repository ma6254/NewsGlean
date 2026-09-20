package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ma6254/news-glean/internal/database"
	"gorm.io/gorm"
)

// EntryDTO 是条目的对外表示（tags/extra 展开为原生结构）。
type EntryDTO struct {
	ID          uint64            `json:"id"`           // 条目ID
	SourceID    uint64            `json:"source_id"`    // 归属渠道实例ID
	GUID        string            `json:"guid"`         // 渠道自带稳定ID
	URL         string            `json:"url"`          // 规范化后的链接
	Title       string            `json:"title"`        // 标题
	Author      string            `json:"author"`       // 作者
	PublishedAt string            `json:"published_at"` // 发布时间（RFC3339）
	Summary     string            `json:"summary"`      // 摘要
	Content     string            `json:"content"`      // 正文
	ContentType string            `json:"content_type"` // 内容类型
	Tags        []string          `json:"tags"`         // 标签
	Extra       map[string]string `json:"extra"`        // 渠道特有字段
	FetchedAt   string            `json:"fetched_at"`   // 抓取时间（RFC3339）
	Read        bool              `json:"read"`         // 是否已读
	Favorite    bool              `json:"favorite"`     // 是否已收藏
	Archive     bool              `json:"archive"`      // 是否已归档
	ReadLater   bool              `json:"read_later"`   // 是否已加入「稍后再阅」
}

// EntryListResponse 是条目列表的响应体。
type EntryListResponse struct {
	Items    []EntryDTO `json:"items"`     // 条目列表
	Total    int64      `json:"total"`     // 总数
	Page     int        `json:"page"`      // 页码
	PageSize int        `json:"page_size"` // 每页数量
}

// toEntryDTO 把条目与阅读状态映射为对外 DTO。st 来自 entry_state 表，
// 由调用方查询后传入，避免本函数反向依赖数据库查询。
func toEntryDTO(e *database.Entry, st database.EntryState) EntryDTO {
	var tags []string
	if e.Tags != "" {
		_ = json.Unmarshal([]byte(e.Tags), &tags)
	}
	if tags == nil {
		tags = []string{}
	}
	var extra map[string]string
	if e.Extra != "" {
		_ = json.Unmarshal([]byte(e.Extra), &extra)
	}
	if extra == nil {
		extra = map[string]string{}
	}
	return EntryDTO{
		ID:          e.ID,
		SourceID:    e.SourceID,
		GUID:        e.GUID,
		URL:         e.URL,
		Title:       e.Title,
		Author:      e.Author,
		PublishedAt: e.PublishedAt,
		Summary:     e.Summary,
		Content:     e.Content,
		ContentType: e.ContentType,
		Tags:        tags,
		Extra:       extra,
		FetchedAt:   e.FetchedAt,
		Read:        st.Read,
		Favorite:    st.Favorite,
		Archive:     st.Archive,
		ReadLater:   st.ReadLater,
	}
}

// parseOptionalBool 解析可选的布尔查询参数；缺省时返回 (nil, nil)，值非法时返回错误。
func parseOptionalBool(q url.Values, key string) (*bool, error) {
	s := q.Get(key)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// handleEntryList 处理 GET /api/entry/list（分页，可按渠道与阅读状态过滤）。
//
// @Summary      列出条目
// @Description  分页列出条目，可按渠道、已读/收藏/归档状态过滤
// @Tags         entry
// @Produce      json
// @Param        page       query     int   false  "页码（从1开始）"
// @Param        page_size  query     int   false  "每页数量"
// @Param        source_id  query     int   false  "按渠道过滤"
// @Param        read       query     bool  false  "按已读状态过滤"
// @Param        favorite   query     bool  false  "按收藏状态过滤"
// @Param        archive    query     bool  false  "按归档状态过滤"
// @Success      200        {object}  EntryListResponse
// @Router       /entry/list [get]
func (s *Server) handleEntryList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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

	list, total, err := s.db.ListEntries(page, pageSize, filter)
	if err != nil {
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

// entryDTOs 把一批条目连同各自阅读状态映射为 DTO 列表。
func (s *Server) entryDTOs(list []database.Entry) ([]EntryDTO, error) {
	ids := make([]uint64, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	states, err := s.db.GetEntryStates(ids)
	if err != nil {
		return nil, err
	}
	items := make([]EntryDTO, 0, len(list))
	for i := range list {
		items = append(items, toEntryDTO(&list[i], states[list[i].ID]))
	}
	return items, nil
}

// handleEntryGet 处理 GET /api/entry/{id}。
//
// @Summary      获取单个条目
// @Tags         entry
// @Produce      json
// @Param        id  path      int  true  "条目ID"
// @Success      200  {object}  EntryDTO
// @Failure      404  {object}  ErrorResponse
// @Router       /entry/{id} [get]
func (s *Server) handleEntryGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := s.db.GetEntry(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state, err := s.db.GetEntryState(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toEntryDTO(entry, state))
}

// handleReadLaterList 处理 GET /api/entry/read-later（分页列出「稍后再阅」条目）。
//
// @Summary      列出稍后再阅条目
// @Description  分页列出标记为「稍后再阅」的条目，按加入时间倒序
// @Tags         entry
// @Produce      json
// @Param        page       query     int  false  "页码（从1开始）"
// @Param        page_size  query     int  false  "每页数量"
// @Success      200        {object}  EntryListResponse
// @Router       /entry/read-later [get]
func (s *Server) handleReadLaterList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))

	list, total, err := s.db.ListReadLater(page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids := make([]uint64, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	states, err := s.db.GetEntryStates(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]EntryDTO, 0, len(list))
	for i := range list {
		st := states[list[i].ID]
		st.ReadLater = true
		items = append(items, toEntryDTO(&list[i], st))
	}
	writeJSON(w, http.StatusOK, EntryListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// handleEntrySetState 是设置/取消某布尔阅读状态的通用实现。
// field 同时是 JSON 请求体字段名与 entry_state 布尔列名（read / favorite / archive / read_later）。
func (s *Server) handleEntrySetState(w http.ResponseWriter, r *http.Request, field string) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req map[string]bool
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	value, ok := req[field]
	if !ok {
		writeError(w, http.StatusBadRequest, "missing field "+field)
		return
	}
	entry, err := s.db.GetEntry(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var state database.EntryState
	switch field {
	case "read":
		state, err = s.db.SetRead(id, value)
	case "favorite":
		state, err = s.db.SetFavorite(id, value)
	case "archive":
		state, err = s.db.SetArchive(id, value)
	case "read_later":
		state, err = s.db.SetReadLater(id, value)
	default:
		writeError(w, http.StatusBadRequest, "unknown field "+field)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toEntryDTO(entry, state))
}

// handleEntrySetRead 处理 PUT /api/entry/{id}/read（设置/取消已读）。
//
// @Summary      设置/取消已读
// @Tags         entry
// @Accept       json
// @Produce      json
// @Param        id   path      int   true  "条目ID"
// @Param        req  body      object true  "{\"read\": true}"
// @Success      200  {object}  EntryDTO
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /entry/{id}/read [put]
func (s *Server) handleEntrySetRead(w http.ResponseWriter, r *http.Request) {
	s.handleEntrySetState(w, r, "read")
}

// handleEntrySetFavorite 处理 PUT /api/entry/{id}/favorite（设置/取消收藏）。
//
// @Summary      设置/取消收藏
// @Tags         entry
// @Accept       json
// @Produce      json
// @Param        id   path      int   true  "条目ID"
// @Param        req  body      object true  "{\"favorite\": true}"
// @Success      200  {object}  EntryDTO
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /entry/{id}/favorite [put]
func (s *Server) handleEntrySetFavorite(w http.ResponseWriter, r *http.Request) {
	s.handleEntrySetState(w, r, "favorite")
}

// handleEntrySetArchive 处理 PUT /api/entry/{id}/archive（设置/取消归档）。
//
// @Summary      设置/取消归档
// @Tags         entry
// @Accept       json
// @Produce      json
// @Param        id   path      int   true  "条目ID"
// @Param        req  body      object true  "{\"archive\": true}"
// @Success      200  {object}  EntryDTO
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /entry/{id}/archive [put]
func (s *Server) handleEntrySetArchive(w http.ResponseWriter, r *http.Request) {
	s.handleEntrySetState(w, r, "archive")
}

// handleEntrySetReadLater 处理 PUT /api/entry/{id}/read-later（设置/取消稍后再阅）。
//
// @Summary      设置/取消稍后再阅
// @Description  标记或取消标记条目的「稍后再阅」状态
// @Tags         entry
// @Accept       json
// @Produce      json
// @Param        id   path      int   true  "条目ID"
// @Param        req  body      object true  "{\"read_later\": true}"
// @Success      200  {object}  EntryDTO
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /entry/{id}/read-later [put]
func (s *Server) handleEntrySetReadLater(w http.ResponseWriter, r *http.Request) {
	s.handleEntrySetState(w, r, "read_later")
}
