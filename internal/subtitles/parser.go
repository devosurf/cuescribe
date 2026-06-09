package subtitles

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/devosurf/cuescribe/internal/transcript"
)

var (
	vttTimestampLine = regexp.MustCompile(`^\s*([0-9:.]+)\s+-->\s+([0-9:.]+)(?:\s+.*)?$`)
	tagPattern       = regexp.MustCompile(`<[^>]+>`)
	vttVoicePattern  = regexp.MustCompile(`(?i)<v(?:\.[^ >]+)?\s+([^>]+)>`)
	speakerPattern   = regexp.MustCompile(`(?i)^(Speaker\s+\d+):\s*(.+)$`)
)

func ParseVTT(r io.Reader) ([]transcript.Segment, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}
	var segments []transcript.Segment
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.EqualFold(line, "WEBVTT") {
			continue
		}
		if isVTTBlockHeader(line) {
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
				i++
			}
			continue
		}
		match := vttTimestampLine.FindStringSubmatch(line)
		if match == nil && i+1 < len(lines) {
			match = vttTimestampLine.FindStringSubmatch(strings.TrimSpace(lines[i+1]))
			if match != nil {
				i++
			}
		}
		if match == nil {
			continue
		}
		start, err := parseCueTime(match[1])
		if err != nil {
			return nil, err
		}
		end, err := parseCueTime(match[2])
		if err != nil {
			return nil, err
		}
		var textLines []string
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" {
				break
			}
			i++
			textLines = append(textLines, next)
		}
		text, speaker := cleanText(strings.Join(textLines, " "))
		if text == "" {
			continue
		}
		segments = append(segments, transcript.Segment{Start: start, End: end, Text: text, Speaker: speaker})
	}
	return collapseRollingCaptions(segments), nil
}

func ParseSRT(r io.Reader) ([]transcript.Segment, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}
	var segments []transcript.Segment
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if _, err := strconv.Atoi(line); err == nil && i+1 < len(lines) {
			i++
			line = strings.TrimSpace(lines[i])
		}
		match := vttTimestampLine.FindStringSubmatch(strings.ReplaceAll(line, ",", "."))
		if match == nil {
			continue
		}
		start, err := parseCueTime(match[1])
		if err != nil {
			return nil, err
		}
		end, err := parseCueTime(match[2])
		if err != nil {
			return nil, err
		}
		var textLines []string
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" {
				break
			}
			i++
			textLines = append(textLines, next)
		}
		text, speaker := cleanText(strings.Join(textLines, " "))
		if text == "" {
			continue
		}
		segments = append(segments, transcript.Segment{Start: start, End: end, Text: text, Speaker: speaker})
	}
	return collapseRollingCaptions(segments), nil
}

func readLines(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, strings.TrimPrefix(scanner.Text(), "\ufeff"))
	}
	return lines, scanner.Err()
}

func isVTTBlockHeader(line string) bool {
	switch strings.ToUpper(line) {
	case "NOTE", "STYLE", "REGION":
		return true
	default:
		return strings.HasPrefix(strings.ToUpper(line), "NOTE ")
	}
}

func parseCueTime(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid subtitle timestamp %q", s)
	}
	var hours int64
	if len(parts) == 3 {
		h, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid subtitle timestamp %q", s)
		}
		hours = h
		parts = parts[1:]
	}
	minutes, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid subtitle timestamp %q", s)
	}
	secParts := strings.SplitN(parts[1], ".", 2)
	seconds, err := strconv.ParseInt(secParts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid subtitle timestamp %q", s)
	}
	var millis int64
	if len(secParts) == 2 {
		ms := secParts[1]
		if len(ms) > 3 {
			ms = ms[:3]
		}
		for len(ms) < 3 {
			ms += "0"
		}
		millis, err = strconv.ParseInt(ms, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid subtitle timestamp %q", s)
		}
	}
	return time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(millis)*time.Millisecond, nil
}

func cleanText(raw string) (string, string) {
	speaker := ""
	if match := vttVoicePattern.FindStringSubmatch(raw); match != nil {
		speaker = strings.TrimSpace(html.UnescapeString(match[1]))
	}
	text := tagPattern.ReplaceAllString(raw, "")
	text = html.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ")
	if match := speakerPattern.FindStringSubmatch(text); match != nil {
		speaker = strings.TrimSpace(match[1])
		text = strings.TrimSpace(match[2])
	}
	return strings.TrimSpace(text), speaker
}

func collapseRollingCaptions(segments []transcript.Segment) []transcript.Segment {
	if !looksLikeRollingCaptions(segments) {
		return segments
	}
	out := make([]transcript.Segment, 0, len(segments))
	var emittedTail []string
	lastSpeaker := ""
	for _, segment := range segments {
		words := strings.Fields(strings.TrimSpace(segment.Text))
		if len(words) == 0 {
			continue
		}
		if segment.Speaker != lastSpeaker {
			emittedTail = nil
			lastSpeaker = segment.Speaker
		}
		overlap := suffixPrefixOverlap(emittedTail, words)
		if overlap == len(words) {
			continue
		}
		segment.Text = strings.Join(words[overlap:], " ")
		out = append(out, segment)
		emittedTail = append(emittedTail, words[overlap:]...)
		if len(emittedTail) > 80 {
			emittedTail = emittedTail[len(emittedTail)-80:]
		}
	}
	return out
}

func looksLikeRollingCaptions(segments []transcript.Segment) bool {
	if len(segments) < 3 {
		return false
	}
	overlappingPairs := 0
	comparablePairs := 0
	for i := 1; i < len(segments); i++ {
		prev := segments[i-1]
		current := segments[i]
		if prev.Speaker != current.Speaker {
			continue
		}
		comparablePairs++
		prevWords := strings.Fields(prev.Text)
		currentWords := strings.Fields(current.Text)
		if rollingOverlap(prevWords, currentWords) {
			overlappingPairs++
		}
	}
	return overlappingPairs >= 2 && overlappingPairs*2 >= comparablePairs
}

func rollingOverlap(prevWords, currentWords []string) bool {
	overlap := suffixPrefixOverlap(prevWords, currentWords)
	return overlap >= 3
}

func suffixPrefixOverlap(prevWords, currentWords []string) int {
	maxOverlap := len(prevWords)
	if len(currentWords) < maxOverlap {
		maxOverlap = len(currentWords)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if sameWords(prevWords[len(prevWords)-overlap:], currentWords[:overlap]) {
			return overlap
		}
	}
	return 0
}

func sameWords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if comparableWord(a[i]) != comparableWord(b[i]) {
			return false
		}
	}
	return true
}

func comparableWord(word string) string {
	return strings.TrimFunc(strings.ToLower(word), func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}
