package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Base(exe) != "cuescribe" {
		return fmt.Errorf("Error: current executable is not an installed cuescribe binary: %s\nFix: install with scripts/install.sh or replace the binary manually", exe)
	}
	tmp, err := os.MkdirTemp("", "cuescribe-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	binaryPath := filepath.Join(tmp, "cuescribe")
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

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func FetchManifest(ctx context.Context, version string) (Manifest, error) {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	bar := progress.NewBar(out, "Downloading update", resp.ContentLength)
	bar.Start(0)
	written, err := copyWithProgress(f, resp.Body, bar)
	if err != nil {
		return err
	}
	bar.Finish(fmt.Sprintf("downloaded %s", progress.HumanBytes(written)))
	return nil
}

func copyWithProgress(dst io.Writer, src io.Reader, bar *progress.Bar) (int64, error) {
	buf := make([]byte, 1024*1024)
	var written int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
			written += int64(nw)
			if bar != nil {
				bar.Set(written)
			}
		}
		if er == io.EOF {
			return written, nil
		}
		if er != nil {
			return written, er
		}
	}
}
