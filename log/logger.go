package log

import "time"

// Logger 门面：带层叠 tag 链与固定字段的日志器。
// 通过 NewLogger 创建；WithTag / With 派生新实例，原实例不受影响。
type Logger struct {
	core   *Core
	tags   Tags
	fields []Field
}

// NewLogger 创建绑定 core 的 Logger。
func NewLogger(core *Core) *Logger {
	return &Logger{core: core}
}

// WithTag 派生带新 tag 的 Logger（追加到链尾）。超过 MaxTagDepth 时返回自身。
func (l *Logger) WithTag(tag string) *Logger {
	next, ok := l.tags.Append(tag)
	if !ok {
		return l
	}
	return &Logger{core: l.core, tags: next, fields: l.fields}
}

// With 派生带固定字段的 Logger，后续每次输出都携带这些字段。
func (l *Logger) With(fields ...Field) *Logger {
	nf := make([]Field, 0, len(l.fields)+len(fields))
	nf = append(nf, l.fields...)
	nf = append(nf, fields...)
	return &Logger{core: l.core, tags: l.tags, fields: nf}
}

// Debug 记录调试信息（access 通道）。
func (l *Logger) Debug(msg string, args ...any) { l.log(LevelDebug, msg, args...) }

// Info 记录常规信息（access 通道）。
func (l *Logger) Info(msg string, args ...any) { l.log(LevelInfo, msg, args...) }

// Warn 记录警告（access 通道）。
func (l *Logger) Warn(msg string, args ...any) { l.log(LevelWarn, msg, args...) }

// Error 记录错误（error 通道，不受 loglevel 过滤，全收）。
func (l *Logger) Error(msg string, args ...any) { l.log(LevelError, msg, args...) }

func (l *Logger) log(level Level, msg string, args ...any) {
	// 热路径短路：级别不达阈值时不做任何格式化与分配。
	if !l.core.enabled(level, l.tags) {
		return
	}
	rec := Record{
		Time:    time.Now(),
		Level:   level,
		Tags:    l.tags,
		Message: msg,
		Fields:  parseFields(l.fields, args...),
	}
	l.core.Log(rec)
}

// parseFields 把参数解析为字段。支持两种写法：
//   - slog 风格键值对：Info("msg", "key", value)
//   - 直接传 Field：   Info("msg", Str("key", "value"))
// 奇数个键值对参数时，最后缺失值的键按 slog 惯例标记为 !BADKEY 并作为值输出。
func parseFields(base []Field, args ...any) []Field {
	if len(args) == 0 {
		return base
	}
	fields := make([]Field, 0, len(base)+len(args)/2+1)
	fields = append(fields, base...)
	for i := 0; i < len(args); i++ {
		if f, ok := args[i].(Field); ok {
			fields = append(fields, f)
			continue
		}
		if i+1 >= len(args) {
			fields = append(fields, Field{"!BADKEY", args[i]})
			break
		}
		key := "!BADKEY"
		if s, ok := args[i].(string); ok {
			key = s
		}
		fields = append(fields, Field{key, args[i+1]})
		i++
	}
	return fields
}

// ---------------- 包级默认实例 ----------------

// std 包级默认 Logger。默认级别 info；默认无 Sink（静默丢弃），
// 由 Configure 挂载 Console/File/WebSocket 输出端。
var std = NewLogger(NewCore())

// Default 返回包级默认 Logger。
func Default() *Logger { return std }

// DefaultCore 返回包级默认核心（供 Configure 与测试使用）。
func DefaultCore() *Core { return std.core }

// 包级便捷函数，等价于 Default().Xxx(...)。
func Debug(msg string, args ...any) { std.Debug(msg, args...) }
func Info(msg string, args ...any)  { std.Info(msg, args...) }
func Warn(msg string, args ...any)  { std.Warn(msg, args...) }
func Error(msg string, args ...any) { std.Error(msg, args...) }

// WithTag 包级便捷函数：从默认 Logger 派生带 tag 的 Logger。
func WithTag(tag string) *Logger { return std.WithTag(tag) }

// With 包级便捷函数：从默认 Logger 派生带固定字段的 Logger。
func With(fields ...Field) *Logger { return std.With(fields...) }
