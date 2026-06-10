package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/devosurf/cuescribe/internal/transcript"
)

func stubServer(t *testing.T, reply func(prompt string, call int) string) (*httptest.Server, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Messages) == 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		prompt := payload.Messages[0].Content
		prompts = append(prompts, prompt)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, reply(prompt, len(prompts)))
	}))
	t.Cleanup(srv.Close)
	return srv, &prompts
}

func docWithText(text string) transcript.Document {
	return transcript.Document{
		Title:            "Test Video",
		DetectedLanguage: "sv",
		Segments:         []transcript.Segment{{Text: text}},
	}
}

func TestSummarizeSinglePass(t *testing.T) {
	srv, prompts := stubServer(t, func(string, int) string { return "  <think>reasoning</think>  En sammanfattning.  " })
	got, err := Summarizer{BaseURL: srv.URL}.Summarize(context.Background(), Options{}, docWithText("hej världen detta är en transkription"))
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if got != "En sammanfattning." {
		t.Fatalf("Summarize() = %q, want think block stripped and trimmed", got)
	}
	if len(*prompts) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*prompts))
	}
	prompt := (*prompts)[0]
	if !strings.Contains(prompt, "hej världen") || !strings.Contains(prompt, `"Test Video"`) {
		t.Fatalf("prompt missing transcript or title: %q", prompt)
	}
	if !strings.Contains(prompt, `language "sv"`) {
		t.Fatalf("prompt missing detected-language instruction: %q", prompt)
	}
}

func TestSummarizeLanguageOverride(t *testing.T) {
	srv, prompts := stubServer(t, func(string, int) string { return "summary" })
	_, err := Summarizer{BaseURL: srv.URL}.Summarize(context.Background(), Options{Language: "en"}, docWithText("text"))
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if !strings.Contains((*prompts)[0], `language "en"`) {
		t.Fatalf("prompt missing override language: %q", (*prompts)[0])
	}
}

func TestSummarizeChunksLongTranscripts(t *testing.T) {
	srv, prompts := stubServer(t, func(prompt string, call int) string {
		return fmt.Sprintf("partial %d", call)
	})
	words := make([]string, maxSinglePassWords+chunkWords)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i)
	}
	got, err := Summarizer{BaseURL: srv.URL}.Summarize(context.Background(), Options{}, docWithText(strings.Join(words, " ")))
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	// 14000 words -> 3 chunks of <=6000 plus one merge call.
	if len(*prompts) != 4 {
		t.Fatalf("expected 3 chunk requests + 1 merge, got %d", len(*prompts))
	}
	merge := (*prompts)[3]
	if !strings.Contains(merge, "partial 1") || !strings.Contains(merge, "partial 3") {
		t.Fatalf("merge prompt missing partials: %q", merge)
	}
	if got != "partial 4" {
		t.Fatalf("Summarize() = %q, want merge response", got)
	}
}

func TestSummarizeEmptyTranscript(t *testing.T) {
	_, err := Summarizer{BaseURL: "http://unused"}.Summarize(context.Background(), Options{}, transcript.Document{})
	if err == nil || !strings.Contains(err.Error(), "nothing to summarize") {
		t.Fatalf("Summarize() error = %v, want empty-transcript failure", err)
	}
}

func TestSummarizeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	_, err := Summarizer{BaseURL: srv.URL}.Summarize(context.Background(), Options{}, docWithText("text"))
	if err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Fatalf("Summarize() error = %v, want server detail", err)
	}
}

// TestSummarizeEndToEnd spawns a real llama-server with the model named in
// CUESCRIBE_SUMMARY_E2E_MODEL. Opt-in: it is skipped unless the variable is
// set, since it needs llama.cpp installed and a multi-GB model on disk.
func TestSummarizeEndToEnd(t *testing.T) {
	modelPath := os.Getenv("CUESCRIBE_SUMMARY_E2E_MODEL")
	if modelPath == "" {
		t.Skip("set CUESCRIBE_SUMMARY_E2E_MODEL to a GGUF path to run")
	}
	doc := docWithText("Idag pratar vi om Apples M1-chip. Det är deras första egna processor för Mac, byggd på ARM-arkitektur. Batteritiden är nästan dubbelt så lång som tidigare. Rosetta 2 översätter Intel-appar förvånansvärt bra.")
	got, err := Summarizer{}.Summarize(context.Background(), Options{ModelPath: modelPath}, doc)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if strings.Contains(got, "<think>") {
		t.Fatalf("Summarize() leaked think block: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "m1") {
		t.Fatalf("Summarize() = %q, expected mention of M1", got)
	}
	t.Logf("summary: %s", got)
}
