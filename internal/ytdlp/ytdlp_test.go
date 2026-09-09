package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
)

func TestSelectSubtitlePrefersManualForAny(t *testing.T) {
	md := Metadata{
		Subtitles: map[string][]SubtitleFormat{
			"sv": {{Ext: "vtt", URL: "manual"}},
		},
		AutomaticCaptions: map[string][]SubtitleFormat{
			"sv": {{Ext: "vtt", URL: "auto"}},
		},
	}
	selection, ok := SelectSubtitle(md, "sv", "any", false)
	if !ok {
		t.Fatal("SelectSubtitle() did not find subtitles")
	}
	if selection.Kind != SubtitleManual || selection.URL != "manual" {
		t.Fatalf("selection = %+v, want manual", selection)
	}
}

func TestSelectSubtitleTranslatePrefersEnglishManual(t *testing.T) {
	md := Metadata{
		Subtitles: map[string][]SubtitleFormat{
			"sv": {{Ext: "vtt", URL: "swedish"}},
			"en": {{Ext: "vtt", URL: "english-manual"}},
		},
		AutomaticCaptions: map[string][]SubtitleFormat{
			"en": {{Ext: "vtt", URL: "english-auto"}},
		},
	}
	selection, ok := SelectSubtitle(md, "sv", "any", true)
	if !ok {
		t.Fatal("SelectSubtitle() did not find subtitles")
	}
	if selection.Kind != SubtitleManual || selection.Lang != "en" || selection.URL != "english-manual" {
		t.Fatalf("selection = %+v, want English manual", selection)
	}
}

func TestSelectSubtitleAutoPrefersMetadataOriginalLanguage(t *testing.T) {
	md := Metadata{
		Language: "sv",
		AutomaticCaptions: map[string][]SubtitleFormat{
			"en":      {{Ext: "vtt", URL: "english"}},
			"sv":      {{Ext: "vtt", URL: "swedish-translated"}},
			"sv-orig": {{Ext: "vtt", URL: "swedish-original"}},
		},
	}
	selection, ok := SelectSubtitle(md, "auto", "auto", false)
	if !ok {
		t.Fatal("SelectSubtitle() did not find subtitles")
	}
	if selection.Kind != SubtitleAuto || selection.Lang != "sv-orig" || selection.URL != "swedish-original" {
		t.Fatalf("selection = %+v, want Swedish original auto captions", selection)
	}
}

func TestSelectSubtitleFallsBackToSRT(t *testing.T) {
	md := Metadata{
		Subtitles: map[string][]SubtitleFormat{
			"en": {
				{Ext: "json3", URL: "json"},
				{Ext: "srt", URL: "srt"},
			},
		},
	}
	selection, ok := SelectSubtitle(md, "en-US", "manual", false)
	if !ok {
		t.Fatal("SelectSubtitle() did not find subtitles")
	}
	if selection.Ext != "srt" || selection.URL != "srt" {
		t.Fatalf("selection = %+v, want srt", selection)
	}
}

func TestDownloadSubtitleRunsYTDLPSubtitleDownload(t *testing.T) {
	var gotArgs []string
	fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" {
			t.Fatalf("name = %q, want yt-dlp", name)
		}
		gotArgs = args
		outTemplate := argAfter(args, "-o")
		if outTemplate == "" {
			t.Fatalf("args missing -o: %v", args)
		}
		path := strings.Replace(outTemplate, "%(ext)s", "sv-orig.vtt", 1)
		if err := os.WriteFile(path, []byte("WEBVTT\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return runner.Result{}, nil
	})
	path, err := DownloadSubtitle(context.Background(), fr, "https://youtu.be/id", t.TempDir(), SubtitleSelection{
		Kind: SubtitleAuto,
		Lang: "sv-orig",
		Ext:  "vtt",
	}, config.CookieConfig{})
	if err != nil {
		t.Fatalf("DownloadSubtitle() error = %v", err)
	}
	got := strings.Join(gotArgs, " ")
	for _, want := range []string{"--write-auto-subs", "--sub-langs sv-orig", "--sub-format vtt", "https://youtu.be/id"} {
		if !strings.Contains(got, want) {
			t.Fatalf("args = %q, missing %q", got, want)
		}
	}
	if !strings.HasSuffix(path, "subtitle.sv-orig.vtt") {
		t.Fatalf("path = %q", path)
	}
}

func TestDownloadMediaKeepsSpacesInReportedPath(t *testing.T) {
	fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		return runner.Result{Stdout: []byte("/tmp/source file.webm\n")}, nil
	})
	path, err := DownloadMedia(context.Background(), fr, "https://youtu.be/id", t.TempDir(), config.CookieConfig{})
	if err != nil {
		t.Fatalf("DownloadMedia() error = %v", err)
	}
	if path != "/tmp/source file.webm" {
		t.Fatalf("path = %q", path)
	}
}

func TestFetchMetadataAddsIgnoreConfig(t *testing.T) {
	fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" {
			t.Fatalf("name = %q, want yt-dlp", name)
		}
		if !strings.Contains(strings.Join(args, " "), "--ignore-config") {
			t.Fatalf("args = %v", args)
		}
		md := Metadata{ID: "xmkSf5IS-zw", Title: "mock", Duration: 1}
		data, _ := json.Marshal(md)
		return runner.Result{Stdout: data}, nil
	})
	md, err := FetchMetadata(context.Background(), fr, "https://youtu.be/xmkSf5IS-zw", config.CookieConfig{})
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}
	if md.ID != "xmkSf5IS-zw" {
		t.Fatalf("ID = %q, want xmkSf5IS-zw", md.ID)
	}
}

func TestDownloadMediaExplainsUnavailableFormat(t *testing.T) {
	fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" {
			t.Fatalf("name = %q, want yt-dlp", name)
		}
		return runner.Result{}, errors.New("yt-dlp failed: ERROR: Requested format is not available. Use --list-formats for a list of available formats")
	})
	_, err := DownloadMedia(context.Background(), fr, "https://youtu.be/id", t.TempDir(), config.CookieConfig{})
	if err == nil {
		t.Fatal("DownloadMedia() error = nil")
	}
	got := err.Error()
	if !strings.Contains(got, "cuescribe --list-formats") || !strings.Contains(got, "brew upgrade yt-dlp") {
		t.Fatalf("error = %q", got)
	}
}

func TestDownloadMediaExplainsOldYTDLPVersion(t *testing.T) {
	call := 0
	fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" {
			t.Fatalf("name = %q, want yt-dlp", name)
		}
		call++
		switch call {
		case 1:
			return runner.Result{}, errors.New("yt-dlp failed: ERROR: Requested format is not available. Use --list-formats for a list of available formats")
		case 2:
			return runner.Result{Stdout: []byte("2026.02.04\n")}, nil
		default:
			t.Fatalf("unexpected yt-dlp call %d", call)
			return runner.Result{}, nil
		}
	})
	_, err := DownloadMedia(context.Background(), fr, "https://youtu.be/id", t.TempDir(), config.CookieConfig{})
	if err == nil {
		t.Fatal("DownloadMedia() error = nil")
	}
	got := err.Error()
	if !strings.Contains(got, "from 2026.02.04") {
		t.Fatalf("error = %q", got)
	}
	if !strings.Contains(got, "at least 2026.03.17") {
		t.Fatalf("error = %q", got)
	}
}

func TestParseThreePartVersion(t *testing.T) {
	v, ok := parseThreePartVersion("2026.03.17")
	if !ok {
		t.Fatal("parseThreePartVersion() = false")
	}
	if v != (threePartVersion{2026, 3, 17}) {
		t.Fatalf("v = %+v", v)
	}
	if !isYTDLPVersionOlder("2026.02.01", minimumYTDLPVersion) {
		t.Fatal("isYTDLPVersionOlder(2026.02.01) = false")
	}
	if isYTDLPVersionOlder("2026.03.18", minimumYTDLPVersion) {
		t.Fatal("isYTDLPVersionOlder(2026.03.18) = true")
	}
}

func TestListFormatsRunsYTDLPListFormats(t *testing.T) {
	fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" {
			t.Fatalf("name = %q, want yt-dlp", name)
		}
		got := strings.Join(args, " ")
		if !strings.Contains(got, "--list-formats") || !strings.Contains(got, "https://youtu.be/id") {
			t.Fatalf("args = %v", args)
		}
		return runner.Result{Stdout: []byte("format list\n")}, nil
	})
	out, err := ListFormats(context.Background(), fr, "https://youtu.be/id", config.CookieConfig{})
	if err != nil {
		t.Fatalf("ListFormats() error = %v", err)
	}
	if out != "format list\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestDownloadAudioOverridesDetectedMusicWithVideoMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, format, channel, artist string
	}{
		{"channel display name", "mp3", "Café 音楽", "Café 音楽"},
		{"uploader fallback", "m4a", "", "Uploader display"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Conflicting music fields reproduce yt-dlp's default precedence.
			fields := map[string]string{
				"title": "Música: \"A/B\" — 100%\n夜", "track": "Detected track",
				"artist": "Detected musician", "album_artist": "Detected band", "album": "Detected album",
				"uploader": "Uploader display", "uploader_id": "@handle",
				"original_url": "https://youtu.be/original", "webpage_url": "https://youtube.com/watch?v=canonical",
			}
			if tc.channel != "" {
				fields["channel"] = tc.channel
			}
			var writtenTags map[string]string
			fr := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
				if name != "yt-dlp" {
					t.Fatalf("unexpected tool: %s", name)
				}
				writtenTags = simulateYTDLPTags(t, args, fields)
				return runner.Result{Stdout: []byte("/stage/source." + tc.format + "\n")}, nil
			})
			if _, err := DownloadAudio(context.Background(), fr, "https://youtu.be/original", t.TempDir(), tc.format, config.CookieConfig{}); err != nil {
				t.Fatal(err)
			}
			for tag, want := range map[string]string{
				"title": "Música: \"A/B\" — 100%\n夜", "artist": tc.artist,
				"album": "Música: \"A/B\" — 100%\n夜", "album_artist": tc.artist,
				"comment": "https://youtu.be/original", "purl": "https://youtu.be/original",
			} {
				if got := writtenTags[tag]; got != want {
					t.Errorf("embedded %s = %q, want %q", tag, got, want)
				}
			}
		})
	}
}

// Simulate the external tool's documented template alternatives, regex capture,
// and meta_* precedence. Evaluating arguments rather than matching literal
// flags catches truncated Unicode/newlines and incorrect fallback or music fields.
// Real container writing is checked separately with generated MP3/M4A smoke files.
func simulateYTDLPTags(t *testing.T, args []string, fields map[string]string) map[string]string {
	t.Helper()
	tags := map[string]string{
		"title": fields["track"], "artist": fields["artist"],
		"album": fields["album"], "album_artist": fields["album_artist"],
		"comment": fields["webpage_url"], "purl": fields["webpage_url"],
	}
	template := regexp.MustCompile(`%\(([^)]+)\)s`)
	for i, arg := range args {
		if arg != "--parse-metadata" {
			continue
		}
		from, pattern, ok := strings.Cut(args[i+1], ":")
		if !ok {
			t.Fatalf("invalid metadata expression: %q", args[i+1])
		}
		value := template.ReplaceAllStringFunc(from, func(match string) string {
			keys, fallback, _ := strings.Cut(template.FindStringSubmatch(match)[1], "|")
			for _, key := range strings.Split(keys, ",") {
				if value, ok := fields[key]; ok {
					return value
				}
			}
			return fallback
		})
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}
		matches := re.FindStringSubmatch(value)
		if matches == nil {
			continue
		}
		for i, name := range re.SubexpNames() {
			if key, ok := strings.CutPrefix(name, "meta_"); ok {
				tags[key] = matches[i]
			}
		}
	}
	return tags
}

type runnerFunc func(ctx context.Context, name string, args ...string) (runner.Result, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (runner.Result, error) {
	return f(ctx, name, args...)
}

func argAfter(args []string, value string) string {
	for i, arg := range args {
		if arg == value && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
