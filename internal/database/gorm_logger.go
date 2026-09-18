package database

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"gorm.io/gorm/logger"
)

// moduleRoot 是项目根目录（go.mod 所在目录）。它从本包源文件的编译期绝对路径上溯两级
// （internal/database → internal → 根）得到，并统一为 / 分隔，与 GORM 日志里
// utils.FileWithLineNum 输出的路径分隔符一致。用于把冗长的绝对源码路径裁剪成相对包根目录的短路径。
var moduleRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	// file = <root>/internal/database/gorm_logger.go
	return filepath.ToSlash(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}()

// gormLogger 构造 GORM 日志器：日志级别与默认一致（Warn），
// 但输出经 sourcePathTrimmer 裁剪掉项目根目录前缀，让打印的 file:line 更短。
func gormLogger() logger.Interface {
	prefix := ""
	if moduleRoot != "" {
		prefix = moduleRoot + "/"
	}
	return logger.New(
		log.New(&sourcePathTrimmer{out: os.Stdout, prefix: prefix}, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold: 200 * time.Millisecond,
			LogLevel:      logger.Warn,
			Colorful:      true,
		},
	)
}

// sourcePathTrimmer 是一个 io.Writer 包装：把写入内容里的绝对源码前缀替换为空，
// 使 GORM 打印的 file:line 变成相对项目根目录的形式。
type sourcePathTrimmer struct {
	out    io.Writer
	prefix string
}

func (w *sourcePathTrimmer) Write(p []byte) (int, error) {
	if w.prefix == "" {
		return w.out.Write(p)
	}
	return w.out.Write(bytes.ReplaceAll(p, []byte(w.prefix), nil))
}
