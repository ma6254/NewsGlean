package log

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/term"
)

// ColorMode 终端彩色三态开关。
type ColorMode int

const (
	// ColorAuto 自动：输出目标是终端且未设置 NO_COLOR 时着色。
	ColorAuto ColorMode = iota
	// ColorAlways 强制着色（重定向时也会输出 ANSI 转义）。
	ColorAlways
	// ColorNever 强制无色。
	ColorNever
)

// ParseColorMode 解析颜色模式字符串（auto/always/never，大小写不敏感）。
func ParseColorMode(s string) (ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ColorAuto, nil
	case "always":
		return ColorAlways, nil
	case "never":
		return ColorNever, nil
	}
	return ColorAuto, fmt.Errorf("invalid color mode %q (want auto/always/never)", s)
}

// ANSI 颜色序列（只给装饰部分着色，正文不着色）。
const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
	ansiCyan  = "\x1b[36m"

	ansiDebug = "\x1b[90m"   // 灰
	ansiInfo  = "\x1b[32m"   // 绿
	ansiWarn  = "\x1b[33m"   // 黄
	ansiError = "\x1b[1;31m" // 红加粗
)

func levelColor(l Level) string {
	switch l {
	case LevelDebug:
		return ansiDebug
	case LevelInfo:
		return ansiInfo
	case LevelWarn:
		return ansiWarn
	case LevelError:
		return ansiError
	}
	return ""
}

// 正文关键词着色：error 系红色、warn 系黄色、success 系亮绿。
// 仅作用于终端彩色模式；文件/JSON 输出保持纯文本。
const ansiSuccess = "\x1b[1;32m" // 亮绿

var (
	kwReEN = regexp.MustCompile(`(?i)\b(error|failed|failure|panic|warn|warning|retry|timeout|disabled|false|success|ok|true|enabled)\b`)
	kwReZH = regexp.MustCompile(`错误|失败|异常|警告|重试|超时|禁用|完成|成功|通过|使能|启用`)

	kwColorEN = map[string]string{
		"error": ansiError, "failed": ansiError, "failure": ansiError, "panic": ansiError,
		"warn": ansiWarn, "warning": ansiWarn, "retry": ansiWarn, "timeout": ansiWarn, "disabled": ansiWarn, "false": ansiWarn,
		"success": ansiSuccess, "ok": ansiSuccess, "true": ansiSuccess, "enabled": ansiSuccess,
	}
	kwColorZH = map[string]string{
		"错误": ansiError, "失败": ansiError, "异常": ansiError,
		"警告": ansiWarn, "重试": ansiWarn, "超时": ansiWarn, "禁用": ansiWarn,
		"完成": ansiSuccess, "成功": ansiSuccess, "通过": ansiSuccess, "使能": ansiSuccess, "启用": ansiSuccess,
	}
)

// highlightKeywords 把消息正文中的关键词按语义着色（error 红 / warn 黄 / success 绿）。
// 英文词带词边界（\b），"terror" 不会被 "error" 命中；中文直接包含匹配。
func highlightKeywords(s string) string {
	s = kwReEN.ReplaceAllStringFunc(s, func(w string) string {
		if c, ok := kwColorEN[strings.ToLower(w)]; ok {
			return c + w + ansiReset
		}
		return w
	})
	s = kwReZH.ReplaceAllStringFunc(s, func(w string) string {
		if c, ok := kwColorZH[w]; ok {
			return c + w + ansiReset
		}
		return w
	})
	return s
}

// ConsoleSink 终端输出端：彩色（三态）、TTY 检测、正文关键词着色、
// 进度条 writer 兼容。writer 可以是任意 io.Writer；与进度条协调时，
// 由调用方注入 processbar 提供的 writer（日志行自动显示在进度条上方）。
type ConsoleSink struct {
	w         io.Writer
	color     ColorMode
	format    string // text/json
	isTTY     bool
	highlight bool // 正文关键词着色（默认开，仅彩色模式生效）

	mu     sync.Mutex
	closed bool
}

// NewConsoleSink 创建终端输出端。
// TTY 检测：writer 是 *os.File 时检测该文件；否则（如进度条协调 writer，
// 最终都写 stderr）以 os.Stderr 的 TTY 状态为准。
func NewConsoleSink(w io.Writer, color ColorMode, format string) *ConsoleSink {
	c := &ConsoleSink{w: w, color: color, format: format, highlight: true}
	if f, ok := w.(*os.File); ok {
		c.isTTY = term.IsTerminal(int(f.Fd()))
	} else {
		c.isTTY = term.IsTerminal(int(os.Stderr.Fd()))
	}
	return c
}

// SetHighlight 开关正文关键词着色（error 红 / warn 黄 / success 绿）。
func (c *ConsoleSink) SetHighlight(on bool) {
	c.mu.Lock()
	c.highlight = on
	c.mu.Unlock()
}

// Open 启用终端 VT 转义处理（Windows 老控制台），终端输出端无需其他初始化。
func (c *ConsoleSink) Open() error {
	enableVT()
	return nil
}

// Write 输出一行日志。整行一次写入（progressbar 的 barLogWriter 依赖此约定）。
func (c *ConsoleSink) Write(rec Record) {
	var line []byte
	if c.format == "json" {
		line = FormatJSONLine(rec)
	} else {
		line = []byte(c.formatText(rec))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	_, _ = c.w.Write(line) // 终端写失败无意义，忽略
}

// formatText 彩色文本行：时间暗色、级别着色、tags 青色、字段暗色、
// ERROR 消息整行红色。无色模式与 FormatTextLine 等价。
func (c *ConsoleSink) formatText(rec Record) string {
	if !c.colorEnabled() {
		return FormatTextLine(rec, "15:04:05.000")
	}
	var sb strings.Builder
	sb.WriteString(ansiDim)
	sb.WriteString(rec.Time.Format("15:04:05.000"))
	sb.WriteString(ansiReset)
	sb.WriteByte(' ')
	sb.WriteString(levelColor(rec.Level))
	sb.WriteString(rec.Level.String())
	sb.WriteString(ansiReset)
	if len(rec.Tags) > 0 {
		sb.WriteByte(' ')
		sb.WriteString(ansiCyan)
		sb.WriteString(rec.Tags.Bracket())
		sb.WriteString(ansiReset)
	}
	if c := rec.Caller(); c != "" {
		sb.WriteByte(' ')
		sb.WriteString(ansiDim)
		sb.WriteString(c)
		sb.WriteString(ansiReset)
	}
	sb.WriteByte(' ')
	if rec.Level >= LevelError {
		sb.WriteString(ansiError)
	}
	// 关键词着色仅用于非 ERROR 行：ERROR 已整行红色，避免嵌套颜色。
	if c.highlight && rec.Level < LevelError {
		sb.WriteString(highlightKeywords(rec.Message))
	} else {
		sb.WriteString(rec.Message)
	}
	if rec.Level >= LevelError {
		sb.WriteString(ansiReset)
	}
	for _, f := range rec.Fields {
		sb.WriteByte(' ')
		sb.WriteString(ansiDim)
		appendFieldText(&sb, f)
		sb.WriteString(ansiReset)
	}
	sb.WriteByte('\n')
	return sb.String()
}

// colorEnabled 按三态 + NO_COLOR + TTY 判定是否着色。
func (c *ConsoleSink) colorEnabled() bool {
	switch c.color {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	// auto：NO_COLOR 环境变量（非空）优先于 TTY 检测。
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return c.isTTY
}

// Close 幂等关闭。
func (c *ConsoleSink) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}
