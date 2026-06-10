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
	for _, want := range []string{`"schema_version": 2`, `"start_ms": 1500`, `"speaker": null`} {
		if !strings.Contains(got, want) {
			t.Fatalf("json missing %q:\n%s", want, got)
		}
	}
}

func TestWriteUsesTimestampOnMarkdownCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.md")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Write(transcript.Document{Title: "Video"}, Options{Format: FormatMarkdown, OutputPath: path}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if out == path {
		t.Fatalf("expected timestamped path, got %q", out)
	}
	if filepath.Dir(out) != filepath.Dir(path) {
		t.Fatalf("timestamped path moved directories: got %q", out)
	}
	if !strings.Contains(filepath.Base(out), "out_") {
		t.Fatalf("expected timestamp in filename: %q", filepath.Base(out))
	}
}

func TestWriteCollisionWithoutForceJSONFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Write(transcript.Document{Title: "Video"}, Options{Format: FormatJSON, OutputPath: path}, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("Write() error = nil, want collision error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error = %v, want --force guidance", err)
	}
}

func TestWriteDefaultsToTitleFile(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	doc := transcript.Document{Title: "Upper Management Meeting.", Mode: transcript.ModeSubtitlesManual}
	written, err := Write(doc, Options{Format: FormatMarkdown}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != "Upper_Management_Meeting.md" {
		t.Fatalf("written = %q", written)
	}
	if _, err := os.Stat(filepath.Join(dir, written)); err != nil {
		t.Fatalf("default output file missing: %v", err)
	}
}

func TestWriteDashOutputsToStdout(t *testing.T) {
	var stdout bytes.Buffer
	written, err := Write(transcript.Document{Title: "Video", Mode: transcript.ModeAudio}, Options{Format: FormatMarkdown, OutputPath: "-"}, &stdout)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != "" {
		t.Fatalf("written = %q, want empty stdout marker", written)
	}
	if !strings.Contains(stdout.String(), "# Video") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestWriteUsesSanitizedTitleForDirectoryOutput(t *testing.T) {
	dir := t.TempDir()
	doc := transcript.Document{Title: "Bad/Name:*?", Mode: transcript.ModeAudio}
	written, err := Write(doc, Options{Format: FormatMarkdown, OutputPath: dir}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if filepath.Base(written) != "Bad_Name.md" {
		t.Fatalf("written = %s", written)
	}
}

func TestSanitizeFilenameSpacesAndSymbols(t *testing.T) {
	path, err := resolvePath("", transcript.Document{Title: "  My  Cool Video: The  Final *Cut* "}, ".md")
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}
	if path != "My_Cool_Video__The_Final__Cut.md" {
		t.Fatalf("resolvePath() = %q, want underscores", path)
	}
}

func TestRenderMarkdownIncludesSummary(t *testing.T) {
	doc := transcript.Document{
		Title:        "Video",
		Summary:      "A short summary.\n\n- point one\n- point two",
		SummaryModel: "qwen3-4b",
		Segments:     []transcript.Segment{{Text: "Hello"}},
	}
	data, _, err := Render(doc, Options{Format: FormatMarkdown})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "## Summary\n\nA short summary.") {
		t.Fatalf("markdown missing summary section: %q", got)
	}
	if !strings.Contains(got, "Summary model: qwen3-4b") {
		t.Fatalf("markdown missing summary model meta: %q", got)
	}
	if strings.Index(got, "## Summary") > strings.Index(got, "## Transcript") {
		t.Fatalf("summary should precede transcript: %q", got)
	}
}

func TestRenderJSONIncludesSummary(t *testing.T) {
	doc := transcript.Document{
		Title:        "Video",
		Summary:      "A short summary.",
		SummaryModel: "qwen3-4b",
	}
	data, _, err := Render(doc, Options{Format: FormatJSON})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"summary": "A short summary."`) || !strings.Contains(got, `"summary_model": "qwen3-4b"`) {
		t.Fatalf("json missing summary fields: %q", got)
	}
}

func TestRenderJSONOmitsEmptySummary(t *testing.T) {
	data, _, err := Render(transcript.Document{Title: "Video"}, Options{Format: FormatJSON})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(string(data), `"summary"`) {
		t.Fatalf("json should omit empty summary: %q", string(data))
	}
}
