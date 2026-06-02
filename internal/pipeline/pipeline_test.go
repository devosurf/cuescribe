package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
	"github.com/devosurf/cuescribe/internal/transcript"
)

type fakeRunner func(ctx context.Context, name string, args ...string) (runner.Result, error)

func (f fakeRunner) Run(ctx context.Context, name string, args ...string) (runner.Result, error) {
	return f(ctx, name, args...)
}

func TestRunPrefersManualSubtitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n<v Alice>Hello</v>\n")
	}))
	defer server.Close()
	fr := fakeRunner(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" || !contains(args, "--dump-json") {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return runner.Result{Stdout: []byte(fmt.Sprintf(`{
			"title":"Video",
			"uploader":"Uploader",
			"duration":12,
			"webpage_url":"https://youtu.be/id",
			"subtitles":{"en":[{"ext":"vtt","url":%q}]},
			"automatic_captions":{"en":[{"ext":"vtt","url":"http://example.invalid/auto.vtt"}]}
		}`, server.URL))}, nil
	})
	paths := config.PathsForHome(t.TempDir())
	doc, err := New(fr).Run(context.Background(), Options{
		Input:        "https://youtu.be/id",
		Config:       config.Default(paths),
		Paths:        paths,
		PlatformOS:   "darwin",
		PlatformArch: "arm64",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if doc.Mode != transcript.ModeSubtitlesManual {
		t.Fatalf("Mode = %s", doc.Mode)
	}
	if doc.DetectedLanguage != "en" || doc.Segments[0].Speaker != "Alice" || doc.Segments[0].Text != "Hello" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestRunLocalFileUsesAudioPipeline(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "lecture.mp4")
	modelPath := filepath.Join(tmp, "model.bin")
	if err := os.WriteFile(input, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	fr := fakeRunner(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		switch name {
		case "ffmpeg":
			return runner.Result{}, nil
		case "whisper-cli":
			outBase := argAfter(args, "--output-file")
			if outBase == "" {
				t.Fatalf("whisper args missing --output-file: %v", args)
			}
			data := `{"result":{"language":"sv"},"transcription":[{"timestamps":{"from":"00:00:01.000","to":"00:00:02.000"},"text":" Hej "}]}`
			if err := os.WriteFile(outBase+".json", []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			return runner.Result{}, nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return runner.Result{}, nil
		}
	})
	paths := config.PathsForHome(tmp)
	cfg := config.Default(paths)
	cfg.Model.Path = modelPath
	doc, err := New(fr).Run(context.Background(), Options{
		Input:        input,
		Lang:         "sv",
		Config:       cfg,
		Paths:        paths,
		PlatformOS:   "darwin",
		PlatformArch: "arm64",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if doc.Mode != transcript.ModeAudio || doc.Title != "lecture" || doc.DetectedLanguage != "sv" {
		t.Fatalf("doc = %+v", doc)
	}
	if len(doc.Segments) != 1 || doc.Segments[0].Text != "Hej" {
		t.Fatalf("segments = %+v", doc.Segments)
	}
}

func TestRunRejectsLocalDirectory(t *testing.T) {
	paths := config.PathsForHome(t.TempDir())
	_, err := New(fakeRunner(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		t.Fatalf("unexpected command: %s %v", name, args)
		return runner.Result{}, nil
	})).Run(context.Background(), Options{
		Input:        t.TempDir(),
		Config:       config.Default(paths),
		Paths:        paths,
		PlatformOS:   "darwin",
		PlatformArch: "arm64",
	})
	if err == nil || !strings.Contains(err.Error(), "directories are not supported") {
		t.Fatalf("Run() error = %v", err)
	}
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func argAfter(args []string, value string) string {
	for i, arg := range args {
		if arg == value && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestYouTubeCookiesOnlyAttachForYouTube(t *testing.T) {
	cookies := config.CookieConfig{Enabled: true, Browser: "safari"}
	if len(youtubeCookies("https://vimeo.com/1", cookies).YTDLPCookieArgs()) != 0 {
		t.Fatal("non-YouTube URL kept cookie args")
	}
	if got := strings.Join(youtubeCookies("https://youtube.com/watch?v=1", cookies).YTDLPCookieArgs(), " "); !strings.Contains(got, "safari") {
		t.Fatalf("YouTube cookie args = %q", got)
	}
}
