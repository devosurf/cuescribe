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
	"time"

	"github.com/devosurf/cuescribe/internal/model"
)

type Manifest struct {
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	BinaryURL    string `json:"binary_url"`
	BinarySHA256 string `json:"binary_sha256"`
}

func SelfUpdate(ctx context.Context, version string, progress io.Writer) error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("Error: unsupported platform %s/%s.\nFix: Cuescribe v1 supports macOS Apple Silicon only", runtime.GOOS, runtime.GOARCH)
	}
	manifest, err := FetchManifest(ctx, version)
	if err != nil {
		return err
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
	fmt.Fprintf(progress, "downloading %s\n", manifest.BinaryURL)
	if err := downloadFile(ctx, manifest.BinaryURL, binaryPath); err != nil {
		return err
	}
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
	fmt.Fprintf(progress, "updated %s to %s\n", exe, manifest.Version)
	return nil
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

func downloadFile(ctx context.Context, url, path string) error {
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
	_, err = io.Copy(f, resp.Body)
	return err
}
