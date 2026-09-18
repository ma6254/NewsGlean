// Package build 存放构建信息，由 build.ps1 通过 -ldflags 注入。
package build

// BuildTime 构建时间（RFC3339）。
var BuildTime = "unknown"

// BuildVersion 版本号，如 "0.1.0"。
var BuildVersion = "dev"
