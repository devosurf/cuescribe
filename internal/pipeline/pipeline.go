package pipeline

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/audio"
	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
	"github.com/devosurf/cuescribe/internal/subtitles"
	"github.com/devosurf/cuescribe/internal/transcript"
	"github.com/devosurf/cuescribe/internal/ytdlp"
)

type Options struct {
	Input        string
	Source       string
	Subs         string
	Lang         string
	Translate    bool
	Config       config.Config
	Paths        config.Paths
	PlatformOS   string
	PlatformArch string
}

type Pipeline struct {
	Runner runner.CommandRunner
	Client *http.Client
}

func New(r runner.CommandRunner) Pipeline {
	return Pipeline{
		Runner: r,
		Client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (p Pipeline) Run(ctx context.Context, opts Options) (transcript.Document, error) {
	if opts.Input == "" {
		return transcript.Document{}, fmt.Errorf("input is required")
	}
	platformOS := normalizeDefault(opts.PlatformOS, runtime.GOOS)
	platformArch := normalizeDefault(opts.PlatformArch, runtime.GOARCH)
	if platformOS != "darwin" || platformArch != "arm64" {
		return transcript.Document{}, fmt.Errorf("Error: unsupported platform %s/%s.\nFix: Cuescribe v1 supports macOS Apple Silicon only", platformOS, platformArch)
	}
	source := normalizeDefault(opts.Source, "auto")
	subsMode := normalizeDefault(opts.Subs, "any")
	lang := normalizeDefault(opts.Lang, "auto")
	if !isURL(opts.Input) {
		if source == "subs" {
			return transcript.Document{}, fmt.Errorf("Error: local files do not support --source subs.\nFix: use --source audio or omit --source")
		}
		return p.fromAudio(ctx, opts, opts.Input, "", "", nil)
	}
	md, err := ytdlp.FetchMetadata(ctx, p.Runner, opts.Input, youtubeCookies(opts.Input, opts.Config.Cookies))
	if err != nil {
		return transcript.Document{}, err
	}
	if md.IsLive || md.LiveStatus == "is_live" || md.LiveStatus == "is_upcoming" {
		return transcript.Document{}, fmt.Errorf("Error: active livestreams are not supported.\nFix: run Cuescribe after the stream has ended")
	}
	if source == "auto" || source == "subs" {
		if selection, ok := ytdlp.SelectSubtitle(md, lang, subsMode, opts.Translate); ok {
			return p.fromSubtitles(ctx, opts, md, selection)
		}
		if source == "subs" {
			return transcript.Document{}, fmt.Errorf("Error: no compatible subtitles found.\nFix: use --source auto or --source audio")
		}
	}
	return p.fromDownloadedAudio(ctx, opts, md)
}

func (p Pipeline) fromSubtitles(ctx context.Context, opts Options, md ytdlp.Metadata, selection ytdlp.SubtitleSelection) (transcript.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, selection.URL, nil)
	if err != nil {
		return transcript.Document{}, err
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return transcript.Document{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return transcript.Document{}, fmt.Errorf("subtitle download failed: %s", resp.Status)
	}
	var segments []transcript.Segment
	switch selection.Ext {
	case "srt":
		segments, err = subtitles.ParseSRT(resp.Body)
	default:
		segments, err = subtitles.ParseVTT(resp.Body)
	}
	if err != nil {
		return transcript.Document{}, err
	}
	mode := transcript.ModeSubtitlesManual
	if selection.Kind == ytdlp.SubtitleAuto {
		mode = transcript.ModeSubtitlesAuto
	}
	return transcript.Document{
		SchemaVersion:    transcript.SchemaVersion,
		Title:            md.Title,
		Source:           ytdlp.SourceURL(md),
		Uploader:         md.Uploader,
		Duration:         ytdlp.Duration(md),
		Language:         normalizeDefault(opts.Lang, "auto"),
		DetectedLanguage: selection.Lang,
		Mode:             mode,
		Translated:       opts.Translate,
		Chapters:         ytdlp.ToChapters(md.Chapters),
		Segments:         segments,
	}, nil
}

func (p Pipeline) fromDownloadedAudio(ctx context.Context, opts Options, md ytdlp.Metadata) (transcript.Document, error) {
	return withTempDir(opts.Paths.CacheDir, func(dir string) (transcript.Document, error) {
		mediaPath, err := ytdlp.DownloadMedia(ctx, p.Runner, opts.Input, dir, youtubeCookies(opts.Input, opts.Config.Cookies))
		if err != nil {
			return transcript.Document{}, err
		}
		return p.fromAudio(ctx, opts, mediaPath, md.Title, ytdlp.SourceURL(md), ytdlp.ToChapters(md.Chapters))
	})
}

func (p Pipeline) fromAudio(ctx context.Context, opts Options, mediaPath, title, source string, chapters []transcript.Chapter) (transcript.Document, error) {
	return withTempDir(opts.Paths.CacheDir, func(dir string) (transcript.Document, error) {
		wavPath, err := audio.Normalize(ctx, p.Runner, mediaPath, dir)
		if err != nil {
			return transcript.Document{}, err
		}
		lang := normalizeDefault(opts.Lang, "auto")
		segments, detectedLanguage, err := audio.Transcribe(ctx, p.Runner, wavPath, dir, lang, opts.Translate, opts.Config.Model)
		if err != nil {
			return transcript.Document{}, err
		}
		if title == "" {
			title = titleFromPath(mediaPath)
		}
		if source == "" {
			source = mediaPath
		}
		return transcript.Document{
			SchemaVersion:    transcript.SchemaVersion,
			Title:            title,
			Source:           source,
			Language:         lang,
			DetectedLanguage: detectedLanguage,
			Mode:             transcript.ModeAudio,
			Translated:       opts.Translate,
			Chapters:         chapters,
			Segments:         segments,
		}, nil
	})
}

func withTempDir(cacheDir string, fn func(string) (transcript.Document, error)) (transcript.Document, error) {
	parent := cacheDir
	if parent == "" {
		parent = os.TempDir()
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return transcript.Document{}, err
	}
	dir, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		return transcript.Document{}, err
	}
	defer os.RemoveAll(dir)
	return fn(dir)
}

func normalizeDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func isURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func youtubeCookies(input string, cookies config.CookieConfig) config.CookieConfig {
	if !strings.Contains(input, "youtube.com") && !strings.Contains(input, "youtu.be") {
		cookies.Enabled = false
	}
	return cookies
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func ReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
