//go:build windows

package log

import "golang.org/x/sys/windows"

// enableVT 在 Windows 上启用 stderr 控制台的 VT 转义处理，
// 兼容老 conhost（Windows 10 1809+ 默认已启用；Windows Terminal 无需）。
// 幂等：重复调用无害。
func enableVT() {
	h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
