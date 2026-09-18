package database

import (
	"bytes"
	"testing"
)

func TestSourcePathTrimmer(t *testing.T) {
	if moduleRoot == "" {
		t.Fatal("moduleRoot should be non-empty")
	}

	const suffix = "/internal/database/source_cursor.go:25 record not found\n"
	var buf bytes.Buffer
	w := &sourcePathTrimmer{out: &buf, prefix: moduleRoot + "/"}
	if _, err := w.Write([]byte(moduleRoot + suffix)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := buf.String()
	want := suffix[1:] // 去掉前导 /，得到相对路径
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSourcePathTrimmerEmptyPrefix(t *testing.T) {
	// 前缀为空时应原样透传（兜底，避免误替换）
	const line = "C:/some/path.go:1 hello\n"
	var buf bytes.Buffer
	w := &sourcePathTrimmer{out: &buf, prefix: ""}
	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != line {
		t.Fatalf("got %q, want %q", buf.String(), line)
	}
}
