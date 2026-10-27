// Package backup saves and restores everything Shortdesk keeps in ~/.shortdesk (settings
// and keys, the vault, saved logins, chats, charters) and, optionally, the shared folders.
//
// A backup holds secrets: it is written readable by the owner only. Restoring never
// deletes anything: existing data is moved aside first.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	manifestName   = "shortdesk-backup.json"
	vaultContainer = "shortdesk-vault"
	vaultDB        = "vault/data/db.sqlite3"
	// helperImage reads and writes the shared folders' storage (it has tar).
	helperImage = "shortdesk/desk:dev"
)

// Volumes are the shared folders' storage, as named in Docker.
var Volumes = []string{"shortdesk-developer", "shortdesk-files"}

// PhoneVolume holds the person's apps and their data; every backup includes it.
const PhoneVolume = "shortdesk-phone"

// skipped are paths under ~/.shortdesk that are rebuilt on start or only make sense on
// this machine: OpenSandbox's settings and records, the vault's scratch space and the
// vault CLI's session.
var skipped = []string{"opensandbox", "vault/data/tmp", "vault/cli", "vault/data/db.sqlite3-wal", "vault/data/db.sqlite3-shm"}

type manifest struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Volumes []string  `json:"volumes,omitempty"`
}

// docker runs the docker CLI; tests replace it.
var docker = func(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %s", args[0], strings.TrimSpace(stderr.String()+" "+err.Error()))
	}
	return nil
}

// Create writes a backup of home (~/.shortdesk) to the new file out. With files, the
// shared folders are included too.
func Create(ctx context.Context, home, out string, files bool, log func(string, ...any)) error {
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(out)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	// The vault's database is copied with Vaultwarden's own backup while it runs, so the
	// copy is consistent; when it's stopped, the file itself is.
	snapshot, err := vaultSnapshot(ctx, home)
	if err != nil {
		return err
	}
	if snapshot != "" {
		defer os.Remove(snapshot)
		if err := addFile(tw, snapshot, vaultDB); err != nil {
			return err
		}
	}

	err = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(home, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if slices.Contains(skipped, rel) || isSnapshotName(rel) || (snapshot != "" && rel == vaultDB) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		return addFile(tw, p, rel)
	})
	if err != nil {
		return err
	}

	m := manifest{Version: 1, Created: time.Now().UTC()}
	// The person's apps and their data are always kept; the shared folders on request.
	vols := []string{PhoneVolume}
	if files {
		vols = append(vols, Volumes...)
	}
	for _, vol := range vols {
		if err := docker(ctx, nil, io.Discard, "volume", "inspect", vol); err != nil {
			continue // never created
		}
		log("Adding %s…", vol)
		if err := addVolume(ctx, tw, vol); err != nil {
			return err
		}
		m.Volumes = append(m.Volumes, vol)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := addBytes(tw, manifestName, b); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func isSnapshotName(rel string) bool {
	dir, name := path.Split(rel)
	return dir == "vault/data/" && strings.HasPrefix(name, "db_") && strings.HasSuffix(name, ".sqlite3")
}

// vaultSnapshot asks a running vault for a consistent copy of its database and returns
// the copy's path on this machine, or "" when the vault isn't running.
func vaultSnapshot(ctx context.Context, home string) (string, error) {
	var state bytes.Buffer
	if docker(ctx, nil, &state, "inspect", "-f", "{{.State.Running}}", vaultContainer) != nil ||
		strings.TrimSpace(state.String()) != "true" {
		return "", nil
	}
	dir := filepath.Join(home, "vault", "data")
	before, _ := filepath.Glob(filepath.Join(dir, "db_*.sqlite3"))
	if err := docker(ctx, nil, io.Discard, "exec", vaultContainer, "/vaultwarden", "backup"); err != nil {
		return "", fmt.Errorf("vault backup: %w", err)
	}
	after, _ := filepath.Glob(filepath.Join(dir, "db_*.sqlite3"))
	for _, p := range after {
		if !slices.Contains(before, p) {
			return p, nil
		}
	}
	return "", errors.New("the vault didn't produce a backup")
}

func addFile(tw *tar.Writer, src, name string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func addBytes(tw *tar.Writer, name string, b []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(b)), ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

// addVolume stores a volume's contents as volumes/<name>.tar. The size must be known
// up front, so it's staged in a temporary file.
func addVolume(ctx context.Context, tw *tar.Writer, vol string) error {
	tmp, err := os.CreateTemp("", "shortdesk-volume-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := docker(ctx, nil, tmp, "run", "--rm", "--user", "0", "--network", "none", "-v", vol+":/v:ro",
		"--entrypoint", "tar", helperImage, "--numeric-owner", "-cf", "-", "-C", "/v", "."); err != nil {
		return fmt.Errorf("read shared folder %s: %w", vol, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return addFile(tw, tmp.Name(), "volumes/"+vol+".tar")
}

// Running reports whether Shortdesk seems to be running, which restoring must not race.
func Running(ctx context.Context, dashboard string) bool {
	if c, err := net.DialTimeout("tcp", dashboard, time.Second); err == nil {
		c.Close()
		return true
	}
	var state bytes.Buffer
	return docker(ctx, nil, &state, "inspect", "-f", "{{.State.Running}}", vaultContainer) == nil &&
		strings.TrimSpace(state.String()) == "true"
}

// Restore puts a backup back into home. Existing data at home is moved to a
// "home.before-restore-<time>" folder first and the path is returned. With files, the
// shared folders are restored into folders that are empty or don't exist yet.
func Restore(ctx context.Context, file, home string, files bool, log func(string, ...any)) (movedTo string, err error) {
	if err := check(file); err != nil {
		return "", err
	}
	if _, err := os.Stat(home); err == nil {
		movedTo = home + ".before-restore-" + time.Now().Format("20060102-150405")
		if err := os.Rename(home, movedTo); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return movedTo, err
	}
	f, err := os.Open(file)
	if err != nil {
		return movedTo, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return movedTo, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return movedTo, nil
		}
		if err != nil {
			return movedTo, err
		}
		name := path.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || name == manifestName {
			continue
		}
		if vol, ok := strings.CutPrefix(name, "volumes/"); ok {
			vol = strings.TrimSuffix(vol, ".tar")
			if !files && vol != PhoneVolume {
				continue
			}
			if !slices.Contains(Volumes, vol) && vol != PhoneVolume {
				return movedTo, fmt.Errorf("unexpected shared folder %q in backup", vol)
			}
			if err := restoreVolume(ctx, tr, vol, log); err != nil {
				return movedTo, err
			}
			continue
		}
		if !safeName(name) {
			return movedTo, fmt.Errorf("unsafe path %q in backup", h.Name)
		}
		dst := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return movedTo, err
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(h.Mode).Perm()|0o600)
		if err != nil {
			return movedTo, err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return movedTo, err
		}
	}
}

func safeName(name string) bool {
	return name != "." && !path.IsAbs(name) && name != ".." && !strings.HasPrefix(name, "../") && !strings.Contains(name, `\`)
}

// check makes sure file is a whole, safe Shortdesk backup before anything is moved.
func check(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s isn't a Shortdesk backup: %w", file, err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s is damaged: %w", file, err)
		}
		if !safeName(path.Clean(h.Name)) {
			return fmt.Errorf("unsafe path %q in backup", h.Name)
		}
		if h.Name == manifestName {
			var m manifest
			if err := json.NewDecoder(tr).Decode(&m); err != nil || m.Version != 1 {
				return fmt.Errorf("%s: unsupported backup format", file)
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%s isn't a Shortdesk backup (no %s)", file, manifestName)
	}
	return nil
}

func restoreVolume(ctx context.Context, r io.Reader, vol string, log func(string, ...any)) error {
	var listing bytes.Buffer
	if docker(ctx, nil, io.Discard, "volume", "inspect", vol) == nil {
		if err := docker(ctx, nil, &listing, "run", "--rm", "--user", "0", "--network", "none", "-v", vol+":/v:ro",
			"--entrypoint", "ls", helperImage, "-A", "/v"); err != nil {
			return err
		}
		if strings.TrimSpace(listing.String()) != "" {
			log("Skipped shared folder %s: it already has files. Empty it first to restore it.", vol)
			_, err := io.Copy(io.Discard, r)
			return err
		}
	}
	log("Restoring shared folder %s…", vol)
	return docker(ctx, r, io.Discard, "run", "--rm", "-i", "--user", "0", "--network", "none", "-v", vol+":/v",
		"--entrypoint", "tar", helperImage, "--numeric-owner", "-xpf", "-", "-C", "/v")
}
