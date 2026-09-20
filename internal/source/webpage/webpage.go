// Package webpage 实现「网页列表页爬取」渠道的拉取适配器。
// 用 goquery（底层 cascadia）按 CSS 选择器从静态 HTML 列表页提取条目：
// 选择器命中的元素既可以是 <a> 锚点本身，也可以是包含首个 <a> 的容器；
// 标题取锚点文本、链接取 href（相对链接按列表页地址解析为绝对）。
// 只处理静态 HTML，JS 渲染页不在本渠道范围。
package webpage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"github.com/ma6254/news-glean/internal/fetch"
	"github.com/ma6254/news-glean/internal/source"
)

const defaultUserAgent = "NewsGlean/0.1 (+https://github.com/ma6254/news-glean)"

// Config 是 webpage 渠道的配置。
type Config struct {
	URL      string `json:"url"`       // 列表页地址（http/https）
	Selector string `json:"selector"`  // CSS 选择器，命中「条目锚点或其容器」元素
	FullText bool   `json:"full_text"` // 是否回源抓取正文（阶段 12 使用；本阶段仅透传，不生效）
}

// connector 实现 source.Connector。
type connector struct {
	url      string
	selector string
	fullText bool
	client   *http.Client
}

// New 根据配置 JSON 与全局参数构造 webpage 渠道实例。
func New(configJSON string, opts source.CreateOptions) (source.Connector, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("webpage: invalid config: %w", err)
	}
	client, err := fetch.NewClient(opts.Proxy)
	if err != nil {
		return nil, fmt.Errorf("webpage: %w", err)
	}
	client.Timeout = 30 * time.Second
	return &connector{url: cfg.URL, selector: cfg.Selector, fullText: cfg.FullText, client: client}, nil
}

func (c *connector) Type() string { return "webpage" }

// Validate 校验列表页地址与 CSS 选择器，在保存配置前尽早报错。
func (c *connector) Validate(_ context.Context) error {
	if strings.TrimSpace(c.url) == "" {
		return errors.New("webpage: url is required")
	}
	u, err := url.Parse(c.url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("webpage: url must be a valid http(s) URL")
	}
	if strings.TrimSpace(c.selector) == "" {
		return errors.New("webpage: selector is required")
	}
	// 选择器语法校验：goquery.Find 内部用 MustCompile，会在非法选择器上 panic；
	// 这里提前用 cascadia.ParseGroup 校验，让保存配置时就能暴露错误。
	if _, err := cascadia.ParseGroup(c.selector); err != nil {
		return fmt.Errorf("webpage: invalid selector %q: %w", c.selector, err)
	}
	return nil
}

// Init 对 webpage 渠道是无操作（无长连接、无游标需预热）。
func (c *connector) Init(_ context.Context, _ source.State) error { return nil }

// cursorData 是 webpage 渠道游标的内容：上次见到的最新条目链接。
// 列表页通常按时间倒序，遇到该链接即可停止，其后的条目都是旧内容。
type cursorData struct {
	NewestURL string `json:"newest_url,omitempty"` // 上次抓取时列表第一条的绝对链接
}

// Fetch 拉取一批新条目。
// 注意：不按 limit 截断返回的条目。若按 limit 截断，而游标只记录「最新链接」，
// 被截断掉的那部分条目会永远漏采；核心层有三层去重兜底，重复条目会被跳过。
func (c *connector) Fetch(ctx context.Context, cursor source.Cursor, _ int) ([]source.Item, source.Cursor, error) {
	body, err := c.fetchHTTP(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := c.parseList(body)
	if err != nil {
		return nil, nil, err
	}

	// 游标去重：列表按时间倒序，遇到上次的最新链接后停止。
	newCursor := cursor
	if len(items) > 0 {
		if cd := decodeCursor(cursor); cd.NewestURL != "" {
			for i := range items {
				if items[i].URL == cd.NewestURL {
					items = items[:i]
					break
				}
			}
		}
		// 仍有新条目才推进游标；否则（无变化）保留旧游标。
		if len(items) > 0 {
			newCursor = encodeCursor(items[0].URL)
		}
	}

	now := time.Now().UTC()
	for i := range items {
		if items[i].FetchedAt.IsZero() {
			items[i].FetchedAt = now
		}
	}
	return items, newCursor, nil
}

// Probe 实现 source.Prober：抓取列表页并提取 <title>，供前端「自动获取显示名」。
func (c *connector) Probe(ctx context.Context) (source.ProbeInfo, error) {
	body, err := c.fetchHTTP(ctx)
	if err != nil {
		return source.ProbeInfo{}, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return source.ProbeInfo{}, fmt.Errorf("webpage: parse html: %w", err)
	}
	return source.ProbeInfo{Title: strings.TrimSpace(doc.Find("title").First().Text())}, nil
}

// Run 返回 ErrPushUnsupported：webpage 渠道只支持拉取模式。
func (c *connector) Run(_ context.Context, _ func(source.Item) error) error {
	return source.ErrPushUnsupported
}

func (c *connector) Close() error { return nil }

// fetchHTTP 用 net/http 抓取列表页原始字节。
func (c *connector) fetchHTTP(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webpage: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("webpage: empty response body")
	}
	return body, nil
}

// parseList 解析列表页，把选择器命中的元素映射为规范化条目。
func (c *connector) parseList(body []byte) ([]source.Item, error) {
	// 选择器语法先校验，避免 goquery.Find 内部 MustCompile 在非法选择器上 panic。
	if _, err := cascadia.ParseGroup(c.selector); err != nil {
		return nil, fmt.Errorf("webpage: invalid selector %q: %w", c.selector, err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("webpage: parse html: %w", err)
	}
	base, err := url.Parse(c.url)
	if err != nil {
		return nil, fmt.Errorf("webpage: invalid url: %w", err)
	}

	var items []source.Item
	doc.Find(c.selector).Each(func(_ int, s *goquery.Selection) {
		if item, ok := itemFromSelection(s, base); ok {
			items = append(items, item)
		}
	})
	return items, nil
}

// itemFromSelection 把命中的元素（锚点本身或其容器）映射为一个条目。
// 返回 ok=false 表示该元素无法提取出有效的 http(s) 链接，跳过。
func itemFromSelection(s *goquery.Selection, base *url.URL) (source.Item, bool) {
	anchor := s
	if goquery.NodeName(s) != "a" {
		anchor = s.Find("a").First()
		if anchor.Length() == 0 {
			return source.Item{}, false
		}
	}
	href, ok := anchor.Attr("href")
	href = strings.TrimSpace(href)
	if !ok || href == "" {
		return source.Item{}, false
	}
	// 丢弃页内锚点链接（#...）：它们不是独立条目，解析后等于列表页本身。
	if strings.HasPrefix(href, "#") {
		return source.Item{}, false
	}
	abs, err := base.Parse(href)
	if err != nil {
		return source.Item{}, false
	}
	// 丢弃非 http(s) 链接（javascript:、mailto:、# 锚点等）。
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return source.Item{}, false
	}
	link := abs.String()
	return source.Item{
		GUID:         link, // 网页列表页无自带 GUID，用绝对链接兜底（与 feed 无 guid 时一致）
		URL:          link,
		Title:        collapseWS(anchor.Text()),
		PublishedAt:  time.Time{}, // 列表页无发布时间，核心层回退为抓取时间
		InferredTime: true,
		ContentType:  "text/html",
	}, true
}

// collapseWS 折叠文本内的连续空白（换行/缩进/制表）为单个空格并去首尾。
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func encodeCursor(newestURL string) source.Cursor {
	data, _ := json.Marshal(cursorData{NewestURL: newestURL})
	return source.Cursor(data)
}

func decodeCursor(c source.Cursor) cursorData {
	if len(c) == 0 {
		return cursorData{}
	}
	var cd cursorData
	if err := json.Unmarshal(c, &cd); err != nil {
		return cursorData{}
	}
	return cd
}
