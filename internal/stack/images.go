package stack

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"regexp"
	"strings"

	"github.com/shortdesk/shortdesk/images"
)

// Registry is where releases publish Shortdesk's images. Set at build time with
// -ldflags "-X github.com/shortdesk/shortdesk/internal/stack.Registry=...".
var Registry = "ghcr.io/dcodefrenzy"

// Shortdesk's images: the local name charters and the vault use, the name each release
// publishes it under, and the recipe embedded in the binary.
var builtImages = []struct{ Name, Published, Dir string }{
	{"shortdesk/desk:dev", "shortdesk-desk", "desk"},
	{"shortdesk/vault-cli:dev", "shortdesk-vault-cli", "vault-cli"},
}

// ensureImages makes sure each image exists locally. A release downloads the images
// published with it (and again after an upgrade); any other build, or a release
// whose download fails, builds them from the embedded recipes instead.
func ensureImages(ctx context.Context, version string, rebuild bool, log func(string, ...any)) error {
	for _, img := range builtImages {
		if !rebuild && releaseVersion.MatchString(version) {
			remote := Registry + "/" + img.Published + ":" + strings.TrimPrefix(version, "v")
			if sameImage(ctx, img.Name, remote) {
				continue
			}
			log("Downloading %s (first time only)…", remote)
			if _, err := docker(ctx, "pull", "-q", remote); err == nil {
				if _, err := docker(ctx, "tag", remote, img.Name); err != nil {
					return err
				}
				continue
			} else {
				log("Couldn't download it (%v); building it here instead.", err)
			}
		} else if !rebuild {
			if _, err := docker(ctx, "image", "inspect", img.Name); err == nil {
				continue
			}
		}
		log("Building %s (this can take several minutes)…", img.Name)
		if err := buildImage(ctx, img.Name, img.Dir); err != nil {
			return fmt.Errorf("build %s: %w", img.Name, err)
		}
	}
	return nil
}

// releaseVersion matches tagged releases (v1.2.3, v1.2.3-rc.1), not local builds such
// as v1.2.3-dirty or v1.2.3-4-gabc1234.
var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-(rc|alpha|beta)(\.?\d+)*)?$`)

// IsRelease reports whether version is a tagged release rather than a local build.
func IsRelease(version string) bool { return releaseVersion.MatchString(version) }

// sameImage reports whether both names exist locally and are the same image.
func sameImage(ctx context.Context, a, b string) bool {
	idA, errA := docker(ctx, "image", "inspect", "-f", "{{.Id}}", a)
	idB, errB := docker(ctx, "image", "inspect", "-f", "{{.Id}}", b)
	return errA == nil && errB == nil && idA == idB
}

// buildImage streams an embedded directory to `docker build` as its build context.
func buildImage(ctx context.Context, tag, dir string) error {
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeTar(pw, dir)) }()
	cmd := exec.CommandContext(ctx, "docker", "build", "-q", "-t", tag, "-")
	cmd.Stdin = pr
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, tail(string(out), 1500))
	}
	return nil
}

func writeTar(w io.Writer, dir string) error {
	tw := tar.NewWriter(w)
	sub, err := fs.Sub(images.FS, dir)
	if err != nil {
		return err
	}
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = path
		// Embedded files lose their modes: scripts must stay executable.
		if d.IsDir() {
			hdr.Mode = 0o755
		} else if strings.Contains(path, "/bin/") || strings.HasSuffix(path, ".sh") {
			hdr.Mode = 0o755
		} else {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := sub.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
