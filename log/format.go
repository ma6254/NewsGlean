package log

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FormatTextLine 格式化一行文本日志（无颜色，末尾带换行）。
// timeLayout 控制时间格式：终端用短格式，文件用完整格式。
func FormatTextLine(rec Record, timeLayout string) string {
	var sb strings.Builder
	sb.Grow(64 + len(rec.Message) + len(rec.Tags)*8 + len(rec.Fields)*8)
	sb.WriteString(rec.Time.Format(timeLayout))
	sb.WriteByte(' ')
	sb.WriteString(rec.Level.String())
	if len(rec.Tags) > 0 {
		sb.WriteByte(' ')
		sb.WriteString(rec.Tags.Bracket())
	}
	sb.WriteByte(' ')
	sb.WriteString(rec.Message)
	for _, f := range rec.Fields {
		sb.WriteByte(' ')
		appendFieldText(&sb, f)
	}
	sb.WriteByte('\n')
	return sb.String()
}

// appendFieldText 输出 "key=value"。
func appendFieldText(sb *strings.Builder, f Field) {
	sb.WriteString(f.Key)
	sb.WriteByte('=')
	sb.WriteString(formatValue(f.Value))
}

// formatValue 格式化字段值：含空白/引号/等号的字符串加引号，保持行内可读。
func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return quoteIfNeeded(t)
	case error:
		return quoteIfNeeded(t.Error())
	case fmt.Stringer:
		return quoteIfNeeded(t.String())
	default:
		return fmt.Sprintf("%v", t)
	}
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\r\"=") {
		return strconv.Quote(s)
	}
	return s
}

// jsonRecord JSON 序列化中间形态（tags 保持有序数组，与输出端无关）。
type jsonRecord struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Channel string         `json:"channel"`
	Tags    []string       `json:"tags,omitempty"`
	Message string         `json:"msg"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// FormatJSONLine 格式化一行 JSON 日志（末尾带换行）。
func FormatJSONLine(rec Record) []byte {
	jr := jsonRecord{
		Time:    rec.Time.Format(time.RFC3339Nano),
		Level:   rec.Level.String(),
		Channel: rec.Channel().String(),
		Message: rec.Message,
	}
	if len(rec.Tags) > 0 {
		jr.Tags = rec.Tags.Clone()
	}
	if len(rec.Fields) > 0 {
		jr.Fields = make(map[string]any, len(rec.Fields))
		for _, f := range rec.Fields {
			jr.Fields[f.Key] = f.Value
		}
	}
	b, err := json.Marshal(jr)
	if err != nil {
		// 字段含不可 JSON 化的值（如 channel/函数）：降级为仅含错误说明的 JSON。
		b, _ = json.Marshal(map[string]any{
			"time":    jr.Time,
			"level":   jr.Level,
			"channel": jr.Channel,
			"msg":     rec.Message,
			"error":   err.Error(),
		})
	}
	return append(b, '\n')
}
