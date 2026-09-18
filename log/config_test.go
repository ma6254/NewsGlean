package log

import (
	"strings"
	"testing"
)

func TestParseConfigJSON(t *testing.T) {
	data := []byte(`{
		"access": "./log/access.log",
		"error": "./log/error.log",
		"loglevel": "warning"
	}`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Access != "./log/access.log" || cfg.Error != "./log/error.log" {
		t.Errorf("paths = %q, %q", cfg.Access, cfg.Error)
	}
	if cfg.LogLevel != "warning" {
		t.Errorf("loglevel = %q", cfg.LogLevel)
	}
	// 未出现的键保留默认值
	if cfg.Color != "auto" || cfg.MaxSizeMB != 10 || cfg.MaxBackups != 5 {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestParseConfigYAML(t *testing.T) {
	data := []byte("access: ./log/access.log\nerror: ./log/error.log\nloglevel: debug\ncolor: never\nws: \":8080\"\n")
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Access != "./log/access.log" || cfg.LogLevel != "debug" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.Color != "never" || cfg.WS != ":8080" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default loglevel = %q, want info", cfg.LogLevel)
	}
	if cfg.Access != "" || cfg.Error != "" {
		t.Errorf("default paths = %q, %q, want empty", cfg.Access, cfg.Error)
	}
}

func TestParseConfigInvalid(t *testing.T) {
	cases := []string{
		`{"loglevel": "bogus"}`,
		`{"color": "sometimes"}`,
		`{"format": "xml"}`,
		`{"max_size_mb": 0}`,
		`{"max_backups": 0}`,
	}
	for _, c := range cases {
		if _, err := ParseConfig([]byte(c)); err == nil {
			t.Errorf("ParseConfig(%s) should fail", c)
		}
	}
}

func TestParseTagRules(t *testing.T) {
	rules, err := ParseTagRules(map[string]string{
		"analyze":           "debug",
		"analyze.chapter:3": "warn",
		"openai":            "none",
	})
	if err != nil {
		t.Fatalf("ParseTagRules: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("rules = %+v, want 3", rules)
	}
	find := func(prefix ...string) *TagRule {
		for i := range rules {
			if len(rules[i].Prefix) == len(prefix) && rules[i].Prefix.HasPrefix(prefix) {
				return &rules[i]
			}
		}
		return nil
	}
	short := find("analyze")
	if short == nil || short.Level != LevelDebug {
		t.Errorf("analyze rule = %+v, want debug", short)
	}
	long := find("analyze", "chapter:3")
	if long == nil || long.Level != LevelWarn {
		t.Errorf("analyze.chapter:3 rule = %+v, want warn", long)
	}
	if off := find("openai"); off == nil || off.Level != LevelNone {
		t.Errorf("openai rule = %+v, want none", off)
	}
}

func TestParseTagRulesInvalid(t *testing.T) {
	if _, err := ParseTagRules(map[string]string{"analyze": "bogus"}); err == nil {
		t.Error("invalid level should fail")
	}
	if _, err := ParseTagRules(map[string]string{"analyze..x": "info"}); err == nil {
		t.Error("empty segment should fail")
	}
}

func TestParseTagRulesNoneSemantics(t *testing.T) {
	// "none" 是规范内的静默级别。
	rules, err := ParseTagRules(map[string]string{"openai": "none"})
	if err != nil {
		t.Fatalf("ParseTagRules: %v", err)
	}
	if len(rules) != 1 || rules[0].Level != LevelNone {
		t.Errorf("rules = %+v", rules)
	}
}

func TestConfigRoundTripJSON(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LogLevel = "warning"
	b, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	got, err := ParseConfig(b)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got.LogLevel != "warning" || !strings.Contains(string(b), `"loglevel"`) {
		t.Errorf("round trip failed: %s", b)
	}
}
