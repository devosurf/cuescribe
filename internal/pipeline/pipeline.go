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
	"github.com/devosurf/cuescribe/internal/progress"
	"github.com/devosurf/cuescribe/internal/runner"
	"github.com/devosurf/cuescribe/internal/subtitles"
	"github.com/devosurf/cuescribe/internal/summary"
	"github.com/devosurf/cuescribe/internal/transcript"
	"github.com/devosurf/cuescribe/internal/ytdlp"
)

type Options struct {
	Input        string
	Source       string
	Subs         string
	Lang         string
	Translate    bool
	Summarize    bool
	SummaryLang  string
	Config       config.Config
	Paths        config.Paths
	PlatformOS   string
	PlatformArch string
	Progress     io.Writer
}

// DocumentSummarizer generates a summary for a finished transcript.
type DocumentSummarizer interface {
	Summarize(ctx context.Context, opts summary.Options, doc transcript.Document) (string, error)
}

type Pipeline struct {
	Runner     runner.CommandRunner
	Client     *http.Client
	Summarizer DocumentSummarizer
}

func New(r runner.CommandRunner) Pipeline {
	return Pipeline{
		Runner:     r,
		Client:     &http.Client{Timeout: 60 * time.Second},
		Summarizer: summary.Summarizer{},
	}
}

func (p Pipeline) Run(ctx context.Context, opts Options) (transcript.Document, error) {
	doc, err := p.transcribe(ctx, opts)
	if err != nil {
		return transcript.Document{}, err
	}
	if opts.Summarize {
		progress.Step(opts.Progress, "Summarizing with %s", opts.Config.Summary.Model)
		text, err := p.Summarizer.Summarize(ctx, summary.Options{
			ModelPath: opts.Config.Summary.Path,
			Language:  opts.SummaryLang,
		}, doc)
		if err != nil {
			return transcript.Document{}, err
		}
		doc.Summary = text
		doc.SummaryModel = opts.Config.Summary.Model
	}
	return doc, nil
}

func (p Pipeline) transcribe(ctx context.Context, opts Options) (transcript.Document, error) {
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
	if !IsURL(opts.Input) {
		if source == "subs" {
			return transcript.Document{}, fmt.Errorf("Error: local files do not support --source subs.\nFix: use --source audio or omit --source")
		}
		if info, err := os.Stat(opts.Input); err == nil && info.IsDir() {
			return transcript.Document{}, fmt.Errorf("Error: directories are not supported.\nFix: pass one media file")
		}
		progress.Step(opts.Progress, "Using local media file")
		return p.fromAudio(ctx, opts, opts.Input, "", "", nil)
	}
	md, err := ytdlp.FetchMetadata(ctx, p.Runner, opts.Input, ytdlp.CookiesForInput(opts.Input, opts.Config.Cookies))
	if err != nil {
		return transcript.Document{}, err
	}
	if md.Title != "" {
		progress.Step(opts.Progress, "Found media: %s", md.Title)
	}
	if md.IsLive || md.LiveStatus == "is_live" || md.LiveStatus == "is_upcoming" {
		return transcript.Document{}, fmt.Errorf("Error: active livestreams are not supported.\nFix: run Cuescribe after the stream has ended")
	}
	if source == "auto" || source == "subs" {
		if selection, ok := ytdlp.SelectSubtitle(md, lang, subsMode, opts.Translate); ok {
			progress.Step(opts.Progress, "Using %s subtitles (%s)", selection.Kind, selection.Lang)
			return p.fromSubtitles(ctx, opts, md, selection)
		}
		if source == "subs" {
			return transcript.Document{}, fmt.Errorf("Error: no compatible subtitles found.\nFix: use --source auto or --source audio")
		}
	}
	if source == "audio" {
		progress.Step(opts.Progress, "Using audio transcription")
	} else {
		progress.Step(opts.Progress, "No matching subtitles; using audio transcription")
	}
	return p.fromDownloadedAudio(ctx, opts, md)
}

func (p Pipeline) fromSubtitles(ctx context.Context, opts Options, md ytdlp.Metadata, selection ytdlp.SubtitleSelection) (transcript.Document, error) {
	progress.Step(opts.Progress, "Downloading subtitles")
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
	progress.Step(opts.Progress, "Parsing subtitles")
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
		mediaPath, err := ytdlp.DownloadMedia(ctx, p.Runner, opts.Input, dir, ytdlp.CookiesForInput(opts.Input, opts.Config.Cookies))
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
		if opts.Config.Model.Name != "" {
			progress.Step(opts.Progress, "Using Whisper model: %s", opts.Config.Model.Name)
		}
		segments, detectedLanguage, err := audio.Transcribe(ctx, p.Runner, wavPath, dir, lang, opts.Translate, opts.Config.Model)
		if err != nil {
			return transcript.Document{}, err
		}
		progress.Step(opts.Progress, "Parsed %d transcript segments", len(segments))
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

// IsURL reports whether the input names a remote media URL rather than a
// local file path.
func IsURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
