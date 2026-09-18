package filter

import "testing"

func TestContentHashDeterministic(t *testing.T) {
	a := ContentHash("Hello World", "Some content")
	b := ContentHash("Hello World", "Some content")
	if a != b {
		t.Fatalf("hash not deterministic: %q != %q", a, b)
	}
	if len(a) != 32 {
		t.Errorf("hash length = %d, want 32 (hex of 16 bytes)", len(a))
	}
}

func TestContentHashNormalizes(t *testing.T) {
	// 空白与大小写差异应得到相同指纹（识别改标题/加空白的重复稿件）
	a := ContentHash("Hello   World", "Some CONTENT")
	b := ContentHash("hello world", "some content")
	if a != b {
		t.Fatalf("normalized hashes differ: %q != %q", a, b)
	}
}

func TestContentHashDiffers(t *testing.T) {
	a := ContentHash("Title A", "content")
	b := ContentHash("Title B", "content")
	if a == b {
		t.Fatal("different titles should yield different hashes")
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://Example.com/Post#frag", "https://example.com/Post"},
		{"https://example.com/path/", "https://example.com/path"},
		{"https://example.com/", "https://example.com/"},
		{"  https://example.com/a  ", "https://example.com/a"},
	}
	for _, tc := range cases {
		if got := NormalizeURL(tc.in); got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
