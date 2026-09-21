package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// VideoStats 是单视频的统计字段（normalize_video_summary.stats）。
type VideoStats struct {
	View     int `json:"view"`
	Danmaku  int `json:"danmaku"`
	Like     int `json:"like"`
	Coin     int `json:"coin"`
	Favorite int `json:"favorite"`
	Share    int `json:"share"`
}

// VideoDetail 是 bili video <bvid> --json 返回的单视频详情（摘要字段）。
type VideoDetail struct {
	BVID        string     `json:"bvid"`
	Title       string     `json:"title"`
	Description string     `json:"description"` // 简介
	Owner       string     `json:"owner"`       // UP 主名
	Stats       VideoStats `json:"stats"`
	Subtitle    string     `json:"subtitle"` // 字幕纯文本（withSubtitle=true 时）
}

// GetVideoDetail 获取单视频详情（简介 + 统计，可选字幕）。
// biliPath 可选覆盖可执行文件路径。这是「简介回填」与后续「字幕回填」共用的取数原语。
func GetVideoDetail(ctx context.Context, bvid, biliPath string, withSubtitle bool) (*VideoDetail, error) {
	return getVideoDetail(ctx, resolveBili(biliPath), bvid, withSubtitle)
}

// getVideoDetail 用已解析的 biliCmd 取单视频详情。
func getVideoDetail(ctx context.Context, cmd biliCmd, bvid string, withSubtitle bool) (*VideoDetail, error) {
	args := []string{"video", bvid}
	if withSubtitle {
		args = append(args, "--subtitle")
	}
	data, err := runBili(ctx, cmd, args...)
	if err != nil {
		return nil, err
	}
	return parseVideoDetail(data)
}

// parseVideoDetail 解析 bili video --json 的 data 字段。
func parseVideoDetail(data []byte) (*VideoDetail, error) {
	var resp struct {
		Video struct {
			BVID        string `json:"bvid"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Owner       struct {
				Name string `json:"name"`
			} `json:"owner"`
			Stats VideoStats `json:"stats"`
		} `json:"video"`
		Subtitle struct {
			Text string `json:"text"`
		} `json:"subtitle"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("bilibili: 解析 video 详情: %w", err)
	}
	return &VideoDetail{
		BVID:        resp.Video.BVID,
		Title:       strings.TrimSpace(resp.Video.Title),
		Description: strings.TrimSpace(resp.Video.Description),
		Owner:       resp.Video.Owner.Name,
		Stats:       resp.Video.Stats,
		Subtitle:    resp.Subtitle.Text,
	}, nil
}
