package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/devosurf/cuescribe/internal/progress"
)

type Result struct {
	Stdout []byte
	Stderr []byte
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

type ExecRunner struct {
	Verbose  bool
	Stderr   io.Writer
	Log      io.Writer
	Progress io.Writer
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, "$ %s %s\n", name, quoteArgs(args))
	}
	var spinner *progress.Spinner
	startLabel, doneLabel := progressLabels(name, args)
	if r.Progress != nil && !r.Verbose && startLabel != "" {
		spinner = progress.StartSpinner(r.Progress, startLabel)
	}
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
	if spinner != nil {
		if err == nil {
			spinner.Done(doneLabel)
		} else {
			spinner.Done("")
		}
	}
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if r.Log != nil && len(result.Stderr) > 0 {
		fmt.Fprintf(r.Log, "stderr:\n%s\n", bytes.TrimSpace(result.Stderr))
	}
	if err != nil {
		if r.Log != nil {
			fmt.Fprintf(r.Log, "exit: %v\n", err)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return result, fmt.Errorf("Error: %s is missing.\nFix: brew install %s", name, brewPackage(name))
		}
		details := bytes.TrimSpace(result.Stderr)
		output := bytes.TrimSpace(result.Stdout)
		if len(details) == 0 && len(output) > 0 {
			details = output
		} else if len(details) > 0 && len(output) > 0 {
			details = append(details, '\n')
			details = append(details, output...)
		}
		if len(details) == 0 {
			return result, fmt.Errorf("%s failed: %w", name, err)
		}
		return result, fmt.Errorf("%s failed: %w\n%s", name, err, details)
	}
	return result, nil
}

func progressLabels(name string, args []string) (string, string) {
	switch name {
	case "yt-dlp":
		if containsArg(args, "--dump-json") {
			return "Fetching media metadata", "Fetched media metadata"
		}
		return "Downloading media", "Downloaded media"
	case "ffmpeg":
		return "Normalizing audio", "Normalized audio"
	case "whisper-cli":
		return "Transcribing audio", "Transcribed audio"
	default:
		return "Running " + name, "Finished " + name
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func quoteArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, strconv.Quote(arg))
	}
	return strings.Join(quoted, " ")
}

func LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func brewPackage(name string) string {
	switch name {
	case "whisper-cli":
		return "whisper-cpp"
	case "llama-server":
		return "llama.cpp"
	default:
		return name
	}
}
