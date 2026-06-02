package ytdlp

import (
	"context"
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

type runnerFunc func(ctx context.Context, name string, args ...string) (runner.Result, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (runner.Result, error) {
	return f(ctx, name, args...)
}
