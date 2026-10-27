// Package logins keeps every desk logged in to the same things, automatically.
//
// Shortdesk regularly looks at running desks for logins (app login files and website
// cookies, see desk.sh). A new or changed login is saved and given to every other
// running desk, and every new desk starts with all saved logins. A login can be
// revoked on one desk: it is removed there and never given back until allowed again.
//
// Logins are saved under dir with owner-only access, one file per login.
package logins

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

//go:embed desk.sh
var deskScript string

// Kinds of login.
const (
	KindApp  = "app"
	KindSite = "site"
)

// Apps lists the app logins desk.sh knows how to find.
var Apps = []string{"claude-code", "github-cli", "git"}

var siteRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// Key identifies a login, e.g. "site/instagram.com" or "app/claude-code".
type Key struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (k Key) String() string { return k.Kind + "/" + k.Name }

// Valid reports whether the key names a login Shortdesk handles.
func (k Key) Valid() bool {
	switch k.Kind {
	case KindApp:
		return slices.Contains(Apps, k.Name)
	case KindSite:
		return siteRe.MatchString(k.Name)
	}
	return false
}

// Login is a saved login.
type Login struct {
	Key
	Hash      string    `json:"hash"`
	From      string    `json:"from"` // name of the desk it was last saved from
	UpdatedAt time.Time `json:"updatedAt"`
}

// Status of a saved login on one desk.
type Status string

const (
	StatusActive  Status = "active"  // the desk has it
	StatusRevoked Status = "revoked" // removed from this desk; not given back until allowed
	StatusMissing Status = "missing" // allowed, but the desk doesn't have it (e.g. browser closed)
)

// DeskLogin is a saved login as seen from one desk.
type DeskLogin struct {
	Login
	Status Status `json:"status"`
}

type found struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type index struct {
	Logins  map[string]Login           `json:"logins"`
	Revoked map[string]map[string]bool `json:"revoked"` // desk ID -> login key -> true
}

// Manager captures, stores and distributes logins.
type Manager struct {
	rt  runtime.Runtime
	dir string
	log *slog.Logger

	mu  sync.Mutex
	idx index
}

// New loads the saved logins in dir.
func New(rt runtime.Runtime, dir string, log *slog.Logger) (*Manager, error) {
	m := &Manager{rt: rt, dir: dir, log: log, idx: index{Logins: map[string]Login{}, Revoked: map[string]map[string]bool{}}}
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err == nil {
		if err := json.Unmarshal(b, &m.idx); err != nil {
			return nil, fmt.Errorf("logins index: %w", err)
		}
		if m.idx.Revoked == nil {
			m.idx.Revoked = map[string]map[string]bool{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return m, nil
}

// Saved lists all saved logins, sorted.
func (m *Manager) Saved() []Login {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Login, 0, len(m.idx.Logins))
	for _, l := range m.idx.Logins {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.String() < out[j].Key.String() })
	return out
}

// Desk lists saved logins with their status on one desk.
func (m *Manager) Desk(ctx context.Context, deskID string) ([]DeskLogin, error) {
	have, err := m.scan(ctx, deskID, true)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, f := range have {
		present[Key{f.Kind, f.Name}.String()] = true
	}
	m.mu.Lock()
	revoked := m.idx.Revoked[deskID]
	m.mu.Unlock()
	out := []DeskLogin{}
	for _, l := range m.Saved() {
		st := StatusMissing
		switch {
		case revoked[l.Key.String()]:
			st = StatusRevoked
		case present[l.Key.String()]:
			st = StatusActive
		}
		out = append(out, DeskLogin{Login: l, Status: st})
	}
	return out, nil
}

// Capture saves new or changed logins from a desk and gives them to the other running
// desks. Logins revoked on this desk are ignored.
func (m *Manager) Capture(ctx context.Context, deskID, deskName string, others []string) error {
	have, err := m.scan(ctx, deskID, false)
	if err != nil {
		return err
	}
	for _, f := range have {
		key := Key{f.Kind, f.Name}
		if !key.Valid() {
			continue
		}
		m.mu.Lock()
		saved, ok := m.idx.Logins[key.String()]
		revoked := m.idx.Revoked[deskID][key.String()]
		m.mu.Unlock()
		if revoked || (ok && saved.Hash == f.Hash) {
			continue
		}
		if err := m.export(ctx, deskID, key); err != nil {
			m.log.Warn("could not save login", "login", key, "desk", deskName, "err", err)
			continue
		}
		m.mu.Lock()
		m.idx.Logins[key.String()] = Login{Key: key, Hash: f.Hash, From: deskName, UpdatedAt: time.Now()}
		err := m.saveIndexLocked()
		m.mu.Unlock()
		if err != nil {
			return err
		}
		m.log.Info("saved login", "login", key, "from", deskName)
		for _, other := range others {
			if other == deskID || m.isRevoked(other, key) {
				continue
			}
			if err := m.give(ctx, other, key); err != nil {
				m.log.Warn("could not give login to desk", "login", key, "desk", other, "err", err)
			}
		}
	}
	return nil
}

// ApplyAll gives a desk every saved login it isn't revoked from.
func (m *Manager) ApplyAll(ctx context.Context, deskID string) error {
	var errs []error
	for _, l := range m.Saved() {
		if m.isRevoked(deskID, l.Key) {
			continue
		}
		if err := m.give(ctx, deskID, l.Key); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", l.Key, err))
		}
	}
	return errors.Join(errs...)
}

// Revoke removes a login from one desk and keeps it away.
func (m *Manager) Revoke(ctx context.Context, deskID string, key Key) error {
	if !key.Valid() {
		return fmt.Errorf("unknown login %s", key)
	}
	m.mu.Lock()
	if m.idx.Revoked[deskID] == nil {
		m.idx.Revoked[deskID] = map[string]bool{}
	}
	m.idx.Revoked[deskID][key.String()] = true
	err := m.saveIndexLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return m.run(ctx, deskID, nil, "remove", key.Kind, key.Name)
}

// Allow lifts a revoke and gives the login back to the desk.
func (m *Manager) Allow(ctx context.Context, deskID string, key Key) error {
	if !key.Valid() {
		return fmt.Errorf("unknown login %s", key)
	}
	m.mu.Lock()
	delete(m.idx.Revoked[deskID], key.String())
	err := m.saveIndexLocked()
	_, saved := m.idx.Logins[key.String()]
	m.mu.Unlock()
	if err != nil || !saved {
		return err
	}
	return m.give(ctx, deskID, key)
}

// Forget drops a removed desk's revokes.
func (m *Manager) Forget(deskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.idx.Revoked, deskID)
	m.saveIndexLocked()
}

func (m *Manager) isRevoked(deskID string, key Key) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idx.Revoked[deskID][key.String()]
}

// scan lists the logins on a desk. Normally a site counts only once the desk's browser
// has visited it (so tracker cookies aren't saved as logins); all counts every site with
// cookies, which is right for checking whether a login given to the desk is there.
func (m *Manager) scan(ctx context.Context, deskID string, all bool) ([]found, error) {
	args := []string{"list"}
	if all {
		args = append(args, "--all")
	}
	var out bytes.Buffer
	if err := m.run(ctx, deskID, &out, args...); err != nil {
		return nil, err
	}
	var res []found
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		var f found
		if json.Unmarshal(sc.Bytes(), &f) == nil && f.Name != "" {
			res = append(res, f)
		}
	}
	return res, nil
}

// boxFile is where a login is staged inside a desk while being copied.
func boxFile(key Key) string { return "/tmp/shortdesk-login-" + key.Kind + "-" + key.Name }

func (m *Manager) file(key Key) string { return filepath.Join(m.dir, key.Kind+"-"+key.Name) }

func (m *Manager) export(ctx context.Context, deskID string, key Key) error {
	files, ok := m.rt.(runtime.Files)
	if !ok {
		return errors.New("this runtime can't copy files out of desks")
	}
	if err := m.run(ctx, deskID, nil, "export", key.Kind, key.Name, boxFile(key)); err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.dir, ".incoming-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = files.Download(ctx, deskID, boxFile(key), tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	m.rt.Exec(ctx, deskID, runtime.ExecRequest{Command: "rm -f " + boxFile(key)})
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), m.file(key))
}

// give copies a saved login into a desk.
func (m *Manager) give(ctx context.Context, deskID string, key Key) error {
	files, ok := m.rt.(runtime.Files)
	if !ok {
		return errors.New("this runtime can't copy files into desks")
	}
	f, err := os.Open(m.file(key))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := files.Upload(ctx, deskID, boxFile(key), f); err != nil {
		return err
	}
	defer m.rt.Exec(ctx, deskID, runtime.ExecRequest{Command: "rm -f " + boxFile(key)})
	return m.run(ctx, deskID, nil, "import", key.Kind, key.Name, boxFile(key))
}

// run executes desk.sh inside the desk. The script travels in the environment, so desks
// don't need it installed.
func (m *Manager) run(ctx context.Context, deskID string, stdout *bytes.Buffer, args ...string) error {
	var stderr bytes.Buffer
	req := runtime.ExecRequest{
		Command: `bash -c "$SHORTDESK_LOGINS" logins ` + strings.Join(args, " "),
		Env:     map[string]string{"SHORTDESK_LOGINS": deskScript, "HOME": "/home/desk", "DISPLAY": ":0"},
		Stderr:  &stderr,
	}
	if stdout != nil {
		req.Stdout = stdout
	}
	if err := m.rt.Exec(ctx, deskID, req); err != nil {
		return fmt.Errorf("%s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (m *Manager) saveIndexLocked() error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m.idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(m.dir, ".index.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, "index.json"))
}
