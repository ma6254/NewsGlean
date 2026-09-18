// Package fetch 提供共享的 HTTP 客户端构造（代理等），供各采集适配器复用。
// 后续「条件请求、重试、限速」等也收敛到本包。
package fetch

import (
	"fmt"
	"net/http"
	"net/url"
)

// NewClient 根据代理地址构造 http.Client。
// proxy 为空时回退到环境变量代理（HTTP_PROXY / HTTPS_PROXY / NO_PROXY，即 http.ProxyFromEnvironment）。
// 超时由调用方（各适配器）按自身需求设置。
func NewClient(proxy string) (*http.Client, error) {
	var proxyFn func(*http.Request) (*url.URL, error)
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url %q: %w", proxy, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid proxy url %q: need scheme and host, e.g. http://127.0.0.1:7890", proxy)
		}
		proxyFn = http.ProxyURL(u)
	} else {
		proxyFn = http.ProxyFromEnvironment
	}

	// 克隆默认 Transport 以保留其拨号、TLS、连接池等默认配置，仅覆盖代理。
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = proxyFn
	return &http.Client{Transport: tr}, nil
}
