// Package export 负责把条目导出为本地文件。
// 阶段 8 实现 Markdown（YAML front matter + 按源/月份分目录）；
// 阶段 9 实现 JSON（单文件数组）与 EPUB（archive/zip 自写容器）。
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ma6254/news-glean/internal/database"
	"gopkg.in/yaml.v3"
)

// Options 是一次 Markdown 导出的参数。
type Options struct {
	Filter database.EntryFilter // 条目过滤条件，零值表示全量
}

// Result 是一次导出的结果。
type Result struct {
	Total   int    `json:"total"`   // 导出条数
	Sources int    `json:"sources"` // 本批次覆盖的渠道数
	Dir     string `json:"dir"`     // 输出根目录
}

// frontMatter 是导出文件头部的 YAML front matter。
// 空字段用 omitempty 省略：无 URL 条目自然不含 url，呼应契约。
type frontMatter struct {
	Title       string            `yaml:"title"`
	Author      string            `yaml:"author,omitempty"`
	URL         string            `yaml:"url,omitempty"`
	Source      string            `yaml:"source"`
	GUID        string            `yaml:"guid,omitempty"`
	PublishedAt string            `yaml:"published_at,omitempty"`
	FetchedAt   string            `yaml:"fetched_at,omitempty"`
	ContentType string            `yaml:"content_type,omitempty"`
	Tags        []string          `yaml:"tags,omitempty"`
	Extra       map[string]string `yaml:"extra,omitempty"`
}

// ExportMarkdown 把满足条件的条目导出为 Markdown。
// 目录结构：dir/<源名>/<YYYY-MM>/<安全化标题>.md；重复导出覆盖同名文件（幂等）。
func ExportMarkdown(db *database.DB, dir string, opts Options) (*Result, error) {
	if dir == "" {
		dir = "./export"
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

	result := &Result{Dir: dir, Total: len(entries)}

	// 统计本批次覆盖的渠道数（有至少一条条目被导出的源）。
	sourceSet := make(map[uint64]bool, len(entries))
	for i := range entries {
		sourceSet[entries[i].SourceID] = true
	}
	result.Sources = len(sourceSet)

	// 每个源目录内，用于同目录文件名去重（本次导出内）。key 为目标目录路径。
	used := make(map[string]map[string]bool)

	for i := range entries {
		e := &entries[i]

		srcName := nameByID[e.SourceID]
		if srcName == "" {
			srcName = fmt.Sprintf("source-%d", e.SourceID)
		}
		srcDir := sanitizeName(srcName)
		if srcDir == "" {
			srcDir = fmt.Sprintf("source-%d", e.SourceID)
		}

		targetDir := filepath.Join(dir, srcDir, monthOf(e))
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return nil, err
		}

		base := sanitizeName(e.Title)
		if base == "" {
			base = fmt.Sprintf("untitled-%d", e.ID)
		}
		if used[targetDir] == nil {
			used[targetDir] = make(map[string]bool)
		}
		name := uniqueBase(used[targetDir], base) + ".md"

		fm := frontMatter{
			Title:       e.Title,
			Author:      e.Author,
			URL:         e.URL,
			Source:      srcName,
			GUID:        e.GUID,
			PublishedAt: e.PublishedAt,
			FetchedAt:   e.FetchedAt,
			ContentType: e.ContentType,
			Tags:        parseTags(e.Tags),
			Extra:       parseExtra(e.Extra),
		}
		body := e.Content
		if body == "" {
			body = e.Summary
		}

		if err := writeMarkdown(filepath.Join(targetDir, name), fm, body); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// writeMarkdown 把 front matter 与正文写入单个 Markdown 文件。
// 正文保留原始 HTML（Markdown 兼容内联 HTML），不在此阶段做清洗。
func writeMarkdown(path string, fm frontMatter, body string) error {
	header, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(header)
	buf.WriteString("---\n\n")
	buf.WriteString(body)
	if body != "" && !strings.HasSuffix(body, "\n") {
		buf.WriteString("\n")
	}
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// monthOf 返回条目的月份分组（YYYY-MM）。优先 published_at，空则 fetched_at，再空用 "unknown"。
// published_at 为 UTC RFC3339，前 7 位即 YYYY-MM。
func monthOf(e *database.Entry) string {
	s := e.PublishedAt
	if s == "" {
		s = e.FetchedAt
	}
	if len(s) >= 7 {
		return s[:7]
	}
	return "unknown"
}

// sanitizeName 清洗文件名/目录名中的非法字符与首尾空白/点，返回可安全落盘的名称。
// 可能返回空串，调用方需自行兜底。
func sanitizeName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			if r < 0x20 || r == 0x7f { // 控制字符
				b.WriteByte('_')
			} else {
				b.WriteRune(r)
			}
		}
	}
	s := strings.Trim(strings.TrimSpace(b.String()), ".")
	if isReservedName(s) {
		s = "_" + s
	}
	return s
}

// isReservedName 判断名称是否为 Windows 保留设备名（作文件名会失败）。
func isReservedName(s string) bool {
	up := strings.ToUpper(s)
	switch up {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(up) == 4 && (up[:3] == "COM" || up[:3] == "LPT") && up[3] >= '1' && up[3] <= '9' {
		return true
	}
	return false
}

// uniqueBase 在 used 集合中为 base 分配一个不冲突的文件基名（不含扩展名）。
// 首次用 base，冲突依次用 base-2、base-3……。因调用顺序确定，序号分配确定，保证幂等。
func uniqueBase(used map[string]bool, base string) string {
	if !used[base] {
		used[base] = true
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !used[cand] {
			used[cand] = true
			return cand
		}
	}
}

// parseTags 把 tags 的 JSON 字符串解析为字符串切片；空或非法返回 nil。
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		return nil
	}
	return tags
}

// parseExtra 把 extra 的 JSON 字符串解析为键值对；空或非法返回 nil。
func parseExtra(s string) map[string]string {
	if s == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
