package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/transcript"
	"github.com/devosurf/cuescribe/internal/version"
)

type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
)

type Options struct {
	Format         Format
	NoTimestamps   bool
	TimestampLinks bool
	OutputPath     string
	Mkdir          bool
	Force          bool
}

func Write(doc transcript.Document, opts Options, stdout io.Writer) (string, error) {
	data, ext, err := Render(doc, opts)
	if err != nil {
		return "", err
	}
	if opts.OutputPath == "-" {
		_, err := stdout.Write(data)
		return "", err
	}
	path, err := resolvePath(opts.OutputPath, doc.Title, ext)
	if err != nil {
		return "", err
	}
	f, path, err := createOutputFile(path, ext, opts)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return path, nil
}

// SaveFile saves a completed media download using the same destination policy
// as transcripts, without buffering the media in memory.
func SaveFile(sourcePath, title string, opts Options) (string, error) {
	if opts.OutputPath == "-" {
		return "", fmt.Errorf("Error: audio downloads cannot be written to stdout.\nFix: choose an output file or directory with -o")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("downloaded audio is not a regular file: %s", sourcePath)
	}
	ext := filepath.Ext(sourcePath)
	if ext == "" {
		return "", fmt.Errorf("downloaded audio has no file extension: %s", sourcePath)
	}
	path, err := resolvePath(opts.OutputPath, title, ext)
	if err != nil {
		return "", err
	}
	if destExt := filepath.Ext(path); destExt == "" {
		path += ext
	} else if !strings.EqualFold(destExt, ext) {
		return "", fmt.Errorf("Error: output extension %q does not match downloaded audio %q.\nFix: use a matching extension, an output directory, or --audio-format mp3|m4a to select the encoding", destExt, ext)
	}
	if destInfo, err := os.Stat(path); err == nil && os.SameFile(info, destInfo) {
		return "", fmt.Errorf("audio source and output are the same file: %s", path)
	}
	dest, path, err := createOutputFile(path, ext, opts)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(dest, source)
	closeErr := dest.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func createOutputFile(path, ext string, opts Options) (*os.File, string, error) {
	if opts.Mkdir {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, path, err
		}
	}
	f, path, err := openOutputFile(path, ext, opts.Force)
	if errors.Is(err, os.ErrExist) {
		return nil, path, fmt.Errorf("Error: output file already exists: %s\nFix: pass --force or choose a different -o path", path)
	}
	return f, path, err
}

func openOutputFile(path, ext string, force bool) (*os.File, string, error) {
	flag := os.O_WRONLY | os.O_CREATE
	if force {
		flag |= os.O_TRUNC
		f, err := os.OpenFile(path, flag, 0o644)
		return f, path, err
	}

	collisionPath := path
	for attempts := 0; attempts < 2; attempts++ {
		f, err := os.OpenFile(collisionPath, flag|os.O_EXCL, 0o644)
		if err == nil {
			return f, collisionPath, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, collisionPath, err
		}
		if ext != ".md" {
			return nil, collisionPath, err
		}
		if attempts == 0 {
			collisionPath = timestampedPath(path)
			continue
		}
		return nil, collisionPath, err
	}
	return nil, collisionPath, fmt.Errorf("unable to allocate unique output path for %s", path)
}

func timestampedPath(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	dir := filepath.Dir(path)
	stamped := fmt.Sprintf("%s_%s%s", base, time.Now().UTC().Format("20060102T150405"), ext)
	if dir == "." {
		return stamped
	}
	return filepath.Join(dir, stamped)
}

func Render(doc transcript.Document, opts Options) ([]byte, string, error) {
	switch opts.Format {
	case "", FormatMarkdown:
		return renderMarkdown(doc, opts), ".md", nil
	case FormatJSON:
		data, err := renderJSON(doc)
		return data, ".json", err
	default:
		return nil, "", fmt.Errorf("unsupported format %q", opts.Format)
	}
}

func renderMarkdown(doc transcript.Document, opts Options) []byte {
	var b bytes.Buffer
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = "Transcript"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	writeMeta(&b, "Source", doc.Source)
	writeMeta(&b, "Uploader", doc.Uploader)
	if doc.Duration > 0 {
		writeMeta(&b, "Duration", transcript.FormatTimestamp(doc.Duration))
	}
	writeMeta(&b, "Language", doc.Language)
	writeMeta(&b, "Detected language", doc.DetectedLanguage)
	writeMeta(&b, "Mode", string(doc.Mode))
	writeMeta(&b, "Translated", yesNo(doc.Translated))
	writeMeta(&b, "Summary model", doc.SummaryModel)
	writeMeta(&b, "Generated by", "Cuescribe "+version.Version)
	b.WriteByte('\n')
	if summary := strings.TrimSpace(doc.Summary); summary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(summary)
		b.WriteString("\n\n")
	}
	if len(doc.Chapters) > 0 {
		b.WriteString("## Chapters\n\n")
		for _, chapter := range doc.Chapters {
			fmt.Fprintf(&b, "[%s] %s\n", transcript.FormatTimestamp(chapter.Start), chapter.Title)
		}
		b.WriteByte('\n')
	}
	b.WriteString("## Transcript\n\n")
	if opts.NoTimestamps {
		text := transcript.PlainText(doc.Segments)
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n")
		}
		return b.Bytes()
	}
	for _, segment := range doc.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		stamp := transcript.FormatTimestamp(segment.Start)
		if opts.TimestampLinks && doc.Source != "" {
			stamp = fmt.Sprintf("[%s](%s)", stamp, timestampURL(doc.Source, segment.Start))
		} else {
			stamp = "[" + stamp + "]"
		}
		if segment.Speaker != "" {
			fmt.Fprintf(&b, "%s **%s:** %s\n", stamp, segment.Speaker, text)
		} else {
			fmt.Fprintf(&b, "%s %s\n", stamp, text)
		}
	}
	return b.Bytes()
}

func renderJSON(doc transcript.Document) ([]byte, error) {
	type jsonChapter struct {
		StartMS int64  `json:"start_ms"`
		Title   string `json:"title"`
	}
	type jsonSegment struct {
		StartMS int64   `json:"start_ms"`
		EndMS   int64   `json:"end_ms"`
		Speaker *string `json:"speaker"`
		Text    string  `json:"text"`
	}
	payload := struct {
		SchemaVersion    int             `json:"schema_version"`
		Title            string          `json:"title"`
		Source           string          `json:"source"`
		Uploader         string          `json:"uploader,omitempty"`
		DurationMS       int64           `json:"duration_ms,omitempty"`
		Language         string          `json:"language"`
		DetectedLanguage string          `json:"detected_language,omitempty"`
		Mode             transcript.Mode `json:"mode"`
		Translated       bool            `json:"translated"`
		Summary          string          `json:"summary,omitempty"`
		SummaryModel     string          `json:"summary_model,omitempty"`
		Chapters         []jsonChapter   `json:"chapters"`
		Segments         []jsonSegment   `json:"segments"`
	}{
		SchemaVersion:    transcript.SchemaVersion,
		Title:            doc.Title,
		Source:           doc.Source,
		Uploader:         doc.Uploader,
		DurationMS:       int64(doc.Duration / time.Millisecond),
		Language:         doc.Language,
		DetectedLanguage: doc.DetectedLanguage,
		Mode:             doc.Mode,
		Translated:       doc.Translated,
		Summary:          doc.Summary,
		SummaryModel:     doc.SummaryModel,
	}
	for _, chapter := range doc.Chapters {
		payload.Chapters = append(payload.Chapters, jsonChapter{StartMS: int64(chapter.Start / time.Millisecond), Title: chapter.Title})
	}
	for _, segment := range doc.Segments {
		var speaker *string
		if segment.Speaker != "" {
			s := segment.Speaker
			speaker = &s
		}
		payload.Segments = append(payload.Segments, jsonSegment{
			StartMS: int64(segment.Start / time.Millisecond),
			EndMS:   int64(segment.End / time.Millisecond),
			Speaker: speaker,
			Text:    segment.Text,
		})
	}
	return json.MarshalIndent(payload, "", "  ")
}

func writeMeta(b *bytes.Buffer, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, value)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func timestampURL(source string, d time.Duration) string {
	sep := "?"
	if strings.Contains(source, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%st=%d", source, sep, int64(d/time.Second))
}

func resolvePath(outputPath, title, ext string) (string, error) {
	if outputPath == "" {
		return sanitizeFilename(title) + ext, nil
	}
	if strings.HasSuffix(outputPath, string(filepath.Separator)) {
		return filepath.Join(outputPath, sanitizeFilename(title)+ext), nil
	}
	info, err := os.Stat(outputPath)
	if err == nil && info.IsDir() {
		return filepath.Join(outputPath, sanitizeFilename(title)+ext), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return outputPath, nil
}

var unsafeFilename = regexp.MustCompile(`[^\w.\- ]+`)
var whitespace = regexp.MustCompile(`\s+`)

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "transcript"
	}
	name = unsafeFilename.ReplaceAllString(name, "_")
	name = whitespace.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	name = strings.Trim(name, "_")
	if name == "" {
		return "transcript"
	}
	if len(name) > 160 {
		name = strings.TrimSpace(name[:160])
	}
	return name
}
