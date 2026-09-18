package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// chromeFetchTimeout 是 chromedp 单次抓取的超时（含浏览器启动）。
const chromeFetchTimeout = 60 * time.Second

// fetchChrome 用 chromedp 加载 URL，返回主文档的原始响应体。
// 无头（chromedp）或有头（chromedp_headed）由 c.mode 决定。
func (c *connector) fetchChrome(ctx context.Context) ([]byte, error) {
	headless := c.mode != fetchModeChromeHeaded

	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.DisableGPU,
		chromedp.WindowSize(1280, 720),
	}
	if headless {
		opts = append(opts, chromedp.Headless)
	}
	if c.chromePath != "" {
		opts = append(opts, chromedp.ExecPath(c.chromePath))
	}
	if c.proxy != "" {
		opts = append(opts, chromedp.ProxyServer(c.proxy))
	}

	// 加超时，避免浏览器挂起时无限等待。
	timeoutCtx, cancel := context.WithTimeout(ctx, chromeFetchTimeout)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(timeoutCtx, opts...)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	// 监听主文档（Document）的响应，记下其请求 ID，导航结束后取原始响应体。
	var requestID network.RequestID
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		if e, ok := ev.(*network.EventResponseReceived); ok && e.Type == network.ResourceTypeDocument {
			requestID = e.RequestID
		}
	})

	if err := chromedp.Run(taskCtx, chromedp.Navigate(c.url)); err != nil {
		return nil, fmt.Errorf("feed: chromedp navigate: %w", err)
	}
	if requestID == "" {
		return nil, fmt.Errorf("feed: chromedp: no document response captured for %s", c.url)
	}

	// GetResponseBody 需要在 chromedp.Run 的执行上下文中调用（该上下文才携带 CDP 执行器）。
	var body []byte
	err := chromedp.Run(taskCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		b, err := network.GetResponseBody(requestID).Do(ctx)
		if err != nil {
			return err
		}
		body = b
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("feed: chromedp get response body: %w", err)
	}
	return body, nil
}
