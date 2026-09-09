package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/logging"
	"github.com/devosurf/cuescribe/internal/output"
	"github.com/devosurf/cuescribe/internal/pipeline"
	"github.com/devosurf/cuescribe/internal/progress"
	"github.com/devosurf/cuescribe/internal/runner"
	"github.com/devosurf/cuescribe/internal/version"
	"github.com/devosurf/cuescribe/internal/ytdlp"
	"github.com/spf13/cobra"
)

func newDownloadCommand() *cobra.Command {
	var format, cover string
	var opts output.Options
	var verbose bool
	cmd := &cobra.Command{
		Use:   "download URL",
		Short: "Download audio for listening without transcribing",
		Long: "Download tagged audio using configured YouTube cookies; no Whisper model needed.\n" +
			"Title and album use the full video title. Artist and album artist use the channel\n" +
			"display name (uploader fallback); the original URL is stored in comment/source tags.\n" +
			"Embed the video thumbnail by default, or choose a channel avatar or no artwork.\n" +
			"Missing artwork warns and keeps tagged audio; metadata-writing errors fail.\n" +
			"Best preserves the source codec and may yield Opus, not Apple Music-compatible audio.\n" +
			"Use mp3 or m4a for Apple Music/Doppler; m4a prefers native AAC when available.",
		Example: "  cuescribe download \"https://youtube.com/watch?v=...\" --audio-format m4a\n" +
			"  cuescribe download \"https://youtube.com/watch?v=...\" --audio-format m4a --cover channel\n" +
			"  cuescribe download \"https://youtube.com/watch?v=...\" --audio-format mp3 --cover none -o car/ --mkdir",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]
			if !pipeline.IsURL(input) {
				return fmt.Errorf("Error: download requires an HTTP or HTTPS URL.\nFix: run cuescribe download URL")
			}
			if err := oneOf("--audio-format", format, "best", "mp3", "m4a"); err != nil {
				return err
			}
			if err := oneOf("--cover", cover, "thumbnail", "channel", "none"); err != nil {
				return err
			}
			if opts.OutputPath == "-" {
				return fmt.Errorf("Error: audio downloads cannot be written to stdout.\nFix: choose an output file or directory with -o")
			}
			if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
				return fmt.Errorf("Error: unsupported platform %s/%s.\nFix: Cuescribe v1 supports macOS Apple Silicon only", runtime.GOOS, runtime.GOARCH)
			}
			if err := ensureRequiredDependencies(cmd.Context(), cmd, []string{"yt-dlp", "ffmpeg", "ffprobe"}); err != nil {
				return err
			}
			cfg, paths, err := config.LoadDefault()
			if err != nil {
				return err
			}
			logFile, logPath, err := logging.OpenRunLog(paths)
			if err != nil {
				return err
			}
			defer logFile.Close()
			fmt.Fprintf(logFile, "cuescribe %s\n", version.Version)
			debug, _ := getBoolFlag(cmd, "debug")
			cookieDebug, _ := getBoolFlag(cmd, "cookie-debug")
			debug = debug || cookieDebug
			if verbose || debug {
				fmt.Fprintf(cmd.ErrOrStderr(), "log: %s\n", logPath)
			}
			r := runner.ExecRunner{
				Verbose:  verbose || debug,
				Stderr:   cmd.ErrOrStderr(),
				Log:      logWriter(rootOptions{debug: debug}, logFile, cmd.ErrOrStderr()),
				Progress: cmd.ErrOrStderr(),
			}
			cookies := ytdlp.CookiesForInput(input, cfg.Cookies)
			md, err := ytdlp.FetchMetadata(cmd.Context(), r, input, cookies)
			if err != nil {
				return err
			}
			if md.IsLive || md.LiveStatus == "is_live" || md.LiveStatus == "is_upcoming" {
				return fmt.Errorf("Error: active livestreams are not supported.\nFix: run Cuescribe after the stream has ended")
			}
			if md.Title != "" {
				progress.Step(cmd.ErrOrStderr(), "Found media: %s", md.Title)
			}
			if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
				return err
			}
			dir, err := os.MkdirTemp(paths.CacheDir, "download-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			source, err := ytdlp.DownloadAudio(cmd.Context(), r, input, dir, format, cookies)
			if err != nil {
				return err
			}
			if err := ytdlp.EmbedCover(cmd.Context(), r, source, md, cover, cookies); err != nil {
				if cmd.Context().Err() != nil {
					return cmd.Context().Err()
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "warn  artwork not embedded: %v\n", err)
			}
			progress.Step(cmd.ErrOrStderr(), "Saving audio")
			written, err := output.SaveFile(source, md.Title, opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", written)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "audio-format", "best", "audio format: best (source codec), mp3, or m4a")
	cmd.Flags().StringVar(&cover, "cover", "thumbnail", "embedded artwork: thumbnail, channel (avatar), or none")
	cmd.Flags().StringVarP(&opts.OutputPath, "output", "o", "", "output file or directory (default: title and audio extension)")
	cmd.Flags().BoolVar(&opts.Mkdir, "mkdir", false, "create output directories")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "overwrite an existing output file")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show raw child process stderr")
	return cmd
}
