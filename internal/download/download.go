// Package download provides shared helpers for long-running HTTP downloads:
// progress-reporting copy and stall detection for transfers that have no
// overall deadline.
package download

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/devosurf/cuescribe/internal/progress"
)

// DefaultStallTimeout is how long a download may go without receiving any
// data before it is aborted.
const DefaultStallTimeout = 60 * time.Second

// StallGuard cancels a download's context when no data has arrived for the
// configured timeout. Unlike http.Client.Timeout it does not bound the total
// transfer time, so it is safe for arbitrarily large files on slow links.
type StallGuard struct {
	timeout time.Duration
	timer   *time.Timer
	cancel  context.CancelFunc
	stalled atomic.Bool
}

// NewStallGuard derives a context for an HTTP request that is cancelled if
// the transfer stalls. Wrap the response body with Reader so that incoming
// data keeps the transfer alive, and call Stop when the download finishes.
func NewStallGuard(ctx context.Context, timeout time.Duration) (context.Context, *StallGuard) {
	ctx, cancel := context.WithCancel(ctx)
	g := &StallGuard{timeout: timeout, cancel: cancel}
	g.timer = time.AfterFunc(timeout, func() {
		g.stalled.Store(true)
		cancel()
	})
	return ctx, g
}

// Reader wraps r so every successful read resets the stall timer.
func (g *StallGuard) Reader(r io.Reader) io.Reader {
	return &stallReader{guard: g, r: r}
}

// Stop releases the guard's timer and context.
func (g *StallGuard) Stop() {
	g.timer.Stop()
	g.cancel()
}

// Wrap translates the generic context-cancellation error produced by a
// stalled transfer into one that explains what happened.
func (g *StallGuard) Wrap(err error) error {
	if err != nil && g.stalled.Load() {
		return fmt.Errorf("download stalled: no data received for %s", g.timeout)
	}
	return err
}

type stallReader struct {
	guard *StallGuard
	r     io.Reader
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.guard.timer.Reset(s.guard.timeout)
	}
	return n, err
}

// Copy streams src into dst, reporting progress on bar starting from the
// given byte offset. It returns the total number of bytes accounted for,
// including the offset.
func Copy(dst io.Writer, src io.Reader, bar *progress.Bar, start int64) (int64, error) {
	buf := make([]byte, 1024*1024)
	written := start
	if bar != nil {
		bar.Start(start)
	}
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
			written += int64(nw)
			if bar != nil {
				bar.Set(written)
			}
		}
		if er == io.EOF {
			return written, nil
		}
		if er != nil {
			return written, er
		}
	}
}
