package hardware

import (
	"runtime"
	"testing"
)

func TestRecommendedWhisperModel(t *testing.T) {
	cases := []struct {
		ramGB int
		want  string
	}{
		{0, "small"},
		{8, "small"},
		{15, "small"},
		{16, "medium"},
		{24, "medium"},
		{32, "large-v3-turbo"},
		{64, "large-v3-turbo"},
	}
	for _, c := range cases {
		if got := RecommendedWhisperModel(Info{RAMGB: c.ramGB}); got != c.want {
			t.Fatalf("RecommendedWhisperModel(%d GB) = %q, want %q", c.ramGB, got, c.want)
		}
	}
}

func TestInfoString(t *testing.T) {
	cases := []struct {
		info Info
		want string
	}{
		{Info{Chip: "Apple M2", RAMGB: 16}, "Apple M2, 16 GB RAM"},
		{Info{RAMGB: 8}, "8 GB RAM"},
		{Info{Chip: "Apple M1"}, "Apple M1"},
		{Info{}, ""},
	}
	for _, c := range cases {
		if got := c.info.String(); got != c.want {
			t.Fatalf("Info%+v.String() = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestDetectOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("detection is darwin-only")
	}
	info := Detect()
	if info.Chip == "" || info.RAMGB <= 0 {
		t.Fatalf("Detect() = %+v, expected chip and RAM on macOS", info)
	}
}
