package log

import (
	"fmt"
	"strings"
)

// Level 日志级别。数值对齐 log/slog 的级别体系，便于未来桥接标准库生态。
type Level int8

const (
	// LevelDebug 调试信息。
	LevelDebug Level = -4
	// LevelInfo 常规信息。
	LevelInfo Level = 0
	// LevelWarn 警告。
	LevelWarn Level = 4
	// LevelError 错误。
	LevelError Level = 8
	// LevelNone 完全静默（v2ray 的 loglevel: none 语义）：所有级别都不输出。
	LevelNone Level = 127
)

// String 返回级别的规范名称。
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelNone:
		return "NONE"
	default:
		return fmt.Sprintf("LEVEL(%d)", int8(l))
	}
}

// Enabled 报告级别 l 是否达到阈值 threshold（l >= threshold 才输出）。
func (l Level) Enabled(threshold Level) bool {
	return l >= threshold
}

// levelAliases 级别字符串别名（大小写不敏感）。
var levelAliases = map[string]Level{
	"none":    LevelNone,
	"error":   LevelError,
	"warning": LevelWarn,
	"warn":    LevelWarn,
	"info":    LevelInfo,
	"debug":   LevelDebug,
}

// ParseLevel 解析级别字符串，兼容 warning/warn 两种写法，大小写不敏感。
func ParseLevel(s string) (Level, error) {
	if l, ok := levelAliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return l, nil
	}
	return LevelInfo, fmt.Errorf("invalid log level %q (want none/error/warning/info/debug)", s)
}
