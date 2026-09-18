package cmd

import (
	"strings"
	"testing"

	"github.com/ma6254/news-glean/internal/build"
)

func TestBanner(t *testing.T) {
	b := banner()
	t.Logf("\n%s", b)

	if !strings.Contains(b, "=") {
		t.Error("banner should contain frame (===)")
	}
	if !strings.Contains(b, "v"+build.BuildVersion) {
		t.Errorf("banner should contain version line, got:\n%s", b)
	}
	if !strings.Contains(b, "_   _") || !strings.Contains(b, `| \ | |`) {
		t.Errorf("banner should contain figlet NewsGlean glyphs, got:\n%s", b)
	}
}
