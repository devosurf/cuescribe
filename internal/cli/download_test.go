package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devosurf/cuescribe/internal/config"
)

func TestDownloadUsesCookiesOnlyForYouTubeWithoutTranscriptionTools(t *testing.T) {
	for _, tc := range []struct {
		name    string
		url     string
		cookies string
	}{
		{"YouTube", "https://youtu.be/example", "chrome:Profile 1"},
		{"other host", "https://youtube.com.example.org/video", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupAudioDownload(t)
			t.Setenv("EXPECTED_COOKIES", tc.cookies)
			outDir := t.TempDir()
			cmd := NewRootCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"download", tc.url, "-o", outDir})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("download: %v; stderr: %s", err, &stderr)
			}
			got, err := os.ReadFile(filepath.Join(outDir, "Car_Talk.opus"))
			if err != nil || string(got) != "audio payload" {
				t.Fatalf("saved audio = %q, error = %v", got, err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("download leaked output to stdout: %q", &stdout)
			}
			assertDownloadStagingRemoved(t, paths.CacheDir)
		})
	}
}

func TestFailedDownloadPreservesExistingOutputAndRemovesPartialMedia(t *testing.T) {
	paths := setupAudioDownload(t)
	t.Setenv("EXPECTED_COOKIES", "chrome:Profile 1")
	t.Setenv("FAIL_DOWNLOAD", "1")
	out := filepath.Join(t.TempDir(), "existing.opus")
	if err := os.WriteFile(out, []byte("original audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"download", "https://youtu.be/example", "-o", out, "--force"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("failed download reported success")
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "original audio" {
		t.Fatalf("existing audio = %q, error = %v", got, err)
	}
	assertDownloadStagingRemoved(t, paths.CacheDir)
}

func setupAudioDownload(t *testing.T) config.Paths {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("download CLI supports macOS Apple Silicon")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FAIL_DOWNLOAD", "")
	paths := config.PathsForHome(os.Getenv("HOME"))
	cfg := config.Default(paths)
	cfg.Cookies = config.CookieConfig{Enabled: true, Browser: "chrome", Profile: "Profile 1"}
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	// No Whisper, llama.cpp, models, or Homebrew are available to this command.
	t.Setenv("PATH", binDir)
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		writeFakeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nexit 1\n")
	}
	writeFakeExecutable(t, filepath.Join(binDir, "yt-dlp"), `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '2026.07.04\n'
  exit 0
fi
cookies=
metadata=
out=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --cookies-from-browser) shift; cookies="$1" ;;
    --dump-json) metadata=yes ;;
    -o) shift; out="$1" ;;
  esac
  shift
done
if [ "$cookies" != "$EXPECTED_COOKIES" ]; then
  printf 'cookie access policy violation\n' >&2
  exit 1
fi
if [ "$metadata" = yes ]; then
  printf '{"title":"Car Talk","duration":60}\n'
  exit 0
fi
out="${out%.*}.opus"
printf 'audio payload' > "$out"
if [ "$FAIL_DOWNLOAD" = 1 ]; then
  printf 'HTTP Error 403: Forbidden\n' >&2
  exit 1
fi
printf '%s\n' "$out"
`)
	return paths
}

func assertDownloadStagingRemoved(t *testing.T, cacheDir string) {
	t.Helper()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "download-") {
			t.Fatalf("download left staging data: %s", entry.Name())
		}
	}
}
