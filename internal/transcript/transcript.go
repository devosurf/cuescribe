package transcript

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const SchemaVersion = 2

type Mode string

const (
	ModeSubtitlesManual Mode = "subtitles-manual"
	ModeSubtitlesAuto   Mode = "subtitles-auto"
	ModeAudio           Mode = "audio"
)

type Segment struct {
	Start   time.Duration
	End     time.Duration
	Text    string
	Speaker string
}

type Chapter struct {
	Start time.Duration
	Title string
}

type Document struct {
	SchemaVersion    int
	Title            string
	Source           string
	Uploader         string
	Duration         time.Duration
	Language         string
	DetectedLanguage string
	Mode             Mode
	Translated       bool
	Summary          string
	SummaryModel     string
	Chapters         []Chapter
	Segments         []Segment
}

type Diarizer interface {
	Diarize(ctx context.Context, segments []Segment) ([]Segment, error)
}

func FormatTimestamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Round(time.Millisecond) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func FormatTimestampMillis(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalMillis := int64(d.Round(time.Millisecond) / time.Millisecond)
	h := totalMillis / 3_600_000
	m := (totalMillis % 3_600_000) / 60_000
	s := (totalMillis % 60_000) / 1000
	ms := totalMillis % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func PlainText(segments []Segment) string {
	var b strings.Builder
	for _, segment := range segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(text)
	}
	return strings.TrimSpace(b.String())
}
