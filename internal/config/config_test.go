package config

import (
	"path/filepath"
	"testing"
)

func TestPathsForHome(t *testing.T) {
	paths := PathsForHome("/home/tester")
	if paths.ConfigFile != "/home/tester/.config/cuescribe/config.toml" {
		t.Fatalf("ConfigFile = %s", paths.ConfigFile)
	}
	if paths.ModelDir != "/home/tester/.local/share/cuescribe/models" {
		t.Fatalf("ModelDir = %s", paths.ModelDir)
	}
}

func TestLoadSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	defaults := Default(PathsForHome(t.TempDir()))
	cfg := defaults
	cfg.Model.Name = "base"
	cfg.Model.Path = "/tmp/model.bin"
	cfg.Cookies.Enabled = true
	cfg.Cookies.Browser = "safari"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Model.Name != "base" || loaded.Model.Path != "/tmp/model.bin" {
		t.Fatalf("loaded model = %+v", loaded.Model)
	}
	if !loaded.Cookies.Enabled || loaded.Cookies.Browser != "safari" {
		t.Fatalf("loaded cookies = %+v", loaded.Cookies)
	}
}

func TestLoadSaveInstallState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.toml")
	state := InstallState{Version: "0.1.0", BinaryPath: "/usr/local/bin/cuescribe"}
	if err := SaveInstallState(path, state); err != nil {
		t.Fatalf("SaveInstallState() error = %v", err)
	}
	loaded, err := LoadInstallState(path)
	if err != nil {
		t.Fatalf("LoadInstallState() error = %v", err)
	}
	if loaded.Version != "0.1.0" || loaded.BinaryPath != "/usr/local/bin/cuescribe" || loaded.InstalledAt == "" {
		t.Fatalf("loaded = %+v", loaded)
	}
}
