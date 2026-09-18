package log

import (
	"errors"
	"sync"
	"testing"
)

// memSink 内存 Sink，用于测试记录捕获。
type memSink struct {
	mu     sync.Mutex
	recs   []Record
	openErr error
}

func (m *memSink) Open() error { return m.openErr }
func (m *memSink) Write(r Record) {
	m.mu.Lock()
	m.recs = append(m.recs, r)
	m.mu.Unlock()
}
func (m *memSink) Close() error { return nil }
func (m *memSink) Records() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Record{}, m.recs...)
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want Level
		ok   bool
	}{
		{"none", LevelNone, true},
		{"error", LevelError, true},
		{"warning", LevelWarn, true},
		{"warn", LevelWarn, true},
		{"WARN", LevelWarn, true},
		{"info", LevelInfo, true},
		{"debug", LevelDebug, true},
		{"bogus", LevelInfo, false},
		{"", LevelInfo, false},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.ok != (err == nil) {
			t.Errorf("ParseLevel(%q) err=%v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseLevel(%q)=%v, want %v", c.in, got, c.want)
		}
	}
}

func TestRecordChannel(t *testing.T) {
	cases := []struct {
		level Level
		want  Channel
	}{
		{LevelDebug, ChannelAccess},
		{LevelInfo, ChannelAccess},
		{LevelWarn, ChannelAccess},
		{LevelError, ChannelError},
	}
	for _, c := range cases {
		if got := (Record{Level: c.level}).Channel(); got != c.want {
			t.Errorf("Level %v channel=%v, want %v", c.level, got, c.want)
		}
	}
}

func TestTagsAppendImmutability(t *testing.T) {
	parent := Tags{"analyze"}
	child, ok := parent.Append("chapter:3")
	if !ok {
		t.Fatal("append rejected within limit")
	}
	if len(parent) != 1 || parent[0] != "analyze" {
		t.Errorf("parent mutated: %v", parent)
	}
	if len(child) != 2 || child[1] != "chapter:3" {
		t.Errorf("child = %v, want [analyze chapter:3]", child)
	}
	if got := child.Bracket(); got != "[analyze][chapter:3]" {
		t.Errorf("Bracket()=%q", got)
	}
}

func TestTagsMaxDepth(t *testing.T) {
	var ts Tags
	for i := 0; i < MaxTagDepth; i++ {
		var ok bool
		ts, ok = ts.Append("t")
		if !ok {
			t.Fatalf("append %d rejected", i)
		}
	}
	if _, ok := ts.Append("overflow"); ok {
		t.Error("append beyond MaxTagDepth should be rejected")
	}
}

func TestTagsHasPrefix(t *testing.T) {
	ts := Tags{"analyze", "chapter:3"}
	cases := []struct {
		prefix Tags
		want   bool
	}{
		{Tags{}, true},
		{Tags{"analyze"}, true},
		{Tags{"analyze", "chapter:3"}, true},
		{Tags{"analyze", "chapter:3", "x"}, false},
		{Tags{"openai"}, false},
	}
	for _, c := range cases {
		if got := ts.HasPrefix(c.prefix); got != c.want {
			t.Errorf("HasPrefix(%v)=%v, want %v", c.prefix, got, c.want)
		}
	}
}

func TestLevelShortCircuit(t *testing.T) {
	core := NewCore()
	core.SetLevel(LevelWarn)
	sink := &memSink{}
	core.AddSink(ChannelAccess, sink)
	core.AddSink(ChannelError, sink)

	l := NewLogger(core)
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")

	recs := sink.Records()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (warn+error): %+v", len(recs), recs)
	}
	if recs[0].Message != "w" || recs[1].Message != "e" {
		t.Errorf("messages = %q, %q; want w, e", recs[0].Message, recs[1].Message)
	}
}

func TestChannelRouting(t *testing.T) {
	core := NewCore()
	access := &memSink{}
	errs := &memSink{}
	core.AddSink(ChannelAccess, access)
	core.AddSink(ChannelError, errs)

	l := NewLogger(core)
	l.Info("flow")
	l.Error("boom")

	if len(access.Records()) != 1 || access.Records()[0].Message != "flow" {
		t.Errorf("access sink = %+v, want [flow]", access.Records())
	}
	if len(errs.Records()) != 1 || errs.Records()[0].Message != "boom" {
		t.Errorf("error sink = %+v, want [boom]", errs.Records())
	}
}

func TestTagRuleOverride(t *testing.T) {
	core := NewCore()
	core.SetLevel(LevelWarn)
	// 全局 warn；analyze 前缀下允许 debug。
	core.SetTagRules([]TagRule{{Prefix: Tags{"analyze"}, Level: LevelDebug}})
	sink := &memSink{}
	core.AddSink(ChannelAccess, sink)

	l := NewLogger(core)
	l.Info("global: dropped")
	l.WithTag("analyze").Debug("analyze: kept")
	l.WithTag("analyze").WithTag("chapter:3").Debug("nested: kept")

	recs := sink.Records()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(recs), recs)
	}
	if recs[0].Message != "analyze: kept" || recs[1].Message != "nested: kept" {
		t.Errorf("records = %q, %q", recs[0].Message, recs[1].Message)
	}
}

func TestLongestPrefixWins(t *testing.T) {
	core := NewCore()
	core.SetLevel(LevelInfo)
	core.SetTagRules([]TagRule{
		{Prefix: Tags{"analyze"}, Level: LevelDebug},
		{Prefix: Tags{"analyze", "chapter:3"}, Level: LevelWarn},
	})
	sink := &memSink{}
	core.AddSink(ChannelAccess, sink)

	l := NewLogger(core).WithTag("analyze")
	l.Debug("debug kept by short rule")
	l.WithTag("chapter:3").Debug("debug dropped by long rule")
	l.WithTag("chapter:3").Warn("warn kept by long rule")

	recs := sink.Records()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(recs), recs)
	}
	if recs[0].Message != "debug kept by short rule" || recs[1].Message != "warn kept by long rule" {
		t.Errorf("records = %q, %q", recs[0].Message, recs[1].Message)
	}
}

func TestLevelNoneSilencesAll(t *testing.T) {
	core := NewCore()
	core.SetLevel(LevelNone)
	sink := &memSink{}
	core.AddSink(ChannelAccess, sink)
	core.AddSink(ChannelError, sink)

	l := NewLogger(core)
	l.Debug("d")
	l.Error("e")
	if got := len(sink.Records()); got != 0 {
		t.Fatalf("loglevel=none should silence everything, got %d records", got)
	}
}

func TestParseFields(t *testing.T) {
	// slog 风格键值对；奇数个参数时最后一个键缺失值 → !BADKEY。
	fields := parseFields(nil, "a", 1, "b", "x", "orphan")
	if len(fields) != 3 {
		t.Fatalf("fields = %+v, want 3", fields)
	}
	if fields[0] != (Field{"a", 1}) || fields[1] != (Field{"b", "x"}) {
		t.Errorf("fields = %+v", fields)
	}
	if fields[2].Key != "!BADKEY" || fields[2].Value != "orphan" {
		t.Errorf("odd arg should become !BADKEY, got %+v", fields[2])
	}

	base := []Field{Str("task", "t1")}
	merged := parseFields(base, "k", "v")
	if len(merged) != 2 || merged[0] != base[0] || merged[1].Key != "k" {
		t.Errorf("merged = %+v", merged)
	}
	if len(base) != 1 {
		t.Error("base must not be mutated")
	}
}

func TestParseFieldsDirectField(t *testing.T) {
	// 直接传 Field 也支持。
	fields := parseFields(nil, Str("a", "1"), Int("n", 2))
	if len(fields) != 2 {
		t.Fatalf("fields = %+v, want 2", fields)
	}
	if fields[0] != (Field{"a", "1"}) || fields[1] != (Field{"n", 2}) {
		t.Errorf("fields = %+v", fields)
	}
}

func TestWithFixedFields(t *testing.T) {
	core := NewCore()
	sink := &memSink{}
	core.AddSink(ChannelAccess, sink)

	l := NewLogger(core).With(Str("task", "t1"))
	l.WithTag("analyze").Info("msg", Int("n", 2))

	recs := sink.Records()
	if len(recs) != 1 {
		t.Fatal("no record")
	}
	rec := recs[0]
	if len(rec.Fields) != 2 || rec.Fields[0] != (Field{"task", "t1"}) || rec.Fields[1] != (Field{"n", 2}) {
		t.Errorf("fields = %+v", rec.Fields)
	}
	if len(rec.Tags) != 1 || rec.Tags[0] != "analyze" {
		t.Errorf("tags = %v", rec.Tags)
	}
}

func TestUnusedTagRulesReport(t *testing.T) {
	core := NewCore()
	core.SetTagRules([]TagRule{
		{Prefix: Tags{"analyze"}, Level: LevelDebug},
		{Prefix: Tags{"openai"}, Level: LevelNone},
	})
	sink := &memSink{}
	core.AddSink(ChannelAccess, sink)

	l := NewLogger(core).WithTag("analyze")
	l.Debug("hits analyze rule")

	unused := core.UnusedRules()
	if len(unused) != 1 || unused[0] != "openai" {
		t.Errorf("UnusedRules = %v, want [openai]", unused)
	}
}

func TestSinkOpenFailureDegrades(t *testing.T) {
	core := NewCore()
	core.AddSink(ChannelAccess, &memSink{openErr: errors.New("boom")})
	core.AddSink(ChannelAccess, &memSink{})
	l := NewLogger(core)
	l.Info("still works")
	// 不崩溃即通过；再验证正常 sink 仍收到记录。
}
