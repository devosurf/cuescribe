package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
)

func TestChannelCoverDoesNotUseBannerOrLeakCookies(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.m4a")
	if err := os.WriteFile(source, []byte("tagged audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name != "yt-dlp" || strings.Contains(strings.Join(args, " "), "--cookies") {
			t.Fatalf("unexpected command or cookie access: %s %v", name, args)
		}
		return runner.Result{Stdout: []byte(`{"thumbnails":[{"id":"banner_uncropped","url":"https://example.org/banner.jpg"}]}`)}, nil
	})
	err := EmbedCover(context.Background(), r, source, Metadata{ChannelURL: "https://youtube.com.example.org/channel"}, "channel", config.CookieConfig{Enabled: true, Browser: "chrome"})
	if err == nil {
		t.Fatal("banner-only channel reported embedded avatar")
	}
	got, readErr := os.ReadFile(source)
	if readErr != nil || string(got) != "tagged audio" {
		t.Fatalf("artwork failure damaged tagged audio: %q, %v", got, readErr)
	}
}

func TestCoverProcessingFailurePreservesTaggedAudio(t *testing.T) {
	for _, failure := range []string{"fetch", "conversion", "attachment"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Cookie") != "" {
					t.Error("browser cookies reached the image host")
				}
				if failure == "fetch" {
					http.Error(w, "unavailable", http.StatusNotFound)
					return
				}
				w.Write([]byte("image fixture"))
			}))
			defer server.Close()
			dir := t.TempDir()
			source := filepath.Join(dir, "source.m4a")
			if err := os.WriteFile(source, []byte("tagged audio"), 0o600); err != nil {
				t.Fatal(err)
			}
			processErr := errors.New("postprocessing failed")
			r := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
				if name != "ffmpeg" || failure == "fetch" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				dest := args[len(args)-1]
				if err := os.WriteFile(dest, []byte("partial output"), 0o600); err != nil {
					t.Fatal(err)
				}
				if failure == "conversion" || argAfter(args, "-frames:v") == "" {
					return runner.Result{}, processErr
				}
				return runner.Result{}, nil
			})
			err := EmbedCover(context.Background(), r, source, Metadata{Thumbnail: server.URL}, "thumbnail", config.CookieConfig{Enabled: true, Browser: "chrome"})
			if err == nil || (failure != "fetch" && !errors.Is(err, processErr)) {
				t.Fatalf("artwork failure not returned: %v", err)
			}
			got, err := os.ReadFile(source)
			if err != nil || string(got) != "tagged audio" {
				t.Fatalf("damaged original audio: %q, %v", got, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "source.m4a" {
				t.Fatalf("temporary artwork leaked: %v, %v", entries, err)
			}
		})
	}
}

func TestChannelCoverFetchesAvatarWithoutImageCookies(t *testing.T) {
	fetched := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fetched <- req.URL.Path
		if req.Header.Get("Cookie") != "" {
			t.Error("browser cookies reached the image host")
		}
		w.Write([]byte("image fixture"))
	}))
	defer server.Close()
	source := filepath.Join(t.TempDir(), "source.mp3")
	if err := os.WriteFile(source, []byte("tagged audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	conversionErr := errors.New("invalid image")
	r := runnerFunc(func(ctx context.Context, name string, args ...string) (runner.Result, error) {
		if name == "yt-dlp" {
			if argAfter(args, "--cookies-from-browser") != "chrome:Profile 1" {
				t.Fatal("YouTube channel lookup did not reuse configured consent")
			}
			return runner.Result{Stdout: []byte(fmt.Sprintf(`{"thumbnails":[
				{"id":"avatar_uncropped","url":%q},
				{"id":"banner_uncropped","url":%q}]}`, server.URL+"/avatar", server.URL+"/banner"))}, nil
		}
		if name != "ffmpeg" {
			t.Fatalf("unexpected command: %s", name)
		}
		return runner.Result{}, conversionErr
	})
	err := EmbedCover(context.Background(), r, source, Metadata{ChannelURL: "https://www.youtube.com/channel/example"}, "channel", config.CookieConfig{Enabled: true, Browser: "chrome", Profile: "Profile 1"})
	if !errors.Is(err, conversionErr) {
		t.Fatalf("conversion failure not returned: %v", err)
	}
	if len(fetched) != 1 {
		t.Fatalf("fetched %d images, want only channel avatar", len(fetched))
	}
	if path := <-fetched; path != "/avatar" {
		t.Fatalf("fetched %q, want channel avatar", path)
	}
}
