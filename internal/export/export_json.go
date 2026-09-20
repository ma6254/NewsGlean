package export

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ma6254/news-glean/internal/database"
)

// EntryJSON 是 JSON 导出的条目结构（tags/extra 展开为原生结构）。
// Source 为渠道显示名，冗余落盘便于脚本免 join 直接消费；空字段 omitempty，
// 无 URL 条目自然不含 url，与 Markdown 导出的契约一致。
type EntryJSON struct {
	ID          uint64            `json:"id"`
	SourceID    uint64            `json:"source_id"`
	Source      string            `json:"source,omitempty"`
	GUID        string            `json:"guid,omitempty"`
	URL         string            `json:"url,omitempty"`
	Title       string            `json:"title"`
	Author      string            `json:"author,omitempty"`
	PublishedAt string            `json:"published_at,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Content     string            `json:"content,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	FetchedAt   string            `json:"fetched_at,omitempty"`
}

// ExportJSON 把满足条件的条目导出为单个 JSON 数组文件（entries.json），
// 供脚本消费；重复导出覆盖同名文件（幂等）。过滤逻辑与 Markdown 导出共用 EntryFilter。
func ExportJSON(db *database.DB, dir string, opts Options) (*Result, error) {
	if dir == "" {
		dir = "./export-json"
	}

	sources, err := db.ListSources()
	if err != nil {
		return nil, err
	}
	nameByID := make(map[uint64]string, len(sources))
	for i := range sources {
		nameByID[sources[i].ID] = sources[i].Name
	}

	entries, err := db.ListAllEntries(opts.Filter)
	if err != nil {
		return nil, err
	}

	items := make([]EntryJSON, 0, len(entries))
	sourceSet := make(map[uint64]bool, len(entries))
	for i := range entries {
		e := &entries[i]
		sourceSet[e.SourceID] = true
		items = append(items, EntryJSON{
			ID:          e.ID,
			SourceID:    e.SourceID,
			Source:      nameByID[e.SourceID],
			GUID:        e.GUID,
			URL:         e.URL,
			Title:       e.Title,
			Author:      e.Author,
			PublishedAt: e.PublishedAt,
			Summary:     e.Summary,
			Content:     e.Content,
			ContentType: e.ContentType,
			Tags:        parseTags(e.Tags),
			Extra:       parseExtra(e.Extra),
			FetchedAt:   e.FetchedAt,
		})
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "entries.json"), data, 0o644); err != nil {
		return nil, err
	}

	return &Result{Total: len(entries), Sources: len(sourceSet), Dir: dir}, nil
}
