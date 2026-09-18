package cmd

import (
	"strings"

	"github.com/common-nighthawk/go-figure"
	"github.com/ma6254/news-glean/internal/build"
)

// banner 返回启动 banner：用 go-figure 库生成的 ASCII 艺术字（figlet standard 字体）
// + 上下边框 + 版本行。用库生成，避免手写艺术字出错；standard 字体与参考项目
// BookCocoon 同风格。
func banner() string {
	art := strings.TrimSuffix(figure.NewFigure("NewsGlean", "standard", true).String(), "\n")
	lines := strings.Split(art, "\n")

	// 边框宽度取最长一行，保证上下对齐。
	width := 0
	for _, ln := range lines {
		if len(ln) > width {
			width = len(ln)
		}
	}
	frame := strings.Repeat("=", width)

	var sb strings.Builder
	sb.WriteString(frame)
	sb.WriteByte('\n')
	sb.WriteString(art)
	sb.WriteByte('\n')
	sb.WriteString(frame)
	sb.WriteByte('\n')
	sb.WriteString("   v" + build.BuildVersion + " (build " + build.BuildTime + ")\n")
	return sb.String()
}
