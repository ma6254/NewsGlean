package log_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ma6254/news-glean/log"
)

// TestCallerMethod 验证派生 Logger 的记录携带正确调用点（相对模块根的 file:line）。
func TestCallerMethod(t *testing.T) {
	core := log.NewCore()
	lg := log.NewLogger(core)
	var got log.Record
	core.AddSink(log.ChannelAccess, log.SinkFunc(func(r log.Record) { got = r }))

	lg.Info("hello")

	if !strings.HasSuffix(got.File, "caller_test.go") {
		t.Fatalf("File = %q, want suffix caller_test.go", got.File)
	}
	if got.Line <= 0 {
		t.Fatalf("Line = %d, want > 0", got.Line)
	}
	if got.Caller() != got.File+":"+strconv.Itoa(got.Line) {
		t.Fatalf("Caller = %q, want %s:%d", got.Caller(), got.File, got.Line)
	}
}

// TestCallerPackageLevel 验证包级 log.Info 的记录同样指向调用点（而非 log 包内部）。
func TestCallerPackageLevel(t *testing.T) {
	core := log.DefaultCore()
	var got log.Record
	core.AddSink(log.ChannelAccess, log.SinkFunc(func(r log.Record) { got = r }))
	defer core.Close()

	log.Info("pkg")

	if !strings.HasSuffix(got.File, "caller_test.go") {
		t.Fatalf("File = %q, want suffix caller_test.go", got.File)
	}
	if got.Line <= 0 {
		t.Fatalf("Line = %d, want > 0", got.Line)
	}
}
