package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
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

type runnerFunc func(ctx context.Context, name string, args ...string) (runner.Result, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (runner.Result, error) {
	return f(ctx, name, args...)
}
