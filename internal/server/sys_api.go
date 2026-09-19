package server

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/ma6254/news-glean/internal/build"
)

// SysInfo 是系统信息响应体（版本号 / 构建时间 / Go 版本 / 启动时间）。
type SysInfo struct {
	StartTime    string `json:"start_time"`    // 服务启动时间（RFC3339）
	BuildTime    string `json:"build_time"`    // 构建时间
	GoVersion    string `json:"go_version"`    // Go 版本
	BuildVersion string `json:"build_version"` // 构建版本
}

// SysState 是系统运行状态响应体。
type SysState struct {
	Uptime       string `json:"uptime"`         // 运行时长
	NumGoroutine int    `json:"num_goroutine"`  // 当前协程数
	HeapUsedMB   int    `json:"heap_used_mb"`   // 堆内存使用量（MB）
	GcTotalCount int    `json:"gc_total_count"` // 垃圾回收总次数
}

// handleSysInfo 处理 GET /api/sys/info。
//
// @Summary      获取系统信息
// @Description  获取服务的启动时间、构建时间与版本信息
// @Tags         系统信息
// @Produce      json
// @Success      200  {object}  SysInfo
// @Router       /sys/info [get]
func (s *Server) handleSysInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, SysInfo{
		StartTime:    s.startTime.Format(time.RFC3339),
		BuildTime:    build.BuildTime,
		GoVersion:    runtime.Version(),
		BuildVersion: build.BuildVersion,
	})
}

// handleSysState 处理 GET /api/sys/state。
//
// @Summary      获取系统状态
// @Description  获取服务的运行时长、协程数、堆内存与 GC 次数
// @Tags         系统状态
// @Produce      json
// @Success      200  {object}  SysState
// @Router       /sys/state [get]
func (s *Server) handleSysState(w http.ResponseWriter, _ *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	writeJSON(w, http.StatusOK, SysState{
		Uptime:       formatDuration(time.Since(s.startTime)),
		NumGoroutine: runtime.NumGoroutine(),
		HeapUsedMB:   int(ms.HeapAlloc / 1024 / 1024),
		GcTotalCount: int(ms.NumGC),
	})
}

// formatDuration 把时长格式化为「Ndd hh mm ss」的可读形式。
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %02dh %02dm %02ds", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%02dh %02dm %02ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%02dm %02ds", minutes, seconds)
	}
	return fmt.Sprintf("%02ds", seconds)
}
