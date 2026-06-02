package model

import (
	"os"
	"path/filepath"
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
