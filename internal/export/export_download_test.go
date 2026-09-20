package export

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteZipDir 验证目录打包：保留相对路径、内部用正斜杠、内容一致。
func TestWriteZipDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteZipDir(&buf, dir); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	content := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		content[f.Name] = string(data)
	}
	if content["a.txt"] != "hello" {
		t.Errorf("a.txt = %q, want hello", content["a.txt"])
	}
	if content["sub/b.txt"] != "world" {
		t.Errorf("sub/b.txt = %q, want world", content["sub/b.txt"])
	}
}
