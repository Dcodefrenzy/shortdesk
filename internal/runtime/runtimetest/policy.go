package runtimetest

import (
	"context"
	"sync"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

// FakeWithPolicy is a Fake whose boxes' allowlists can be changed live.
type FakeWithPolicy struct {
	Fake
	Files
	policyMu sync.Mutex
	Policies map[string][]string // box ID -> last allowlist set
}

var _ runtime.NetworkPolicy = (*FakeWithPolicy)(nil)

func (f *FakeWithPolicy) SetAllowedHosts(_ context.Context, id string, hosts []string) error {
	f.policyMu.Lock()
	defer f.policyMu.Unlock()
	if f.Policies == nil {
		f.Policies = map[string][]string{}
	}
	f.Policies[id] = append([]string(nil), hosts...)
	return nil
}

// Policy returns the last allowlist set for a box.
func (f *FakeWithPolicy) Policy(id string) []string {
	f.policyMu.Lock()
	defer f.policyMu.Unlock()
	return f.Policies[id]
}
