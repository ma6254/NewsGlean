package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ma6254/news-glean/internal/source"
)

// ---- envelope 解析（mock 样本：成功 / 业务失败 / 乱码）----

func TestParseEnvelopeSuccess(t *testing.T) {
	raw := `{"ok":true,"schema_version":"1","data":{"page":1,"items":[{"id":"BV1","bvid":"BV1","title":"T"}]}}`
	env, err := parseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("parseEnvelope: %v", err)
	}
	if !env.OK {
		t.Error("ok = false, want true")
	}
	if len(env.Data) == 0 {
		t.Error("data empty")
	}
}

func TestParseEnvelopeError(t *testing.T) {
	raw := `{"ok":false,"schema_version":"1","error":{"code":"not_authenticated","message":"未登录"}}`
	env, err := parseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("parseEnvelope: %v", err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil || env.Error.Code != "not_authenticated" {
		t.Errorf("error = %+v", env.Error)
	}
}

func TestParseEnvelopeGarbled(t *testing.T) {
	if _, err := parseEnvelope([]byte("����ħ��ʦ")); err == nil {
		t.Error("乱码样本应解析失败")
	}
	if _, err := parseEnvelope(nil); err == nil {
		t.Error("空输出应解析失败")
	}
}

// ---- Item 映射 ----

func TestMapHistoryItem(t *testing.T) {
	item := mapHistoryItem(historyItem{ID: "BV1", BVID: "BV1", Title: "标题", Author: "作者", ViewedAt: "2026-01-02T03:04:05"})
	if item.GUID != "BV1" || item.URL != "https://www.bilibili.com/video/BV1" {
		t.Errorf("guid/url = %q/%q", item.GUID, item.URL)
	}
	if item.Author != "作者" || item.Title != "标题" {
		t.Errorf("author/title = %q/%q", item.Author, item.Title)
	}
	if item.InferredTime {
		t.Error("有 viewed_at，不应推断时间")
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	if !item.PublishedAt.Equal(want) {
		t.Errorf("published = %v, want %v", item.PublishedAt, want)
	}
}

func TestMapHistoryItemNoTime(t *testing.T) {
	item := mapHistoryItem(historyItem{ID: "BV2", Title: "T", Author: "A"})
	if item.GUID != "BV2" {
		t.Errorf("guid = %q, want BV2（回退到 id）", item.GUID)
	}
	if !item.InferredTime {
		t.Error("无 viewed_at，应推断时间")
	}
}

func TestMapFavoriteItem(t *testing.T) {
	it := favoriteItem{ID: "BV1", BVID: "BV1", Title: "T", DurationSeconds: 11, Duration: "00:11"}
	it.Upper.Name = "UP"
	item := mapFavoriteItem(it)
	if item.GUID != "BV1" || item.URL != "https://www.bilibili.com/video/BV1" {
		t.Errorf("guid/url = %q/%q", item.GUID, item.URL)
	}
	if item.Author != "UP" {
		t.Errorf("author = %q", item.Author)
	}
	if !item.InferredTime {
		t.Error("favorites 无 fav_time，应推断时间")
	}
	if item.Extra["duration_seconds"] != "11" {
		t.Errorf("extra = %v", item.Extra)
	}
}

func TestParseViewedAt(t *testing.T) {
	if _, ok := parseViewedAt(""); ok {
		t.Error("空串应失败")
	}
	if _, ok := parseViewedAt("garbage"); ok {
		t.Error("乱码应失败")
	}
	if ts, ok := parseViewedAt("2026-01-02T03:04:05Z"); !ok || ts.Year() != 2026 {
		t.Errorf("RFC3339 解析失败: %v %v", ts, ok)
	}
	ts, ok := parseViewedAt("2026-01-02T03:04:05")
	if !ok {
		t.Fatal("naive 本地时间解析失败")
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	if !ts.Equal(want) {
		t.Errorf("naive = %v, want %v", ts, want)
	}
}

// ---- 错误码映射 ----

func TestMapCliError(t *testing.T) {
	if e := mapCliError(&cliErr{Code: "not_authenticated", Message: "m"}); e == nil || e.Error() == "" {
		t.Error("not_authenticated 应映射")
	}
	if e := mapCliError(&cliErr{Code: "rate_limited", Message: "m"}); e == nil || e.Error() == "" {
		t.Error("rate_limited 应映射")
	}
	if _, ok := mapCliError(&cliErr{Code: "other", Message: "m"}).(*cliErr); !ok {
		t.Error("未知错误码应原样透传为 cliErr")
	}
}

// ---- Validate ----

func TestValidate(t *testing.T) {
	if err := (&connector{mode: ModeHistory}).Validate(context.Background()); err != nil {
		t.Errorf("history validate: %v", err)
	}
	if err := (&connector{mode: ModeFavorites}).Validate(context.Background()); err == nil {
		t.Error("favorites 缺 fav_id 应失败")
	}
	if err := (&connector{mode: ModeFavorites, favID: "123"}).Validate(context.Background()); err != nil {
		t.Errorf("favorites with fav_id: %v", err)
	}
	if err := (&connector{mode: "bogus"}).Validate(context.Background()); err == nil {
		t.Error("非法 mode 应失败")
	}
}

// ---- 二进制探测 ----

func TestResolveBiliOverride(t *testing.T) {
	const override = `C:\does\not\exist\bili.exe`
	got := resolveBili(override)
	if got.argv0 != override {
		t.Errorf("argv0 = %q, want %q（覆盖路径即使不存在也原样返回）", got.argv0, override)
	}
}

func TestResolveBiliLookPath(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "bili.exe")
	if err := os.WriteFile(fake, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := resolveBili("")
	if got.argv0 != fake {
		t.Errorf("argv0 = %q, want %q", got.argv0, fake)
	}
	if len(got.prefix) != 0 {
		t.Errorf("prefix = %v, want empty", got.prefix)
	}
}

// ---- 构造与注册 ----

func TestNew(t *testing.T) {
	conn, err := New(`{"mode":"history"}`, source.CreateOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if conn.Type() != "bilibili" {
		t.Errorf("type = %q", conn.Type())
	}
	if _, ok := conn.(source.Connector); !ok {
		t.Error("New 返回的不是 source.Connector")
	}
}

func TestRegistered(t *testing.T) {
	meta, _, _, err := source.Get("bilibili")
	if err != nil {
		t.Fatalf("Get bilibili: %v", err)
	}
	if meta.Name != "bilibili" || !meta.Pull || meta.Push {
		t.Errorf("meta = %+v", meta)
	}
}

func TestImplementsEnvChecker(t *testing.T) {
	conn, err := New(`{"mode":"history"}`, source.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := conn.(source.EnvChecker); !ok {
		t.Error("connector 应实现 source.EnvChecker")
	}
	if _, ok := conn.(source.Prober); !ok {
		t.Error("connector 应实现 source.Prober")
	}
	if _, ok := conn.(source.Enricher); !ok {
		t.Error("connector 应实现 source.Enricher")
	}
}

func TestEnrichDisabled(t *testing.T) {
	c := &connector{mode: ModeHistory, fetchDetail: false}
	item := source.Item{GUID: "BV1"}
	got, err := c.Enrich(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "" || got.Extra != nil {
		t.Errorf("fetch_detail 关闭时应原样返回: %+v", got)
	}
}

func TestEnrichNoGUID(t *testing.T) {
	c := &connector{mode: ModeHistory, fetchDetail: true}
	item := source.Item{GUID: ""}
	got, err := c.Enrich(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "" {
		t.Errorf("无 GUID 应原样返回: %+v", got)
	}
}

func TestParseVideoDetail(t *testing.T) {
	raw := `{"video":{"bvid":"BV1","title":"标题","description":"简介内容","owner":{"name":"UP主"},"stats":{"view":100,"danmaku":2,"like":3,"coin":4,"favorite":5,"share":6}},"subtitle":{"text":""}}`
	d, err := parseVideoDetail([]byte(raw))
	if err != nil {
		t.Fatalf("parseVideoDetail: %v", err)
	}
	if d.BVID != "BV1" || d.Title != "标题" || d.Description != "简介内容" {
		t.Errorf("detail = %+v", d)
	}
	if d.Owner != "UP主" {
		t.Errorf("owner = %q", d.Owner)
	}
	if d.Stats.View != 100 || d.Stats.Like != 3 {
		t.Errorf("stats = %+v", d.Stats)
	}
}

func TestRetryStopsOnSuccess(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, time.Millisecond, func(error) bool { return true }, func() error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestRetryStopsOnNonRetryable(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, time.Millisecond, func(error) bool { return false }, func() error {
		calls++
		return errors.New("fatal")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetryExhausts(t *testing.T) {
	calls := 0
	err := retry(context.Background(), 3, time.Millisecond, func(error) bool { return true }, func() error {
		calls++
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRateLimiterWait(t *testing.T) {
	l := newRateLimiter(20 * time.Millisecond)
	start := time.Now()
	_ = l.Wait(context.Background())
	_ = l.Wait(context.Background())
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 20ms", elapsed)
	}
}

func TestCliRetryable(t *testing.T) {
	if !cliRetryable(&cliErr{Code: "rate_limited"}) {
		t.Error("rate_limited 应可重试")
	}
	if !cliRetryable(&cliErr{Code: "network_error"}) {
		t.Error("network_error 应可重试")
	}
	if cliRetryable(&cliErr{Code: "not_authenticated"}) {
		t.Error("not_authenticated 不应重试")
	}
}

func TestHttpRetryable(t *testing.T) {
	if !httpRetryable(&httpStatusError{status: 412}) {
		t.Error("412 应可重试")
	}
	if !httpRetryable(&httpStatusError{status: 502}) {
		t.Error("5xx 应可重试")
	}
	if httpRetryable(&httpStatusError{status: 404}) {
		t.Error("404 不应重试")
	}
}

func TestWriteCredentialFileMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")

	// 模拟已存在含 buvid4/dedeuserid 的凭证文件
	pre := credentialFile{Sessdata: "old", BiliJct: "oldjct", Buvid4: "keep4", Dedeuserid: "d1"}
	preData, _ := json.Marshal(&pre)
	if err := os.WriteFile(path, preData, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeCredentialFile(path, "S1", "J1", "B3"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got credentialFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Sessdata != "S1" || got.BiliJct != "J1" || got.Buvid3 != "B3" {
		t.Errorf("写入后字段 = %+v", got)
	}
	if got.Buvid4 != "keep4" || got.Dedeuserid != "d1" {
		t.Errorf("已有字段应保留: %+v", got)
	}
	if got.SavedAt == 0 {
		t.Error("saved_at 应被设置")
	}
}
