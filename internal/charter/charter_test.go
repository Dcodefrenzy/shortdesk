package charter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAppliesDefaults(t *testing.T) {
	c, err := Parse([]byte(`
charter: assistant
permissions:
  network:
    allow: ["claude.ai", "*.anthropic.com"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Desk.Image != DefaultImage || c.Desk.Resources.CPU != DefaultCPU || c.Desk.Resources.Memory != DefaultMemory {
		t.Errorf("defaults not applied: %+v", c.Desk)
	}
	if time.Duration(c.Limits.DeskLifetime) != DefaultLifetime {
		t.Errorf("lifetime = %v, want %v", time.Duration(c.Limits.DeskLifetime), DefaultLifetime)
	}
	if c.Permissions.Network.Unrestricted() {
		t.Error("allowlist reported as unrestricted")
	}
}

func TestParseDuration(t *testing.T) {
	c, err := Parse([]byte(`
charter: a
permissions: {network: {allow: ["*"]}}
limits: {desk_lifetime: 90m}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Duration(c.Limits.DeskLifetime); got != 90*time.Minute {
		t.Errorf("lifetime = %v, want 90m", got)
	}
	if !c.Permissions.Network.Unrestricted() {
		t.Error(`["*"] not reported as unrestricted`)
	}
}

func TestParseRejects(t *testing.T) {
	tests := map[string]struct {
		yaml, wantErr string
	}{
		"unknown field": {
			yaml:    "charter: a\npermissions: {network: {allow: ['*']}}\ndesk: {apps: [chrome]}",
			wantErr: "field apps not found",
		},
		"missing network rules": {
			yaml:    "charter: a",
			wantErr: "permissions.network.allow is required",
		},
		"bad host": {
			yaml:    "charter: a\npermissions: {network: {allow: ['https://claude.ai']}}",
			wantErr: "is not a host",
		},
		"star mixed with hosts": {
			yaml:    "charter: a\npermissions: {network: {allow: ['*', 'claude.ai']}}",
			wantErr: "cannot be combined",
		},
		"bad name": {
			yaml:    "charter: My Desk\npermissions: {network: {allow: ['*']}}",
			wantErr: "lowercase",
		},
		"short lifetime": {
			yaml:    "charter: a\npermissions: {network: {allow: ['*']}}\nlimits: {desk_lifetime: 10s}",
			wantErr: "at least",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.yaml", "charter: a\npermissions: {network: {allow: ['*']}}")
	write("b.yml", "charter: b\npermissions: {network: {allow: ['*']}}")
	write("notes.txt", "ignored")

	got, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a"] == nil || got["b"] == nil {
		t.Errorf("loaded %v", got)
	}

	write("dup.yaml", "charter: a\npermissions: {network: {allow: ['*']}}")
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("err = %v, want duplicate error", err)
	}
}

func TestAddAllowedHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	orig := "# my desk\ncharter: a\nversion: 3\npermissions:\n  network:\n    allow:\n      - claude.ai # main site\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddAllowedHost(path, "*.booking.com"); err != nil {
		t.Fatal(err)
	}
	if err := AddAllowedHost(path, "*.booking.com"); err != nil { // idempotent
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Permissions.Network.Allow, ",") != "claude.ai,*.booking.com" || c.Version != 4 {
		t.Errorf("allow=%v version=%d", c.Permissions.Network.Allow, c.Version)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# my desk") || !strings.Contains(string(b), "# main site") {
		t.Errorf("comments lost:\n%s", b)
	}
	if err := AddAllowedHost(path, "https://x.com"); err == nil {
		t.Error("invalid host accepted")
	}
}
