package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
	"github.com/devosurf/cuescribe/internal/transcript"
)

func Normalize(ctx context.Context, r runner.CommandRunner, input, dir string) (string, error) {
	out := filepath.Join(dir, "audio-16khz-mono.wav")
	_, err := r.Run(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", input,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		out,
	)
	if err != nil {
		return "", err
	}
	return out, nil
}

func Transcribe(ctx context.Context, r runner.CommandRunner, wavPath, dir, lang string, translate bool, cfg config.ModelConfig) ([]transcript.Segment, string, error) {
	outBase := filepath.Join(dir, "whisper")
	args := []string{
		"--output-json-full",
		"--output-file", outBase,
		"--no-prints",
		"--file", wavPath,
	}
	if strings.TrimSpace(cfg.Path) != "" {
		if _, err := os.Stat(cfg.Path); err != nil {
			return nil, "", fmt.Errorf("Error: Whisper model is missing: %s\nFix: run cuescribe setup model", cfg.Path)
		}
		args = append(args, "--model", cfg.Path)
	}
	if strings.TrimSpace(lang) != "" {
		args = append(args, "--language", lang)
	}
	if translate {
		args = append(args, "--translate")
	}
	if _, err := r.Run(ctx, "whisper-cli", args...); err != nil {
		return nil, "", err
	}
	return parseWhisperJSON(outBase + ".json")
}

func parseWhisperJSON(path string) ([]transcript.Segment, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var payload struct {
		Result struct {
			Language string `json:"language"`
		} `json:"result"`
		Transcription []struct {
			Timestamps struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"timestamps"`
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, "", fmt.Errorf("failed to parse whisper JSON: %w", err)
	}
	segments := make([]transcript.Segment, 0, len(payload.Transcription))
	for _, item := range payload.Transcription {
		text := strings.Join(strings.Fields(item.Text), " ")
		if text == "" {
			continue
		}
		start, err := parseWhisperTimestamp(item.Timestamps.From)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse whisper JSON: %w", err)
		}
		end, err := parseWhisperTimestamp(item.Timestamps.To)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse whisper JSON: %w", err)
		}
		segments = append(segments, transcript.Segment{
			Start: start,
			End:   end,
			Text:  text,
		})
	}
	return segments, payload.Result.Language, nil
}

func parseWhisperTimestamp(s string) (time.Duration, error) {
	raw := strings.TrimSpace(s)
	normalized := strings.ReplaceAll(raw, ",", ".")
	parts := strings.Split(normalized, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", raw)
	}
	hours, err := time.ParseDuration(parts[0] + "h")
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp %q", raw)
	}
	minutes, err := time.ParseDuration(parts[1] + "m")
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp %q", raw)
	}
	sec, err := time.ParseDuration(parts[2] + "s")
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp %q", raw)
	}
	return hours + minutes + sec, nil
}
