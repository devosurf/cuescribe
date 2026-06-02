package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Paths struct {
	ConfigFile  string
	ConfigDir   string
	DataDir     string
	ModelDir    string
	StateDir    string
	InstallFile string
	CacheDir    string
	LogDir      string
}

type Config struct {
	Model   ModelConfig  `toml:"model"`
	Cookies CookieConfig `toml:"cookies"`
}

type ModelConfig struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

type CookieConfig struct {
	Enabled bool   `toml:"enabled"`
	Browser string `toml:"browser"`
	Profile string `toml:"profile"`
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return PathsForHome(home), nil
}

func PathsForHome(home string) Paths {
	configDir := filepath.Join(home, ".config", "cuescribe")
	dataDir := filepath.Join(home, ".local", "share", "cuescribe")
	stateDir := filepath.Join(home, ".local", "state", "cuescribe")
	cacheDir := filepath.Join(home, ".cache", "cuescribe")
	return Paths{
		ConfigFile:  filepath.Join(configDir, "config.toml"),
		ConfigDir:   configDir,
		DataDir:     dataDir,
		ModelDir:    filepath.Join(dataDir, "models"),
		StateDir:    stateDir,
		InstallFile: filepath.Join(stateDir, "install.toml"),
		CacheDir:    cacheDir,
		LogDir:      filepath.Join(cacheDir, "logs"),
	}
}

func Default(paths Paths) Config {
	return Config{
		Model: ModelConfig{
			Name: "small",
			Path: filepath.Join(paths.ModelDir, "ggml-small.bin"),
		},
		Cookies: CookieConfig{Enabled: false},
	}
}

func Load(path string, defaults Config) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg := defaults
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func LoadDefault() (Config, Paths, error) {
	paths, err := ResolvePaths()
	if err != nil {
		return Config{}, Paths{}, err
	}
	cfg, err := Load(paths.ConfigFile, Default(paths))
	if err != nil {
		return Config{}, Paths{}, err
	}
	return cfg, paths, nil
}

func (c CookieConfig) YTDLPCookieArgs() []string {
	if !c.Enabled || strings.TrimSpace(c.Browser) == "" {
		return nil
	}
	spec := strings.TrimSpace(c.Browser)
	if strings.TrimSpace(c.Profile) != "" {
		spec += ":" + strings.TrimSpace(c.Profile)
	}
	return []string{"--cookies-from-browser", spec}
}
