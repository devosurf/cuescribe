package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devosurf/cuescribe/internal/config"
)

func OpenRunLog(paths config.Paths) (*os.File, string, error) {
	if err := os.MkdirAll(paths.LogDir, 0o755); err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("%s-%d.log", time.Now().UTC().Format("20060102T150405Z"), os.Getpid())
	path := filepath.Join(paths.LogDir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}
