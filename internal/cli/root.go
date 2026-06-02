package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/logging"
	"github.com/devosurf/cuescribe/internal/model"
	"github.com/devosurf/cuescribe/internal/output"
	"github.com/devosurf/cuescribe/internal/pipeline"
	"github.com/devosurf/cuescribe/internal/runner"
	appupdate "github.com/devosurf/cuescribe/internal/update"
	"github.com/devosurf/cuescribe/internal/version"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	source         string
	subs           string
	lang           string
	translate      bool
	format         string
	noTimestamps   bool
	timestampLinks bool
	outputPath     string
	mkdir          bool
	force          bool
	verbose        bool
}

func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func NewRootCommand() *cobra.Command {
	opts := rootOptions{}
	cmd := &cobra.Command{
		Use:           "cuescribe INPUT",
		Short:         "Create local Markdown or JSON transcripts from YouTube URLs and media files",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if err := validateRootOptions(opts); err != nil {
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
			if opts.verbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "log: %s\n", logPath)
			}
			doc, err := pipeline.New(runner.ExecRunner{Verbose: opts.verbose, Stderr: cmd.ErrOrStderr(), Log: logFile}).Run(cmd.Context(), pipeline.Options{
				Input:     args[0],
				Source:    opts.source,
				Subs:      opts.subs,
				Lang:      opts.lang,
				Translate: opts.translate,
				Config:    cfg,
				Paths:     paths,
			})
			if err != nil {
				return err
			}
			written, err := output.Write(doc, output.Options{
				Format:         output.Format(opts.format),
				NoTimestamps:   opts.noTimestamps,
				TimestampLinks: opts.timestampLinks,
				OutputPath:     opts.outputPath,
				Mkdir:          opts.mkdir,
				Force:          opts.force,
			}, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if written != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", written)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.source, "source", "auto", "source mode: auto, subs, or audio")
	cmd.Flags().StringVar(&opts.subs, "subs", "any", "subtitle preference: any, manual, or auto")
	cmd.Flags().StringVar(&opts.lang, "lang", "auto", "spoken language, such as auto, sv, en, or Swedish")
	cmd.Flags().BoolVar(&opts.translate, "translate", false, "translate to English")
	cmd.Flags().StringVar(&opts.format, "format", "markdown", "output format: markdown or json")
	cmd.Flags().BoolVar(&opts.noTimestamps, "no-timestamps", false, "omit timestamps in Markdown output")
	cmd.Flags().BoolVar(&opts.timestampLinks, "timestamp-links", false, "link Markdown timestamps to the source URL")
	cmd.Flags().StringVarP(&opts.outputPath, "output", "o", "", "output file or directory")
	cmd.Flags().BoolVar(&opts.mkdir, "mkdir", false, "create output directories")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite an existing output file")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "show raw child process stderr")

	cmd.AddCommand(newSetupCommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newVersionCommand())
	cmd.AddCommand(newSelfUpdateCommand())
	cmd.AddCommand(newUninstallCommand())
	cmd.AddCommand(newCompletionCommand(cmd))
	return cmd
}

func validateRootOptions(opts rootOptions) error {
	if err := oneOf("--source", opts.source, "auto", "subs", "audio"); err != nil {
		return err
	}
	if err := oneOf("--subs", opts.subs, "any", "manual", "auto"); err != nil {
		return err
	}
	if err := oneOf("--format", opts.format, "markdown", "json"); err != nil {
		return err
	}
	return nil
}

func oneOf(name, value string, allowed ...string) error {
	for _, item := range allowed {
		if value == item {
			return nil
		}
	}
	return fmt.Errorf("Error: invalid %s value %q.\nFix: use one of %s", name, value, strings.Join(allowed, ", "))
}

func newSetupCommand() *cobra.Command {
	var yes bool
	var modelName string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Prepare dependencies, model, config, and cookies",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runSetupDeps(cmd.Context(), cmd, yes); err != nil {
				return err
			}
			if err := runSetupModel(cmd.Context(), cmd, modelName); err != nil {
				return err
			}
			if err := runSetupCookies(cmd, "", "", false); err != nil {
				return err
			}
			return saveInstallState(cmd)
		},
	}
	cmd.PersistentFlags().BoolVar(&yes, "yes", false, "install missing Homebrew dependencies without prompting")
	cmd.PersistentFlags().StringVar(&modelName, "model", "small", "model to download")
	cmd.AddCommand(&cobra.Command{
		Use:   "deps",
		Short: "Check or install Homebrew dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupDeps(cmd.Context(), cmd, yes)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "model",
		Short: "Download and verify a Whisper model",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupModel(cmd.Context(), cmd, modelName)
		},
	})
	var browser, profile string
	var disable bool
	cookiesCmd := &cobra.Command{
		Use:   "cookies",
		Short: "Configure YouTube browser cookies by browser/profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupCookies(cmd, browser, profile, disable)
		},
	}
	cookiesCmd.Flags().StringVar(&browser, "browser", "", "browser name for yt-dlp cookies, such as safari, chrome, or firefox")
	cookiesCmd.Flags().StringVar(&profile, "profile", "", "browser profile name")
	cookiesCmd.Flags().BoolVar(&disable, "disable", false, "disable browser cookies")
	cmd.AddCommand(cookiesCmd)
	return cmd
}

func runSetupDeps(ctx context.Context, cmd *cobra.Command, yes bool) error {
	required := []string{"yt-dlp", "ffmpeg", "whisper-cli"}
	var missing []string
	for _, name := range required {
		if !runner.LookPath(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "dependencies ok")
		return nil
	}
	if !runner.LookPath("brew") {
		return fmt.Errorf("Error: Homebrew is missing.\nFix: install Homebrew, then run brew install yt-dlp ffmpeg whisper-cpp")
	}
	pkgs := brewPackages(missing)
	if !yes {
		return fmt.Errorf("Error: missing dependencies: %s\nFix: run brew install %s", strings.Join(missing, ", "), strings.Join(pkgs, " "))
	}
	args := append([]string{"install"}, pkgs...)
	_, err := runner.ExecRunner{Verbose: true, Stderr: cmd.ErrOrStderr()}.Run(ctx, "brew", args...)
	return err
}

func runSetupModel(ctx context.Context, cmd *cobra.Command, modelName string) error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	entry, ok := model.Get(modelName)
	if !ok {
		return fmt.Errorf("Error: unknown model %q.\nFix: use one of %s", modelName, strings.Join(model.Names(), ", "))
	}
	dest := filepath.Join(paths.ModelDir, entry.File)
	if err := model.Download(ctx, entry, dest, cmd.OutOrStdout()); err != nil {
		return err
	}
	cfg, err := config.Load(paths.ConfigFile, config.Default(paths))
	if err != nil {
		return err
	}
	cfg.Model.Name = entry.Name
	cfg.Model.Path = dest
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "config saved: %s\n", paths.ConfigFile)
	return nil
}

func runSetupCookies(cmd *cobra.Command, browser, profile string, disable bool) error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths.ConfigFile, config.Default(paths))
	if err != nil {
		return err
	}
	if disable {
		cfg.Cookies = config.CookieConfig{}
	} else if browser != "" {
		cfg.Cookies.Enabled = true
		cfg.Cookies.Browser = browser
		cfg.Cookies.Profile = profile
	} else if isTerminal(os.Stdin) {
		detected := detectBrowsers()
		if len(detected) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no supported browsers detected; cookies disabled")
			return config.Save(paths.ConfigFile, cfg)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "detected browsers: %s\n", strings.Join(detected, ", "))
		fmt.Fprint(cmd.OutOrStdout(), "enable YouTube browser cookies? [y/N] ")
		reader := bufio.NewReader(cmd.InOrStdin())
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "cookies disabled")
			return config.Save(paths.ConfigFile, cfg)
		}
		browser = detected[0]
		if len(detected) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "browser [%s]: ", browser)
			selected, _ := reader.ReadString('\n')
			selected = strings.TrimSpace(selected)
			if selected != "" {
				browser = selected
			}
		}
		fmt.Fprint(cmd.OutOrStdout(), "profile (optional): ")
		selectedProfile, _ := reader.ReadString('\n')
		selectedProfile = strings.TrimSpace(selectedProfile)
		if selectedProfile != "" {
			profile = selectedProfile
		}
		cfg.Cookies.Enabled = true
		cfg.Cookies.Browser = browser
		cfg.Cookies.Profile = profile
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "cookies disabled")
		fmt.Fprintln(cmd.OutOrStdout(), "enable with: cuescribe setup cookies --browser safari")
		return config.Save(paths.ConfigFile, cfg)
	}
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "config saved: %s\n", paths.ConfigFile)
	return nil
}

func saveInstallState(cmd *cobra.Command) error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	exe, _ := os.Executable()
	return config.SaveInstallState(paths.InstallFile, config.InstallState{
		Version:    version.Version,
		BinaryPath: exe,
	})
}

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect or update Cuescribe config"}
	var modelName, modelPath string
	modelCmd := &cobra.Command{
		Use:   "model",
		Short: "Inspect or update model config",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := config.ResolvePaths()
			if err != nil {
				return err
			}
			cfg, err := config.Load(paths.ConfigFile, config.Default(paths))
			if err != nil {
				return err
			}
			changed := false
			if modelName != "" {
				cfg.Model.Name = modelName
				if entry, ok := model.Get(modelName); ok && modelPath == "" {
					cfg.Model.Path = filepath.Join(paths.ModelDir, entry.File)
				}
				changed = true
			}
			if modelPath != "" {
				cfg.Model.Path = modelPath
				changed = true
			}
			if changed {
				if err := config.Save(paths.ConfigFile, cfg); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "model.name=%s\nmodel.path=%s\n", cfg.Model.Name, cfg.Model.Path)
			return nil
		},
	}
	modelCmd.Flags().StringVar(&modelName, "name", "", "model name")
	modelCmd.Flags().StringVar(&modelPath, "path", "", "custom model path")
	cmd.AddCommand(modelCmd)

	var browser, profile string
	var enable, disable bool
	cookiesCmd := &cobra.Command{
		Use:   "cookies",
		Short: "Inspect or update cookie config",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := config.ResolvePaths()
			if err != nil {
				return err
			}
			cfg, err := config.Load(paths.ConfigFile, config.Default(paths))
			if err != nil {
				return err
			}
			changed := false
			if disable {
				cfg.Cookies = config.CookieConfig{}
				changed = true
			}
			if enable || browser != "" || profile != "" {
				cfg.Cookies.Enabled = true
				if browser != "" {
					cfg.Cookies.Browser = browser
				}
				if profile != "" {
					cfg.Cookies.Profile = profile
				}
				changed = true
			}
			if changed {
				if err := config.Save(paths.ConfigFile, cfg); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "cookies.enabled=%t\ncookies.browser=%s\ncookies.profile=%s\n", cfg.Cookies.Enabled, cfg.Cookies.Browser, cfg.Cookies.Profile)
			return nil
		},
	}
	cookiesCmd.Flags().BoolVar(&enable, "enable", false, "enable cookies")
	cookiesCmd.Flags().BoolVar(&disable, "disable", false, "disable cookies")
	cookiesCmd.Flags().StringVar(&browser, "browser", "", "browser name")
	cookiesCmd.Flags().StringVar(&profile, "profile", "", "browser profile")
	cmd.AddCommand(cookiesCmd)
	return cmd
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check platform, dependencies, model, config, and cookies",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, err := config.LoadDefault()
			if err != nil {
				return err
			}
			failures := 0
			check := func(ok bool, level, name, detail string) {
				if ok {
					fmt.Fprintf(cmd.OutOrStdout(), "ok    %s\n", name)
					return
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-5s %s: %s\n", level, name, detail)
				if level == "error" {
					failures++
				}
			}
			check(runtime.GOOS == "darwin" && runtime.GOARCH == "arm64", "error", "platform", fmt.Sprintf("%s/%s unsupported in v1", runtime.GOOS, runtime.GOARCH))
			check(runner.LookPath("yt-dlp"), "error", "yt-dlp", "brew install yt-dlp")
			check(runner.LookPath("ffmpeg"), "error", "ffmpeg", "brew install ffmpeg")
			check(runner.LookPath("ffprobe"), "warn", "ffprobe", "installed with ffmpeg; metadata checks may be limited")
			check(runner.LookPath("whisper-cli"), "error", "whisper-cli", "brew install whisper-cpp")
			if cfg.Model.Path != "" {
				check(fileExists(cfg.Model.Path), "error", "model", "run cuescribe setup model")
				if entry, ok := model.Get(cfg.Model.Name); ok && fileExists(cfg.Model.Path) {
					check(model.VerifyFile(cfg.Model.Path, entry.SHA256) == nil, "error", "model checksum", "delete the corrupt model, then run cuescribe setup model")
				}
			}
			check(true, "ok", "config", paths.ConfigFile)
			if state, err := config.LoadInstallState(paths.InstallFile); err == nil {
				check(state.Version != "", "error", "install state", paths.InstallFile)
			} else {
				check(false, "warn", "install state", "run cuescribe setup")
			}
			if cfg.Cookies.Enabled {
				check(cfg.Cookies.Browser != "", "error", "cookies", "browser is required when cookies are enabled")
				if cfg.Cookies.Browser != "" && runner.LookPath("yt-dlp") {
					ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
					defer cancel()
					args := []string{"--simulate", "--skip-download", "--no-warnings"}
					args = append(args, cfg.Cookies.YTDLPCookieArgs()...)
					args = append(args, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
					_, err := runner.ExecRunner{Verbose: false}.Run(ctx, "yt-dlp", args...)
					check(err == nil, "error", "YouTube cookie access", "check browser/profile access or disable cookies")
				}
			} else {
				check(true, "ok", "cookies", "disabled")
			}
			if failures > 0 {
				return fmt.Errorf("doctor found %d error(s)", failures)
			}
			return nil
		},
	}
}

func newVersionCommand() *cobra.Command {
	var asJSON bool
	var check bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				result, err := version.CheckLatest(cmd.Context())
				if err != nil {
					return err
				}
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
				}
				if result.Latest == "" {
					fmt.Fprintf(cmd.OutOrStdout(), "cuescribe %s (%s)\n", result.Current, result.Message)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "cuescribe %s; latest %s\n%s\n", result.Current, result.Latest, result.URL)
				}
				return nil
			}
			info := version.Current()
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "cuescribe %s\n", info.Version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&check, "check", false, "check latest GitHub release")
	return cmd
}

func newSelfUpdateCommand() *cobra.Command {
	var updateVersion string
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Download, verify, and replace the installed binary",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := appupdate.SelfUpdate(cmd.Context(), updateVersion, cmd.OutOrStdout()); err != nil {
				return err
			}
			return saveInstallState(cmd)
		},
	}
	cmd.Flags().StringVar(&updateVersion, "version", "latest", "release version or latest")
	return cmd
}

func newUninstallCommand() *cobra.Command {
	var yes bool
	var purge bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Cuescribe state managed by the CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("Error: uninstall requires --yes.\nFix: rerun with cuescribe uninstall --yes")
			}
			paths, err := config.ResolvePaths()
			if err != nil {
				return err
			}
			for _, path := range []string{paths.CacheDir} {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
			}
			if purge {
				for _, path := range []string{paths.ConfigDir, paths.DataDir, paths.StateDir} {
					if err := os.RemoveAll(path); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Homebrew dependencies were not removed.")
			fmt.Fprintln(cmd.OutOrStdout(), "Optional cleanup: brew uninstall yt-dlp ffmpeg whisper-cpp")
			if err := removeInstalledBinary(cmd); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm uninstall")
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove config and downloaded models")
	return cmd
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion bash|zsh|fish",
		Short: "Generate shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("Error: unsupported shell %q.\nFix: use bash, zsh, or fish", args[0])
			}
		},
	}
}

func brewPackages(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "whisper-cli" {
			out = append(out, "whisper-cpp")
		} else {
			out = append(out, name)
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func removeInstalledBinary(cmd *cobra.Command) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Base(exe) != "cuescribe" {
		fmt.Fprintf(cmd.OutOrStdout(), "skipped binary removal: current executable is %s\n", exe)
		return nil
	}
	if err := os.Remove(exe); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", exe)
	return nil
}

func detectBrowsers() []string {
	candidates := []struct {
		name string
		path string
	}{
		{"safari", "/Applications/Safari.app"},
		{"chrome", "/Applications/Google Chrome.app"},
		{"firefox", "/Applications/Firefox.app"},
		{"brave", "/Applications/Brave Browser.app"},
		{"edge", "/Applications/Microsoft Edge.app"},
	}
	var found []string
	for _, candidate := range candidates {
		if fileExists(candidate.path) {
			found = append(found, candidate.name)
		}
	}
	return found
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
