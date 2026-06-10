package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/hardware"
	"github.com/devosurf/cuescribe/internal/logging"
	"github.com/devosurf/cuescribe/internal/model"
	"github.com/devosurf/cuescribe/internal/output"
	"github.com/devosurf/cuescribe/internal/pipeline"
	"github.com/devosurf/cuescribe/internal/progress"
	"github.com/devosurf/cuescribe/internal/runner"
	appupdate "github.com/devosurf/cuescribe/internal/update"
	"github.com/devosurf/cuescribe/internal/version"
	"github.com/devosurf/cuescribe/internal/ytdlp"
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
	debug          bool
	listFormats    bool
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
				if opts.listFormats {
					return fmt.Errorf("Error: --list-formats requires INPUT.\nFix: run cuescribe --list-formats URL")
				}
				return cmd.Help()
			}
			if opts.listFormats {
				if err := ensureDependencies(cmd.Context(), cmd, args[0], true); err != nil {
					return err
				}
				return runListFormats(cmd.Context(), cmd, args[0])
			}
			if err := validateRootOptions(opts); err != nil {
				return err
			}
			if err := ensureDependencies(cmd.Context(), cmd, args[0], false); err != nil {
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
			if opts.verbose || opts.debug {
				fmt.Fprintf(cmd.ErrOrStderr(), "log: %s\n", logPath)
			}
			progressOut := cmd.ErrOrStderr()
			doc, err := pipeline.New(runner.ExecRunner{Verbose: opts.verbose || opts.debug, Stderr: cmd.ErrOrStderr(), Log: logWriter(opts, logFile, cmd.ErrOrStderr()), Progress: progressOut}).Run(cmd.Context(), pipeline.Options{
				Input:     args[0],
				Source:    opts.source,
				Subs:      opts.subs,
				Lang:      opts.lang,
				Translate: opts.translate,
				Config:    cfg,
				Paths:     paths,
				Progress:  progressOut,
			})
			if err != nil {
				return err
			}
			progress.Step(progressOut, "Writing output")
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
	cmd.Flags().StringVarP(&opts.outputPath, "output", "o", "", "output file or directory; use - for stdout")
	cmd.Flags().BoolVar(&opts.mkdir, "mkdir", false, "create output directories")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite an existing output file")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "show raw child process stderr")
	cmd.PersistentFlags().BoolVar(&opts.debug, "debug", false, "show detailed logs for troubleshooting")
	cmd.PersistentFlags().BoolVar(&opts.debug, "cookie-debug", false, "deprecated: use --debug")
	cmd.Flags().BoolVar(&opts.listFormats, "list-formats", false, "list yt-dlp formats for the input and exit")

	cmd.AddCommand(newSetupCommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newVersionCommand())
	cmd.AddCommand(newUpgradeCommand())
	cmd.AddCommand(newUninstallCommand())
	cmd.AddCommand(newCompletionCommand(cmd))
	return cmd
}

func runListFormats(ctx context.Context, cmd *cobra.Command, input string) error {
	cfg, _, err := config.LoadDefault()
	if err != nil {
		return err
	}
	formats, err := ytdlp.ListFormats(ctx, runner.ExecRunner{Verbose: true, Stderr: cmd.ErrOrStderr()}, input, ytdlp.CookiesForInput(input, cfg.Cookies))
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), formats)
	if formats != "" && !strings.HasSuffix(formats, "\n") {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
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
	var cookiesBrowser string
	var cookiesProfile string
	var requireCookies bool
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
			if err := runSetupCookies(cmd.Context(), cmd, cookieSetupOptions{
				Browser: cookiesBrowser,
				Profile: cookiesProfile,
				Require: requireCookies,
				Prompt:  !yes,
			}); err != nil {
				return err
			}
			return saveInstallState(cmd)
		},
	}
	cmd.PersistentFlags().BoolVar(&yes, "yes", false, "install missing Homebrew dependencies without prompting")
	cmd.PersistentFlags().StringVar(&modelName, "model", "", "model to download (default: recommended for detected hardware)")
	cmd.PersistentFlags().BoolVar(&requireCookies, "require-cookies", false, "fail setup unless YouTube browser cookies are configured and usable")
	cmd.PersistentFlags().StringVar(&cookiesBrowser, "cookies-browser", "", "browser name for YouTube cookies, such as safari, chrome, or firefox")
	cmd.PersistentFlags().StringVar(&cookiesProfile, "cookies-profile", "", "browser profile name for YouTube cookies")
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
			return runSetupCookies(cmd.Context(), cmd, cookieSetupOptions{
				Browser: browser,
				Profile: profile,
				Disable: disable,
				Require: !disable,
				Prompt:  true,
			})
		},
	}
	cookiesCmd.Flags().StringVar(&browser, "browser", "", "browser name for yt-dlp cookies, such as safari, chrome, or firefox")
	cookiesCmd.Flags().StringVar(&profile, "profile", "", "browser profile name")
	cookiesCmd.Flags().BoolVar(&disable, "disable", false, "disable browser cookies")
	cmd.AddCommand(cookiesCmd)
	return cmd
}

func runSetupDeps(ctx context.Context, cmd *cobra.Command, yes bool) error {
	return installDependencies(ctx, cmd, yes, requiredDependencies())
}

func requiredDependencies() []string {
	return []string{"yt-dlp", "ffmpeg", "whisper-cli"}
}

func runUpgradeDeps(ctx context.Context, cmd *cobra.Command) error {
	return upgradeDependencies(ctx, cmd, requiredDependencies())
}

func ensureDependencies(ctx context.Context, cmd *cobra.Command, input string, listFormats bool) error {
	deps := requiredDependenciesForInput(input, listFormats)
	missing := []string{}
	for _, dep := range deps {
		if !runner.LookPath(dep) {
			missing = append(missing, dep)
		}
	}
	needsYTDLPUpdate := false
	currentYTDLPVersion := ""
	if containsString(deps, "yt-dlp") && runner.LookPath("yt-dlp") {
		if version, ok := ytdlp.CurrentYTDLPVersion(ctx, runner.ExecRunner{Stderr: cmd.ErrOrStderr()}); ok {
			currentYTDLPVersion = version
			needsYTDLPUpdate = ytdlp.IsYTDLPVersionOlder(currentYTDLPVersion, ytdlp.MinimumYTDLPVersion)
		}
	}
	if len(missing) == 0 && !needsYTDLPUpdate {
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "dependency check found issues:")
	if len(missing) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  - missing: %s\n", strings.Join(missing, ", "))
	}
	if needsYTDLPUpdate {
		fmt.Fprintf(cmd.OutOrStdout(), "  - yt-dlp upgrade needed: current %s, required %s\n", currentYTDLPVersion, ytdlp.MinimumYTDLPVersion)
	}
	if !isInteractiveInput(cmd) {
		req := strings.Join(deps, ", ")
		return fmt.Errorf("Error: this environment is missing or out of date for required dependencies: %s\nFix: run cuescribe upgrade", req)
	}
	fmt.Fprint(cmd.OutOrStdout(), "upgrade required dependencies now? [Y/n] ")
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
		return fmt.Errorf("Error: dependency upgrade declined.\nFix: rerun with an explicit yes or manually run cuescribe upgrade")
	}
	if len(missing) > 0 {
		if err := upgradeDependencies(ctx, cmd, deps); err != nil {
			return err
		}
		return nil
	}
	if needsYTDLPUpdate {
		if err := upgradeDependencies(ctx, cmd, []string{"yt-dlp"}); err != nil {
			return err
		}
	}
	return nil
}

func requiredDependenciesForInput(input string, listFormats bool) []string {
	if listFormats {
		return []string{"yt-dlp"}
	}
	if pipeline.IsURL(input) {
		return requiredDependencies()
	}
	return []string{"ffmpeg", "whisper-cli"}
}

func installDependencies(ctx context.Context, cmd *cobra.Command, yes bool, names []string) error {
	missing := []string{}
	for _, name := range names {
		if !runner.LookPath(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if !runner.LookPath("brew") {
		return fmt.Errorf("Error: Homebrew is missing.\nFix: install Homebrew, then run brew install %s", strings.Join(brewPackages(missing), " "))
	}
	pkgs := brewPackages(missing)
	if !yes {
		return fmt.Errorf("Error: missing dependencies: %s\nFix: run brew install %s", strings.Join(missing, ", "), strings.Join(pkgs, " "))
	}
	args := append([]string{"install"}, pkgs...)
	_, err := runner.ExecRunner{Verbose: true, Stderr: cmd.ErrOrStderr()}.Run(ctx, "brew", args...)
	return err
}

func upgradeDependencies(ctx context.Context, cmd *cobra.Command, names []string) error {
	if !runner.LookPath("brew") {
		return fmt.Errorf("Error: Homebrew is missing.\nFix: install Homebrew, then run brew upgrade %s", strings.Join(brewPackages(names), " "))
	}
	packages := brewPackages(names)
	installed, err := brewInstalledPackages(ctx, packages)
	if err != nil {
		return err
	}
	install := []string{}
	present := []string{}
	for _, pkg := range packages {
		if installed[pkg] {
			present = append(present, pkg)
		} else {
			install = append(install, pkg)
		}
	}
	upgrade := []string{}
	if len(present) > 0 {
		outdated, err := brewOutdatedPackages(ctx, present)
		if err != nil {
			return err
		}
		for _, pkg := range present {
			if outdated[pkg] {
				upgrade = append(upgrade, pkg)
			}
		}
	}
	if len(install) > 0 {
		progress.Step(cmd.OutOrStdout(), "Installing dependencies")
		if err := runBrewCommand(ctx, cmd, "install", install); err != nil {
			return err
		}
	}
	if len(upgrade) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "All dependencies are up to date.")
		return nil
	}
	progress.Step(cmd.OutOrStdout(), "Upgrading dependencies")
	return runBrewCommand(ctx, cmd, "upgrade", upgrade)
}

// brewQuery runs a read-only brew command in one batched invocation.
// Brew exits 1 from queries like "list --versions" when some of the named
// packages are missing or unknown — that is an answer, not a failure — so
// exit status 1 is tolerated and the output parsed as-is.
func brewQuery(ctx context.Context, args ...string) (string, error) {
	result, err := runner.ExecRunner{Verbose: false, Stderr: io.Discard}.Run(ctx, "brew", args...)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", err
		}
	}
	return string(result.Stdout), nil
}

// brewInstalledPackages reports which of the given formulas are installed,
// based on "brew list --versions", which prints one "<name> <versions...>"
// line per installed formula and nothing for missing ones.
func brewInstalledPackages(ctx context.Context, packages []string) (map[string]bool, error) {
	out, err := brewQuery(ctx, append([]string{"list", "--versions"}, packages...)...)
	if err != nil {
		return nil, err
	}
	return brewNamesFromLines(out), nil
}

// brewOutdatedPackages reports which of the given installed formulas are
// outdated, based on "brew outdated --formula --quiet", which prints one
// formula name per outdated package.
func brewOutdatedPackages(ctx context.Context, packages []string) (map[string]bool, error) {
	out, err := brewQuery(ctx, append([]string{"outdated", "--formula", "--quiet"}, packages...)...)
	if err != nil {
		return nil, err
	}
	return brewNamesFromLines(out), nil
}

func brewNamesFromLines(out string) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Names may be printed with a tap prefix such as homebrew/core/ffmpeg.
		name := fields[0]
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		names[name] = true
	}
	return names
}

func runBrewCommand(ctx context.Context, cmd *cobra.Command, subcommand string, packages []string) error {
	_, err := runBrewCommandResult(ctx, cmd, subcommand, packages)
	return err
}

func runBrewCommandResult(ctx context.Context, cmd *cobra.Command, subcommand string, packages []string) (runner.Result, error) {
	result, err := runner.ExecRunner{Verbose: false, Stderr: io.Discard}.Run(ctx, "brew", append([]string{subcommand}, packages...)...)
	if err == nil {
		output := strings.TrimSpace(string(result.Stderr))
		if output != "" {
			printFilteredBrewOutput(cmd.ErrOrStderr(), output)
		}
		return result, nil
	}
	if len(result.Stdout) == 0 && len(result.Stderr) == 0 {
		return result, fmt.Errorf("brew %s %s failed: %w", subcommand, strings.Join(packages, " "), err)
	}
	return result, err
}

func printFilteredBrewOutput(w io.Writer, output string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Warning:") && strings.Contains(line, "already installed") {
			continue
		}
		fmt.Fprintln(w, line)
	}
}

func runSetupModel(ctx context.Context, cmd *cobra.Command, modelName string) error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	modelName, err = resolveSetupModel(cmd, modelName, hardware.Detect(), isInteractiveInput(cmd))
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

// resolveSetupModel picks the Whisper model to install: an explicit --model
// wins; otherwise the hardware-based recommendation is used, which
// interactive users can override at a prompt.
func resolveSetupModel(cmd *cobra.Command, modelName string, hw hardware.Info, interactive bool) (string, error) {
	if strings.TrimSpace(modelName) != "" {
		return strings.TrimSpace(modelName), nil
	}
	recommended := hardware.RecommendedWhisperModel(hw)
	if desc := hw.String(); desc != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "detected hardware: %s\n", desc)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "recommended model: %s\n", recommended)
	if !interactive {
		return recommended, nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "model [%s] (options: %s): ", recommended, strings.Join(model.Names(), ", "))
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return recommended, nil
	}
	return answer, nil
}

type cookieSetupOptions struct {
	Browser       string
	Profile       string
	Disable       bool
	Require       bool
	Prompt        bool
	AssumeConsent bool
}

func runSetupCookies(ctx context.Context, cmd *cobra.Command, opts cookieSetupOptions) error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths.ConfigFile, config.Default(paths))
	if err != nil {
		return err
	}
	if opts.Disable {
		cfg.Cookies = config.CookieConfig{}
	} else if opts.Profile != "" && opts.Browser == "" {
		return fmt.Errorf("Error: cookie profile requires a browser.\nFix: pass --cookies-browser chrome --cookies-profile %s", strconv.Quote(opts.Profile))
	} else if opts.Browser != "" {
		browser := normalizeBrowserName(opts.Browser)
		if !config.IsSupportedCookieBrowser(browser) {
			return fmt.Errorf("Error: unsupported browser %q.\nFix: use one of %s", opts.Browser, strings.Join(config.SupportedCookieBrowsers, ", "))
		}
		profile := opts.Profile
		if browser == "chrome" && profile == "" && opts.Prompt && isInteractiveInput(cmd) {
			profile = promptChromeProfile(cmd, bufio.NewReader(cmd.InOrStdin()))
		}
		cfg.Cookies.Enabled = true
		cfg.Cookies.Browser = browser
		cfg.Cookies.Profile = profile
	} else if opts.AssumeConsent || (opts.Prompt && isInteractiveInput(cmd)) {
		detected := detectBrowsers()
		defaultBrowser := detectDefaultBrowser()
		detected = mergeDetectedDefaultBrowser(detected, defaultBrowser)
		if len(detected) == 0 {
			if opts.Require {
				return fmt.Errorf("Error: no supported browsers detected for YouTube cookies.\nFix: install Safari, Chrome, Firefox, Brave, or Edge; or rerun without --require-cookies")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "warn  cookies: no supported browsers detected; cookies disabled")
			return config.Save(paths.ConfigFile, cfg)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "detected browsers: %s\n", strings.Join(detected, ", "))
		if containsString(detected, defaultBrowser) {
			fmt.Fprintf(cmd.OutOrStdout(), "default browser: %s\n", defaultBrowser)
		}
		reader := bufio.NewReader(cmd.InOrStdin())
		if !opts.AssumeConsent {
			fmt.Fprint(cmd.OutOrStdout(), "enable YouTube browser cookies? [Y/n] ")
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "n" || answer == "no" {
				if opts.Require {
					return fmt.Errorf("Error: YouTube browser cookies are required.\nFix: rerun setup and consent to browser cookie access, or rerun without --require-cookies")
				}
				fmt.Fprintln(cmd.OutOrStdout(), "warn  cookies: disabled")
				return config.Save(paths.ConfigFile, cfg)
			}
		}
		browser := preferredBrowser(detected, defaultBrowser)
		if len(detected) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "browser [%s]: ", browser)
			selected, _ := reader.ReadString('\n')
			selected = strings.TrimSpace(selected)
			if selected != "" {
				browser = selected
			}
		}
		browser = normalizeBrowserName(browser)
		if !config.IsSupportedCookieBrowser(browser) {
			return fmt.Errorf("Error: unsupported browser %q.\nFix: use one of %s", browser, strings.Join(config.SupportedCookieBrowsers, ", "))
		}
		profile := ""
		if browser == "chrome" {
			profile = promptChromeProfile(cmd, reader)
		} else {
			profile = promptOptionalProfile(cmd, reader)
		}
		cfg.Cookies.Enabled = true
		cfg.Cookies.Browser = browser
		cfg.Cookies.Profile = profile
	} else {
		if opts.Require {
			return fmt.Errorf("Error: YouTube browser cookies are required in non-interactive setup.\nFix: pass --cookies-browser and, for Chrome, --cookies-profile; or rerun without --require-cookies")
		}
		fmt.Fprintln(cmd.OutOrStdout(), "warn  cookies: disabled")
		fmt.Fprintln(cmd.OutOrStdout(), "enable with: cuescribe setup cookies --browser chrome --profile Default")
		return config.Save(paths.ConfigFile, cfg)
	}
	if cfg.Cookies.Enabled {
		if err := validateYouTubeCookieAccess(ctx, cmd, cfg.Cookies, shouldDebug(cmd)); err != nil {
			return err
		}
	}
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "config saved: %s\n", paths.ConfigFile)
	return nil
}

func validateYouTubeCookieAccess(ctx context.Context, cmd *cobra.Command, cookies config.CookieConfig, debug bool) error {
	if !cookies.Enabled {
		return fmt.Errorf("Error: YouTube browser cookies are disabled.\nFix: run cuescribe setup cookies --browser chrome --profile Default")
	}
	if strings.TrimSpace(cookies.Browser) == "" {
		return fmt.Errorf("Error: browser is required when cookies are enabled.\nFix: run cuescribe setup cookies --browser chrome --profile Default")
	}
	if !runner.LookPath("yt-dlp") {
		return fmt.Errorf("Error: yt-dlp is missing.\nFix: brew install yt-dlp")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := []string{"--simulate", "--skip-download", "--no-warnings"}
	args = append(args, cookies.YTDLPCookieArgs()...)
	args = append(args, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if debug {
		fmt.Fprintf(cmd.ErrOrStderr(), "debug: validating YouTube cookie access %q\n", cookies.YTDLPCookieArgs())
	}
	if _, err := (runner.ExecRunner{Verbose: debug, Stderr: cmd.ErrOrStderr()}).Run(ctx, "yt-dlp", args...); err != nil {
		return fmt.Errorf("Error: YouTube cookie access failed.\nFix: grant terminal/browser profile access, close the browser if needed, or rerun cuescribe setup cookies --browser chrome --profile Default.\n\n%w", err)
	}
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
	var strict bool
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check platform, dependencies, model, config, and cookies",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, err := config.LoadDefault()
			if err != nil {
				return err
			}
			failures := 0
			printCheck := func(ok bool, level, name, detail string) {
				if ok {
					fmt.Fprintf(cmd.OutOrStdout(), "ok    %s\n", name)
					return
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-5s %s: %s\n", level, name, detail)
			}
			check := func(ok bool, level, name, detail string) {
				printCheck(ok, level, name, detail)
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
			cookieFailures, err := runDoctorCookieChecks(cmd.Context(), cmd, cfg, strict, fix, shouldDebug(cmd), printCheck)
			if err != nil {
				return err
			}
			failures += cookieFailures
			if failures > 0 {
				return fmt.Errorf("doctor found %d error(s)", failures)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "fail when recommended YouTube cookies are disabled or broken")
	cmd.Flags().BoolVar(&fix, "fix", false, "interactively repair YouTube browser cookie configuration")
	return cmd
}

func runDoctorCookieChecks(ctx context.Context, cmd *cobra.Command, cfg config.Config, strict, fix, debug bool, check func(bool, string, string, string)) (int, error) {
	failures := 0
	report := func(ok bool, level, name, detail string) {
		check(ok, level, name, detail)
		if !ok && level == "error" {
			failures++
		}
	}
	if !cfg.Cookies.Enabled {
		if fix {
			repaired, repairedCfg, err := repairCookieConfig(ctx, cmd)
			if err != nil {
				return failures, err
			}
			if repaired {
				cfg.Cookies = repairedCfg.Cookies
			}
		}
		if !cfg.Cookies.Enabled {
			level := "warn"
			if strict {
				level = "error"
			}
			report(false, level, "cookies", "disabled; run cuescribe setup cookies --browser chrome --profile Default")
			return failures, nil
		}
	}
	if cfg.Cookies.Browser == "" {
		if fix {
			repaired, repairedCfg, err := repairCookieConfig(ctx, cmd)
			if err != nil {
				return failures, err
			}
			if repaired {
				cfg.Cookies = repairedCfg.Cookies
			}
		}
		if cfg.Cookies.Browser == "" {
			report(false, "error", "cookie config", "browser is required when cookies are enabled")
			return failures, nil
		}
	}
	if err := validateYouTubeCookieAccess(ctx, cmd, cfg.Cookies, debug); err != nil {
		if fix {
			repaired, repairedCfg, repairErr := repairCookieConfig(ctx, cmd)
			if repairErr != nil {
				return failures, repairErr
			}
			if repaired {
				cfg.Cookies = repairedCfg.Cookies
				err = validateYouTubeCookieAccess(ctx, cmd, cfg.Cookies, debug)
			}
		}
		if err != nil {
			report(false, "error", "YouTube cookie access", "check browser/profile access, or run cuescribe doctor --fix")
			return failures, nil
		}
	}
	report(true, "ok", "YouTube cookie access", "")
	return failures, nil
}

func shouldDebug(cmd *cobra.Command) bool {
	verbose, err := getBoolFlag(cmd, "verbose")
	if err == nil && verbose {
		return true
	}
	cookieDebug, err := getBoolFlag(cmd, "cookie-debug")
	if err == nil && cookieDebug {
		return true
	}
	debug, err := getBoolFlag(cmd, "debug")
	if err != nil {
		return false
	}
	return debug
}

func getBoolFlag(cmd *cobra.Command, name string) (bool, error) {
	if value, err := cmd.Flags().GetBool(name); err == nil {
		return value, nil
	}
	return cmd.InheritedFlags().GetBool(name)
}

func logWriter(opts rootOptions, logFile *os.File, errOut io.Writer) io.Writer {
	if opts.debug {
		return io.MultiWriter(logFile, errOut)
	}
	return logFile
}

func repairCookieConfig(ctx context.Context, cmd *cobra.Command) (bool, config.Config, error) {
	if !isInteractiveInput(cmd) {
		return false, config.Config{}, fmt.Errorf("Error: doctor --fix requires an interactive terminal.\nFix: run cuescribe setup cookies --browser chrome --profile Default")
	}
	fmt.Fprint(cmd.OutOrStdout(), "configure YouTube browser cookies now? [y/N] ")
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		return false, config.Config{}, nil
	}
	if err := runSetupCookies(ctx, cmd, cookieSetupOptions{
		Require:       true,
		Prompt:        true,
		AssumeConsent: true,
	}); err != nil {
		return false, config.Config{}, err
	}
	cfg, _, err := config.LoadDefault()
	if err != nil {
		return false, config.Config{}, err
	}
	return true, cfg, nil
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

func newUpgradeCommand() *cobra.Command {
	var updateVersion string
	var skipDependencies bool
	cmd := &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"self-update"},
		Short:   "Upgrade Cuescribe, and Homebrew dependencies by default",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := appupdate.SelfUpdate(cmd.Context(), updateVersion, cmd.OutOrStdout()); err != nil {
				return err
			}
			if skipDependencies {
				return saveInstallState(cmd)
			}
			if err := runUpgradeDeps(cmd.Context(), cmd); err != nil {
				return err
			}
			return saveInstallState(cmd)
		},
	}
	cmd.Flags().StringVar(&updateVersion, "version", "latest", "release version or latest")
	cmd.Flags().BoolVar(&skipDependencies, "skip-dependencies", false, "only update the Cuescribe binary")
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

type browserCandidate struct {
	name     string
	path     string
	bundleID string
}

var browserCandidates = []browserCandidate{
	{"safari", "/Applications/Safari.app", "com.apple.Safari"},
	{"chrome", "/Applications/Google Chrome.app", "com.google.Chrome"},
	{"firefox", "/Applications/Firefox.app", "org.mozilla.firefox"},
	{"brave", "/Applications/Brave Browser.app", "com.brave.Browser"},
	{"edge", "/Applications/Microsoft Edge.app", "com.microsoft.edgemac"},
}

func detectBrowsers() []string {
	var found []string
	for _, candidate := range browserCandidates {
		if fileExists(candidate.path) {
			found = append(found, candidate.name)
		}
	}
	return found
}

func detectDefaultBrowser() string {
	script := `ObjC.import("AppKit"); ObjC.import("Foundation"); var appURL=$.NSWorkspace.sharedWorkspace.URLForApplicationToOpenURL($.NSURL.URLWithString("https://example.com")); var bundle=$.NSBundle.bundleWithURL(appURL); bundle ? ObjC.unwrap(bundle.bundleIdentifier) : "";`
	out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return ""
	}
	bundleID := strings.TrimSpace(string(out))
	for _, candidate := range browserCandidates {
		if strings.EqualFold(candidate.bundleID, bundleID) {
			return candidate.name
		}
	}
	return ""
}

func preferredBrowser(detected []string, defaultBrowser string) string {
	if containsString(detected, defaultBrowser) {
		return defaultBrowser
	}
	if len(detected) == 0 {
		return ""
	}
	return detected[0]
}

func mergeDetectedDefaultBrowser(detected []string, defaultBrowser string) []string {
	if defaultBrowser == "" || containsString(detected, defaultBrowser) {
		return detected
	}
	return append([]string{defaultBrowser}, detected...)
}

func normalizeBrowserName(value string) string {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "google chrome", "chrome":
		return "chrome"
	case "apple safari", "safari":
		return "safari"
	case "mozilla firefox", "firefox":
		return "firefox"
	case "brave browser", "brave":
		return "brave"
	case "microsoft edge", "edge":
		return "edge"
	default:
		return trimmed
	}
}

func containsString(items []string, value string) bool {
	if value == "" {
		return false
	}
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

type browserProfile struct {
	ID   string
	Name string
}

func (p browserProfile) label() string {
	if p.Name == "" || p.Name == p.ID {
		return p.ID
	}
	return fmt.Sprintf("%s (%s)", p.Name, p.ID)
}

func promptChromeProfile(cmd *cobra.Command, reader *bufio.Reader) string {
	return promptChromeProfileFromProfiles(cmd, reader, detectChromeProfiles())
}

func promptChromeProfileFromProfiles(cmd *cobra.Command, reader *bufio.Reader, profiles []browserProfile) string {
	if len(profiles) == 0 {
		return promptOptionalProfile(cmd, reader)
	}
	if len(profiles) == 1 {
		fmt.Fprintf(cmd.OutOrStdout(), "chrome profile: %s\n", profiles[0].label())
		return profiles[0].ID
	}
	fmt.Fprintln(cmd.OutOrStdout(), "chrome profiles:")
	for i, profile := range profiles {
		fmt.Fprintf(cmd.OutOrStdout(), "%d. %s\n", i+1, profile.label())
	}
	defaultProfile := profiles[0]
	fmt.Fprintf(cmd.OutOrStdout(), "profile [%s]: ", defaultProfile.ID)
	selected, _ := reader.ReadString('\n')
	return resolveChromeProfileSelection(profiles, selected)
}

func resolveChromeProfileSelection(profiles []browserProfile, selected string) string {
	if len(profiles) == 0 {
		return strings.TrimSpace(selected)
	}
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return profiles[0].ID
	}
	if idx, err := strconv.Atoi(selected); err == nil && idx >= 1 && idx <= len(profiles) {
		return profiles[idx-1].ID
	}
	for _, profile := range profiles {
		if strings.EqualFold(selected, profile.ID) || strings.EqualFold(selected, profile.Name) {
			return profile.ID
		}
	}
	return selected
}

func promptOptionalProfile(cmd *cobra.Command, reader *bufio.Reader) string {
	fmt.Fprint(cmd.OutOrStdout(), "profile (optional): ")
	selected, _ := reader.ReadString('\n')
	return strings.TrimSpace(selected)
}

func detectChromeProfiles() []browserProfile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return detectChromeProfilesInHome(home)
}

func detectChromeProfilesInHome(home string) []browserProfile {
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	profiles := chromeProfilesFromLocalState(filepath.Join(root, "Local State"))
	if len(profiles) > 0 {
		return profiles
	}
	return chromeProfilesFromDirs(root)
}

func chromeProfilesFromLocalState(path string) []browserProfile {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}
	var profiles []browserProfile
	for id, info := range state.Profile.InfoCache {
		if isChromeProfileID(id) {
			profiles = append(profiles, browserProfile{ID: id, Name: info.Name})
		}
	}
	sortChromeProfiles(profiles)
	return profiles
}

func chromeProfilesFromDirs(root string) []browserProfile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var profiles []browserProfile
	for _, entry := range entries {
		if entry.IsDir() && isChromeProfileID(entry.Name()) {
			profiles = append(profiles, browserProfile{ID: entry.Name(), Name: entry.Name()})
		}
	}
	sortChromeProfiles(profiles)
	return profiles
}

func sortChromeProfiles(profiles []browserProfile) {
	sort.Slice(profiles, func(i, j int) bool {
		left := chromeProfileRank(profiles[i].ID)
		right := chromeProfileRank(profiles[j].ID)
		if left != right {
			return left < right
		}
		return profiles[i].ID < profiles[j].ID
	})
}

func chromeProfileRank(id string) int {
	if id == "Default" {
		return 0
	}
	if strings.HasPrefix(id, "Profile ") {
		n, err := strconv.Atoi(strings.TrimPrefix(id, "Profile "))
		if err == nil {
			return 100 + n
		}
	}
	return 1000
}

func isChromeProfileID(id string) bool {
	return id == "Default" || strings.HasPrefix(id, "Profile ")
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func isInteractiveInput(cmd *cobra.Command) bool {
	file, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	return isTerminal(file)
}
