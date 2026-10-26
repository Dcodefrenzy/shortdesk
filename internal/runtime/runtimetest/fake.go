// Package runtimetest provides an in-memory runtime.Runtime for tests.
package runtimetest

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

// Fake keeps boxes in memory and records what it was asked to do.
type Fake struct {
	mu sync.Mutex

	Caps        runtime.Capabilities
	Boxes       []runtime.Box
	Created     []runtime.Spec
	Deleted     []string
	Execs       []runtime.ExecRequest
	EndpointFor func(id string, port int) runtime.Endpoint
	// ExecOutput is written to ExecRequest.Stdout by every Exec.
	ExecOutput string
}

var _ runtime.Runtime = (*Fake)(nil)

func (f *Fake) Name() string                       { return "fake" }
func (f *Fake) Capabilities() runtime.Capabilities { return f.Caps }

func (f *Fake) Create(_ context.Context, s runtime.Spec) (runtime.Box, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Created = append(f.Created, s)
	b := runtime.Box{
		ID:        fmt.Sprintf("box-%d", len(f.Created)),
		State:     "Running",
		Labels:    s.Labels,
		CreatedAt: time.Now(),
	}
	if s.Lifetime > 0 {
		b.ExpiresAt = b.CreatedAt.Add(s.Lifetime)
	}
	f.Boxes = append(f.Boxes, b)
	return b, nil
}

func (f *Fake) Get(_ context.Context, id string) (runtime.Box, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.Boxes {
		if b.ID == id {
			return b, nil
		}
	}
	return runtime.Box{}, runtime.ErrNotFound
}

func (f *Fake) List(_ context.Context, labels map[string]string) ([]runtime.Box, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []runtime.Box
	for _, b := range f.Boxes {
		if hasLabels(b.Labels, labels) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *Fake) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, b := range f.Boxes {
		if b.ID == id {
			f.Boxes = append(f.Boxes[:i], f.Boxes[i+1:]...)
			f.Deleted = append(f.Deleted, id)
			return nil
		}
	}
	return runtime.ErrNotFound
}

func (f *Fake) Endpoint(_ context.Context, id string, port int) (runtime.Endpoint, error) {
	if f.EndpointFor == nil {
		return runtime.Endpoint{Host: "127.0.0.1:1"}, nil
	}
	return f.EndpointFor(id, port), nil
}

func (f *Fake) Exec(_ context.Context, _ string, r runtime.ExecRequest) error {
	f.mu.Lock()
	f.Execs = append(f.Execs, r)
	out := f.ExecOutput
	f.mu.Unlock()
	if r.Stdout != nil {
		r.Stdout.Write([]byte(out))
	}
	return nil
}

func hasLabels(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// Files records uploads and serves downloads from memory.
type Files struct {
	mu    sync.Mutex
	Store map[string][]byte // "boxID:path" -> content
}

func (f *Files) Upload(_ context.Context, id, path string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Store == nil {
		f.Store = map[string][]byte{}
	}
	f.Store[id+":"+path] = b
	return nil
}

func (f *Files) Download(_ context.Context, id, path string, w io.Writer) error {
	f.mu.Lock()
	b, ok := f.Store[id+":"+path]
	f.mu.Unlock()
	if !ok {
		return fmt.Errorf("no file %s in %s", path, id)
	}
	_, err := w.Write(b)
	return err
}
