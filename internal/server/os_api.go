package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// OsInfo 是操作系统信息响应体。
type OsInfo struct {
	Hostname   string `json:"hostname"`     // 主机名
	OsName     string `json:"os_name"`      // 操作系统名称
	Platform   string `json:"platform"`     // 系统平台
	Arch       string `json:"arch"`         // 系统架构
	StartTime  string `json:"start_time"`   // 系统启动时间（RFC3339）
	TotalMemMB uint64 `json:"total_mem_mb"` // 总内存（MB）
	CPUCount   int    `json:"cpu_count"`    // CPU 核心数
	CPUModel   string `json:"cpu_model"`    // CPU 型号
}

// OsState 是操作系统运行状态响应体。
type OsState struct {
	Uptime     string `json:"uptime"`      // 运行时长
	FreeMemMB  uint64 `json:"free_mem_mb"` // 空闲内存（MB）
	CPUPercent uint8  `json:"cpu_percent"` // CPU 使用率（%）
	CPUFreq    string `json:"cpu_freq"`    // CPU 频率
}

// handleOsInfo 处理 GET /api/os/info。
//
// @Summary      获取操作系统信息
// @Description  获取主机名、操作系统、CPU 与内存等基础信息
// @Tags         系统信息
// @Produce      json
// @Success      200  {object}  OsInfo
// @Router       /os/info [get]
func (s *Server) handleOsInfo(w http.ResponseWriter, _ *http.Request) {
	v, _ := mem.VirtualMemory()
	info, _ := host.Info()
	bootTime, _ := host.BootTime()

	cpuCount, _ := cpu.Counts(true)
	cpuInfo, _ := cpu.Info()

	osInfo := OsInfo{}
	if info != nil {
		osInfo.Hostname = info.Hostname
		osInfo.OsName = info.OS
		osInfo.Platform = info.Platform
		osInfo.Arch = info.KernelArch
	}
	if bootTime > 0 {
		osInfo.StartTime = time.Unix(int64(bootTime), 0).Format(time.RFC3339)
	}
	if v != nil {
		osInfo.TotalMemMB = v.Total / 1024 / 1024
	}
	osInfo.CPUCount = cpuCount
	if len(cpuInfo) > 0 {
		osInfo.CPUModel = cpuInfo[0].ModelName
	}
	writeJSON(w, http.StatusOK, osInfo)
}

// handleOsState 处理 GET /api/os/state。
//
// @Summary      获取操作系统状态
// @Description  获取系统的运行时长、空闲内存与 CPU 使用率
// @Tags         系统信息
// @Produce      json
// @Success      200  {object}  OsState
// @Router       /os/state [get]
func (s *Server) handleOsState(w http.ResponseWriter, _ *http.Request) {
	v, _ := mem.VirtualMemory()
	bootTime, _ := host.BootTime()
	cpuPercent, _ := cpu.Percent(0, false)
	cpuInfo, _ := cpu.Info()

	osState := OsState{}
	if bootTime > 0 {
		osState.Uptime = formatDuration(time.Since(time.Unix(int64(bootTime), 0)))
	}
	if v != nil {
		osState.FreeMemMB = v.Free / 1024 / 1024
	}
	if len(cpuPercent) > 0 {
		osState.CPUPercent = uint8(cpuPercent[0])
	}
	if len(cpuInfo) > 0 {
		osState.CPUFreq = fmt.Sprintf("%.2f MHz", cpuInfo[0].Mhz)
	}
	writeJSON(w, http.StatusOK, osState)
}
