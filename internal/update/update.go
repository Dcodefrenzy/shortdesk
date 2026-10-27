// Package update replaces the running shortdesk binary with the latest GitHub release,
// checked against the release's checksums.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from. Set at build time with
// -ldflags "-X github.com/shortdesk/shortdesk/internal/update.Repo=owner/name".
var Repo = "Dcodefrenzy/shortdesk"

// baseURL is GitHub; tests point it elsewhere.
var baseURL = "https://github.com"

var client = &http.Client{Timeout: 5 * time.Minute}

// Latest returns the newest release's tag, e.g. "v0.1.2".
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, baseURL+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	// GitHub answers with a redirect to .../releases/tag/<tag>; no API rate limits.
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	tag := path.Base(resp.Header.Get("Location"))
	if !strings.Contains(resp.Header.Get("Location"), "/releases/tag/") || tag == "" {
		return "", fmt.Errorf("no releases found for %s", Repo)
	}
	return tag, nil
}

// Newer reports whether release tag a is newer than b (both like v1.2.3).
func Newer(a, b string) bool {
	pa, okA := parse(a)
	pb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// AssetName is the release file for this system.
func AssetName() string {
	ext := ".tar.gz"
	if goruntime.GOOS == "windows" {
		ext = ".zip"
	}
	return "shortdesk_" + goruntime.GOOS + "_" + goruntime.GOARCH + ext
}

// Apply downloads release tag, checks it, and replaces the executable at exe.
func Apply(ctx context.Context, tag, exe string) error {
	asset := AssetName()
	archive, err := download(ctx, tag, asset)
	if err != nil {
		return err
	}
	sums, err := download(ctx, tag, "checksums.txt")
	if err != nil {
		return err
	}
	if err := verify(archive, asset, sums); err != nil {
		return err
	}
	bin, err := extract(archive, asset)
	if err != nil {
		return err
	}
	return replace(exe, bin)
}

func download(ctx context.Context, tag, name string) ([]byte, error) {
	url := baseURL + "/" + Repo + "/releases/download/" + tag + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func verify(archive []byte, asset string, sums []byte) error {
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			if fields[0] != got {
				return fmt.Errorf("%s doesn't match its checksum; not installing it", asset)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum for %s in the release", asset)
}

// extract returns the shortdesk binary from a release archive.
func extract(archive []byte, asset string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == "shortdesk.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("shortdesk.exe not found in the release")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("shortdesk not found in the release")
		}
		if err != nil {
			return nil, err
		}
		if path.Base(h.Name) == "shortdesk" && h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// replace swaps exe for bin. The running program keeps working: on Windows, which
// can't overwrite a running .exe, the old one is moved aside first.
func replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".shortdesk-update-*")
	if err != nil {
		return fmt.Errorf("can't write to %s (%w); run the install command again instead", dir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if goruntime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), exe)
}
