package audio

import (
	"testing"
	"time"
)

func TestParseWhisperTimestampAcceptsCommaMillis(t *testing.T) {
	got, err := parseWhisperTimestamp("00:01:02,250")
	if err != nil {
		t.Fatalf("parseWhisperTimestamp() error = %v", err)
	}
	want := time.Minute + 2*time.Second + 250*time.Millisecond
	if got != want {
		t.Fatalf("parseWhisperTimestamp() = %v, want %v", got, want)
	}
}

func TestParseWhisperTimestampRejectsMalformedInput(t *testing.T) {
	for _, input := range []string{"", "N/A", "00:01", "aa:bb:cc", "00:01:02:03"} {
		if _, err := parseWhisperTimestamp(input); err == nil {
			t.Fatalf("parseWhisperTimestamp(%q) = nil error, want failure", input)
		}
	}
}
