package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devosurf/cuescribe/internal/config"
)

func TestPreferredBrowserUsesDetectedDefault(t *testing.T) {
	got := preferredBrowser([]string{"safari", "chrome"}, "chrome")
	if got != "chrome" {
		t.Fatalf("preferredBrowser() = %q, want chrome", got)
	}
}

func TestPreferredBrowserFallsBackToFirstDetected(t *testing.T) {
	got := preferredBrowser([]string{"safari", "chrome"}, "firefox")
	if got != "safari" {
		t.Fatalf("preferredBrowser() = %q, want safari", got)
	}
}

func TestMergeDetectedDefaultBrowserPrependsMissingDefault(t *testing.T) {
	got := mergeDetectedDefaultBrowser([]string{"safari"}, "chrome")
	want := []string{"chrome", "safari"}
	assertStrings(t, got, want)
}

func TestMergeDetectedDefaultBrowserKeepsExistingOrder(t *testing.T) {
	got := mergeDetectedDefaultBrowser([]string{"safari", "chrome"}, "chrome")
	want := []string{"safari", "chrome"}
	assertStrings(t, got, want)
}

func TestNormalizeBrowserName(t *testing.T) {
	tests := map[string]string{
		" Chrome ":        "chrome",
		"Google Chrome":   "chrome",
		"Safari":          "safari",
		"Mozilla Firefox": "firefox",
		"Microsoft Edge":  "edge",
		"custom":          "custom",
	}
	for input, want := range tests {
		got := normalizeBrowserName(input)
		if got != want {
			t.Fatalf("normalizeBrowserName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestListFormatsRequiresInput(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--list-formats"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--list-formats requires INPUT") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestShouldDebugIncludesDeprecatedCookieDebug(t *testing.T) {
	cmd := NewRootCommand()
	if err := cmd.PersistentFlags().Set("cookie-debug", "true"); err != nil {
		t.Fatalf("set cookie-debug: %v", err)
	}
	if !shouldDebug(cmd) {
		t.Fatalf("shouldDebug() = false, want true")
	}
}

func TestListFormatsRunsYTDLP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable uses /bin/sh")
	}
	binDir := t.TempDir()
	fakeYTDLP := filepath.Join(binDir, "yt-dlp")
	if err := os.WriteFile(fakeYTDLP, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir())

	cmd := NewRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--list-formats", "https://youtu.be/id"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; stderr = %q", err, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "--list-formats") || !strings.Contains(got, "https://youtu.be/id") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestUpgradeCommandIsPrimaryAndSelfUpdateAliasWorks(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "upgrade") {
		t.Fatalf("help missing upgrade command: %q", got)
	}
	if strings.Contains(got, "self-update") {
		t.Fatalf("help still exposes self-update command: %q", got)
	}

	found, _, err := NewRootCommand().Find([]string{"self-update"})
	if err != nil {
		t.Fatalf("Find(self-update) error = %v", err)
	}
	if found.Name() != "upgrade" {
		t.Fatalf("Find(self-update) = %q, want upgrade", found.Name())
	}
}

func TestUpgradeCommandHasSkipDependenciesFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"upgrade", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "--skip-dependencies") {
		t.Fatalf("upgrade help missing --skip-dependencies: %q", out.String())
	}
}

func TestRunUpgradeDepsRunsBrewUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	for _, name := range []string{"yt-dlp", "ffmpeg", "whisper-cli"} {
		writeFakeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nexit 0\n")
	}
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$BREW_LOG\"\nif [ \"$1\" = \"list\" ]; then\n  shift 2\n  for pkg in \"$@\"; do\n    echo \"$pkg 1.0.0\"\n  done\n  exit 0\nfi\nif [ \"$1\" = \"outdated\" ]; then\n  shift 3\n  for pkg in \"$@\"; do\n    echo \"$pkg\"\n  done\n  exit 0\nfi\nexit 0\n")
	logPath := filepath.Join(t.TempDir(), "brew.log")
	t.Setenv("PATH", binDir)
	t.Setenv("BREW_LOG", logPath)

	cmd := NewRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := runUpgradeDeps(context.Background(), cmd); err != nil {
		t.Fatalf("runUpgradeDeps() error = %v; stderr = %q", err, errOut.String())
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "list") || !strings.Contains(got, "--versions") {
		t.Fatalf("brew args = %q, expected list check", got)
	}
	for _, want := range []string{"upgrade", "yt-dlp", "ffmpeg", "whisper-cpp"} {
		if !strings.Contains(got, want) {
			t.Fatalf("brew args = %q, missing %q", got, want)
		}
	}
}

func TestRunUpgradeDepsInstallsMissingFormula(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	for _, name := range []string{"yt-dlp", "ffmpeg", "whisper-cli"} {
		writeFakeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nexit 0\n")
	}
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$BREW_LOG\"\nif [ \"$1\" = \"list\" ]; then\n  shift 2\n  status=0\n  for pkg in \"$@\"; do\n    if [ \"$pkg\" = \"whisper-cpp\" ]; then\n      status=1\n    else\n      echo \"$pkg 1.0.0\"\n    fi\n  done\n  exit $status\nfi\nif [ \"$1\" = \"outdated\" ]; then\n  shift 3\n  for pkg in \"$@\"; do\n    echo \"$pkg\"\n  done\n  exit 0\nfi\nexit 0\n")
	logPath := filepath.Join(t.TempDir(), "brew.log")
	t.Setenv("PATH", binDir)
	t.Setenv("BREW_LOG", logPath)

	cmd := NewRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := runUpgradeDeps(context.Background(), cmd); err != nil {
		t.Fatalf("runUpgradeDeps() error = %v; stderr = %q", err, errOut.String())
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "\ninstall\nwhisper-cpp\n") {
		t.Fatalf("brew args = %q, expected install whisper-cpp", got)
	}
	if !strings.Contains(got, "\nupgrade\nyt-dlp\nffmpeg\n") {
		t.Fatalf("brew args = %q, expected upgrade yt-dlp ffmpeg", got)
	}
}

func TestRunBrewCommandIncludesCommandOnSilentFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", binDir)

	cmd := NewRootCommand()
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	err := runBrewCommand(context.Background(), cmd, "upgrade", []string{"whisper-cpp"})
	if err == nil {
		t.Fatal("runBrewCommand() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "brew upgrade whisper-cpp") {
		t.Fatalf("runBrewCommand() error = %v", err)
	}
}

func TestRunUpgradeDepsNoOpWhenAllDependenciesCurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	for _, name := range []string{"yt-dlp", "ffmpeg", "whisper-cli"} {
		writeFakeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nexit 0\n")
	}
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$BREW_LOG\"\nif [ \"$1\" = \"list\" ]; then\n  shift 2\n  for pkg in \"$@\"; do\n    echo \"$pkg 1.0.0\"\n  done\n  exit 0\nfi\nif [ \"$1\" = \"outdated\" ]; then\n  exit 0\nfi\nexit 0\n")
	logPath := filepath.Join(t.TempDir(), "brew.log")
	t.Setenv("PATH", binDir)
	t.Setenv("BREW_LOG", logPath)

	cmd := NewRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := runUpgradeDeps(context.Background(), cmd); err != nil {
		t.Fatalf("runUpgradeDeps() error = %v; stderr = %q", err, errOut.String())
	}
	if !strings.Contains(out.String(), "All dependencies are up to date.") {
		t.Fatalf("runUpgradeDeps() output = %q", out.String())
	}
	data, err := os.ReadFile(logPath)
	if err == nil && strings.Contains(string(data), "\nupgrade\n") {
		t.Fatalf("brew args = %q, unexpected upgrade call", string(data))
	}
}

func TestBrewOutdatedPackages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nif [ \"$1\" = \"outdated\" ]; then\n  echo 'yt-dlp'\n  exit 1\nfi\nexit 0\n")
	t.Setenv("PATH", binDir)

	outdated, err := brewOutdatedPackages(context.Background(), []string{"yt-dlp", "ffmpeg"})
	if err != nil {
		t.Fatalf("brewOutdatedPackages() error = %v", err)
	}
	if !outdated["yt-dlp"] {
		t.Fatalf("brewOutdatedPackages() missing yt-dlp: %v", outdated)
	}
	if outdated["ffmpeg"] {
		t.Fatalf("brewOutdatedPackages() unexpectedly includes ffmpeg: %v", outdated)
	}
}

func TestBrewInstalledPackagesTreatsSilentExitOneAsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	// Real brew prints versions for installed formulas and exits 1 with no
	// output at all for missing ones.
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nif [ \"$1\" = \"list\" ]; then\n  echo 'yt-dlp 2026.03.17'\n  echo 'ffmpeg 7.1'\n  exit 1\nfi\nexit 0\n")
	t.Setenv("PATH", binDir)

	installed, err := brewInstalledPackages(context.Background(), []string{"yt-dlp", "ffmpeg", "whisper-cpp"})
	if err != nil {
		t.Fatalf("brewInstalledPackages() error = %v", err)
	}
	if !installed["yt-dlp"] || !installed["ffmpeg"] {
		t.Fatalf("brewInstalledPackages() missing installed packages: %v", installed)
	}
	if installed["whisper-cpp"] {
		t.Fatalf("brewInstalledPackages() unexpectedly includes whisper-cpp: %v", installed)
	}
}

func TestBrewInstalledPackagesStripsTapPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nif [ \"$1\" = \"list\" ]; then\n  echo 'homebrew/core/ffmpeg 7.1'\n  exit 0\nfi\nexit 0\n")
	t.Setenv("PATH", binDir)

	installed, err := brewInstalledPackages(context.Background(), []string{"ffmpeg"})
	if err != nil {
		t.Fatalf("brewInstalledPackages() error = %v", err)
	}
	if !installed["ffmpeg"] {
		t.Fatalf("brewInstalledPackages() = %v, want ffmpeg", installed)
	}
}

func TestEnsureDependenciesSkipsWhenAllPresentAndCurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	for _, name := range []string{"yt-dlp", "ffmpeg", "whisper-cli"} {
		writeFakeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo '2026.03.17'\nelse\n  exit 0\nfi\n")
	}
	t.Setenv("PATH", binDir)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(&bytes.Buffer{})
	if err := ensureDependencies(context.Background(), cmd, "https://www.youtube.com/watch?v=abc", false); err != nil {
		t.Fatalf("ensureDependencies() error = %v; stdout = %q", err, out.String())
	}
}

func TestEnsureDependenciesErrorsWhenMissingDependenciesNonInteractive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	// Deliberately omit yt-dlp to force a dependency gap.
	writeFakeExecutable(t, filepath.Join(binDir, "ffmpeg"), "#!/bin/sh\nexit 0\n")
	writeFakeExecutable(t, filepath.Join(binDir, "whisper-cli"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)
	cmd := NewRootCommand()
	cmd.SetIn(strings.NewReader("y\n"))
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := ensureDependencies(context.Background(), cmd, "https://www.youtube.com/watch?v=abc", false)
	if err == nil {
		t.Fatal("ensureDependencies() expected non-interactive missing dependency error, got nil")
	}
	if !strings.Contains(err.Error(), "run cuescribe upgrade") {
		t.Fatalf("ensureDependencies() error = %q", err)
	}
}

func TestEnsureDependenciesFailsNonInteractiveWhenYTDLPOld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executables use /bin/sh")
	}
	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "yt-dlp"), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo '2026.02.04'\nelse\n  exit 0\nfi\n")
	writeFakeExecutable(t, filepath.Join(binDir, "ffmpeg"), "#!/bin/sh\nexit 0\n")
	writeFakeExecutable(t, filepath.Join(binDir, "whisper-cli"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)
	cmd := NewRootCommand()
	cmd.SetIn(&bytes.Buffer{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := ensureDependencies(context.Background(), cmd, "https://www.youtube.com/watch?v=abc", false)
	if err == nil {
		t.Fatalf("ensureDependencies() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing or out of date") || !strings.Contains(err.Error(), "run cuescribe upgrade") {
		t.Fatalf("ensureDependencies() error = %q", err)
	}
}

func TestRequiredDependenciesForInput(t *testing.T) {
	got := requiredDependenciesForInput("https://youtu.be/example", false)
	assertStrings(t, got, []string{"yt-dlp", "ffmpeg", "whisper-cli"})
	got = requiredDependenciesForInput("/path/to/file.mp4", false)
	assertStrings(t, got, []string{"ffmpeg", "whisper-cli"})
	got = requiredDependenciesForInput("https://youtu.be/example", true)
	assertStrings(t, got, []string{"yt-dlp"})
}

func TestRunSetupCookiesRequireNonInteractiveNeedsBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(bytes.NewBuffer(nil))
	err := runSetupCookies(context.Background(), cmd, cookieSetupOptions{Require: true})
	if err == nil || !strings.Contains(err.Error(), "cookies are required in non-interactive setup") {
		t.Fatalf("runSetupCookies() error = %v", err)
	}
}

func TestRunSetupCookiesProfileNeedsBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := NewRootCommand()
	err := runSetupCookies(context.Background(), cmd, cookieSetupOptions{Profile: "Default"})
	if err == nil || !strings.Contains(err.Error(), "profile requires a browser") {
		t.Fatalf("runSetupCookies() error = %v", err)
	}
}

func TestValidateYouTubeCookieAccessWritesDebugOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable uses /bin/sh")
	}
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "yt-dlp"), "#!/bin/sh\n>&2 echo \"yt-dlp cookie debug line\"\nexit 1\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cmd := NewRootCommand()
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	err := validateYouTubeCookieAccess(context.Background(), cmd, config.CookieConfig{
		Enabled: true,
		Browser: "chrome",
		Profile: "Default",
	}, true)
	if err == nil {
		t.Fatal("expected validateYouTubeCookieAccess to fail")
	}
	if !strings.Contains(errOut.String(), "debug: validating YouTube cookie access") {
		t.Fatalf("debug output missing: %q", errOut.String())
	}
}

func TestRunSetupCookiesExplicitBrowserValidatesAndSaves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable uses /bin/sh")
	}
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "yt-dlp"), "#!/bin/sh\nexit 0\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runSetupCookies(context.Background(), cmd, cookieSetupOptions{
		Browser: "chrome",
		Profile: "Default",
		Require: true,
	}); err != nil {
		t.Fatalf("runSetupCookies() error = %v", err)
	}
	paths := config.PathsForHome(home)
	cfg, err := config.Load(paths.ConfigFile, config.Default(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Cookies.Enabled || cfg.Cookies.Browser != "chrome" || cfg.Cookies.Profile != "Default" {
		t.Fatalf("cookies = %+v", cfg.Cookies)
	}
}

func TestDoctorStrictFailsWhenCookiesDisabled(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	failures, err := runDoctorCookieChecks(context.Background(), cmd, config.Config{}, true, false, false, func(ok bool, level, name, detail string) {
		if ok {
			fmt.Fprintf(&out, "ok    %s\n", name)
			return
		}
		fmt.Fprintf(&out, "%-5s %s: %s\n", level, name, detail)
	})
	if err != nil {
		t.Fatalf("runDoctorCookieChecks() error = %v", err)
	}
	if failures != 1 || !strings.Contains(out.String(), "error cookies: disabled") {
		t.Fatalf("failures = %d, output = %q", failures, out.String())
	}
}

func TestChromeProfilesFromLocalState(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"profile": {
			"info_cache": {
				"Profile 10": {"name": "Archive"},
				"System Profile": {"name": "System"},
				"Default": {"name": "Personal"},
				"Profile 2": {"name": "Work"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}

	profiles := detectChromeProfilesInHome(home)
	assertProfileIDs(t, profiles, []string{"Default", "Profile 2", "Profile 10"})
	if profiles[0].Name != "Personal" || profiles[1].Name != "Work" || profiles[2].Name != "Archive" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestChromeProfilesFromDirs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	for _, name := range []string{"Profile 1", "Guest Profile", "Default"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	profiles := detectChromeProfilesInHome(home)
	assertProfileIDs(t, profiles, []string{"Default", "Profile 1"})
}

func TestResolveChromeProfileSelection(t *testing.T) {
	profiles := []browserProfile{
		{ID: "Default", Name: "Personal"},
		{ID: "Profile 2", Name: "Work"},
	}
	tests := []struct {
		name     string
		selected string
		want     string
	}{
		{name: "blank uses default", selected: "\n", want: "Default"},
		{name: "number", selected: "2\n", want: "Profile 2"},
		{name: "profile name", selected: "Work\n", want: "Profile 2"},
		{name: "profile id", selected: "Profile 2\n", want: "Profile 2"},
		{name: "custom value", selected: "Canary\n", want: "Canary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveChromeProfileSelection(profiles, tt.selected)
			if got != tt.want {
				t.Fatalf("resolveChromeProfileSelection() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptChromeProfileUsesOnlyProfile(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	got := promptChromeProfileFromProfiles(cmd, bufio.NewReader(strings.NewReader("")), []browserProfile{
		{ID: "Default", Name: "Personal"},
	})
	if got != "Default" {
		t.Fatalf("profile = %q, want Default", got)
	}
	if !strings.Contains(out.String(), "chrome profile: Personal (Default)") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPromptChromeProfileEnterChoosesDefaultProfile(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	got := promptChromeProfileFromProfiles(cmd, bufio.NewReader(strings.NewReader("\n")), []browserProfile{
		{ID: "Default", Name: "Personal"},
		{ID: "Profile 2", Name: "Work"},
	})
	if got != "Default" {
		t.Fatalf("profile = %q, want Default", got)
	}
	if !strings.Contains(out.String(), "chrome profiles:") || !strings.Contains(out.String(), "profile [Default]:") {
		t.Fatalf("output = %q", out.String())
	}
}

func writeFakeExecutable(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertProfileIDs(t *testing.T, profiles []browserProfile, want []string) {
	t.Helper()
	if len(profiles) != len(want) {
		t.Fatalf("len(profiles) = %d, want %d: %+v", len(profiles), len(want), profiles)
	}
	for i := range want {
		if profiles[i].ID != want[i] {
			t.Fatalf("profiles[%d].ID = %q, want %q: %+v", i, profiles[i].ID, want[i], profiles)
		}
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q: %v", i, got[i], want[i], got)
		}
	}
}
