// Package summary generates a local LLM summary of a transcript by running
// llama-server as a short-lived child process and talking to its
// OpenAI-compatible API. The HTTP contract is stable across llama.cpp
// releases, unlike the CLI tools' text output.
package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/transcript"
)

const (
	// maxSinglePassWords keeps a one-shot prompt within the context budget
	// (~1.75 tokens/word plus instruction and output headroom).
	maxSinglePassWords = 8000
	chunkWords         = 6000
	maxSummaryTokens   = 768
	contextTokens      = 16384
	serverReadyTimeout = 3 * time.Minute
)

type Options struct {
	ModelPath string
	Language  string    // optional target language; "" = same as transcript
	LogWriter io.Writer // llama-server logs; nil discards
}

// Summarizer talks to a llama-server. With an empty BaseURL it spawns one
// for the duration of the call; tests set BaseURL to a stub server.
type Summarizer struct {
	BaseURL string
	Client  *http.Client
}

func (s Summarizer) Summarize(ctx context.Context, opts Options, doc transcript.Document) (string, error) {
	text := transcript.PlainText(doc.Segments)
	if text == "" {
		return "", fmt.Errorf("transcript is empty; nothing to summarize")
	}
	base := s.BaseURL
	if base == "" {
		if strings.TrimSpace(opts.ModelPath) == "" {
			return "", fmt.Errorf("Error: no summary model configured.\nFix: run cuescribe setup summary")
		}
		if _, err := os.Stat(opts.ModelPath); err != nil {
			return "", fmt.Errorf("Error: summary model file is missing: %s\nFix: run cuescribe setup summary", opts.ModelPath)
		}
		srv, err := startServer(ctx, opts)
		if err != nil {
			return "", err
		}
		defer srv.stop()
		base = srv.baseURL
		if err := srv.waitReady(ctx, s.client()); err != nil {
			return "", err
		}
	}
	words := strings.Fields(text)
	if len(words) <= maxSinglePassWords {
		return s.complete(ctx, base, singlePassPrompt(doc, opts.Language, text))
	}
	var partials []string
	for i, chunk := range splitWords(words, chunkWords) {
		partial, err := s.complete(ctx, base, chunkPrompt(doc, i+1, chunk))
		if err != nil {
			return "", err
		}
		partials = append(partials, partial)
	}
	return s.complete(ctx, base, mergePrompt(doc, opts.Language, partials))
}

func (s Summarizer) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 15 * time.Minute}
}

func languageInstruction(doc transcript.Document, language string) string {
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = strings.TrimSpace(doc.DetectedLanguage)
	}
	if lang == "" || strings.EqualFold(lang, "auto") {
		return "Write the summary in the same language as the transcript."
	}
	return fmt.Sprintf("Write the summary in the language %q.", lang)
}

func singlePassPrompt(doc transcript.Document, language, text string) string {
	var b strings.Builder
	b.WriteString("Summarize the following transcript. Respond with only the summary: one short paragraph followed by 3-6 key points as a bulleted list. Do not add commentary or headings. ")
	b.WriteString(languageInstruction(doc, language))
	b.WriteString("\n\n")
	if doc.Title != "" {
		fmt.Fprintf(&b, "Transcript of %q:\n", doc.Title)
	}
	b.WriteString(text)
	return b.String()
}

func chunkPrompt(doc transcript.Document, part int, words []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The following is part %d of a longer transcript. Summarize this part in 5-8 sentences, keeping every important fact, name, and number. Respond with only the summary, in the same language as the text.\n\n", part)
	if doc.Title != "" {
		fmt.Fprintf(&b, "Transcript of %q:\n", doc.Title)
	}
	b.WriteString(strings.Join(words, " "))
	return b.String()
}

func mergePrompt(doc transcript.Document, language string, partials []string) string {
	var b strings.Builder
	b.WriteString("The following are sequential partial summaries of one transcript. Combine them into a single coherent summary: one short paragraph followed by 3-6 key points as a bulleted list. Do not add commentary or headings. ")
	b.WriteString(languageInstruction(doc, language))
	b.WriteString("\n\n")
	for i, partial := range partials {
		fmt.Fprintf(&b, "Part %d:\n%s\n\n", i+1, partial)
	}
	return b.String()
}

func splitWords(words []string, size int) [][]string {
	var chunks [][]string
	for start := 0; start < len(words); start += size {
		end := start + size
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, words[start:end])
	}
	return chunks
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

func (s Summarizer) complete(ctx context.Context, baseURL, prompt string) (string, error) {
	payload := struct {
		Messages []map[string]string `json:"messages"`
		Temp     float64             `json:"temperature"`
		MaxTok   int                 `json:"max_tokens"`
	}{
		Messages: []map[string]string{{"role": "user", "content": prompt}},
		Temp:     0.3,
		MaxTok:   maxSummaryTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("summary request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("summary request failed: %s\n%s", resp.Status, strings.TrimSpace(string(detail)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("failed to parse summary response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("summary response contained no choices")
	}
	content := thinkBlock.ReplaceAllString(parsed.Choices[0].Message.Content, "")
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("summary model returned an empty response")
	}
	return content, nil
}

type server struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	baseURL string
	done    chan error
}

func startServer(ctx context.Context, opts Options) (*server, error) {
	if _, err := exec.LookPath("llama-server"); err != nil {
		return nil, fmt.Errorf("Error: llama-server is missing.\nFix: brew install llama.cpp")
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	logw := opts.LogWriter
	if logw == nil {
		logw = io.Discard
	}
	cmd := exec.CommandContext(ctx, "llama-server",
		"-m", opts.ModelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--jinja",
		"--reasoning-budget", "0",
		"-c", strconv.Itoa(contextTokens),
		"--no-webui",
	)
	cmd.Stdout = logw
	cmd.Stderr = logw
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return &server{
		cmd:     cmd,
		cancel:  cancel,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		done:    done,
	}, nil
}

func (s *server) stop() {
	s.cancel()
	<-s.done
}

func (s *server) waitReady(ctx context.Context, client *http.Client) error {
	deadline := time.Now().Add(serverReadyTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.done:
			s.done <- err
			return fmt.Errorf("llama-server exited before becoming ready: %v", err)
		case <-time.After(250 * time.Millisecond):
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}
	return fmt.Errorf("llama-server did not become ready within %s", serverReadyTimeout)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
