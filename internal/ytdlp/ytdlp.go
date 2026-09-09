package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	Language          string                      `json:"language"`
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

const minimumYTDLPVersion = "2026.03.17"

const MinimumYTDLPVersion = minimumYTDLPVersion

func FetchMetadata(ctx context.Context, r runner.CommandRunner, input string, cookies config.CookieConfig) (Metadata, error) {
	args := []string{"--ignore-config", "--dump-json", "--skip-download", "--no-playlist", "--no-warnings"}
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

func ListFormats(ctx context.Context, r runner.CommandRunner, input string, cookies config.CookieConfig) (string, error) {
	args := []string{"--ignore-config", "--list-formats", "--no-playlist", "--no-warnings"}
	args = append(args, cookies.YTDLPCookieArgs()...)
	args = append(args, input)
	result, err := r.Run(ctx, "yt-dlp", args...)
	if err != nil {
		return "", err
	}
	return string(result.Stdout), nil
}

func DownloadMedia(ctx context.Context, r runner.CommandRunner, input, dir string, cookies config.CookieConfig) (string, error) {
	return downloadMedia(ctx, r, input, dir, "", cookies)
}

// DownloadAudio extracts listening-quality audio without speech normalization.
func DownloadAudio(ctx context.Context, r runner.CommandRunner, input, dir, format string, cookies config.CookieConfig) (string, error) {
	switch format {
	case "best", "mp3", "m4a":
	default:
		return "", fmt.Errorf("unsupported audio format %q: use best, mp3, or m4a", format)
	}
	return downloadMedia(ctx, r, input, dir, format, cookies)
}

func downloadMedia(ctx context.Context, r runner.CommandRunner, input, dir, format string, cookies config.CookieConfig) (string, error) {
	outTemplate := filepath.Join(dir, "source.%(ext)s")
	args := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-warnings",
		"-f", "bestaudio/best",
		"-o", outTemplate,
		"--print", "after_move:filepath",
	}
	if format != "" {
		args = append(args, "--extract-audio", "--audio-format", format, "--audio-quality", "0")
	}
	args = append(args, cookies.YTDLPCookieArgs()...)
	args = append(args, input)
	result, err := r.Run(ctx, "yt-dlp", args...)
	if err != nil {
		if isRequestedFormatUnavailable(err) {
			if current, ok := CurrentYTDLPVersion(ctx, r); ok && IsYTDLPVersionOlder(current, MinimumYTDLPVersion) {
				return "", fmt.Errorf("Error: YouTube download failed because this yt-dlp version is too old for this video or extractor path.\nFix: upgrade yt-dlp from %s to at least %s (`brew upgrade yt-dlp`), then run cuescribe --list-formats %s to confirm formats.\n\n%s", current, minimumYTDLPVersion, strconv.Quote(input), err)
			}
			return "", fmt.Errorf("Error: yt-dlp could not download a compatible audio format.\nFix: upgrade yt-dlp with brew upgrade yt-dlp, run cuescribe --list-formats %s to inspect available formats, and if cuescribe doctor reports YouTube cookie access errors, run cuescribe setup cookies --disable or reconfigure cookies.\n\n%s", strconv.Quote(input), err)
		}
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

func DownloadSubtitle(ctx context.Context, r runner.CommandRunner, input, dir string, selection SubtitleSelection, cookies config.CookieConfig) (string, error) {
	outTemplate := filepath.Join(dir, "subtitle.%(ext)s")
	writeFlag := "--write-subs"
	if selection.Kind == SubtitleAuto {
		writeFlag = "--write-auto-subs"
	}
	ext := selection.Ext
	if ext == "" {
		ext = "vtt"
	}
	args := []string{
		"--ignore-config",
		"--skip-download",
		"--no-playlist",
		"--no-warnings",
		writeFlag,
		"--sub-langs", selection.Lang,
		"--sub-format", ext,
		"-o", outTemplate,
	}
	args = append(args, cookies.YTDLPCookieArgs()...)
	args = append(args, input)
	if _, err := r.Run(ctx, "yt-dlp", args...); err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "subtitle.*"))
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	for _, match := range matches {
		if strings.EqualFold(filepath.Ext(match), "."+ext) {
			return match, nil
		}
	}
	if len(matches) > 0 {
		return matches[0], nil
	}
	return "", fmt.Errorf("yt-dlp did not write subtitle file for language %s", selection.Lang)
}

func CookiesForInput(input string, cookies config.CookieConfig) config.CookieConfig {
	if !isYouTubeURL(input) {
		cookies.Enabled = false
	}
	return cookies
}

func SelectSubtitle(md Metadata, lang, subs string, translate bool) (SubtitleSelection, bool) {
	if translate {
		if sel, ok := selectFromMap(md.Subtitles, SubtitleManual, languagePreferences(md, "en", SubtitleManual)); ok {
			return sel, true
		}
		return selectFromMap(md.AutomaticCaptions, SubtitleAuto, languagePreferences(md, "en", SubtitleAuto))
	}
	switch subs {
	case "manual":
		return selectFromMap(md.Subtitles, SubtitleManual, languagePreferences(md, lang, SubtitleManual))
	case "auto":
		return selectFromMap(md.AutomaticCaptions, SubtitleAuto, languagePreferences(md, lang, SubtitleAuto))
	default:
		if sel, ok := selectFromMap(md.Subtitles, SubtitleManual, languagePreferences(md, lang, SubtitleManual)); ok {
			return sel, true
		}
		return selectFromMap(md.AutomaticCaptions, SubtitleAuto, languagePreferences(md, lang, SubtitleAuto))
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

func selectFromMap(options map[string][]SubtitleFormat, kind SubtitleKind, preferences []string) (SubtitleSelection, bool) {
	if len(options) == 0 {
		return SubtitleSelection{}, false
	}
	for _, candidate := range langCandidates(options, preferences) {
		if format, ok := preferredFormat(options[candidate]); ok {
			return SubtitleSelection{Kind: kind, Lang: candidate, Ext: strings.ToLower(format.Ext), URL: format.URL}, true
		}
	}
	return SubtitleSelection{}, false
}

func languagePreferences(md Metadata, lang string, kind SubtitleKind) []string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang != "" && lang != "auto" {
		if kind == SubtitleAuto {
			return uniqueStrings([]string{lang + "-orig", lang})
		}
		return []string{lang}
	}
	metadataLang := strings.ToLower(strings.TrimSpace(md.Language))
	preferences := []string{}
	if metadataLang != "" {
		if kind == SubtitleAuto {
			preferences = append(preferences, metadataLang+"-orig")
		}
		preferences = append(preferences, metadataLang)
	}
	if kind == SubtitleAuto {
		preferences = append(preferences, "-orig")
	}
	preferences = append(preferences, "en")
	return uniqueStrings(preferences)
}

func langCandidates(options map[string][]SubtitleFormat, preferences []string) []string {
	seen := map[string]bool{}
	candidates := []string{}
	for _, preference := range preferences {
		preference = strings.ToLower(strings.TrimSpace(preference))
		if preference == "" || preference == "auto" {
			continue
		}
		var exact []string
		var prefix []string
		for key := range options {
			lower := strings.ToLower(key)
			if lower == preference {
				exact = append(exact, key)
			} else if strings.HasPrefix(preference, "-") && strings.HasSuffix(lower, preference) {
				prefix = append(prefix, key)
			} else if strings.HasPrefix(lower, preference+"-") || strings.HasPrefix(preference, lower+"-") {
				prefix = append(prefix, key)
			}
		}
		sort.Strings(exact)
		sort.Strings(prefix)
		for _, candidate := range append(exact, prefix...) {
			if !seen[candidate] {
				seen[candidate] = true
				candidates = append(candidates, candidate)
			}
		}
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return append(candidates, keys...)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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

func isRequestedFormatUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Requested format is not available")
}

func CurrentYTDLPVersion(ctx context.Context, r runner.CommandRunner) (string, bool) {
	return ytDLPVersion(ctx, r)
}

func IsYTDLPVersionOlder(value, minimum string) bool {
	return isYTDLPVersionOlder(value, minimum)
}

func ytDLPVersion(ctx context.Context, r runner.CommandRunner) (string, bool) {
	result, err := r.Run(ctx, "yt-dlp", "--version")
	if err != nil {
		return "", false
	}
	version := strings.TrimSpace(string(result.Stdout))
	if version == "" {
		return "", false
	}
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func isYTDLPVersionOlder(value, minimum string) bool {
	parsed, ok := parseThreePartVersion(value)
	if !ok {
		return false
	}
	min, ok := parseThreePartVersion(minimum)
	if !ok {
		return false
	}
	if parsed.Major != min.Major {
		return parsed.Major < min.Major
	}
	if parsed.Minor != min.Minor {
		return parsed.Minor < min.Minor
	}
	return parsed.Patch < min.Patch
}

type threePartVersion struct {
	Major int
	Minor int
	Patch int
}

func parseThreePartVersion(raw string) (threePartVersion, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) < 3 {
		return threePartVersion{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return threePartVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return threePartVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return threePartVersion{}, false
	}
	return threePartVersion{Major: major, Minor: minor, Patch: patch}, true
}

func isYouTubeURL(input string) bool {
	u, err := url.Parse(input)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "youtube.com" ||
		strings.HasSuffix(host, ".youtube.com") ||
		host == "youtu.be" ||
		strings.HasSuffix(host, ".youtu.be")
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
