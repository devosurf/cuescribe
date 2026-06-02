package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/devosurf/cuescribe/internal/progress"
)

type Entry struct {
	Name   string
	File   string
	URL    string
	SHA256 string
	Size   int64
}

var entries = map[string]Entry{
	"tiny": {
		Name:   "tiny",
		File:   "ggml-tiny.bin",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.bin?download=true",
		SHA256: "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21",
		Size:   77691713,
	},
	"base": {
		Name:   "base",
		File:   "ggml-base.bin",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin?download=true",
		SHA256: "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe",
		Size:   147951465,
	},
	"small": {
		Name:   "small",
		File:   "ggml-small.bin",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin?download=true",
		SHA256: "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
		Size:   487601967,
	},
	"medium": {
		Name:   "medium",
		File:   "ggml-medium.bin",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.bin?download=true",
		SHA256: "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208",
		Size:   1533763059,
	},
	"large-v3-turbo": {
		Name:   "large-v3-turbo",
		File:   "ggml-large-v3-turbo.bin",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin?download=true",
		SHA256: "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69",
		Size:   1624555275,
	},
}

func Get(name string) (Entry, bool) {
	entry, ok := entries[name]
	return entry, ok
}

func Names() []string {
	return []string{"tiny", "base", "small", "medium", "large-v3-turbo"}
}

func VerifyFile(path, want string) error {
	got, err := SHA256File(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", path, got, want)
	}
	return nil
}

func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Download(ctx context.Context, entry Entry, dest string, out io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		if err := VerifyFile(dest, entry.SHA256); err == nil {
			fmt.Fprintf(out, "model already present: %s\n", dest)
			return nil
		}
	}
	partPath := dest + ".part"
	start := fileSize(partPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	appendMode := start > 0 && resp.StatusCode == http.StatusPartialContent
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("model download failed: %s", resp.Status)
	}
	if start > 0 && !appendMode {
		start = 0
	}
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flag, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(out, "model: %s (%s)\n", entry.Name, progress.HumanBytes(entry.Size))
	fmt.Fprintf(out, "destination: %s\n", dest)
	if start > 0 {
		fmt.Fprintf(out, "resuming at %s\n", progress.HumanBytes(start))
	}
	bar := progress.NewBar(out, "Downloading model", entry.Size)
	written, err := copyWithProgress(f, resp.Body, bar, start)
	if err != nil {
		return err
	}
	bar.Finish(fmt.Sprintf("downloaded %s", progress.HumanBytes(written)))
	progress.Step(out, "Verifying checksum")
	if err := VerifyFile(partPath, entry.SHA256); err != nil {
		return err
	}
	if err := os.Rename(partPath, dest); err != nil {
		return err
	}
	fmt.Fprintf(out, "model installed: %s\n", dest)
	return nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func copyWithProgress(dst io.Writer, src io.Reader, bar *progress.Bar, start int64) (int64, error) {
	buf := make([]byte, 1024*1024)
	written := start
	if bar != nil {
		bar.Start(start)
	}
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
