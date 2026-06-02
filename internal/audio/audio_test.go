package audio

import (
	"testing"
	"time"
)

func TestParseWhisperTimestampAcceptsCommaMillis(t *testing.T) {
	got := parseWhisperTimestamp("00:01:02,250")
	want := time.Minute + 2*time.Second + 250*time.Millisecond
	if got != want {
		t.Fatalf("parseWhisperTimestamp() = %v, want %v", got, want)
	}
}
