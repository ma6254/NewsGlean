package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config v2ray 风格的日志配置块。JSON 与 YAML 两种格式均支持
// （YAML 解析器兼容 JSON 语法，两种写法等价）。
//
//	v2ray 兼容三键：
//	  access     access 通道输出（文件地址；空 = 不落盘，只打 stderr）
//	  error      error 通道输出（文件地址；空 = 不落盘，只打 stderr）
//	  loglevel   级别过滤（none/error/warning/info/debug），作用于 access 通道；
//	             error 通道不受过滤（全收）
//
//	扩展键：
//	  access_level  access 通道单独级别（默认跟随 loglevel）
//	  color         auto/always/never（终端彩色三态）
//	  ws            WebSocket 监听地址（如 ":8080"；空 = 不启用）
//	  tag_rules     tag 前缀 → 级别 映射（如 {"analyze": "debug"}）
//	  format        text/json（终端与文件格式）
//	  highlight     正文关键词着色：error 红 / warn 黄 / success 绿（默认 true）
//	  max_size_mb   文件轮转大小（默认 10）
//	  max_backups   轮转保留份数（默认 5）
type Config struct {
	Access      string            `json:"access" yaml:"access"`
	Error       string            `json:"error" yaml:"error"`
	LogLevel    string            `json:"loglevel" yaml:"loglevel"`
	AccessLevel string            `json:"access_level" yaml:"access_level"`
	Color       string            `json:"color" yaml:"color"`
	WS          string            `json:"ws" yaml:"ws"`
	TagRules    map[string]string `json:"tag_rules" yaml:"tag_rules"`
	Format      string            `json:"format" yaml:"format"`
	Highlight   bool              `json:"highlight" yaml:"highlight"`
	MaxSizeMB   int               `json:"max_size_mb" yaml:"max_size_mb"`
	MaxBackups  int               `json:"max_backups" yaml:"max_backups"`

	// ConsoleWriter 终端输出端的目标 writer（nil 时用 os.Stderr）。
	// 非配置键：供调用方注入进度条协调 writer（如 processbar.LogWriter()）。
	ConsoleWriter io.Writer `json:"-" yaml:"-"`
}

// DefaultConfig 返回带默认值的配置：
// 级别 info、彩色 auto、文本格式、关键词着色开、轮转 10MB×5、不落盘（只打 stderr）、无 WS。
func DefaultConfig() *Config {
	return &Config{
		LogLevel:    "info",
		AccessLevel: "",
		Color:       "auto",
		WS:          "",
		Format:      "text",
		Highlight:   true,
		MaxSizeMB:   10,
		MaxBackups:  5,
	}
}

// ParseConfig 解析 JSON 或 YAML 格式的配置块，未出现的键保留默认值。
func ParseConfig(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse log config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate 校验配置字段的合法性。
func (c *Config) Validate() error {
	if _, err := ParseLevel(c.LogLevel); err != nil {
		return fmt.Errorf("loglevel: %w", err)
	}
	if c.AccessLevel != "" {
		if _, err := ParseLevel(c.AccessLevel); err != nil {
			return fmt.Errorf("access_level: %w", err)
		}
	}
	if _, err := ParseColorMode(c.Color); err != nil {
		return err
	}
	switch strings.ToLower(c.Format) {
	case "", "text", "json":
	default:
		return fmt.Errorf("format: invalid value %q (want text/json)", c.Format)
	}
	if c.MaxSizeMB < 1 {
		return fmt.Errorf("max_size_mb: must be >= 1, got %d", c.MaxSizeMB)
	}
	if c.MaxBackups < 1 {
		return fmt.Errorf("max_backups: must be >= 1, got %d", c.MaxBackups)
	}
	return nil
}

// Apply 把配置应用到核心：设置级别与 tag 规则，并挂载输出端。
// 始终挂载 stderr 终端输出（access + error 两通道都收）；
// access/error 配置了文件地址时再各自挂载文件输出端。
// WebSocket 输出端由 ApplyWS 单独启用（Phase 5 后可用）。
func (c *Config) Apply(core *Core) error {
	level, err := ParseLevel(c.LogLevel)
	if err != nil {
		return err
	}
	core.SetLevel(level)

	if c.AccessLevel != "" {
		al, err := ParseLevel(c.AccessLevel)
		if err != nil {
			return err
		}
		core.SetAccessLevel(al)
	}

	if len(c.TagRules) > 0 {
		rules, err := ParseTagRules(c.TagRules)
		if err != nil {
			return err
		}
		core.SetTagRules(rules)
	}

	color, err := ParseColorMode(c.Color)
	if err != nil {
		return err
	}

	// 终端输出端：两个通道都收（access/error 混排显示）。
	// writer 可由 ConsoleWriter 注入（如进度条协调 writer），默认 stderr。
	w := c.ConsoleWriter
	if w == nil {
		w = os.Stderr
	}
	core.SetRawWriter(w)
	console := NewConsoleSink(w, color, c.Format)
	console.SetHighlight(c.Highlight)
	core.AddSink(ChannelAccess, console)
	core.AddSink(ChannelError, console)

	// 文件输出端：按通道分文件（access.log / error.log）。
	if c.Access != "" {
		core.AddSink(ChannelAccess, NewFileSink(c.Access, c.Format, c.MaxSizeMB, c.MaxBackups))
	}
	if c.Error != "" {
		core.AddSink(ChannelError, NewFileSink(c.Error, c.Format, c.MaxSizeMB, c.MaxBackups))
	}

	// WebSocket 输出端：两个通道都收（同一实例注册两次，Open 幂等）。
	if c.WS != "" {
		wsSink := NewWSSink(c.WS, 1000)
		core.AddSink(ChannelAccess, wsSink)
		core.AddSink(ChannelError, wsSink)
	}
	return nil
}

// ParseTagRules 把 "前缀(点分) → 级别字符串" 映射解析为 TagRule 列表。
// 键支持层叠写法："analyze" 或 "analyze.chapter:3"。
func ParseTagRules(m map[string]string) ([]TagRule, error) {
	rules := make([]TagRule, 0, len(m))
	for key, lv := range m {
		level, err := ParseLevel(lv)
		if err != nil {
			return nil, fmt.Errorf("tag rule %q: %w", key, err)
		}
		key = strings.TrimSpace(key)
		var prefix Tags
		if key != "" {
			for _, part := range strings.Split(key, ".") {
				part = strings.TrimSpace(part)
				if part == "" {
					return nil, fmt.Errorf("tag rule %q: empty segment", key)
				}
				prefix, _ = prefix.Append(part)
			}
		}
		rules = append(rules, TagRule{Prefix: prefix, Level: level})
	}
	return rules, nil
}

// MarshalJSON 输出配置块的规范 JSON 形态（便于调试与文档示例）。
func (c *Config) MarshalJSON() ([]byte, error) {
	type alias Config
	return json.Marshal((*alias)(c))
}
