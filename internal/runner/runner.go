package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

type Result struct {
	Stdout []byte
	Stderr []byte
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

type ExecRunner struct {
	Verbose bool
	Stderr  io.Writer
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	if r.Verbose && r.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, r.Stderr)
	} else {
		cmd.Stderr = &stderr
	}
	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return result, fmt.Errorf("Error: %s is missing.\nFix: brew install %s", name, brewPackage(name))
		}
		return result, fmt.Errorf("%s failed: %w\n%s", name, err, bytes.TrimSpace(result.Stderr))
	}
	return result, nil
}

func LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func brewPackage(name string) string {
	switch name {
	case "whisper-cli":
		return "whisper-cpp"
	default:
		return name
	}
}
