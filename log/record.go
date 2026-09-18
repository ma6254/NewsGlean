package log

import (
	"strconv"
	"time"
)

// Channel 日志通道：access（业务流水）或 error（错误诊断）。
type Channel int

const (
	// ChannelAccess 业务流水通道：Debug/Info/Warn 级别。
	ChannelAccess Channel = iota
	// ChannelError 错误诊断通道：Error 级别，不受 loglevel 过滤（全收）。
	ChannelError
)

// String 返回通道的规范名。
func (c Channel) String() string {
	if c == ChannelError {
		return "error"
	}
	return "access"
}

// Field 一条结构化字段（键值对）。
type Field struct {
	Key   string
	Value any
}

// Record 一条完整的结构化日志记录，是与输出端无关的中间形态。
// 各 Sink 自行决定如何呈现（终端文本/彩色、文件纯文本、JSON 事件流）。
type Record struct {
	Time    time.Time
	Level   Level
	Tags    Tags
	Message string
	Fields  []Field
	File    string // 调用点文件（相对模块根的斜杠路径，如 internal/app/app.go）
	Line    int    // 调用点行号
}

// Caller 返回 "file:line" 形式的调用点描述；未捕获到时返回空串。
func (r Record) Caller() string {
	if r.File == "" {
		return ""
	}
	return r.File + ":" + strconv.Itoa(r.Line)
}

// Channel 按级别路由通道：Debug/Info/Warn → access，Error → error。
func (r Record) Channel() Channel {
	if r.Level >= LevelError {
		return ChannelError
	}
	return ChannelAccess
}

// Field 便捷构造函数。
func Str(key, value string) Field       { return Field{key, value} }
func Int(key string, value int) Field   { return Field{key, value} }
func Bool(key string, value bool) Field { return Field{key, value} }
func Err(err error) Field               { return Field{"error", err} }
func Any(key string, value any) Field   { return Field{key, value} }
