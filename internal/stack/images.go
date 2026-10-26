package stack

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strings"

	"github.com/shortdesk/shortdesk/images"
)

// Images Shortdesk builds from the recipes embedded in the binary.
var builtImages = []struct{ Name, Dir string }{
	{"shortdesk/desk:dev", "desk"},
	{"shortdesk/vault-cli:dev", "vault-cli"},
}

// ensureImages builds any missing image from its embedded recipe.
func ensureImages(ctx context.Context, rebuild bool, log func(string, ...any)) error {
	for _, img := range builtImages {
		if !rebuild {
			if _, err := docker(ctx, "image", "inspect", img.Name); err == nil {
				continue
			}
		}
		log("Building %s (first time only; this can take several minutes)…", img.Name)
		if err := buildImage(ctx, img.Name, img.Dir); err != nil {
			return fmt.Errorf("build %s: %w", img.Name, err)
		}
	}
	return nil
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
