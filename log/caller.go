package log

import (
	"path/filepath"
	"runtime"
)

// moduleRoot 是模块根目录（log 包的父目录），用于把调用点文件名缩成相对模块根的短路径。
var moduleRoot = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}()

// caller 返回调用点的文件名（相对模块根的斜杠路径）与行号。
// 固定跳过 3 帧：caller → (*Logger).log → Info（方法或包级函数）→ 用户代码。
// 包级便捷函数与派生 Logger 都收敛到 (*Logger).log，因此深度一致，无需扫栈。
func caller() (string, int) {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "???", 0
	}
	if rel, err := filepath.Rel(moduleRoot, file); err == nil {
		return filepath.ToSlash(rel), line
	}
	return filepath.ToSlash(file), line
}
