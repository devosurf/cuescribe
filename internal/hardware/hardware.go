// Package hardware detects the host machine's chip and memory so setup can
// recommend a Whisper model size that fits.
package hardware

import "fmt"

type Info struct {
	Chip  string
	RAMGB int
}

// String formats the detected hardware for display, handling partial
// detection gracefully. It returns "" when nothing was detected.
func (i Info) String() string {
	switch {
	case i.Chip != "" && i.RAMGB > 0:
		return fmt.Sprintf("%s, %d GB RAM", i.Chip, i.RAMGB)
	case i.RAMGB > 0:
		return fmt.Sprintf("%d GB RAM", i.RAMGB)
	default:
		return i.Chip
	}
}

// RecommendedWhisperModel maps detected hardware to the Whisper model that
// balances accuracy against memory headroom while transcribing alongside
// other applications. Unknown hardware gets the conservative default.
func RecommendedWhisperModel(info Info) string {
	switch {
	case info.RAMGB >= 32:
		return "large-v3-turbo"
	case info.RAMGB >= 16:
		return "medium"
	default:
		return "small"
	}
}
