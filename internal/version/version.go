package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const Version = "0.1.10"

var (
	Commit = "dev"
	Date   = ""
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date,omitempty"`
}

type ReleaseCheck struct {
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	URL     string `json:"url,omitempty"`
	Message string `json:"message,omitempty"`
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, Date: Date}
}

func CheckLatest(ctx context.Context) (ReleaseCheck, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/devosurf/cuescribe/releases/latest", nil)
	if err != nil {
		return ReleaseCheck{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseCheck{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ReleaseCheck{Current: Version, Message: "no GitHub releases found"}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return ReleaseCheck{}, fmt.Errorf("release check failed: GitHub returned %s", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleaseCheck{}, err
	}
	if payload.TagName == "" {
		return ReleaseCheck{}, errors.New("release check failed: latest release did not include a tag")
	}
	return ReleaseCheck{Current: Version, Latest: payload.TagName, URL: payload.HTMLURL}, nil
}
