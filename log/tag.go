package log

import "strings"

// MaxTagDepth 层叠 tag 链的最大深度，防止滥用撑爆日志行。
const MaxTagDepth = 8

// Tags 是一条层叠 tag 链，例如 ["analyze", "chapter:3", "step:parse"]。
// 链是不可变的：追加会复制底层切片，父 Logger 不受影响（并发安全的基础）。
type Tags []string

// Append 返回追加一个 tag 后的新链。超过 MaxTagDepth 时返回原链和 false。
func (t Tags) Append(tag string) (Tags, bool) {
	if len(t) >= MaxTagDepth {
		return t, false
	}
	next := make(Tags, len(t)+1)
	copy(next, t)
	next[len(t)] = tag
	return next, true
}

// Clone 返回链的独立副本。
func (t Tags) Clone() Tags {
	if t == nil {
		return nil
	}
	cp := make(Tags, len(t))
	copy(cp, t)
	return cp
}

// HasPrefix 报告 tag 链是否以 prefix 开头（前缀匹配，用于规则过滤）。
// 空 prefix 匹配一切。
func (t Tags) HasPrefix(prefix Tags) bool {
	if len(prefix) > len(t) {
		return false
	}
	for i, p := range prefix {
		if t[i] != p {
			return false
		}
	}
	return true
}

// String 返回点分形式："analyze.chapter:3"。
func (t Tags) String() string {
	return strings.Join(t, ".")
}

// Bracket 返回方括号形式："[analyze][chapter:3]"，用于终端/文件展示。
func (t Tags) Bracket() string {
	var sb strings.Builder
	for _, tag := range t {
		sb.WriteByte('[')
		sb.WriteString(tag)
		sb.WriteByte(']')
	}
	return sb.String()
}
