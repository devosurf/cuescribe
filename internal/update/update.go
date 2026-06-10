package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/download"
	"github.com/devosurf/cuescribe/internal/model"
	"github.com/devosurf/cuescribe/internal/progress"
	"github.com/devosurf/cuescribe/internal/version"
)

type Manifest struct {
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	BinaryURL    string `json:"binary_url"`
	BinarySHA256 string `json:"binary_sha256"`
}

// versionPattern matches release tags such as v0.1.15 or 0.2.0-rc.1. It
// rejects anything that could alter the manifest URL path (slashes, "..",
// query separators).
var versionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+([-+.][A-Za-z0-9.-]+)?$`)

// allowedBinaryHosts are the only hosts a manifest's binary_url may point
// to. The SHA256 in the manifest comes from the same source as the URL, so
// pinning the host is what keeps a tampered manifest from redirecting the
// download to arbitrary infrastructure.
var allowedBinaryHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

func SelfUpdate(ctx context.Context, target string, out io.Writer) error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("Error: unsupported platform %s/%s.\nFix: Cuescribe v1 supports macOS Apple Silicon only", runtime.GOOS, runtime.GOARCH)
	}
	progress.Step(out, "Checking release manifest")
	manifest, err := FetchManifest(ctx, target)
	if err != nil {
		return err
	}
	targetVersion := normalizeVersion(manifest.Version)
	if targetVersion == normalizeVersion(version.Current().Version) {
		fmt.Fprintf(out, "cuescribe %s is already up to date\n", version.Current().Version)
		return nil
	}
	if manifest.BinaryURL == "" || manifest.BinarySHA256 == "" {
		return fmt.Errorf("manifest is missing binary_url or binary_sha256")
	}
	if err := validateBinaryURL(manifest.BinaryURL); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Base(exe) != "cuescribe" {
		return fmt.Errorf("Error: current executable is not an installed cuescribe binary: %s\nFix: install with scripts/install.sh or replace the binary manually", exe)
	}
	// Stage the new binary next to the installed one so both renames below
	// are atomic same-filesystem operations; a temp dir could sit on another
	// volume, where os.Rename fails with EXDEV.
	binaryPath := exe + ".new"
	_ = os.Remove(binaryPath)
	defer os.Remove(binaryPath)
	if err := downloadFile(ctx, manifest.BinaryURL, binaryPath, out); err != nil {
		return err
	}
	progress.Step(out, "Verifying checksum")
	if err := model.VerifyFile(binaryPath, manifest.BinarySHA256); err != nil {
		return err
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return err
	}
	backup := exe + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return err
	}
	if err := os.Rename(binaryPath, exe); err != nil {
		_ = os.Rename(backup, exe)
		return err
	}
	_ = os.Remove(backup)
	fmt.Fprintf(out, "updated %s to %s\n", exe, manifest.Version)
	return nil
}

func validateBinaryURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("manifest binary_url is not a valid URL: %w", err)
	}
	if u.Scheme != "https" || !allowedBinaryHosts[u.Hostname()] {
		return fmt.Errorf("manifest binary_url must be an https GitHub URL, got %q", raw)
	}
	return nil
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func FetchManifest(ctx context.Context, version string) (Manifest, error) {
	version = strings.TrimSpace(version)
	if version != "" && version != "latest" && !versionPattern.MatchString(version) {
		return Manifest{}, fmt.Errorf("Error: invalid version %q.\nFix: pass a release tag such as v0.1.15, or omit --version for the latest release", version)
	}
	url := manifestURL(version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Manifest{}, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("manifest download failed: %s", resp.Status)
	}
	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func manifestURL(version string) string {
	if version == "" || version == "latest" {
		return "https://github.com/devosurf/cuescribe/releases/latest/download/manifest.json"
	}
	return fmt.Sprintf("https://github.com/devosurf/cuescribe/releases/download/%s/manifest.json", version)
}

func downloadFile(ctx context.Context, url, path string, out io.Writer) error {
	ctx, guard := download.NewStallGuard(ctx, download.DefaultStallTimeout)
	defer guard.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// No client timeout: the binary download may legitimately take a long
	// time; the stall guard aborts it if data stops flowing.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return guard.Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	bar := progress.NewBar(out, "Downloading update", resp.ContentLength)
	written, copyErr := download.Copy(f, guard.Reader(resp.Body), bar, 0)
	closeErr := f.Close()
	if copyErr != nil {
		return guard.Wrap(copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	bar.Finish(fmt.Sprintf("downloaded %s", progress.HumanBytes(written)))
	return nil
}
