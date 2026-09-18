package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

func TestFormatTextLine(t *testing.T) {
	rec := Record{
		Time:    time.Date(2026, 8, 29, 10, 0, 1, 123000000, time.Local),
		Level:   LevelInfo,
		Tags:    Tags{"analyze", "chapter:3"},
		Message: "开始解析",
		Fields:  []Field{Str("k", "v"), Str("spaced", "a b")},
	}
	line := FormatTextLine(rec, "15:04:05.000")
	want := "10:00:01.123 INFO [analyze][chapter:3] 开始解析 k=v spaced=\"a b\"\n"
	if line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

func TestFormatJSONLine(t *testing.T) {
	rec := Record{
		Time:    time.Date(2026, 8, 29, 10, 0, 1, 0, time.Local),
		Level:   LevelWarn,
		Tags:    Tags{"openai"},
		Message: "重试",
		Fields:  []Field{Int("attempt", 2)},
	}
	line := FormatJSONLine(rec)
	s := string(line)
	for _, want := range []string{`"level":"WARN"`, `"channel":"access"`, `"tags":["openai"]`, `"msg":"重试"`, `"attempt":2`} {
		if !strings.Contains(s, want) {
			t.Errorf("json line missing %s: %s", want, s)
		}
	}
	if !strings.HasSuffix(s, "\n") {
		t.Error("json line must end with newline")
	}
}

func TestConsoleSinkColorNever(t *testing.T) {
	var buf bytes.Buffer
	sink := NewConsoleSink(&buf, ColorNever, "text")
	if err := sink.Open(); err != nil {
		t.Fatal(err)
	}
	sink.Write(Record{Time: time.Now(), Level: LevelInfo, Message: "hi"})
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("never mode must not emit ANSI: %q", buf.String())
	}
}

func TestConsoleSinkColorAlways(t *testing.T) {
	var buf bytes.Buffer
	sink := NewConsoleSink(&buf, ColorAlways, "text")
	sink.Write(Record{Time: time.Now(), Level: LevelError, Tags: Tags{"a"}, Message: "boom"})
	s := buf.String()
	if !strings.Contains(s, "\x1b[1;31m") {
		t.Errorf("always mode should color ERROR red: %q", s)
	}
	if !strings.Contains(s, "\x1b[36m[a]") {
		t.Errorf("tags should be cyan: %q", s)
	}
}

func TestConsoleSinkNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	sink := NewConsoleSink(&buf, ColorAuto, "text")
	// 非 TTY + NO_COLOR → 无色
	sink.Write(Record{Time: time.Now(), Level: LevelInfo, Message: "hi"})
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("NO_COLOR must disable color: %q", buf.String())
	}
}

func TestParseColorMode(t *testing.T) {
	for _, s := range []string{"", "auto", "AUTO", "always", "never"} {
		if _, err := ParseColorMode(s); err != nil {
			t.Errorf("ParseColorMode(%q): %v", s, err)
		}
	}
	if _, err := ParseColorMode("sometimes"); err == nil {
		t.Error("invalid mode should fail")
	}
}

// stubWriter 模拟非 *os.File 的 writer（如进度条协调 writer）。
type stubWriter struct{}

func (stubWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestConsoleSinkTTYFallback 非文件 writer（进度条协调）时 TTY 状态回退到 stderr。
func TestConsoleSinkTTYFallback(t *testing.T) {
	sink := NewConsoleSink(stubWriter{}, ColorAuto, "text")
	want := term.IsTerminal(int(os.Stderr.Fd()))
	if sink.isTTY != want {
		t.Errorf("isTTY = %v, want %v (stderr tty)", sink.isTTY, want)
	}
}

func TestHighlightKeywords(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"调用 OpenAI 失败", "调用 OpenAI " + ansiError + "失败" + ansiReset},
		{"处理成功", "处理" + ansiSuccess + "成功" + ansiReset},
		{"warning: retry", ansiWarn + "warning" + ansiReset + ": " + ansiWarn + "retry" + ansiReset},
		{"ERROR 级别", ansiError + "ERROR" + ansiReset + " 级别"},
		{"无关键词", "无关键词"},
		// 英文词边界："terror" 不应命中 "error"
		{"terror", "terror"},
		{"Success!", ansiSuccess + "Success" + ansiReset + "!"},
	}
	for _, c := range cases {
		if got := highlightKeywords(c.in); got != c.want {
			t.Errorf("highlightKeywords(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConsoleSinkKeywordHighlight(t *testing.T) {
	var buf bytes.Buffer
	sink := NewConsoleSink(&buf, ColorAlways, "text")
	// INFO 行消息含"成功" → 亮绿
	sink.Write(Record{Time: time.Now(), Level: LevelInfo, Message: "保存成功"})
	if !strings.Contains(buf.String(), ansiSuccess+"成功"+ansiReset) {
		t.Errorf("INFO 行关键词未着色: %q", buf.String())
	}

	// ERROR 行整行红，不嵌套关键词色
	buf.Reset()
	sink.Write(Record{Time: time.Now(), Level: LevelError, Message: "保存失败"})
	if strings.Contains(buf.String(), ansiError+"失败"+ansiReset) {
		t.Errorf("ERROR 行不应嵌套关键词色: %q", buf.String())
	}

	// 关闭高亮后正文原样
	buf.Reset()
	sink.SetHighlight(false)
	sink.Write(Record{Time: time.Now(), Level: LevelInfo, Message: "保存成功"})
	if strings.Contains(buf.String(), ansiSuccess) {
		t.Errorf("SetHighlight(false) 后不应着色: %q", buf.String())
	}

	// 无色模式（never）正文原样
	buf.Reset()
	plain := NewConsoleSink(&buf, ColorNever, "text")
	plain.Write(Record{Time: time.Now(), Level: LevelInfo, Message: "保存成功"})
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("never 模式不应有任何 ANSI: %q", buf.String())
	}
}

func TestApplyHighlightOff(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Highlight = false
	cfg.Color = "always"
	core := NewCore()
	if err := cfg.Apply(core); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	l := NewLogger(core)
	l.Info("保存成功")
	core.Close()
	// Apply 不写 stdout，这里只验证 Apply 不报错；着色开关由
	// TestConsoleSinkKeywordHighlight 覆盖。
}

func TestFileSinkWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	sink := NewFileSink(path, "text", 10, 5)
	if err := sink.Open(); err != nil {
		t.Fatal(err)
	}
	rec := Record{Time: time.Now(), Level: LevelInfo, Message: "hello"}
	sink.Write(rec)
	sink.Write(rec)
	sink.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "hello"); got != 2 {
		t.Errorf("file has %d lines, want 2", got)
	}
}

func TestFileSinkRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	sink := NewFileSink(path, "text", 1, 3) // 1MB 上限，3 份备份
	if err := sink.Open(); err != nil {
		t.Fatal(err)
	}
	// 写入超过 1MB 的数据触发轮转（每行 64KB）。
	big := strings.Repeat("x", 64*1024)
	rec := Record{Time: time.Now(), Level: LevelInfo, Message: big}
	for i := 0; i < 40; i++ {
		sink.Write(rec)
	}
	sink.Close()

	// 应有当前文件 + 若干备份
	entries, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Errorf("rotation produced %d backups, want >= 2: %v", len(entries), entries)
	}
	// 备份份数不超过配置
	if len(entries) > 3 {
		t.Errorf("backups %d exceed max 3", len(entries))
	}
	cur, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Size() > 1<<20 {
		t.Errorf("current file size %d exceeds 1MB", cur.Size())
	}
}

func TestApplyIntegration(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.LogLevel = "warning"
	cfg.Access = filepath.Join(dir, "access.log")
	cfg.Error = filepath.Join(dir, "error.log")
	cfg.Color = "never"

	core := NewCore()
	if err := cfg.Apply(core); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	l := NewLogger(core)
	l.Info("dropped") // info < warning，不落 access 文件
	l.Warn("kept")    // access 通道，warning 达标
	l.Error("boom")   // error 通道，全收
	core.Close()

	accessData, _ := os.ReadFile(cfg.Access)
	if strings.Contains(string(accessData), "dropped") {
		t.Error("info record must be filtered by loglevel=warning")
	}
	if !strings.Contains(string(accessData), "kept") {
		t.Errorf("warn record missing from access.log: %q", accessData)
	}
	if strings.Contains(string(accessData), "boom") {
		t.Error("error record must not go to access.log")
	}
	errData, _ := os.ReadFile(cfg.Error)
	if !strings.Contains(string(errData), "boom") {
		t.Errorf("error record missing from error.log: %q", errData)
	}
	if strings.Contains(string(errData), "kept") {
		t.Error("warn record must not go to error.log")
	}
}

func TestApplyAccessLevelOverride(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.LogLevel = "warning"
	cfg.AccessLevel = "debug" // access 通道单独放宽
	cfg.Access = filepath.Join(dir, "access.log")
	cfg.Color = "never"

	core := NewCore()
	if err := cfg.Apply(core); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	l := NewLogger(core)
	l.Debug("debug kept by access_level")
	l.Error("err") // 无 error 文件，不落盘
	core.Close()

	data, _ := os.ReadFile(cfg.Access)
	if !strings.Contains(string(data), "debug kept by access_level") {
		t.Errorf("access_level=debug should keep debug: %q", data)
	}
}
