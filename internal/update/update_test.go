package update

import "testing"

func TestManifestURL(t *testing.T) {
	if got := manifestURL("latest"); got != "https://github.com/devosurf/cuescribe/releases/latest/download/manifest.json" {
		t.Fatalf("manifestURL(latest) = %s", got)
	}
	if got := manifestURL("v0.1.0"); got != "https://github.com/devosurf/cuescribe/releases/download/v0.1.0/manifest.json" {
		t.Fatalf("manifestURL(version) = %s", got)
	}
}
