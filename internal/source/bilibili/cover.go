package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultUserAgent = "NewsGlean/0.1 (+https://github.com/ma6254/news-glean)"

// coverClient 复用于封面抓取（公开接口，无鉴权）。
var coverClient = &http.Client{Timeout: 10 * time.Second}

// GetVideoCover 通过 B 站公开 view 接口获取视频封面 URL。
// 这是「采集走 CLI」之外的唯一直连路径，仅用于展示封面；限速 + 对可重试错误退避重试，失败返回空串。
func GetVideoCover(ctx context.Context, bvid string) (string, error) {
	if err := biliRateLimiter.Wait(ctx); err != nil {
		return "", err
	}
	var pic string
	err := retry(ctx, maxRetries, retryBaseDelay, httpRetryable, func() error {
		p, err := fetchCover(ctx, bvid)
		if err == nil {
			pic = p
		}
		return err
	})
	return pic, err
}

// fetchCover 单次抓取封面。
func fetchCover(ctx context.Context, bvid string) (string, error) {
	api := "https://api.bilibili.com/x/web-interface/view?bvid=" + url.QueryEscape(bvid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")

	resp, err := coverClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{status: resp.StatusCode}
	}
	var v struct {
		Code int `json:"code"`
		Data struct {
			Pic string `json:"pic"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if v.Code != 0 {
		return "", fmt.Errorf("bilibili: view 接口 code=%d", v.Code)
	}
	pic := strings.TrimSpace(v.Data.Pic)
	// 统一 https，避免浏览器混合内容
	if strings.HasPrefix(pic, "http://") {
		pic = "https://" + strings.TrimPrefix(pic, "http://")
	}
	return pic, nil
}
