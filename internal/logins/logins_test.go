package logins

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/internal/runtime/runtimetest"
)

// fakeDesks plays desk.sh: each desk holds logins as name -> content.
type fakeDesks struct {
	runtimetest.FakeWithPolicy
	mu    sync.Mutex
	desks map[string]map[string]string // desk -> "kind/name" -> content
}

func (f *fakeDesks) Exec(ctx context.Context, id string, req runtime.ExecRequest) error {
	args := strings.Fields(strings.TrimPrefix(req.Command, `bash -c "$SHORTDESK_LOGINS" logins `))
	if len(args) == 0 || strings.HasPrefix(req.Command, "rm -f") {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.desks[id]
	if d == nil {
		d = map[string]string{}
		f.desks[id] = d
	}
	switch args[0] {
	case "list":
		for k, v := range d {
			kind, name, _ := strings.Cut(k, "/")
			fmt.Fprintf(req.Stdout, `{"kind":%q,"name":%q,"hash":%q}`+"\n", kind, name, v)
		}
	case "export":
		f.FakeWithPolicy.Files.Upload(ctx, id, args[3], strings.NewReader(d[args[1]+"/"+args[2]]))
	case "import":
		var b strings.Builder
		f.FakeWithPolicy.Files.Download(ctx, id, args[3], &b)
		d[args[1]+"/"+args[2]] = b.String()
	case "remove":
		delete(d, args[1]+"/"+args[2])
	}
	return nil
}

func (f *fakeDesks) has(desk, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.desks[desk][key]
	return v, ok
}

func newTest(t *testing.T) (*Manager, *fakeDesks) {
	t.Helper()
	f := &fakeDesks{desks: map[string]map[string]string{}}
	m, err := New(f, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return m, f
}

func TestCaptureSavesAndPushesToOtherDesks(t *testing.T) {
	m, f := newTest(t)
	ctx := context.Background()
	f.desks["a"] = map[string]string{"site/instagram.com": "IG-SESSION", "app/claude-code": "CLAUDE"}

	if err := m.Capture(ctx, "a", "desk-a", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if len(m.Saved()) != 2 {
		t.Fatalf("saved = %+v", m.Saved())
	}
	if v, _ := f.has("b", "site/instagram.com"); v != "IG-SESSION" {
		t.Error("running desk b did not get the new login")
	}

	// A brand-new desk starts with everything.
	if err := m.ApplyAll(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.has("c", "app/claude-code"); v != "CLAUDE" {
		t.Error("new desk c did not get claude-code")
	}
}

func TestRevokeRemovesAndKeepsAway(t *testing.T) {
	m, f := newTest(t)
	ctx := context.Background()
	f.desks["a"] = map[string]string{"site/instagram.com": "IG-1"}
	m.Capture(ctx, "a", "desk-a", []string{"a", "b"})

	ig := Key{KindSite, "instagram.com"}
	if err := m.Revoke(ctx, "b", ig); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.has("b", "site/instagram.com"); ok {
		t.Fatal("revoke did not remove the login")
	}
	st, _ := m.Desk(ctx, "b")
	if len(st) != 1 || st[0].Status != StatusRevoked {
		t.Errorf("status on b = %+v", st)
	}

	// A fresh login elsewhere is not pushed to the desk it's revoked on...
	f.desks["a"]["site/instagram.com"] = "IG-2"
	m.Capture(ctx, "a", "desk-a", []string{"a", "b"})
	if _, ok := f.has("b", "site/instagram.com"); ok {
		t.Error("revoked desk got the login again")
	}
	// ...until it's allowed again.
	if err := m.Allow(ctx, "b", ig); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.has("b", "site/instagram.com"); v != "IG-2" {
		t.Errorf("after allow, b has %q", v)
	}
}

func TestUnchangedLoginsAreNotResaved(t *testing.T) {
	m, f := newTest(t)
	ctx := context.Background()
	f.desks["a"] = map[string]string{"app/claude-code": "C1"}
	m.Capture(ctx, "a", "desk-a", nil)
	first := m.Saved()[0].UpdatedAt
	m.Capture(ctx, "a", "desk-a", nil)
	if !m.Saved()[0].UpdatedAt.Equal(first) {
		t.Error("unchanged login was saved again")
	}
}

func TestIndexSurvivesRestart(t *testing.T) {
	m, f := newTest(t)
	ctx := context.Background()
	f.desks["a"] = map[string]string{"app/claude-code": "C1"}
	m.Capture(ctx, "a", "desk-a", nil)
	m.Revoke(ctx, "z", Key{KindApp, "claude-code"})

	again, err := New(f, m.dir, m.log)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Saved()) != 1 || !again.isRevoked("z", Key{KindApp, "claude-code"}) {
		t.Errorf("after restart: saved=%+v", again.Saved())
	}
}

func TestKeysAreValidated(t *testing.T) {
	m, _ := newTest(t)
	for _, k := range []Key{{KindSite, "../../etc"}, {KindApp, "ssh"}, {"file", "x"}} {
		if err := m.Allow(context.Background(), "d", k); err == nil {
			t.Errorf("Allow accepted %s", k)
		}
		if err := m.Revoke(context.Background(), "d", k); err == nil {
			t.Errorf("Revoke accepted %s", k)
		}
	}
}
