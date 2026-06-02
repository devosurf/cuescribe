package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

func Step(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "==> "+format+"\n", args...)
}

func HumanBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(n)
	unit := units[0]
	for i := 1; i < len(units) && value >= 1000; i++ {
		value /= 1000
		unit = units[i]
	}
	if unit == "B" {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

type Bar struct {
	w        io.Writer
	label    string
	total    int64
	terminal bool
	width    int
	started  time.Time
	last     time.Time
	current  int64
	rendered bool
}

func NewBar(w io.Writer, label string, total int64) *Bar {
	return &Bar{
		w:        w,
		label:    label,
		total:    total,
		terminal: IsTerminal(w),
		width:    28,
	}
}

func (b *Bar) Start(current int64) {
	if b.w == nil {
		return
	}
	b.started = time.Now()
	b.current = current
	if b.terminal {
		b.render(true)
		return
	}
	fmt.Fprintf(b.w, "%s...\n", b.label)
}

func (b *Bar) Add(delta int64) {
	b.Set(b.current + delta)
}

func (b *Bar) Set(current int64) {
	if b.w == nil {
		return
	}
	b.current = current
	now := time.Now()
	if !b.terminal {
		if b.total > 0 && now.Sub(b.last) >= 5*time.Second {
			fmt.Fprintf(b.w, "%s: %s of %s (%d%%)\n", b.label, HumanBytes(b.current), HumanBytes(b.total), b.percent())
			b.last = now
		}
		return
	}
	if now.Sub(b.last) < 100*time.Millisecond && b.current < b.total {
		return
	}
	b.render(false)
}

func (b *Bar) Finish(message string) {
	if b.w == nil {
		return
	}
	if b.terminal {
		b.render(true)
		fmt.Fprint(b.w, "\n")
	}
	if message != "" {
		fmt.Fprintf(b.w, "%s\n", message)
	}
}

func (b *Bar) render(force bool) {
	if !force && !b.terminal {
		return
	}
	if b.started.IsZero() {
		b.started = time.Now()
	}
	if b.current < 0 {
		b.current = 0
	}
	filled := 0
	if b.total > 0 {
		filled = int((b.current * int64(b.width)) / b.total)
	}
	if filled > b.width {
		filled = b.width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", b.width-filled)
	elapsed := time.Since(b.started).Seconds()
	speed := int64(0)
	if elapsed > 0 {
		speed = int64(float64(b.current) / elapsed)
	}
	if b.total > 0 {
		fmt.Fprintf(b.w, "\r%s [%s] %3d%% %s/%s %s/s", b.label, bar, b.percent(), HumanBytes(b.current), HumanBytes(b.total), HumanBytes(speed))
	} else {
		fmt.Fprintf(b.w, "\r%s [%s] %s %s/s", b.label, bar, HumanBytes(b.current), HumanBytes(speed))
	}
	b.last = time.Now()
	b.rendered = true
}

func (b *Bar) percent() int {
	if b.total <= 0 {
		return 0
	}
	pct := int((b.current * 100) / b.total)
	if pct > 100 {
		return 100
	}
	return pct
}

type Spinner struct {
	w        io.Writer
	label    string
	terminal bool
	done     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
}

type spinnerStyle struct {
	interval time.Duration
	frames   []string
}

var defaultSpinner = spinnerStyle{
	interval: 80 * time.Millisecond,
	frames:   []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
}

func StartSpinner(w io.Writer, label string) *Spinner {
	s := &Spinner{
		w:        w,
		label:    label,
		terminal: IsTerminal(w),
		done:     make(chan struct{}),
	}
	if w == nil {
		return s
	}
	if !s.terminal {
		fmt.Fprintf(w, "%s...\n", label)
		return s
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(defaultSpinner.interval)
		defer ticker.Stop()
		i := 0
		renderSpinnerFrame(w, label, defaultSpinner.frames, i)
		i++
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				renderSpinnerFrame(w, label, defaultSpinner.frames, i)
				i++
			}
		}
	}()
	return s
}

func renderSpinnerFrame(w io.Writer, label string, frames []string, index int) {
	if len(frames) == 0 {
		return
	}
	fmt.Fprintf(w, "\r\033[K%s %s", frames[index%len(frames)], label)
}

func (s *Spinner) Done(message string) {
	if s == nil || s.w == nil {
		return
	}
	s.once.Do(func() {
		close(s.done)
		s.wg.Wait()
		if s.terminal {
			fmt.Fprint(s.w, "\r\033[K")
		}
		if message != "" {
			fmt.Fprintf(s.w, "%s\n", message)
		}
	})
}

func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
