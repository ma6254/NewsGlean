// Package config 定义 YAML 配置结构与加载逻辑。
// 配置优先级：命令行参数 > 环境变量 > 配置文件 > 内置默认值（环境变量覆盖在后续里程碑接入）。
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是应用配置的根结构，对应 config.yml。
type Config struct {
	Log         LogConfig         `yaml:"log"`
	Server      ServerConfig      `yaml:"server"`
	Web         WebConfig         `yaml:"web"`
	Database    DatabaseConfig    `yaml:"database"`
	Fetch       FetchConfig       `yaml:"fetch"`
	Credentials CredentialsConfig `yaml:"credentials"`
	Filter      FilterConfig      `yaml:"filter"`
	LLM         LLMConfig         `yaml:"llm"`
	Export      ExportConfig      `yaml:"export"`
}

// LogConfig 日志参数。
type LogConfig struct {
	Dir        string `yaml:"dir"`         // 日志文件路径
	Level      string `yaml:"level"`       // 日志级别：debug, info, warn, error
	MaxSize    int    `yaml:"max_size"`    // 日志文件最大大小（MB）
	MaxBackups int    `yaml:"max_backups"` // 最大备份数量
	MaxAge     int    `yaml:"max_age"`     // 最大保存天数
}

// ServerConfig HTTP 服务参数。
type ServerConfig struct {
	HTTPAddr string `yaml:"http_addr"` // 监听地址，默认仅监听本机
	BaseURL  string `yaml:"base_url"`  // 反向代理/公网回调场景下的外部地址
}

// WebConfig Web 前端参数。
type WebConfig struct {
	Mode     string `yaml:"mode"`      // 前端加入方式：embed（默认，内嵌二进制）| proxy（反代外部服务）| dir（本地目录）| gz（打包文件）| off（仅 API）
	ProxyURL string `yaml:"proxy_url"` // mode=proxy 时的前端服务地址
	Dir      string `yaml:"dir"`       // mode=dir 时的静态资源目录
	Archive  string `yaml:"archive"`   // mode=gz 时的打包文件路径（.tar.gz）
}

// DatabaseConfig 数据库参数。
type DatabaseConfig struct {
	Driver string       `yaml:"driver"` // sqlite（默认）| mysql
	SQLite SQLiteConfig `yaml:"sqlite"`
	MySQL  MySQLConfig  `yaml:"mysql"`
}

// SQLiteConfig SQLite 参数。
type SQLiteConfig struct {
	File string `yaml:"file"` // 数据库文件路径
}

// MySQLConfig MySQL 参数。
type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

// FetchConfig 采集参数。
type FetchConfig struct {
	Interval    string `yaml:"interval"`    // 全局默认刷新间隔
	Concurrency int    `yaml:"concurrency"` // 并发采集上限
	RateLimit   string `yaml:"rate_limit"`  // 后台采集全局请求最小间隔（礼貌限速），如 "1s"；空或 "0" 表示不限速
	Timeout     string `yaml:"timeout"`     // 单次抓取超时
	UserAgent   string `yaml:"user_agent"`  // 采集请求的 User-Agent
	Proxy       string `yaml:"proxy"`       // 代理地址，留空则读环境变量
	ChromePath  string `yaml:"chrome_path"` // Chrome 可执行文件路径，留空自动探测（chromedp 抓取用）
	FullText    bool   `yaml:"full_text"`   // 是否对全部渠道抓取正文
}

// CredentialsConfig 凭据引用方式。
type CredentialsConfig struct {
	Store         string `yaml:"store"`          // env（默认，仅存变量名）| file（加密落盘）
	PassphraseEnv string `yaml:"passphrase_env"` // 加密口令所在环境变量名
}

// FilterConfig 过滤与去重参数。
type FilterConfig struct {
	Dedup            bool  `yaml:"dedup"`              // 是否启用去重
	CrossSourceDedup bool  `yaml:"cross_source_dedup"` // 跨渠道内容指纹去重
	Rules            []any `yaml:"rules"`              // 过滤规则（见「去重与过滤」）
}

// LLMConfig LLM 增强参数（可选）。
type LLMConfig struct {
	Enabled   bool   `yaml:"enabled"`   // 关闭时以下字段全部忽略
	BaseURL   string `yaml:"base_url"`  // OpenAI 兼容端点
	APIKey    string `yaml:"api_key"`   // API 密钥
	Model     string `yaml:"model"`     // 模型名
	Summarize bool   `yaml:"summarize"` // 是否生成摘要
	Classify  bool   `yaml:"classify"`  // 是否分类打标
}

// ExportConfig 导出参数。
type ExportConfig struct {
	MarkdownDir string `yaml:"markdown_dir"` // Markdown 导出目录
	JSONDir     string `yaml:"json_dir"`     // JSON 导出目录
	EPUBDir     string `yaml:"epub_dir"`     // EPUB 导出目录
	EPUB        bool   `yaml:"epub"`         // 是否启用 EPUB 导出
}

// Default 返回内置默认配置。
func Default() *Config {
	return &Config{
		Log: LogConfig{
			Dir:        "log",
			Level:      "info",
			MaxSize:    10,
			MaxBackups: 5,
			MaxAge:     30,
		},
		Server: ServerConfig{
			HTTPAddr: "127.0.0.1:28080",
			BaseURL:  "",
		},
		Web: WebConfig{
			Mode:     "embed",
			ProxyURL: "http://127.0.0.1:38080",
			Dir:      "./web",
			Archive:  "",
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			SQLite: SQLiteConfig{File: "./data.db"},
			MySQL: MySQLConfig{
				Host:     "127.0.0.1",
				Port:     3306,
				User:     "",
				Password: "",
				Database: "news_glean",
			},
		},
		Fetch: FetchConfig{
			Interval:    "30m",
			Concurrency: 4,
			RateLimit:   "1s",
			Timeout:     "20s",
			UserAgent:   "NewsGlean/0.1 (+https://github.com/ma6254/news-glean)",
			Proxy:       "",
			ChromePath:  "",
			FullText:    false,
		},
		Credentials: CredentialsConfig{
			Store:         "env",
			PassphraseEnv: "NEWSGLEAN_MASTER_KEY",
		},
		Filter: FilterConfig{
			Dedup:            true,
			CrossSourceDedup: true,
			Rules:            []any{},
		},
		LLM: LLMConfig{
			Enabled:   false,
			BaseURL:   "https://api.openai.com/v1",
			APIKey:    "",
			Model:     "",
			Summarize: false,
			Classify:  false,
		},
		Export: ExportConfig{
			MarkdownDir: "./export",
			JSONDir:     "./export-json",
			EPUBDir:     "./export-epub",
			EPUB:        false,
		},
	}
}

// Load 从指定路径读取 YAML 配置；path 为空时返回内置默认值。
// 文件中的字段覆盖默认值，未出现的字段保持默认值。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
