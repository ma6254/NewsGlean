package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
)

// FavoriteFolder 是 bilibili-cli 返回的收藏夹。
type FavoriteFolder struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	MediaCount int    `json:"media_count"`
}

// ListFavorites 列出当前登录用户的收藏夹（bili favorites --json，需登录）。
// biliPath 可选覆盖可执行文件路径。
func ListFavorites(ctx context.Context, biliPath string) ([]FavoriteFolder, error) {
	data, err := runBili(ctx, resolveBili(biliPath), "favorites")
	if err != nil {
		return nil, err
	}
	var folders []FavoriteFolder
	if err := json.Unmarshal(data, &folders); err != nil {
		return nil, fmt.Errorf("bilibili: 解析收藏夹列表: %w", err)
	}
	return folders, nil
}
