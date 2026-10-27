package phone

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Manifest is an app's app.json: what the app is and what it keeps.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`  // an emoji
	Color       string `json:"color"` // the icon tile, #rrggbb
	Description string `json:"description"`
	Version     string `json:"version"`
	// Collections are the kinds of records the app keeps, e.g. ["messages"].
	Collections []string `json:"collections"`
	// CopiedFrom names the app this one was copied from, e.g. "tracker@1.2.0".
	CopiedFrom string `json:"copiedFrom,omitempty"`
	// Logins are the websites the app's work needs logins for, e.g. ["wg-gesucht.de"].
	Logins []string `json:"logins,omitempty"`
}

// App is an installed app.
type App struct {
	Manifest
	Builtin   bool      `json:"builtin"` // shipped with Shortdesk and not changed since
	Installed time.Time `json:"installed"`
	CanUndo   bool      `json:"canUndo"` // a previous version is kept
}

var (
	appIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	collRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

var errBadApp = errors.New("invalid app")

func badApp(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errBadApp, fmt.Sprintf(format, a...))
}

func (m Manifest) check() error {
	switch {
	case !appIDRe.MatchString(m.ID):
		return badApp("id %q: use lower case letters, digits and dashes", m.ID)
	case strings.TrimSpace(m.Name) == "" || len(m.Name) > 40:
		return badApp("name is required (at most 40 characters)")
	case m.Icon == "" || len(m.Icon) > 16:
		return badApp("icon must be an emoji")
	case m.Color != "" && !colorRe.MatchString(m.Color):
		return badApp("color must look like #ffd66b")
	case len(m.Description) > 300:
		return badApp("description is too long")
	case m.Version == "" || len(m.Version) > 20:
		return badApp("version is required, e.g. 1.0.0")
	case len(m.Collections) > 20:
		return badApp("at most 20 collections")
	}
	for _, c := range m.Collections {
		if !collRe.MatchString(c) {
			return badApp("collection %q: use lower case letters, digits and underscores", c)
		}
	}
	return nil
}

func (m Manifest) keeps(coll string) bool {
	for _, c := range m.Collections {
		if c == coll {
			return true
		}
	}
	return false
}

const (
	maxPackage      = 20 << 20
	maxPackageFiles = 500
	builtinMarker   = ".builtin"
)

// apps keeps installed apps as folders: dir/<id>/app.json and dir/<id>/web/…, with the
// previous version in dir/<id>.prev.
type apps struct{ dir string }

func (a apps) path(id string) string { return filepath.Join(a.dir, id) }

func (a apps) list() ([]App, error) {
	entries, err := os.ReadDir(a.dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := []App{}
	for _, e := range entries {
		if !e.IsDir() || !appIDRe.MatchString(e.Name()) {
			continue
		}
		if app, err := a.get(e.Name()); err == nil {
			out = append(out, app)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (a apps) get(id string) (App, error) {
	if !appIDRe.MatchString(id) {
		return App{}, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(a.path(id), "app.json"))
	if err != nil {
		return App{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return App{}, err
	}
	info, _ := os.Stat(filepath.Join(a.path(id), "app.json"))
	app := App{Manifest: m}
	if info != nil {
		app.Installed = info.ModTime().UTC()
	}
	_, err = os.Stat(filepath.Join(a.path(id), builtinMarker))
	app.Builtin = err == nil
	_, err = os.Stat(a.path(id) + ".prev")
	app.CanUndo = err == nil
	return app, nil
}

// installTar installs or updates an app from a .tar.gz of its folder.
func (a apps) installTar(r io.Reader) (Manifest, error) {
	gz, err := gzip.NewReader(io.LimitReader(r, maxPackage))
	if err != nil {
		return Manifest{}, badApp("the package must be a .tar.gz of the app's folder")
	}
	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	total := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, badApp("reading the package: %v", err)
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return Manifest{}, badApp("%s: only plain files are allowed", h.Name)
		}
		name, ok := cleanName(h.Name)
		if !ok {
			return Manifest{}, badApp("%s: bad file name", h.Name)
		}
		if len(files) >= maxPackageFiles {
			return Manifest{}, badApp("too many files (at most %d)", maxPackageFiles)
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxPackage+1))
		if err != nil {
			return Manifest{}, badApp("reading %s: %v", name, err)
		}
		if total += len(b); total > maxPackage {
			return Manifest{}, badApp("the app is too big (at most %d MB)", maxPackage>>20)
		}
		files[name] = b
	}
	return a.install(files, false)
}

// installFS installs a built-in app from fsys (a folder with app.json and web/).
func (a apps) installFS(fsys fs.FS) (Manifest, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		files[p] = b
		return err
	})
	if err != nil {
		return Manifest{}, err
	}
	return a.install(files, true)
}

func (a apps) install(files map[string][]byte, builtin bool) (Manifest, error) {
	raw, ok := files["app.json"]
	if !ok {
		return Manifest{}, badApp("app.json is missing")
	}
	if _, ok := files["web/index.html"]; !ok {
		return Manifest{}, badApp("web/index.html is missing")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, badApp("app.json: %v", err)
	}
	if err := m.check(); err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return Manifest{}, err
	}
	tmp := filepath.Join(a.dir, ".new-"+randHex())
	for name, b := range files {
		p := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Manifest{}, err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			os.RemoveAll(tmp)
			return Manifest{}, err
		}
	}
	if builtin {
		os.WriteFile(filepath.Join(tmp, builtinMarker), nil, 0o644)
	}
	// The old version becomes the one undo goes back to.
	dst := a.path(m.ID)
	os.RemoveAll(dst + ".prev")
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, dst+".prev"); err != nil {
			os.RemoveAll(tmp)
			return Manifest{}, err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// undo puts back the previous version.
func (a apps) undo(id string) error {
	dst := a.path(id)
	if _, err := os.Stat(dst + ".prev"); err != nil {
		return fmt.Errorf("%w: no earlier version of %s", os.ErrNotExist, id)
	}
	gone := dst + ".gone-" + randHex()
	if err := os.Rename(dst, gone); err != nil {
		return err
	}
	if err := os.Rename(dst+".prev", dst); err != nil {
		os.Rename(gone, dst)
		return err
	}
	return os.RemoveAll(gone)
}

func (a apps) remove(id string) error {
	if !appIDRe.MatchString(id) {
		return os.ErrNotExist
	}
	os.RemoveAll(a.path(id) + ".prev")
	return os.RemoveAll(a.path(id))
}

// cleanName keeps a package path inside the app's folder.
func cleanName(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false
		}
	}
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" || name == "." || strings.HasPrefix(name, "..") {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.HasPrefix(part, ".") && part != ".well-known" {
			return "", false
		}
	}
	return name, true
}

func randHex() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
