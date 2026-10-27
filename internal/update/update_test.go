package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func releaseArchive(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if goruntime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("shortdesk.exe")
		w.Write([]byte(content))
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"./LICENSE": "license", "./shortdesk": content} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func fakeGitHub(t *testing.T, archive []byte, checksum string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+Repo+"/releases/tag/v0.2.0", http.StatusFound)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/v0.2.0/"+AssetName(), func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/v0.2.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("abc123  shortdesk_other.tar.gz\n" + checksum + "  " + AssetName() + "\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })
}

func TestLatestAndApply(t *testing.T) {
	archive := releaseArchive(t, "new binary")
	sum := sha256.Sum256(archive)
	fakeGitHub(t, archive, hex.EncodeToString(sum[:]))

	ctx := context.Background()
	tag, err := Latest(ctx)
	if err != nil || tag != "v0.2.0" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
	exe := filepath.Join(t.TempDir(), "shortdesk")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	if err := Apply(ctx, tag, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Errorf("exe = %q", b)
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 && goruntime.GOOS != "windows" {
		t.Errorf("left files behind: %v", entries)
	}
}

func TestApplyRefusesBadChecksum(t *testing.T) {
	fakeGitHub(t, releaseArchive(t, "tampered"), strings.Repeat("0", 64))
	exe := filepath.Join(t.TempDir(), "shortdesk")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	err := Apply(context.Background(), "v0.2.0", exe)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old binary" {
		t.Errorf("exe changed: %q", b)
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v0.1.1", "v0.1.0", true},
		{"v0.2.0", "v0.1.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.1.1", false},
		{"v0.1.1", "dev", false},
	} {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%s, %s) = %v", tc.a, tc.b, got)
		}
	}
}
