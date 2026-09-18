package source

import (
	"fmt"
	"sort"
	"sync"

	"github.com/spf13/pflag"
)

// Metadata 描述一个渠道类型的静态信息。
type Metadata struct {
	Name        string // 类型标识，如 "feed"、"webpage"、"telegram"
	Description string // 一句话说明
	Pull        bool   // 支持拉取（定时调用 Fetch）
	Push        bool   // 支持推送（Run 长驻 / HTTP 回调）
}

// CreateOptions 是构造渠道实例时的全局参数。
type CreateOptions struct {
	Proxy      string // 网络代理地址，留空读环境变量
	ChromePath string // Chrome 可执行文件路径，留空自动探测（chromedp 抓取用）
}

// CreateFunc 根据渠道配置 JSON 字符串与全局参数构造一个 Connector。
// config 是不透明 JSON，由适配器自行解析；凭据通过 credentials 层单独注入。
type CreateFunc func(config string, opts CreateOptions) (Connector, error)

// ExtraFlagFunc 在 source add 子命令上注册该渠道专属的额外 flag。
// 可为 nil，表示该渠道没有额外 flag。
type ExtraFlagFunc func(fs *pflag.FlagSet)

type registryEntry struct {
	meta      Metadata
	create    CreateFunc
	extraFlag ExtraFlagFunc
}

var (
	registryMu sync.RWMutex
	registry   = map[string]registryEntry{}
)

// Register 注册一个渠道类型。name 为类型标识，重复注册会 panic。
func Register(name string, meta Metadata, create CreateFunc, extraFlag ExtraFlagFunc) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[name]; ok {
		panic(fmt.Sprintf("source type %q already registered", name))
	}
	registry[name] = registryEntry{meta: meta, create: create, extraFlag: extraFlag}
}

// Get 返回已注册渠道类型的元数据与构造函数。
func Get(name string) (Metadata, CreateFunc, ExtraFlagFunc, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	e, ok := registry[name]
	if !ok {
		return Metadata{}, nil, nil, fmt.Errorf("source type %q not registered", name)
	}
	return e.meta, e.create, e.extraFlag, nil
}

// Create 根据类型标识、配置 JSON 与全局参数构造一个 Connector 实例。
func Create(name, config string, opts CreateOptions) (Connector, error) {
	_, create, _, err := Get(name)
	if err != nil {
		return nil, err
	}
	return create(config, opts)
}

// Types 返回所有已注册渠道类型的标识，按字典序排序。
func Types() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
