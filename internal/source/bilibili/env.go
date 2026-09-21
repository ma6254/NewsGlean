package bilibili

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ma6254/news-glean/internal/source"
)

// CheckEnv 实现 source.EnvChecker：检测 bili 是否安装、是否登录，返回登录用户信息。
func (c *connector) CheckEnv(ctx context.Context) (source.EnvCheck, error) {
	if c.cmd.empty() {
		return source.EnvCheck{
			Ready:   false,
			Missing: []string{"未检测到 bilibili-cli（未找到 bili 命令）"},
			Hints: []string{
				"安装：uv tool install bilibili-cli（或 pipx install bilibili-cli / pip install bilibili-cli）",
				"确保 ~/.local/bin 在 PATH，或在源配置里填 bili_path",
			},
		}, nil
	}

	check := source.EnvCheck{Path: c.cmd.argv0}
	ver, verErr := c.version(ctx)
	if verErr != nil {
		check.Ready = false
		check.Missing = append(check.Missing, "bili 无法执行："+verErr.Error())
		return check, nil
	}
	check.Ready = true
	check.Version = ver

	user, authed := c.whoami(ctx)
	check.Authed = authed
	check.User = user
	return check, nil
}

// version 返回 bili 的版本号（bili --version 输出 "bili, version 0.6.2"）。
func (c *connector) version(ctx context.Context) (string, error) {
	out, err := runRaw(ctx, c.cmd, "--version")
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(out)
	if i := strings.LastIndex(s, "version "); i >= 0 {
		return strings.TrimSpace(s[i+len("version "):]), nil
	}
	return s, nil
}

// whoami 读取登录用户信息。未登录（或校验失败）返回 (nil, false)。
func (c *connector) whoami(ctx context.Context) (*source.EnvUser, bool) {
	data, err := runBili(ctx, c.cmd, "whoami")
	if err != nil {
		return nil, false
	}
	var resp struct {
		User struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Level int    `json:"level"`
			Sign  string `json:"sign"`
			Coins int    `json:"coins"`
		} `json:"user"`
		Relation struct {
			Following int `json:"following"`
			Follower  int `json:"follower"`
		} `json:"relation"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, false
	}
	return &source.EnvUser{
		ID:        resp.User.ID,
		Name:      resp.User.Name,
		Level:     resp.User.Level,
		Sign:      resp.User.Sign,
		Coins:     resp.User.Coins,
		Following: resp.Relation.Following,
		Follower:  resp.Relation.Follower,
	}, true
}
