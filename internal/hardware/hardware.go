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

// Recommendation pairs the Whisper and summary models suited to the
// detected hardware. The two never run concurrently — transcription
// finishes before summarization starts — so each tier is sized against
// total RAM independently, with headroom for other applications.
type Recommendation struct {
	WhisperModel string
	SummaryModel string
}

// Recommend maps detected hardware to model choices. Unknown hardware gets
// the conservative defaults.
func Recommend(info Info) Recommendation {
	switch {
	case info.RAMGB >= 32:
		return Recommendation{WhisperModel: "large-v3-turbo", SummaryModel: "qwen3-8b"}
	case info.RAMGB >= 16:
		return Recommendation{WhisperModel: "medium", SummaryModel: "qwen3-4b"}
	default:
		return Recommendation{WhisperModel: "small", SummaryModel: "qwen3-1.7b"}
	}
}

// RecommendedWhisperModel maps detected hardware to the Whisper model that
// balances accuracy against memory headroom while transcribing.
func RecommendedWhisperModel(info Info) string {
	return Recommend(info).WhisperModel
}
