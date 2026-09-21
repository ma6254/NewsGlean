package bilibili

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// errNotInstalled 表示未定位到 bili 命令。
var errNotInstalled = errors.New("bilibili: 未检测到 bilibili-cli，请先安装（uv tool install bilibili-cli，或 pipx/pip 安装）")

// biliCmd 表示一条可执行 bili 的调用前缀。
// argv0 为可执行文件路径；当 argv0=="uv" 时，prefix 提供 "tool run bilibili-cli" 前缀。
type biliCmd struct {
	argv0  string
	prefix []string
}

// empty 表示未找到 bili。
func (c biliCmd) empty() bool { return c.argv0 == "" }

// resolveBili 按四级探测定位 bili 命令：
// 1) 源配置 bili_path 覆盖；2) PATH 上的 `bili`；3) uv tool run；4) 已知 uv 安装路径。
func resolveBili(override string) biliCmd {
	if override != "" {
		// 覆盖路径即使不存在也原样返回，让 exec 报出明确错误，而不是回退到其它探测。
		return biliCmd{argv0: override}
	}
	if p, err := exec.LookPath("bili"); err == nil {
		return biliCmd{argv0: p}
	}
	if _, err := exec.LookPath("uv"); err == nil {
		return biliCmd{argv0: "uv", prefix: []string{"tool", "run", "bilibili-cli"}}
	}
	for _, p := range knownBiliPaths() {
		if _, err := os.Stat(p); err == nil {
			return biliCmd{argv0: p}
		}
	}
	return biliCmd{}
}

// knownBiliPaths 返回 uv 工具常见的安装位置。
func knownBiliPaths() []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths,
			filepath.Join(home, ".local", "bin", "bili.exe"),
			filepath.Join(home, ".local", "bin", "bili"),
		)
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		paths = append(paths, filepath.Join(ad, "uv", "tools", "bilibili-cli", "Scripts", "bili.exe"))
	}
	return paths
}

// envelope 是 bilibili-cli 的机器可读输出信封。
// 成功：{ok:true, schema_version:"1", data:…}；失败：{ok:false, error:{code,message}}。
type envelope struct {
	OK            bool            `json:"ok"`
	SchemaVersion string          `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
	Error         *envError       `json:"error"`
}

type envError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// cliErr 是 bilibili-cli 返回的业务错误（ok=false）。
type cliErr struct {
	Code    string
	Message string
}

func (e *cliErr) Error() string {
	if e.Code == "" {
		return "bilibili: " + e.Message
	}
	return fmt.Sprintf("bilibili: %s（%s）", e.Message, e.Code)
}

// parseEnvelope 解析 stdout 为 envelope。
func parseEnvelope(b []byte) (envelope, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return envelope{}, errors.New("bilibili: 空输出")
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return envelope{}, err
	}
	return env, nil
}

// execBili 执行 bili 命令并捕获 stdout/stderr，统一注入 UTF-8 环境。
// 必须显式设置 PYTHONUTF8/PYTHONIOENCODING：否则含 emoji/中文的输出会在 GBK 控制台崩溃或乱码。
func execBili(ctx context.Context, cmd biliCmd, args ...string) (stdout, stderr string, err error) {
	if cmd.empty() {
		return "", "", errNotInstalled
	}
	full := make([]string, 0, len(cmd.prefix)+len(args))
	full = append(full, cmd.prefix...)
	full = append(full, args...)

	c := exec.CommandContext(ctx, cmd.argv0, full...)
	c.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	var out, errb bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errb
	runErr := c.Run()
	return out.String(), errb.String(), runErr
}

// runBili 执行 bili 子命令并返回信封的 data 字段。
// 限速 + 对可重试错误（rate_limited/network_error）做指数退避重试。
func runBili(ctx context.Context, cmd biliCmd, args ...string) (json.RawMessage, error) {
	if err := biliRateLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	args = append(args, "--json")

	var data json.RawMessage
	err := retry(ctx, maxRetries, retryBaseDelay, cliRetryable, func() error {
		stdout, stderr, runErr := execBili(ctx, cmd, args...)
		// 优先按 envelope 解析 stdout（成功与业务失败都走这里）。
		if env, perr := parseEnvelope([]byte(stdout)); perr == nil {
			if !env.OK {
				return mapCliError(&cliErr{Code: env.Error.Code, Message: env.Error.Message})
			}
			data = env.Data
			return nil
		}
		// stdout 不是合法 envelope（崩溃 / 未加 --json），带上进程错误与 stderr。
		if runErr != nil {
			return fmt.Errorf("bilibili: bili %s: %w（stderr: %s）", args[0], runErr, strings.TrimSpace(stderr))
		}
		return fmt.Errorf("bilibili: bili %s: 无法把输出解析为 JSON envelope", args[0])
	})
	return data, err
}

// runRaw 执行 bili 命令并原样返回 stdout（用于 --version 等非 JSON 输出）。
func runRaw(ctx context.Context, cmd biliCmd, args ...string) (string, error) {
	stdout, stderr, runErr := execBili(ctx, cmd, args...)
	if runErr != nil {
		return "", fmt.Errorf("bilibili: bili %s: %w（stderr: %s）", args[0], runErr, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

// mapCliError 把 bilibili-cli 的错误码翻译成可读错误。
func mapCliError(e *cliErr) error {
	switch e.Code {
	case "not_authenticated":
		return fmt.Errorf("bilibili: 未登录：%s（请先运行 `bili login` 或用 SESSDATA/bili_jct 手动登录）", e.Message)
	case "rate_limited":
		return fmt.Errorf("bilibili: 触发限流/风控：%s", e.Message)
	default:
		return e
	}
}
