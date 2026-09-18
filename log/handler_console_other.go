//go:build !windows

package log

// enableVT 启用终端 VT 转义处理。Windows 实现见 handler_console_windows.go；
// 其他平台无需处理，空实现。
func enableVT() {}
