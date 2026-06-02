package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
	"github.com/devosurf/cuescribe/internal/transcript"
)

type Metadata struct {
	ID                string                      `json:"id"`
	Title             string                      `json:"title"`
	Uploader          string                      `json:"uploader"`
	Duration          float64                     `json:"duration"`
	WebpageURL        string                      `json:"webpage_url"`
	OriginalURL       string                      `json:"original_url"`
	LiveStatus        string                      `json:"live_status"`
	IsLive            bool                        `json:"is_live"`
	WasLive           bool                        `json:"was_live"`
	Subtitles         map[string][]SubtitleFormat `json:"subtitles"`
	AutomaticCaptions map[string][]SubtitleFormat `json:"automatic_captions"`
	Chapters          []Chapter                   `json:"chapters"`
}

type SubtitleFormat struct {
	Ext  string `json:"ext"`
	URL  string `json:"url"`
	Name string `json:"name"`
}

type Chapter struct {
	Title     string  `json:"title"`
	StartTime float64 `json:"start_time"`
}

type SubtitleKind string

const (
	SubtitleManual SubtitleKind = "manual"
	SubtitleAuto   SubtitleKind = "auto"
)

type SubtitleSelection struct {
	Kind SubtitleKind
	Lang string
	Ext  string
	URL  string
}

func FetchMetadata(ctx context.Context, r runner.CommandRunner, input string, cookies config.CookieConfig) (Metadata, error) {
	args := []string{"--dump-json", "--skip-download", "--no-playlist", "--no-warnings"}
	args = append(args, cookies.YTDLPCookieArgs()...)
	args = append(args, input)
	result, err := r.Run(ctx, "yt-dlp", args...)
	if err != nil {
		return Metadata{}, err
	}
	var md Metadata
	if err := json.Unmarshal(result.Stdout, &md); err != nil {
		return Metadata{}, fmt.Errorf("failed to parse yt-dlp metadata: %w", err)
	}
	if md.WebpageURL == "" {
		md.WebpageURL = input
	}
	if md.OriginalURL == "" {
		md.OriginalURL = input
	}
	return md, nil
}

func DownloadMedia(ctx context.Context, r runner.CommandRunner, input, dir string, cookies config.CookieConfig) (string, error) {
	outTemplate := filepath.Join(dir, "source.%(ext)s")
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"-f", "bestaudio/best",
		"-o", outTemplate,
		"--print", "after_move:filepath",
	}
	args = append(args, cookies.YTDLPCookieArgs()...)
	args = append(args, input)
	result, err := r.Run(ctx, "yt-dlp", args...)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("yt-dlp did not report a downloaded file path")
	}
	return lines[len(lines)-1], nil
}

func SelectSubtitle(md Metadata, lang, subs string, translate bool) (SubtitleSelection, bool) {
	if translate {
		if sel, ok := selectFromMap(md.Subtitles, SubtitleManual, "en"); ok {
			return sel, true
		}
		return selectFromMap(md.AutomaticCaptions, SubtitleAuto, "en")
	}
	switch subs {
	case "manual":
		return selectFromMap(md.Subtitles, SubtitleManual, lang)
	case "auto":
		return selectFromMap(md.AutomaticCaptions, SubtitleAuto, lang)
	default:
		if sel, ok := selectFromMap(md.Subtitles, SubtitleManual, lang); ok {
			return sel, true
		}
		return selectFromMap(md.AutomaticCaptions, SubtitleAuto, lang)
	}
}

func ToChapters(chapters []Chapter) []transcript.Chapter {
	out := make([]transcript.Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		if strings.TrimSpace(chapter.Title) == "" {
			continue
		}
		out = append(out, transcript.Chapter{
			Title: strings.TrimSpace(chapter.Title),
			Start: secondsDuration(chapter.StartTime),
		})
	}
	return out
}

func Duration(md Metadata) time.Duration {
	return secondsDuration(md.Duration)
}

func SourceURL(md Metadata) string {
	if md.WebpageURL != "" {
		return md.WebpageURL
	}
	if md.OriginalURL != "" {
		return md.OriginalURL
	}
	return ""
}

func selectFromMap(options map[string][]SubtitleFormat, kind SubtitleKind, lang string) (SubtitleSelection, bool) {
	if len(options) == 0 {
		return SubtitleSelection{}, false
	}
	for _, candidate := range langCandidates(options, lang) {
		if format, ok := preferredFormat(options[candidate]); ok {
			return SubtitleSelection{Kind: kind, Lang: candidate, Ext: strings.ToLower(format.Ext), URL: format.URL}, true
		}
	}
	return SubtitleSelection{}, false
}

func langCandidates(options map[string][]SubtitleFormat, lang string) []string {
	if lang != "" && lang != "auto" {
		var exact []string
		var prefix []string
		normalized := strings.ToLower(lang)
		for key := range options {
			lower := strings.ToLower(key)
			if lower == normalized {
				exact = append(exact, key)
			} else if strings.HasPrefix(lower, normalized+"-") || strings.HasPrefix(normalized, lower+"-") {
				prefix = append(prefix, key)
			}
		}
		sort.Strings(exact)
		sort.Strings(prefix)
		return append(exact, prefix...)
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if strings.HasPrefix(keys[i], "en") != strings.HasPrefix(keys[j], "en") {
			return strings.HasPrefix(keys[i], "en")
		}
		return keys[i] < keys[j]
	})
	return keys
}

func preferredFormat(formats []SubtitleFormat) (SubtitleFormat, bool) {
	priority := map[string]int{"vtt": 0, "srt": 1}
	var best SubtitleFormat
	bestScore := 99
	for _, format := range formats {
		if strings.TrimSpace(format.URL) == "" {
			continue
		}
		ext := strings.ToLower(format.Ext)
		score, ok := priority[ext]
		if !ok {
			continue
		}
		if score < bestScore {
			best = format
			bestScore = score
		}
	}
	return best, bestScore != 99
}

func secondsDuration(v float64) time.Duration {
	if v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

func ParseDurationSeconds(value any) time.Duration {
	switch v := value.(type) {
	case float64:
		return secondsDuration(v)
	case string:
		parsed, _ := strconv.ParseFloat(v, 64)
		return secondsDuration(parsed)
	default:
		return 0
	}
}
