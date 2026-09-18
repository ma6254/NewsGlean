package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileSink 文件输出端：大小轮转（maxSizeMB × backups 份）、并发安全、
// 打开/写入失败降级（限频警告一次，不崩溃）。
type FileSink struct {
	path    string
	format  string
	maxSize int64
	backups int

	mu     sync.Mutex
	f      *os.File
	size   int64
	closed bool
	warned bool
}

// NewFileSink 创建文件输出端。
func NewFileSink(path, format string, maxSizeMB, backups int) *FileSink {
	return &FileSink{
		path:    path,
		format:  format,
		maxSize: int64(maxSizeMB) << 20,
		backups: backups,
	}
}

// Open 创建父目录并以追加模式打开文件。
func (s *FileSink) Open() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	s.mu.Lock()
	s.f = f
	s.size = info.Size()
	s.mu.Unlock()
	return nil
}

// Write 追加一行日志；超过大小阈值时先轮转。
func (s *FileSink) Write(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.f == nil {
		return
	}
	if s.size >= s.maxSize {
		if err := s.rotate(); err != nil {
			s.report(err)
			return
		}
	}
	var line []byte
	if s.format == "json" {
		line = FormatJSONLine(rec)
	} else {
		line = []byte(FormatTextLine(rec, "2006-01-02 15:04:05.000"))
	}
	n, err := s.f.Write(line)
	s.size += int64(n)
	if err != nil {
		s.report(fmt.Errorf("write %s: %w", s.path, err))
	}
}

// rotate 大小轮转：当前文件 → .1，旧备份依次后移，最旧一份删除。
// 从后往前移动，兼容 Windows rename 不覆盖已存在文件的行为。
func (s *FileSink) rotate() error {
	if err := s.f.Close(); err != nil {
		return err
	}
	s.f = nil
	for i := s.backups; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", s.path, i)
		if i == s.backups {
			_ = os.Remove(old) // 最旧一份直接删
			continue
		}
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, fmt.Sprintf("%s.%d", s.path, i+1)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(s.path, s.path+".1"); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	s.f = f
	s.size = 0
	return nil
}

// Close 幂等关闭并刷盘。
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	s.closed = true
	return err
}

// report 写失败时限频警告（仅首次，避免刷屏）。
func (s *FileSink) report(err error) {
	if s.warned {
		return
	}
	s.warned = true
	fmt.Fprintf(os.Stderr, "log: %v\n", err)
}
