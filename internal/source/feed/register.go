package feed

import (
	"github.com/ma6254/news-glean/internal/source"
	"github.com/spf13/pflag"
)

// init 在包被导入时注册 feed 渠道类型。
func init() {
	source.Register("feed", source.Metadata{
		Name:        "feed",
		Description: "RSS 2.0 / Atom / JSON Feed",
		Pull:        true,
		Push:        false,
	}, New, extraFlag)
}

// extraFlag 在 source add 子命令上注册 feed 渠道的专属 flag。
func extraFlag(fs *pflag.FlagSet) {
	fs.String("url", "", "feed 地址（http/https）")
	fs.String("fetch-mode", "http", "抓取方式：http|chromedp|chromedp_headed")
}
