package export

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteZipDir 把目录 dir 递归打包成 zip 写入 w，条目保留相对 dir 的路径
// （内部用正斜杠）。供下载端点把 Markdown 目录或多本 EPUB 打包下发。
// 流式写入：调用方需在写入前设置好响应头，中途出错只能终止数据流。
func WriteZipDir(w io.Writer, dir string) error {
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fw, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		cerr := f.Close()
		if err != nil {
			return err
		}
		return cerr
	})
	if err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}
