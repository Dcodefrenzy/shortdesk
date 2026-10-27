// Package access manages which websites a desk may reach: the charter's allowlist
// plus per-desk additions, changed live on the running desk. Agents can only ask
// (shortdesk-request-access); a human decides.
package access

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/runtime"
)

// Where a rule came from.
const (
	SourceCharter  = "charter"
	SourceSettings = "settings" // added in the desk's Settings tab
	SourceChat     = "chat"     // approved from an agent's request
	SourceModels   = "models"   // the API of a model the router uses (Workspace settings)
)

// Decision answers an agent's access request.
type Decision string

const (
	AllowDesk    Decision = "desk"    // allow on this desk only
	AllowCharter Decision = "charter" // allow and add to the charter (all its desks, now and later)
	Deny         Decision = "deny"
)

// Errors callers can check for.
var (
	ErrNotEditable    = errors.New("this desk's website rules can't be changed")
	ErrUnknownRequest = errors.New("no pending access request with that id")
)

// Rule is one allowed host and its origin.
type Rule struct {
	Host    string    `json:"host"`
	Source  string    `json:"source"`
	AddedAt time.Time `json:"addedAt,omitzero"`
}

// Rules is a desk's effective network access.
type Rules struct {
	Unrestricted bool `json:"unrestricted"` // the charter allows every website
	Editable     bool `json:"editable"`
	// AllowAll is the owner's switch to let this desk reach every website for now.
	// Hosts still lists the rules that apply again when it is turned off.
	AllowAll bool   `json:"allowAll"`
	Hosts    []Rule `json:"hosts"`
}

type request struct {
	deskID, host string
}

// Manager keeps per-desk additions (in a file, with Persist) and applies them to
// running desks.
type Manager struct {
	desks       *desk.Manager
	runner      *agent.Runner
	chartersDir string
	log         *slog.Logger

	mu        sync.Mutex
	path      string            // where additions are saved; empty keeps them in memory
	overrides map[string][]Rule // desk ID -> added rules
	allowAll  map[string]bool   // desk ID -> every website allowed
	pending   map[string]request
}

// New returns a Manager and registers it to receive agents' access requests.
func New(desks *desk.Manager, runner *agent.Runner, chartersDir string, log *slog.Logger) *Manager {
	m := &Manager{
		desks: desks, runner: runner, chartersDir: chartersDir, log: log,
		overrides: map[string][]Rule{}, allowAll: map[string]bool{}, pending: map[string]request{},
	}
	runner.SetEventHook(m.onAgentEvent)
	return m
}

// state is what Persist saves.
type state struct {
	Overrides map[string][]Rule `json:"overrides"`
	AllowAll  map[string]bool   `json:"allowAll"`
}

// Persist loads per-desk additions from path and saves them there after every change,
// so a restart keeps what the owner allowed.
func (m *Manager) Persist(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if st.Overrides != nil {
		m.overrides = st.Overrides
	}
	if st.AllowAll != nil {
		m.allowAll = st.AllowAll
	}
	return nil
}

// saveLocked writes the additions to the Persist file, if any. m.mu must be held.
func (m *Manager) saveLocked() {
	if m.path == "" {
		return
	}
	b, err := json.MarshalIndent(state{Overrides: m.overrides, AllowAll: m.allowAll}, "", "  ")
	if err == nil {
		tmp := m.path + ".tmp"
		if err = os.WriteFile(tmp, b, 0o600); err == nil {
			err = os.Rename(tmp, m.path)
		}
	}
	if err != nil {
		m.log.Warn("could not save website rules", "path", m.path, "err", err)
	}
}

// onAgentEvent gives each access request an ID so a human can answer it.
func (m *Manager) onAgentEvent(deskID string, ev *agent.Event) {
	if ev.Kind != agent.KindAccessRequest {
		return
	}
	id := newID()
	m.mu.Lock()
	m.pending[id] = request{deskID: deskID, host: ev.Data["host"]}
	m.mu.Unlock()
	ev.Data["requestId"] = id
}

// Rules returns the desk's effective rules: charter first, then additions.
func (m *Manager) Rules(d desk.Desk) (Rules, error) {
	c, err := m.charter(d.Charter)
	if err != nil {
		return Rules{}, err
	}
	_, canSet := m.desks.Runtime().(runtime.NetworkPolicy)
	r := Rules{Unrestricted: c.Permissions.Network.Unrestricted(), Hosts: []Rule{}}
	r.Editable = canSet && !r.Unrestricted
	if r.Unrestricted {
		return r, nil
	}
	for _, h := range c.Permissions.Network.Allow {
		r.Hosts = append(r.Hosts, Rule{Host: h, Source: SourceCharter})
	}
	for _, h := range m.desks.ExtraAllowedHosts() {
		if !hasHost(r.Hosts, h) {
			r.Hosts = append(r.Hosts, Rule{Host: h, Source: SourceModels})
		}
	}
	m.mu.Lock()
	r.AllowAll = m.allowAll[d.ID]
	for _, o := range m.overrides[d.ID] {
		if !hasHost(r.Hosts, o.Host) {
			r.Hosts = append(r.Hosts, o)
		}
	}
	m.mu.Unlock()
	return r, nil
}

// Add allows host on this desk and applies it immediately.
func (m *Manager) Add(ctx context.Context, d desk.Desk, host, source string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if !charter.ValidHost(host) {
		return fmt.Errorf("%q is not a website host (e.g. booking.com or *.booking.com)", host)
	}
	rules, err := m.Rules(d)
	if err != nil {
		return err
	}
	if !rules.Editable {
		return ErrNotEditable
	}
	if hasHost(rules.Hosts, host) {
		return nil
	}
	m.mu.Lock()
	m.overrides[d.ID] = append(m.overrides[d.ID], Rule{Host: host, Source: source, AddedAt: time.Now()})
	m.saveLocked()
	m.mu.Unlock()
	return m.apply(ctx, d)
}

// SetAllowAll lets the desk reach every website (on) or only its rules again (off).
func (m *Manager) SetAllowAll(ctx context.Context, d desk.Desk, on bool) error {
	rules, err := m.Rules(d)
	if err != nil {
		return err
	}
	if !rules.Editable {
		return ErrNotEditable
	}
	m.mu.Lock()
	prev := m.allowAll[d.ID]
	if on {
		m.allowAll[d.ID] = true
	} else {
		delete(m.allowAll, d.ID)
	}
	m.mu.Unlock()
	err = m.apply(ctx, d)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		if prev {
			m.allowAll[d.ID] = true
		} else {
			delete(m.allowAll, d.ID)
		}
		return err
	}
	m.saveLocked()
	return nil
}

// ApplyAll pushes every running desk's rules again, e.g. after the router's models
// changed. Desks whose rules can't change are skipped.
func (m *Manager) ApplyAll(ctx context.Context) error {
	all, err := m.desks.List(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, d := range all {
		if rules, err := m.Rules(d); err != nil || !rules.Editable {
			continue
		}
		if err := m.apply(ctx, d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name, err))
		}
	}
	return errors.Join(errs...)
}

// Remove takes back a per-desk addition. Charter rules can only change in the charter.
func (m *Manager) Remove(ctx context.Context, d desk.Desk, host string) error {
	m.mu.Lock()
	before := len(m.overrides[d.ID])
	m.overrides[d.ID] = slices.DeleteFunc(m.overrides[d.ID], func(r Rule) bool { return r.Host == host })
	removed := len(m.overrides[d.ID]) < before
	if removed {
		m.saveLocked()
	}
	m.mu.Unlock()
	if !removed {
		return fmt.Errorf("%s is not a rule added to this desk (charter rules are changed in the charter)", host)
	}
	return m.apply(ctx, d)
}

// Decide answers an agent's request, records the decision in the chat, and lets the
// agent continue.
func (m *Manager) Decide(ctx context.Context, d desk.Desk, requestID string, dec Decision) error {
	m.mu.Lock()
	req, ok := m.pending[requestID]
	if ok && req.deskID == d.ID {
		delete(m.pending, requestID)
	}
	m.mu.Unlock()
	if !ok || req.deskID != d.ID {
		return ErrUnknownRequest
	}

	var err error
	switch dec {
	case AllowDesk:
		err = m.Add(ctx, d, req.host, SourceChat)
	case AllowCharter:
		err = m.addToCharter(ctx, d, req.host)
	case Deny:
	default:
		err = fmt.Errorf("unknown decision %q", dec)
	}
	if err != nil {
		m.mu.Lock()
		m.pending[requestID] = req // let the human try again
		m.mu.Unlock()
		return err
	}

	m.runner.Store().Append(d.ID, agent.Event{
		Kind: agent.KindAccessDecision,
		Text: string(dec),
		Data: map[string]string{"requestId": requestID, "host": req.host, "decision": string(dec)},
	})
	prompt := fmt.Sprintf("The desk's owner allowed access to %s. Continue the previous task.", req.host)
	if dec == Deny {
		prompt = fmt.Sprintf("The desk's owner denied access to %s. Continue without it, or explain what you cannot do.", req.host)
	}
	if _, err := m.runner.Start(d.ID, prompt); err != nil && !errors.Is(err, agent.ErrBusy) {
		m.log.Warn("could not resume agent after access decision", "desk", d.Name, "err", err)
	}
	return nil
}

// addToCharter saves host in the desk's charter file and applies it to every running
// desk made from that charter.
func (m *Manager) addToCharter(ctx context.Context, d desk.Desk, host string) error {
	c, err := m.charter(d.Charter)
	if err != nil {
		return err
	}
	if c.Path == "" {
		return errors.New("charter file unknown")
	}
	if err := charter.AddAllowedHost(c.Path, host); err != nil {
		return err
	}
	all, err := m.desks.List(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, other := range all {
		if other.Charter == d.Charter {
			errs = append(errs, m.apply(ctx, other))
		}
	}
	return errors.Join(errs...)
}

// apply pushes the desk's effective allowlist to its running box.
func (m *Manager) apply(ctx context.Context, d desk.Desk) error {
	np, ok := m.desks.Runtime().(runtime.NetworkPolicy)
	if !ok {
		return ErrNotEditable
	}
	rules, err := m.Rules(d)
	if err != nil {
		return err
	}
	if rules.Unrestricted {
		return ErrNotEditable // never narrow a desk whose charter allows everything
	}
	if rules.AllowAll {
		return np.SetAllowedHosts(ctx, d.ID, runtime.AllowAllHosts)
	}
	hosts := make([]string, 0, len(rules.Hosts))
	for _, r := range rules.Hosts {
		hosts = append(hosts, r.Host)
	}
	return np.SetAllowedHosts(ctx, d.ID, hosts)
}

// Forget drops a removed desk's additions and pending requests.
func (m *Manager) Forget(deskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.overrides, deskID)
	delete(m.allowAll, deskID)
	m.saveLocked()
	for id, r := range m.pending {
		if r.deskID == deskID {
			delete(m.pending, id)
		}
	}
}

func (m *Manager) charter(name string) (*charter.Charter, error) {
	all, err := charter.LoadDir(m.chartersDir)
	if err != nil {
		return nil, err
	}
	c, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("charter %q not found in %s", name, m.chartersDir)
	}
	return c, nil
}

func hasHost(rules []Rule, host string) bool {
	return slices.ContainsFunc(rules, func(r Rule) bool { return r.Host == host })
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
