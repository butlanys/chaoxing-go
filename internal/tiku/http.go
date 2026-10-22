package tiku

import (
	"net/http"
	"net/url"
	"time"
)

// newHTTPClient 构造带超时的 HTTP 客户端。
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// newHTTPClientWithProxy 构造带超时与 HTTP 代理的客户端；proxyURL 为空时退化为普通客户端。
func newHTTPClientWithProxy(timeout time.Duration, proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return newHTTPClient(timeout), nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}
