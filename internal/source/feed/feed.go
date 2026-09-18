// Package feed 实现 RSS 2.0 / Atom / JSON Feed 渠道的拉取适配器。
// 解析层用标准库 encoding/xml / encoding/json 自写，不依赖 gofeed，
// 以满足离线构建约束，并覆盖常见的畸形 feed 样本。
package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ma6254/news-glean/internal/fetch"
	"github.com/ma6254/news-glean/internal/source"
)

const defaultUserAgent = "NewsGlean/0.1 (+https://github.com/ma6254/news-glean)"

// 抓取方式。http 走 net/http；chromedp 走无头浏览器；chromedp_headed 走有头浏览器（交互）。
const (
	fetchModeHTTP         = "http"            // net/http 直接抓取（默认）
	fetchModeChrome       = "chromedp"        // 无头 chromedp
	fetchModeChromeHeaded = "chromedp_headed" // 有头 chromedp（交互）
)

// Config 是 feed 渠道的配置。
type Config struct {
	URL       string `json:"url"`        // feed 地址（http/https）
	FetchMode string `json:"fetch_mode"` // 抓取方式：http（默认）| chromedp | chromedp_headed
}

// connector 实现 source.Connector。
type connector struct {
	url        string
	mode       string
	proxy      string
	chromePath string
	client     *http.Client // http 模式用
}

// New 根据配置 JSON 与全局参数构造 feed 渠道实例。
func New(configJSON string, opts source.CreateOptions) (source.Connector, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("feed: invalid config: %w", err)
	}

	mode := cfg.FetchMode
	if mode == "" {
		mode = fetchModeHTTP
	}
	switch mode {
	case fetchModeHTTP, fetchModeChrome, fetchModeChromeHeaded:
	default:
		return nil, fmt.Errorf("feed: invalid fetch_mode %q (want http|chromedp|chromedp_headed)", cfg.FetchMode)
	}

	client, err := fetch.NewClient(opts.Proxy)
	if err != nil {
		return nil, fmt.Errorf("feed: %w", err)
	}
	client.Timeout = 30 * time.Second
	return &connector{
		url:        cfg.URL,
		mode:       mode,
		proxy:      opts.Proxy,
		chromePath: opts.ChromePath,
		client:     client,
	}, nil
}

func (c *connector) Type() string { return "feed" }

// Validate 校验 feed 地址，在保存配置前尽早报错。
func (c *connector) Validate(_ context.Context) error {
	if strings.TrimSpace(c.url) == "" {
		return errors.New("feed: url is required")
	}
	u, err := url.Parse(c.url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("feed: url must be a valid http(s) URL")
	}
	return nil
}

// Init 对 feed 渠道是无操作（无长连接、无游标需预热），但保留以符合契约生命周期。
func (c *connector) Init(_ context.Context, _ source.State) error { return nil }

// cursorData 是 feed 渠道游标的内容：HTTP 条件请求所需的两项。
type cursorData struct {
	ETag         string `json:"etag,omitempty"`          // 上次响应的 ETag
	LastModified string `json:"last_modified,omitempty"` // 上次响应的 Last-Modified
}

// Fetch 拉取一批新条目。
// http 模式使用 ETag/Last-Modified 条件请求；chromedp 模式每次全量加载、无游标。
func (c *connector) Fetch(ctx context.Context, cursor source.Cursor, limit int) ([]source.Item, source.Cursor, error) {
	var (
		body      []byte
		newCursor source.Cursor
		unchanged bool
		err       error
	)
	switch c.mode {
	case fetchModeChrome, fetchModeChromeHeaded:
		body, err = c.fetchChrome(ctx)
	default:
		body, newCursor, unchanged, err = c.fetchHTTP(ctx, cursor)
	}
	if err != nil {
		return nil, nil, err
	}
	if unchanged {
		return nil, newCursor, nil
	}

	items, err := parseFeed(body)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	for i := range items {
		if items[i].FetchedAt.IsZero() {
			items[i].FetchedAt = now
		}
	}

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, newCursor, nil
}

// fetchHTTP 用 net/http 抓取，支持 ETag/Last-Modified 条件请求。
// unchanged 为 true 表示命中 304、无新内容。
func (c *connector) fetchHTTP(ctx context.Context, cursor source.Cursor) (body []byte, newCursor source.Cursor, unchanged bool, err error) {
	cd := decodeCursor(cursor)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, nil, false, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	if cd.ETag != "" {
		req.Header.Set("If-None-Match", cd.ETag)
	}
	if cd.LastModified != "" {
		req.Header.Set("If-Modified-Since", cd.LastModified)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, cursor, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, false, fmt.Errorf("feed: unexpected status %s", resp.Status)
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, false, err
	}
	return body, encodeCursor(resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")), false, nil
}

// Run 返回 ErrPushUnsupported：feed 渠道只支持拉取模式。
func (c *connector) Run(_ context.Context, _ func(source.Item) error) error {
	return source.ErrPushUnsupported
}

func (c *connector) Close() error { return nil }

func encodeCursor(etag, lastModified string) source.Cursor {
	if etag == "" && lastModified == "" {
		return nil
	}
	data, _ := json.Marshal(cursorData{ETag: etag, LastModified: lastModified})
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

// parseFeed 依据响应体判断格式并解析为规范化条目。
func parseFeed(data []byte) ([]source.Item, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("feed: empty response body")
	}
	switch trimmed[0] {
	case '{', '[':
		return parseJSONFeed(trimmed)
	case '<':
		return parseXMLFeed(trimmed)
	default:
		return nil, errors.New("feed: unrecognized content, expected XML or JSON")
	}
}

func parseXMLFeed(data []byte) ([]source.Item, error) {
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("feed: invalid XML: %w", err)
	}
	switch root.XMLName.Local {
	case "rss":
		return parseRSS(data)
	case "feed":
		return parseAtom(data)
	default:
		return nil, fmt.Errorf("feed: unsupported XML root element %q", root.XMLName.Local)
	}
}

// ---- RSS 2.0 ----

type rssDoc struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Author      string `xml:"author"`
	Creator     string `xml:"creator"` // dc:creator
	Encoded     string `xml:"encoded"` // content:encoded
	Category    string `xml:"category"`
}

func parseRSS(data []byte) ([]source.Item, error) {
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("feed: invalid RSS: %w", err)
	}
	items := make([]source.Item, 0, len(doc.Channel.Items))
	for _, it := range doc.Channel.Items {
		guid := strings.TrimSpace(it.GUID)
		link := strings.TrimSpace(it.Link)
		if guid == "" {
			guid = link
		}
		if link == "" {
			link = guid
		}

		author := strings.TrimSpace(it.Author)
		if author == "" {
			author = strings.TrimSpace(it.Creator)
		}
		content := strings.TrimSpace(it.Encoded)
		if content == "" {
			content = strings.TrimSpace(it.Description)
		}

		published, ok := parseTime(it.PubDate)
		item := source.Item{
			GUID:         guid,
			URL:          link,
			Title:        strings.TrimSpace(it.Title),
			Author:       author,
			PublishedAt:  published,
			InferredTime: !ok,
			Summary:      strings.TrimSpace(it.Description),
			Content:      content,
			ContentType:  "text/html",
		}
		if strings.TrimSpace(it.Category) != "" {
			item.Tags = []string{strings.TrimSpace(it.Category)}
		}
		items = append(items, item)
	}
	return items, nil
}

// ---- Atom ----

type atomDoc struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     string         `xml:"title"`
	ID        string         `xml:"id"`
	Links     []atomLink     `xml:"link"`
	Updated   string         `xml:"updated"`
	Published string         `xml:"published"`
	Author    atomAuthor     `xml:"author"`
	Summary   string         `xml:"summary"`
	Content   string         `xml:"content"`
	Category  []atomCategory `xml:"category"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

func parseAtom(data []byte) ([]source.Item, error) {
	var doc atomDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("feed: invalid Atom: %w", err)
	}
	items := make([]source.Item, 0, len(doc.Entries))
	for _, en := range doc.Entries {
		link := pickAtomLink(en.Links)
		published, ok := parseTime(en.Published)
		if published.IsZero() {
			published, ok = parseTime(en.Updated)
		}
		item := source.Item{
			GUID:         strings.TrimSpace(en.ID),
			URL:          link,
			Title:        strings.TrimSpace(en.Title),
			Author:       strings.TrimSpace(en.Author.Name),
			PublishedAt:  published,
			InferredTime: !ok,
			Summary:      strings.TrimSpace(en.Summary),
			Content:      strings.TrimSpace(en.Content),
			ContentType:  "text/html",
		}
		for _, cat := range en.Category {
			if t := strings.TrimSpace(cat.Term); t != "" {
				item.Tags = append(item.Tags, t)
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// pickAtomLink 优先取 rel="alternate"，否则取第一条链接。
func pickAtomLink(links []atomLink) string {
	if len(links) == 0 {
		return ""
	}
	for _, l := range links {
		if l.Rel == "alternate" || l.Rel == "" {
			return strings.TrimSpace(l.Href)
		}
	}
	return strings.TrimSpace(links[0].Href)
}

// ---- JSON Feed ----

type jsonFeedDoc struct {
	Items []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string         `json:"id"`
	URL           string         `json:"url"`
	ExternalURL   string         `json:"external_url"`
	Title         string         `json:"title"`
	ContentHTML   string         `json:"content_html"`
	ContentText   string         `json:"content_text"`
	Summary       string         `json:"summary"`
	DatePublished string         `json:"date_published"`
	Author        jsonFeedAuthor `json:"author"`
	Tags          []string       `json:"tags"`
}

type jsonFeedAuthor struct {
	Name string `json:"name"`
}

func parseJSONFeed(data []byte) ([]source.Item, error) {
	var doc jsonFeedDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("feed: invalid JSON Feed: %w", err)
	}
	items := make([]source.Item, 0, len(doc.Items))
	for _, it := range doc.Items {
		link := strings.TrimSpace(it.URL)
		if link == "" {
			link = strings.TrimSpace(it.ExternalURL)
		}
		guid := strings.TrimSpace(it.ID)
		if guid == "" {
			guid = link
		}

		content := strings.TrimSpace(it.ContentHTML)
		contentType := "text/html"
		if content == "" {
			content = strings.TrimSpace(it.ContentText)
			contentType = "text/plain"
		}

		published, ok := parseTime(it.DatePublished)
		items = append(items, source.Item{
			GUID:         guid,
			URL:          link,
			Title:        strings.TrimSpace(it.Title),
			Author:       strings.TrimSpace(it.Author.Name),
			PublishedAt:  published,
			InferredTime: !ok,
			Summary:      strings.TrimSpace(it.Summary),
			Content:      content,
			ContentType:  contentType,
			Tags:         it.Tags,
		})
	}
	return items, nil
}

// parseTime 尝试多种常见时间格式解析。返回 (时间, 是否成功)。
func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
