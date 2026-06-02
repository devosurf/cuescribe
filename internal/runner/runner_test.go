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

func TestExecRunnerWritesProgressWhenConfigured(t *testing.T) {
	var out bytes.Buffer
	_, err := ExecRunner{Progress: &out}.Run(context.Background(), "sh", "-c", "true")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Running sh") || !strings.Contains(got, "Finished sh") {
		t.Fatalf("progress output = %q", got)
	}
}
