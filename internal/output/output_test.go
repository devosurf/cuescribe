package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devosurf/cuescribe/internal/transcript"
)

func TestRenderMarkdownWithTimestamps(t *testing.T) {
	doc := transcript.Document{
		Title:            "Video",
		Source:           "https://youtu.be/id",
		Language:         "auto",
		DetectedLanguage: "sv",
		Mode:             transcript.ModeSubtitlesManual,
		Segments: []transcript.Segment{
			{Start: 12 * time.Second, End: 14 * time.Second, Speaker: "Alice", Text: "Hej"},
		},
	}
	data, ext, err := Render(doc, Options{Format: FormatMarkdown})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if ext != ".md" {
		t.Fatalf("ext = %q, want .md", ext)
	}
	got := string(data)
	for _, want := range []string{"# Video", "Mode: subtitles-manual", "[00:00:12] **Alice:** Hej"} {
		if !strings.Contains(got, want) {
			t.Fatalf("markdown missing %q:\n%s", want, got)
		}
	}
}

func TestRenderJSONKeepsSegmentTiming(t *testing.T) {
	doc := transcript.Document{
		Title:    "Video",
		Source:   "source",
		Language: "sv",
		Mode:     transcript.ModeAudio,
		Segments: []transcript.Segment{
			{Start: 1500 * time.Millisecond, End: 2500 * time.Millisecond, Text: "Hej"},
		},
	}
	data, _, err := Render(doc, Options{Format: FormatJSON})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := string(data)
	for _, want := range []string{`"schema_version": 1`, `"start_ms": 1500`, `"speaker": null`} {
		if !strings.Contains(got, want) {
			t.Fatalf("json missing %q:\n%s", want, got)
		}
	}
}

func TestWritePreventsCollisionWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.md")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Write(transcript.Document{Title: "Video"}, Options{Format: FormatMarkdown, OutputPath: path}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Write() error = nil, want collision error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error = %v, want --force guidance", err)
	}
}

func TestWriteUsesSanitizedTitleForDirectoryOutput(t *testing.T) {
	dir := t.TempDir()
	doc := transcript.Document{Title: "Bad/Name:*?", Mode: transcript.ModeAudio}
	written, err := Write(doc, Options{Format: FormatMarkdown, OutputPath: dir}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if filepath.Base(written) != "Bad_Name_.md" {
		t.Fatalf("written = %s", written)
	}
}
