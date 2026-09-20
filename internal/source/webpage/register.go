package webpage

import (
	"github.com/ma6254/news-glean/internal/source"
	"github.com/spf13/pflag"
)

// init 在包被导入时注册 webpage 渠道类型。
func init() {
	source.Register("webpage", source.Metadata{
		Name:        "webpage",
		Description: "网页列表页爬取（CSS 选择器）",
		Pull:        true,
		Push:        false,
	}, New, extraFlag)
}

// extraFlag 在 source add 子命令上注册 webpage 渠道的专属 flag。
func extraFlag(fs *pflag.FlagSet) {
	fs.String("url", "", "列表页地址（http/https）")
	fs.String("selector", "", "条目 CSS 选择器（如 div.article-list > a）")
	fs.Bool("full-text", false, "是否回源抓取正文（阶段 12 生效）")
}
