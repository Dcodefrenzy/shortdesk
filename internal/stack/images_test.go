package stack

import (
	"strings"
	"testing"
)

func TestReleaseVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.1.0": true, "v1.2.3-rc.1": true, "v1.2.3-beta2": true, "v0.1.0-dirty": false,
		"dev": false, "eda4831": false, "eda4831-dirty": false, "v0.1.0-3-gabc1234-dirty": false,
	} {
		if got := releaseVersion.MatchString(v); got != want {
			t.Errorf("%q: got %v, want %v", v, got, want)
		}
	}
}

func TestResourceWarning(t *testing.T) {
	for out, want := range map[string]string{
		"8350298112 8 docker-desktop": "",
		"2061524992 2 colima":         "colima start --cpu 4 --memory 8",
		"4123049984 4 docker-desktop": "Docker Desktop → Settings",
		"garbage":                     "",
	} {
		got := resourceWarning(out)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%q: got %q", out, got)
		}
	}
}
