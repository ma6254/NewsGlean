package log

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

// TagRule 一条 tag 前缀规则：前缀匹配的记录改用 Rule.Level 判定（覆盖全局级别）。
type TagRule struct {
	Prefix Tags
	Level  Level
}

// Core 日志核心：全局级别、access 通道独立级别、tag 规则、Sink 注册与按通道路由分发。
// 所有方法并发安全。
type Core struct {
	mu          sync.RWMutex
	level       Level
	accessLevel Level
	accessSet   bool
	rules       []TagRule
	ruleHits    []atomic.Bool // 与 rules 平行：记录每条规则是否被命中（未命中警告用）
	access      []Sink
	errSinks    []Sink
	onError     func(err error)
}

// NewCore 创建日志核心。默认级别 info；sink 内部错误默认打 stderr 一行。
func NewCore() *Core {
	return &Core{
		level: LevelInfo,
		onError: func(err error) {
			fmt.Fprintf(os.Stderr, "log: %v\n", err)
		},
	}
}

// SetLevel 设置全局级别。
func (c *Core) SetLevel(l Level) {
	c.mu.Lock()
	c.level = l
	c.mu.Unlock()
}

// Level 返回当前全局级别。
func (c *Core) Level() Level {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.level
}

// SetAccessLevel 单独设置 access 通道的过滤级别（覆盖全局级别）。
// error 通道始终全收，不受此影响。
func (c *Core) SetAccessLevel(l Level) {
	c.mu.Lock()
	c.accessLevel, c.accessSet = l, true
	c.mu.Unlock()
}

// SetTagRules 整体替换 tag 规则集（并重置命中标记）。
func (c *Core) SetTagRules(rules []TagRule) {
	c.mu.Lock()
	c.rules = rules
	c.ruleHits = make([]atomic.Bool, len(rules))
	c.mu.Unlock()
}

// UnusedRules 返回从未被任何记录命中的规则描述（用于拼写错误预警）。
func (c *Core) UnusedRules() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []string
	for i, r := range c.rules {
		if !c.ruleHits[i].Load() {
			out = append(out, r.Prefix.String())
		}
	}
	return out
}

// AddSink 注册一个 Sink 到指定通道。Open 失败时上报错误并忽略该 sink（降级不崩溃）。
func (c *Core) AddSink(ch Channel, s Sink) {
	if err := s.Open(); err != nil {
		c.report(fmt.Errorf("open %T: %w", s, err))
		return
	}
	c.mu.Lock()
	if ch == ChannelError {
		c.errSinks = append(c.errSinks, s)
	} else {
		c.access = append(c.access, s)
	}
	c.mu.Unlock()
}

// Close 关闭所有 Sink（错误逐个上报，不中断），并报告未命中的 tag 规则。
func (c *Core) Close() {
	for _, r := range c.UnusedRules() {
		c.report(fmt.Errorf("tag rule %q never matched any log (typo?)", r))
	}
	c.mu.Lock()
	sinks := make([]Sink, 0, len(c.access)+len(c.errSinks))
	sinks = append(sinks, c.access...)
	sinks = append(sinks, c.errSinks...)
	c.access, c.errSinks = nil, nil
	c.mu.Unlock()
	for _, s := range sinks {
		if err := s.Close(); err != nil {
			c.report(fmt.Errorf("close %T: %w", s, err))
		}
	}
}

// enabled 快速级别判定：tag 规则覆盖全局级别，取最长前缀匹配的规则。
// 供 Logger 在构造 Record 之前短路（热路径避免格式化与分配）。
func (c *Core) enabled(level Level, tags Tags) bool {
	c.mu.RLock()
	threshold := c.effectiveLevelLocked(level, tags)
	c.mu.RUnlock()
	return level.Enabled(threshold)
}

// effectiveLevelLocked 返回 tags 命中的生效级别（需持有读锁），
// 并标记命中的规则（供未命中警告）。
// 优先级：最长前缀 tag 规则 > access 通道独立级别 > 全局级别。
func (c *Core) effectiveLevelLocked(level Level, tags Tags) Level {
	var bestIdx = -1
	var best *TagRule
	for i := range c.rules {
		r := &c.rules[i]
		if tags.HasPrefix(r.Prefix) && (best == nil || len(r.Prefix) > len(best.Prefix)) {
			best = r
			bestIdx = i
		}
	}
	if best != nil {
		c.ruleHits[bestIdx].Store(true)
		return best.Level
	}
	if c.accessSet && level < LevelError {
		return c.accessLevel
	}
	return c.level
}

// Log 处理一条记录：级别判定（含 tag 规则覆盖）→ 按通道分发到各 Sink。
func (c *Core) Log(rec Record) {
	c.mu.RLock()
	if !rec.Level.Enabled(c.effectiveLevelLocked(rec.Level, rec.Tags)) {
		c.mu.RUnlock()
		return
	}
	var sinks []Sink
	if rec.Channel() == ChannelError {
		sinks = c.errSinks
	} else {
		sinks = c.access
	}
	c.mu.RUnlock()
	for _, s := range sinks {
		s.Write(rec)
	}
}

func (c *Core) report(err error) {
	c.mu.RLock()
	onError := c.onError
	c.mu.RUnlock()
	if onError != nil {
		onError(err)
	}
}
