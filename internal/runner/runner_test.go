package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecRunnerWritesCommandAndStderrToLog(t *testing.T) {
	var log bytes.Buffer
	_, err := ExecRunner{Log: &log}.Run(context.Background(), "sh", "-c", "echo noisy >&2")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := log.String()
	if !strings.Contains(got, `$ sh "-c" "echo noisy >&2"`) {
		t.Fatalf("log missing command: %q", got)
	}
	if !strings.Contains(got, "stderr:\nnoisy") {
		t.Fatalf("log missing stderr: %q", got)
	}
}
