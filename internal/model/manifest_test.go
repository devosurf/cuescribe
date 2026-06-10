package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"); err != nil {
		t.Fatalf("VerifyFile() error = %v", err)
	}
	if err := VerifyFile(path, "bad"); err == nil {
		t.Fatal("VerifyFile() error = nil, want mismatch")
	}
}

func TestManifestIncludesDefaultSmallModel(t *testing.T) {
	entry, ok := Get("small")
	if !ok {
		t.Fatal("Get(small) did not find model")
	}
	if entry.File != "ggml-small.bin" || entry.SHA256 == "" || entry.URL == "" {
		t.Fatalf("entry = %+v", entry)
	}
}

type rangeServer struct {
	content  []byte
	requests []string // recorded Range headers, "" when absent
	srv      *httptest.Server
}

func newRangeServer(t *testing.T, content []byte) *rangeServer {
	t.Helper()
	rs := &rangeServer{content: content}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		rs.requests = append(rs.requests, rangeHeader)
		if rangeHeader == "" {
			w.Write(rs.content)
			return
		}
		var start int64
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); err != nil || start >= int64(len(rs.content)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(rs.content[start:])
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func testEntry(rs *rangeServer) Entry {
	sum := sha256.Sum256(rs.content)
	return Entry{
		Name:   "test-model",
		File:   "test-model.bin",
		URL:    rs.srv.URL,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(rs.content)),
	}
}

func TestDownloadResumesValidPart(t *testing.T) {
	content := bytes.Repeat([]byte("cuescribe"), 1000)
	rs := newRangeServer(t, content)
	entry := testEntry(rs)
	dest := filepath.Join(t.TempDir(), entry.File)
	if err := os.WriteFile(dest+".part", content[:1234], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), entry, dest, io.Discard); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if err := VerifyFile(dest, entry.SHA256); err != nil {
		t.Fatalf("VerifyFile() error = %v", err)
	}
	if len(rs.requests) != 1 || rs.requests[0] != "bytes=1234-" {
		t.Fatalf("requests = %v, want one resume request", rs.requests)
	}
}

func TestDownloadDiscardsOversizedPart(t *testing.T) {
	content := bytes.Repeat([]byte("cuescribe"), 1000)
	rs := newRangeServer(t, content)
	entry := testEntry(rs)
	dest := filepath.Join(t.TempDir(), entry.File)
	oversized := append(append([]byte{}, content...), []byte("garbage from overlapping downloads")...)
	if err := os.WriteFile(dest+".part", oversized, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Download(context.Background(), entry, dest, &out); err != nil {
		t.Fatalf("Download() error = %v; output = %q", err, out.String())
	}
	if err := VerifyFile(dest, entry.SHA256); err != nil {
		t.Fatalf("VerifyFile() error = %v", err)
	}
	if !strings.Contains(out.String(), "restarting from scratch") {
		t.Fatalf("output missing restart warning: %q", out.String())
	}
	if len(rs.requests) != 1 || rs.requests[0] != "" {
		t.Fatalf("requests = %v, want one full download", rs.requests)
	}
}

func TestDownloadRecoversFrom416(t *testing.T) {
	content := bytes.Repeat([]byte("cuescribe"), 1000)
	rs := newRangeServer(t, content)
	entry := testEntry(rs)
	// Lie about the size so the oversized-part preflight passes and the
	// server's 416 is what triggers recovery.
	entry.Size = int64(len(content)) * 2
	dest := filepath.Join(t.TempDir(), entry.File)
	if err := os.WriteFile(dest+".part", content, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Download(context.Background(), entry, dest, &out)
	// Recovery re-downloads the real content, whose checksum matches.
	if err != nil {
		t.Fatalf("Download() error = %v; output = %q", err, out.String())
	}
	if err := VerifyFile(dest, entry.SHA256); err != nil {
		t.Fatalf("VerifyFile() error = %v", err)
	}
	if len(rs.requests) != 2 || rs.requests[0] == "" || rs.requests[1] != "" {
		t.Fatalf("requests = %v, want 416d resume then full download", rs.requests)
	}
}

func TestDownloadCompletedPartIsRenamedWithoutNetwork(t *testing.T) {
	content := bytes.Repeat([]byte("cuescribe"), 1000)
	rs := newRangeServer(t, content)
	entry := testEntry(rs)
	dest := filepath.Join(t.TempDir(), entry.File)
	if err := os.WriteFile(dest+".part", content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), entry, dest, io.Discard); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if err := VerifyFile(dest, entry.SHA256); err != nil {
		t.Fatalf("VerifyFile() error = %v", err)
	}
	if len(rs.requests) != 0 {
		t.Fatalf("requests = %v, want no network use", rs.requests)
	}
}

func TestDownloadRemovesPartOnFinalChecksumMismatch(t *testing.T) {
	content := bytes.Repeat([]byte("cuescribe"), 1000)
	rs := newRangeServer(t, content)
	entry := testEntry(rs)
	entry.SHA256 = strings.Repeat("0", 64) // pinned hash will never match
	dest := filepath.Join(t.TempDir(), entry.File)
	err := Download(context.Background(), entry, dest, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Download() error = %v, want checksum mismatch", err)
	}
	if _, statErr := os.Stat(dest + ".part"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("corrupt .part left behind: %v", statErr)
	}
	if len(rs.requests) != 2 {
		t.Fatalf("requests = %v, want initial download plus one retry", rs.requests)
	}
}
