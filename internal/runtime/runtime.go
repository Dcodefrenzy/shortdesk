// Package runtime defines the box interface Shortdesk builds on: something that can
// create, inspect, run commands in, and delete isolated boxes.
//
// The rest of Shortdesk depends only on this interface; OpenSandbox and plain Docker
// are interchangeable implementations.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrNotFound is returned when a box does not exist.
var ErrNotFound = errors.New("box not found")

// Spec describes a box to create.
type Spec struct {
	Image      string
	Entrypoint []string
	Env        map[string]string
	CPU        string // e.g. "2"
	Memory     string // e.g. "4Gi"
	Lifetime   time.Duration
	Labels     map[string]string
	// AllowedHosts restricts outbound traffic to these hosts ("claude.ai", "*.claude.ai").
	// Nil means unrestricted.
	AllowedHosts []string
	// Ports the box serves that callers need endpoints for.
	Ports []int
	// Mounts are storage volumes shared into the box (live, read-write). The same volume
	// in several boxes is the same files.
	Mounts []Mount
}

// Mount attaches a runtime-managed storage volume to a box.
type Mount struct {
	Volume string // volume name, created if missing (e.g. "shortdesk-developer")
	Path   string // absolute path inside the box
}

// Box is a created box.
type Box struct {
	ID        string
	State     string
	Labels    map[string]string
	CreatedAt time.Time
	ExpiresAt time.Time // zero if the box never expires
}

// Endpoint is how to reach a port inside a box over HTTP.
type Endpoint struct {
	Host       string // host:port
	PathPrefix string // e.g. "/proxy/6080", or "" when the port is served at the root
}

// URL returns the base URL for the endpoint, without a trailing slash.
func (e Endpoint) URL() string { return "http://" + e.Host + e.PathPrefix }

// ExecRequest is a non-interactive command to run inside a box.
type ExecRequest struct {
	Command string // run with bash -c
	Env     map[string]string
	Stdout  io.Writer // may be nil
	Stderr  io.Writer // may be nil
}

// ExitError reports a command that ran but exited non-zero.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("command exited with status %d", e.Code) }

// Capabilities describe what a runtime can enforce.
type Capabilities struct {
	// EgressPolicy means the runtime enforces Spec.AllowedHosts.
	EgressPolicy bool
}

// Runtime creates and manages boxes.
type Runtime interface {
	Name() string
	Capabilities() Capabilities
	Create(ctx context.Context, spec Spec) (Box, error)
	Get(ctx context.Context, id string) (Box, error)
	// List returns boxes whose labels include all of the given labels.
	List(ctx context.Context, labels map[string]string) ([]Box, error)
	Delete(ctx context.Context, id string) error
	Endpoint(ctx context.Context, id string, port int) (Endpoint, error)
	Exec(ctx context.Context, id string, req ExecRequest) error
}

// Shell is implemented by runtimes that can open an interactive terminal in a box
// from this machine. It returns the command to run locally.
type Shell interface {
	ShellCommand(id string) ([]string, error)
}

// NetworkPolicy is implemented by runtimes that can change a running box's outbound
// allowlist without restarting it.
type NetworkPolicy interface {
	// SetAllowedHosts replaces the box's allowlist; all other hosts are denied.
	// AllowAllHosts (["*"]) allows every host.
	SetAllowedHosts(ctx context.Context, id string, hosts []string) error
}

// Files is implemented by runtimes that can copy files into and out of a box.
type Files interface {
	// Upload writes r to path inside the box.
	Upload(ctx context.Context, id, path string, r io.Reader) error
	// Download copies the file at path inside the box to w.
	Download(ctx context.Context, id, path string, w io.Writer) error
}

// AllowAllHosts is the allowlist that allows every host.
var AllowAllHosts = []string{"*"}

// PrivateNetwork is implemented by runtimes whose desks can reach each other directly,
// e.g. helper desks reaching the Phone Desk on the main desk.
type PrivateNetwork interface {
	// PrivateAddr returns the desk's address on the network desks share.
	PrivateAddr(ctx context.Context, id string) (string, error)
}

// Renewer is implemented by runtimes that can move a desk's expiry, e.g. to keep the
// main desk alive.
type Renewer interface {
	// Renew sets when the desk is removed; it returns the time the runtime set.
	Renew(ctx context.Context, id string, expiresAt time.Time) (time.Time, error)
}
