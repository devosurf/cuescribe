package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestCopyReportsTotalIncludingOffset(t *testing.T) {
	var dst bytes.Buffer
	written, err := Copy(&dst, strings.NewReader("hello"), nil, 10)
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if written != 15 {
		t.Fatalf("Copy() written = %d, want 15", written)
	}
	if dst.String() != "hello" {
		t.Fatalf("Copy() wrote %q", dst.String())
	}
}

type blockingReader struct {
	ctx context.Context
}

func (b blockingReader) Read(p []byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func TestStallGuardCancelsStalledTransfer(t *testing.T) {
	ctx, guard := NewStallGuard(context.Background(), 20*time.Millisecond)
	defer guard.Stop()
	_, err := io.Copy(io.Discard, guard.Reader(blockingReader{ctx: ctx}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read error = %v, want context.Canceled", err)
	}
	wrapped := guard.Wrap(err)
	if wrapped == nil || !strings.Contains(wrapped.Error(), "stalled") {
		t.Fatalf("Wrap() = %v, want stall explanation", wrapped)
	}
}

func TestStallGuardLeavesActiveTransferAlone(t *testing.T) {
	ctx, guard := NewStallGuard(context.Background(), time.Minute)
	defer guard.Stop()
	if err := ctx.Err(); err != nil {
		t.Fatalf("ctx.Err() = %v before timeout", err)
	}
	var dst bytes.Buffer
	if _, err := Copy(&dst, guard.Reader(strings.NewReader("data")), nil, 0); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if err := guard.Wrap(nil); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
}
