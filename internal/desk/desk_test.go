package desk

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/internal/runtime/runtimetest"
)

func mustCharter(t *testing.T, y string) *charter.Charter {
	t.Helper()
	c, err := charter.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const restricted = `
charter: assistant
tenant: ayo
permissions: {network: {allow: ["claude.ai", "*.anthropic.com"]}}
limits: {desk_lifetime: 90m}
`

func TestUpTranslatesCharter(t *testing.T) {
	rt := &runtimetest.Fake{Caps: runtime.Capabilities{EgressPolicy: true}}
	d, err := NewManager(rt).Up(context.Background(), mustCharter(t, restricted), "my-desk")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "my-desk" || d.Charter != "assistant" || d.Tenant != "ayo" {
		t.Errorf("desk = %+v", d)
	}
	s := rt.Created[0]
	if s.Image != charter.DefaultImage || s.Entrypoint[0] != Entrypoint || s.Lifetime != 90*time.Minute {
		t.Errorf("spec = %+v", s)
	}
	if strings.Join(s.AllowedHosts, ",") != "claude.ai,*.anthropic.com" {
		t.Errorf("allowed hosts = %v", s.AllowedHosts)
	}
	if s.Labels[LabelManaged] != "true" {
		t.Error("managed label missing")
	}
}

func TestUpUnrestrictedSendsNoAllowlist(t *testing.T) {
	rt := &runtimetest.Fake{}
	_, err := NewManager(rt).Up(context.Background(),
		mustCharter(t, "charter: a\npermissions: {network: {allow: ['*']}}"), "")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Created[0].AllowedHosts != nil {
		t.Errorf("allowed hosts = %v, want nil", rt.Created[0].AllowedHosts)
	}
	if !strings.HasPrefix(rt.Created[0].Labels[LabelName], "a-") {
		t.Errorf("generated name = %q", rt.Created[0].Labels[LabelName])
	}
}

func TestUpRefusesUnenforceableRules(t *testing.T) {
	rt := &runtimetest.Fake{Caps: runtime.Capabilities{EgressPolicy: false}}
	_, err := NewManager(rt).Up(context.Background(), mustCharter(t, restricted), "")
	if err == nil || !strings.Contains(err.Error(), "cannot enforce") {
		t.Errorf("err = %v, want refusal", err)
	}
	if len(rt.Created) != 0 {
		t.Error("box created despite unenforceable rules")
	}
}

func TestUpRejectsDuplicateName(t *testing.T) {
	rt := &runtimetest.Fake{Caps: runtime.Capabilities{EgressPolicy: true}}
	m := NewManager(rt)
	c := mustCharter(t, restricted)
	if _, err := m.Up(context.Background(), c, "same"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(context.Background(), c, "same"); err == nil {
		t.Error("duplicate name accepted")
	}
}

func TestFind(t *testing.T) {
	managed := func(name string) map[string]string {
		return map[string]string{LabelManaged: "true", LabelName: name}
	}
	rt := &runtimetest.Fake{Boxes: []runtime.Box{
		{ID: "1", Labels: managed("a")},
		{ID: "2", Labels: managed("b")},
		{ID: "3", Labels: map[string]string{LabelName: "not-ours"}},
	}}
	m := NewManager(rt)
	if d, err := m.Find(context.Background(), "b"); err != nil || d.ID != "2" {
		t.Errorf("by name: %+v %v", d, err)
	}
	if d, err := m.Find(context.Background(), "1"); err != nil || d.Name != "a" {
		t.Errorf("by id: %+v %v", d, err)
	}
	if _, err := m.Find(context.Background(), ""); err == nil {
		t.Error("empty ref with 2 desks should be ambiguous")
	}
	if _, err := m.Find(context.Background(), "3"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unmanaged box found: err = %v", err)
	}
}

func TestViewerURL(t *testing.T) {
	tests := []struct {
		ep   runtime.Endpoint
		want string
	}{
		{runtime.Endpoint{Host: "127.0.0.1:56109", PathPrefix: "/proxy/6080"},
			"http://127.0.0.1:56109/proxy/6080/?path=proxy%2F6080%2Fwebsockify"},
		{runtime.Endpoint{Host: "127.0.0.1:6081"},
			"http://127.0.0.1:6081/?path=websockify"},
	}
	for _, tt := range tests {
		ep := tt.ep
		rt := &runtimetest.Fake{EndpointFor: func(string, int) runtime.Endpoint { return ep }}
		got, err := NewManager(rt).ViewerURL(context.Background(), Desk{ID: "x"})
		if err != nil || got != tt.want {
			t.Errorf("ViewerURL(%+v) = %q, %v; want %q", tt.ep, got, err, tt.want)
		}
	}
}

func TestClipboardRoundTripEncoding(t *testing.T) {
	text := "line one\nline two\t\"quoted\" $HOME ✓\n"
	rt := &runtimetest.Fake{ExecOutput: base64.StdEncoding.EncodeToString([]byte(text)) + "\n"}
	m := NewManager(rt)

	if err := m.Paste(context.Background(), Desk{ID: "x"}, text); err != nil {
		t.Fatal(err)
	}
	sent, _ := base64.StdEncoding.DecodeString(rt.Execs[0].Env["SHORTDESK_CLIP"])
	if string(sent) != text {
		t.Errorf("pasted %q, want %q", sent, text)
	}

	got, err := m.Copy(context.Background(), Desk{ID: "x"})
	if err != nil || got != text {
		t.Errorf("Copy = %q, %v; want %q", got, err, text)
	}
}

func TestShareFoldersAttachesToNewDesks(t *testing.T) {
	rt := &runtimetest.Fake{Caps: runtime.Capabilities{EgressPolicy: true}}
	m := NewManager(rt)
	m.ShareFolders()
	if _, err := m.Up(context.Background(), mustCharter(t, restricted), "d"); err != nil {
		t.Fatal(err)
	}
	got := rt.Created[0].Mounts
	if len(got) != 2 || got[0].Path != "/home/desk/Developer" || got[0].Volume != "shortdesk-developer" {
		t.Errorf("mounts = %+v", got)
	}
}
