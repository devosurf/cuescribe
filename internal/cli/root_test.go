package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferredBrowserUsesDetectedDefault(t *testing.T) {
	got := preferredBrowser([]string{"safari", "chrome"}, "chrome")
	if got != "chrome" {
		t.Fatalf("preferredBrowser() = %q, want chrome", got)
	}
}

func TestPreferredBrowserFallsBackToFirstDetected(t *testing.T) {
	got := preferredBrowser([]string{"safari", "chrome"}, "firefox")
	if got != "safari" {
		t.Fatalf("preferredBrowser() = %q, want safari", got)
	}
}

func TestMergeDetectedDefaultBrowserPrependsMissingDefault(t *testing.T) {
	got := mergeDetectedDefaultBrowser([]string{"safari"}, "chrome")
	want := []string{"chrome", "safari"}
	assertStrings(t, got, want)
}

func TestMergeDetectedDefaultBrowserKeepsExistingOrder(t *testing.T) {
	got := mergeDetectedDefaultBrowser([]string{"safari", "chrome"}, "chrome")
	want := []string{"safari", "chrome"}
	assertStrings(t, got, want)
}

func TestNormalizeBrowserName(t *testing.T) {
	tests := map[string]string{
		" Chrome ":        "chrome",
		"Google Chrome":   "chrome",
		"Safari":          "safari",
		"Mozilla Firefox": "firefox",
		"Microsoft Edge":  "edge",
		"custom":          "custom",
	}
	for input, want := range tests {
		got := normalizeBrowserName(input)
		if got != want {
			t.Fatalf("normalizeBrowserName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestChromeProfilesFromLocalState(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"profile": {
			"info_cache": {
				"Profile 10": {"name": "Archive"},
				"System Profile": {"name": "System"},
				"Default": {"name": "Personal"},
				"Profile 2": {"name": "Work"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}

	profiles := detectChromeProfilesInHome(home)
	assertProfileIDs(t, profiles, []string{"Default", "Profile 2", "Profile 10"})
	if profiles[0].Name != "Personal" || profiles[1].Name != "Work" || profiles[2].Name != "Archive" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestChromeProfilesFromDirs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	for _, name := range []string{"Profile 1", "Guest Profile", "Default"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	profiles := detectChromeProfilesInHome(home)
	assertProfileIDs(t, profiles, []string{"Default", "Profile 1"})
}

func TestResolveChromeProfileSelection(t *testing.T) {
	profiles := []browserProfile{
		{ID: "Default", Name: "Personal"},
		{ID: "Profile 2", Name: "Work"},
	}
	tests := []struct {
		name     string
		selected string
		want     string
	}{
		{name: "blank uses default", selected: "\n", want: "Default"},
		{name: "number", selected: "2\n", want: "Profile 2"},
		{name: "profile name", selected: "Work\n", want: "Profile 2"},
		{name: "profile id", selected: "Profile 2\n", want: "Profile 2"},
		{name: "custom value", selected: "Canary\n", want: "Canary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveChromeProfileSelection(profiles, tt.selected)
			if got != tt.want {
				t.Fatalf("resolveChromeProfileSelection() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertProfileIDs(t *testing.T, profiles []browserProfile, want []string) {
	t.Helper()
	if len(profiles) != len(want) {
		t.Fatalf("len(profiles) = %d, want %d: %+v", len(profiles), len(want), profiles)
	}
	for i := range want {
		if profiles[i].ID != want[i] {
			t.Fatalf("profiles[%d].ID = %q, want %q: %+v", i, profiles[i].ID, want[i], profiles)
		}
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q: %v", i, got[i], want[i], got)
		}
	}
}
