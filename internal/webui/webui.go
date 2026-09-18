// Package webui 用 go:embed 打包前端构建产物（NewsGlean-web 的 dist/）。
// build.ps1 在 go build 前会把 ../NewsGlean-web/dist 复制到本目录下的 dist/，
// 未构建时保留 dist/index.html 占位页（前端仍可用 proxy 模式提供）。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Dist 返回以 dist 为根的文件系统，供 http.FileServer 使用。
func Dist() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}
