package bilibili

import (
	"github.com/ma6254/news-glean/internal/source"
	"github.com/spf13/pflag"
)

// init 在包被导入时注册 bilibili 渠道类型。
func init() {
	source.Register("bilibili", source.Metadata{
		Name:        "bilibili",
		Description: "B 站个人数据（观看历史 / 收藏夹），经 bilibili-cli 子进程取数",
		Pull:        true,
		Push:        false,
	}, New, extraFlag)
}

// extraFlag 在 source add 子命令上注册 bilibili 渠道的专属 flag。
func extraFlag(fs *pflag.FlagSet) {
	fs.String("mode", "", "bilibili 模式：history|favorites")
	fs.String("fav-id", "", "favorites 模式的收藏夹 ID")
}
