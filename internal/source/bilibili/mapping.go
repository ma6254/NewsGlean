package bilibili

import (
	"strconv"
	"strings"
	"time"

	"github.com/ma6254/news-glean/internal/source"
)

// historyItem 对应 `bili history --json` 的 data.items 元素（normalize_history_item）。
type historyItem struct {
	ID       string `json:"id"`
	BVID     string `json:"bvid"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	ViewedAt string `json:"viewed_at"`
}

// favoriteItem 对应 `bili favorites <id> --json` 的 data.items 元素（normalize_favorite_media）。
type favoriteItem struct {
	ID              string `json:"id"`
	BVID            string `json:"bvid"`
	Title           string `json:"title"`
	DurationSeconds int    `json:"duration_seconds"`
	Duration        string `json:"duration"`
	Upper           struct {
		Name string `json:"name"`
	} `json:"upper"`
}

// videoURL 由 bvid 构造视频页链接。
func videoURL(bvid string) string {
	if bvid == "" {
		return ""
	}
	return "https://www.bilibili.com/video/" + bvid
}

// mapHistoryItem 把观看历史条目映射为规范化 Item。
// viewed_at 是 naive 本地时间（无时区偏移，datetime.fromtimestamp().isoformat()），
// 按本地时区解析还原为绝对时间；解析失败时退化为抓取时间。
func mapHistoryItem(it historyItem) source.Item {
	bvid := strings.TrimSpace(it.BVID)
	if bvid == "" {
		bvid = strings.TrimSpace(it.ID)
	}
	item := source.Item{
		GUID:         bvid,
		URL:          videoURL(bvid),
		Title:        strings.TrimSpace(it.Title),
		Author:       strings.TrimSpace(it.Author),
		InferredTime: true,
		ContentType:  "text/plain",
	}
	if t, ok := parseViewedAt(it.ViewedAt); ok {
		item.PublishedAt = t
		item.InferredTime = false
	} else if ts := strings.TrimSpace(it.ViewedAt); ts != "" {
		item.Extra = map[string]string{"viewed_at": ts}
	}
	return item
}

// mapFavoriteItem 把收藏夹条目映射为规范化 Item。
// favorites 无 fav_time（收藏时间），PublishedAt 缺失 → InferredTime。
func mapFavoriteItem(it favoriteItem) source.Item {
	bvid := strings.TrimSpace(it.BVID)
	if bvid == "" {
		bvid = strings.TrimSpace(it.ID)
	}
	extra := map[string]string{
		"duration_seconds": strconv.Itoa(it.DurationSeconds),
		"duration":         strings.TrimSpace(it.Duration),
	}
	return source.Item{
		GUID:         bvid,
		URL:          videoURL(bvid),
		Title:        strings.TrimSpace(it.Title),
		Author:       strings.TrimSpace(it.Upper.Name),
		InferredTime: true,
		ContentType:  "text/plain",
		Extra:        extra,
	}
}

// parseViewedAt 解析 bilibili-cli 输出的时间戳：优先 RFC3339（带偏移），
// 否则按 naive 本地时间解析（历史记录的 viewed_at 常见形态）。
func parseViewedAt(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
}
