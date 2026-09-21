package source

import (
	"context"
	"errors"
)

// ErrEnvCheckUnsupported 表示该渠道类型不支持「运行环境检测」。
var ErrEnvCheckUnsupported = errors.New("env check not supported")

// EnvCheck 是渠道运行环境的一次检测结果。
type EnvCheck struct {
	Ready   bool     `json:"ready"`   // 是否可正常采集
	Version string   `json:"version"` // 外部依赖版本（空=未装）
	Path    string   `json:"path"`    // 命中的可执行文件路径
	Authed  bool     `json:"authed"`  // 登录态（仅登录类 mode 有意义）
	User    *EnvUser `json:"user"`    // 登录用户信息（未登录为 nil）
	Missing []string `json:"missing"` // 缺失项的人话描述
	Hints   []string `json:"hints"`   // 安装/修复提示（供前端渲染教程）
}

// EnvUser 是登录用户的展示信息。
type EnvUser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"` // 首版为空（占位），后续上游补 face
	Level     int    `json:"level"`
	Sign      string `json:"sign"`
	Coins     int    `json:"coins"`
	Following int    `json:"following"`
	Follower  int    `json:"follower"`
}

// EnvChecker 是 Connector 的可选能力：检测运行环境（如外部 CLI 是否安装/登录）。
// 实现方只需在依赖外部运行时时实现；不实现则核心层返回 ErrEnvCheckUnsupported。
type EnvChecker interface {
	// CheckEnv 检测运行环境。实现应低成本、可重复调用（前端「重新检测」按钮）。
	CheckEnv(ctx context.Context) (EnvCheck, error)
}
