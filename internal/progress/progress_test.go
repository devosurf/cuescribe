package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	tests := map[int64]string{
		0:          "0 B",
		999:        "999 B",
		1000:       "1.0 KB",
		1500000:    "1.5 MB",
		2000000000: "2.0 GB",
	}
	for input, want := range tests {
		if got := HumanBytes(input); got != want {
			t.Fatalf("HumanBytes(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestBarWritesReadableMessagesForNonTerminal(t *testing.T) {
	var out bytes.Buffer
	bar := NewBar(&out, "Downloading model", 1000)
	bar.Start(0)
	bar.Set(1000)
	bar.Finish("downloaded 1.0 KB")
	got := out.String()
	if !strings.Contains(got, "Downloading model") || !strings.Contains(got, "downloaded 1.0 KB") {
		t.Fatalf("progress output = %q", got)
	}
}

func TestSpinnerWritesMessagesForNonTerminal(t *testing.T) {
	var out bytes.Buffer
	spinner := StartSpinner(&out, "Transcribing audio")
	spinner.Done("Transcribed audio")
	got := out.String()
	if !strings.Contains(got, "Transcribing audio") || !strings.Contains(got, "Transcribed audio") {
		t.Fatalf("spinner output = %q", got)
	}
}
