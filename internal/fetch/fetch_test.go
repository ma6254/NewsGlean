package fetch

import (
	"net/http"
	"net/url"
	"testing"
)

func TestNewClientProxy(t *testing.T) {
	c, err := NewClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.Transport)
	}
	if tr.Proxy == nil {
		t.Fatal("expected Proxy to be set")
	}
	u, err := tr.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}})
	if err != nil {
		t.Fatalf("Proxy: %v", err)
	}
	if u.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = %v, want http://127.0.0.1:7890", u)
	}
}

func TestNewClientProxyEmptyUsesEnvironment(t *testing.T) {
	c, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	tr := c.Transport.(*http.Transport)
	if tr.Proxy == nil {
		t.Fatal("expected Proxy fallback to be set")
	}
	// 空代理应回退到 http.ProxyFromEnvironment；这里仅验证函数非 nil，行为由标准库保证。
}

func TestNewClientInvalidProxy(t *testing.T) {
	for _, p := range []string{"://bad", "127.0.0.1:7890", "http://"} {
		if _, err := NewClient(p); err == nil {
			t.Errorf("NewClient(%q) expected error, got nil", p)
		}
	}
}
