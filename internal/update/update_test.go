package update

import (
	"context"
	"strings"
	"testing"
)

func TestManifestURL(t *testing.T) {
	if got := manifestURL("latest"); got != "https://github.com/devosurf/cuescribe/releases/latest/download/manifest.json" {
		t.Fatalf("manifestURL(latest) = %s", got)
	}
	if got := manifestURL("v0.1.0"); got != "https://github.com/devosurf/cuescribe/releases/download/v0.1.0/manifest.json" {
		t.Fatalf("manifestURL(version) = %s", got)
	}
}

func TestFetchManifestRejectsInvalidVersion(t *testing.T) {
	for _, version := range []string{"../../../other/repo", "v1.0?x=", "v1.0.0/extra", "..", "v1"} {
		_, err := FetchManifest(context.Background(), version)
		if err == nil || !strings.Contains(err.Error(), "invalid version") {
			t.Fatalf("FetchManifest(%q) error = %v, want invalid version", version, err)
		}
	}
}

func TestVersionPatternAcceptsReleaseTags(t *testing.T) {
	for _, version := range []string{"v0.1.15", "0.2.0", "v1.0.0-rc.1", "v1.0.0+build.5"} {
		if !versionPattern.MatchString(version) {
			t.Fatalf("versionPattern rejected %q", version)
		}
	}
}

func TestValidateBinaryURL(t *testing.T) {
	valid := []string{
		"https://github.com/devosurf/cuescribe/releases/download/v0.1.0/cuescribe",
		"https://objects.githubusercontent.com/some/asset",
	}
	for _, u := range valid {
		if err := validateBinaryURL(u); err != nil {
			t.Fatalf("validateBinaryURL(%q) error = %v", u, err)
		}
	}
	invalid := []string{
		"https://evil.example.com/cuescribe",
		"http://github.com/devosurf/cuescribe/releases/download/v0.1.0/cuescribe",
		"https://github.com.evil.example.com/cuescribe",
		"ftp://github.com/x",
		"",
	}
	for _, u := range invalid {
		if err := validateBinaryURL(u); err == nil {
			t.Fatalf("validateBinaryURL(%q) = nil, want error", u)
		}
	}
}
