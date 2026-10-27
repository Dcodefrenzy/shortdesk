package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noDocker(t *testing.T) {
	t.Helper()
	old := docker
	docker = func(context.Context, io.Reader, io.Writer, ...string) error { return errors.New("no docker") }
	t.Cleanup(func() { docker = old })
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackupAndRestore(t *testing.T) {
	noDocker(t)
	ctx := context.Background()
	nolog := func(string, ...any) {}
	home := filepath.Join(t.TempDir(), ".shortdesk")
	write(t, home, map[string]string{
		"admin-token":                    "tok",
		"workspace.json":                 `{"secrets":{}}`,
		"chats/d1.jsonl":                 "{}",
		"vault/data/db.sqlite3":          "vault db",
		"vault/data/rsa_key.pem":         "rsa",
		"vault/data/db_20260101.sqlite3": "old snapshot",
		"vault/cli/data.json":            "session",
		"opensandbox/opensandbox.db":     "machine-specific",
	})

	out := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := Create(ctx, home, out, false, nolog); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(out); info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", info.Mode().Perm())
	}
	if err := Create(ctx, home, out, false, nolog); err == nil {
		t.Error("overwrote an existing file")
	}

	// Restore over different data: it's moved aside, never deleted.
	os.WriteFile(filepath.Join(home, "admin-token"), []byte("changed"), 0o600)
	moved, err := Restore(ctx, out, home, false, nolog)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(moved, "admin-token")); string(b) != "changed" {
		t.Errorf("moved-aside data = %q", b)
	}
	for name, want := range map[string]string{
		"admin-token": "tok", "chats/d1.jsonl": "{}", "vault/data/db.sqlite3": "vault db", "vault/data/rsa_key.pem": "rsa",
	} {
		if b, err := os.ReadFile(filepath.Join(home, name)); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v", name, b, err)
		}
	}
	for _, name := range []string{"opensandbox/opensandbox.db", "vault/cli/data.json", "vault/data/db_20260101.sqlite3", manifestName} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			t.Errorf("%s should not be restored", name)
		}
	}
}

func TestRestoreRejectsOtherFilesWithoutTouchingData(t *testing.T) {
	noDocker(t)
	home := filepath.Join(t.TempDir(), ".shortdesk")
	write(t, home, map[string]string{"admin-token": "tok"})

	notBackup := filepath.Join(t.TempDir(), "x.tar.gz")
	writeArchive(t, notBackup, map[string]string{"hello.txt": "hi"})
	if _, err := Restore(context.Background(), notBackup, home, false, func(string, ...any) {}); err == nil ||
		!strings.Contains(err.Error(), "isn't a Shortdesk backup") {
		t.Errorf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "admin-token")); string(b) != "tok" {
		t.Error("data was touched")
	}

	evil := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeArchive(t, evil, map[string]string{manifestName: `{"version":1}`, "../escape.txt": "x"})
	if _, err := Restore(context.Background(), evil, home, false, func(string, ...any) {}); err == nil ||
		!strings.Contains(err.Error(), "unsafe path") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(home), "escape.txt")); err == nil {
		t.Error("file written outside home")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "admin-token")); string(b) != "tok" {
		t.Error("data was moved before the backup was checked")
	}
}

func writeArchive(t *testing.T, file string, files map[string]string) {
	t.Helper()
	f, _ := os.Create(file)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()
}
