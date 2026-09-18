// Package filter 提供去重与过滤的纯函数实现。
// 本包不访问数据库，只做确定性计算，供 app 层编排调用。
// 它是核心层的一部分：新增渠道时不应改动本包。
package filter

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

// Normalize 归一化文本用于指纹计算：折叠连续空白、转小写。
func Normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// ContentHash 计算内容指纹：标题 + 正文归一化后的 SHA-256 前 16 字节的十六进制。
// 用于识别改标题重发的稿件（跨渠道去重）。
func ContentHash(title, content string) string {
	raw := Normalize(title) + "\x00" + Normalize(content)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

// NormalizeURL 规范化链接用于去重比较：
// 去掉 fragment、统一 scheme/host 小写、去掉路径末尾斜杠。
// 无法解析的 URL 原样返回（仅做 trim）。
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "" && u.Host == "") {
		// 解析失败或形如 "example.com/path"（被当成相对路径）时，原样返回
		return raw
	}
	if u.Scheme == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path != "" && u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}
