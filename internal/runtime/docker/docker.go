// Package docker implements runtime.Runtime with the local Docker CLI.
//
// It is for development and machines without OpenSandbox: it cannot enforce network
// allowlists and does not expire boxes on its own.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

const (
	containerPrefix = "shortdesk-"
	labelManaged    = "dev.shortdesk.runtime"
	labelExpires    = "dev.shortdesk.expires-at"
)

// Runtime runs boxes as local Docker containers.
type Runtime struct {
	bin string
}

var _ runtime.Runtime = (*Runtime)(nil)
var _ runtime.Shell = (*Runtime)(nil)

// New returns a Docker runtime using the docker binary on PATH.
func New() *Runtime { return &Runtime{bin: "docker"} }

func (r *Runtime) Name() string                       { return "docker" }
func (r *Runtime) Capabilities() runtime.Capabilities { return runtime.Capabilities{} }

// Create runs a detached container. Ports are published on 127.0.0.1 only.
func (r *Runtime) Create(ctx context.Context, spec runtime.Spec) (runtime.Box, error) {
	if spec.AllowedHosts != nil {
		return runtime.Box{}, errors.New("docker runtime cannot enforce a network allowlist")
	}
	id, err := newID()
	if err != nil {
		return runtime.Box{}, err
	}
	args := []string{"run", "-d", "--name", containerPrefix + id, "--shm-size", "1g",
		"--label", labelManaged + "=docker"}
	if spec.CPU != "" {
		args = append(args, "--cpus", spec.CPU)
	}
	if spec.Memory != "" {
		args = append(args, "--memory", dockerMemory(spec.Memory))
	}
	if spec.Lifetime > 0 {
		args = append(args, "--label", labelExpires+"="+time.Now().Add(spec.Lifetime).UTC().Format(time.RFC3339))
	}
	for _, k := range sortedKeys(spec.Labels) {
		args = append(args, "--label", k+"="+spec.Labels[k])
	}
	for _, k := range sortedKeys(spec.Env) {
		args = append(args, "-e", k+"="+spec.Env[k])
	}
	for _, p := range spec.Ports {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1::%d", p))
	}
	for _, m := range spec.Mounts {
		args = append(args, "-v", m.Volume+":"+m.Path)
	}
	if len(spec.Entrypoint) > 0 {
		args = append(args, "--entrypoint", spec.Entrypoint[0])
	}
	args = append(args, spec.Image)
	if len(spec.Entrypoint) > 1 {
		args = append(args, spec.Entrypoint[1:]...)
	}
	if _, err := r.run(ctx, args...); err != nil {
		return runtime.Box{}, fmt.Errorf("create container: %w", err)
	}
	return r.Get(ctx, id)
}

type inspectOutput struct {
	Name    string `json:"Name"`
	Created time.Time
	State   struct{ Status string }
	Config  struct{ Labels map[string]string }
}

func (r *Runtime) Get(ctx context.Context, id string) (runtime.Box, error) {
	out, err := r.run(ctx, "inspect", containerPrefix+id)
	if err != nil {
		if strings.Contains(err.Error(), "No such object") {
			return runtime.Box{}, runtime.ErrNotFound
		}
		return runtime.Box{}, err
	}
	var items []inspectOutput
	if err := json.Unmarshal(out, &items); err != nil || len(items) != 1 {
		return runtime.Box{}, fmt.Errorf("inspect %s: unexpected output", id)
	}
	it := items[0]
	// ExpiresAt stays zero: Docker does not remove containers on its own. The
	// expires-at label is recorded for a future reaper.
	return runtime.Box{
		ID:        strings.TrimPrefix(strings.TrimPrefix(it.Name, "/"), containerPrefix),
		State:     it.State.Status,
		Labels:    it.Config.Labels,
		CreatedAt: it.Created,
	}, nil
}

func (r *Runtime) List(ctx context.Context, labels map[string]string) ([]runtime.Box, error) {
	args := []string{"ps", "-a", "--format", "{{.Names}}", "--filter", "label=" + labelManaged}
	for _, k := range sortedKeys(labels) {
		args = append(args, "--filter", "label="+k+"="+labels[k])
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var boxes []runtime.Box
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(name, containerPrefix) {
			continue
		}
		box, err := r.Get(ctx, strings.TrimPrefix(name, containerPrefix))
		if errors.Is(err, runtime.ErrNotFound) {
			continue // removed between ps and inspect
		}
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, box)
	}
	return boxes, nil
}

func (r *Runtime) Delete(ctx context.Context, id string) error {
	_, err := r.run(ctx, "rm", "-f", containerPrefix+id)
	if err != nil && strings.Contains(err.Error(), "No such container") {
		return runtime.ErrNotFound
	}
	return err
}

func (r *Runtime) Endpoint(ctx context.Context, id string, port int) (runtime.Endpoint, error) {
	out, err := r.run(ctx, "port", containerPrefix+id, strconv.Itoa(port))
	if err != nil {
		return runtime.Endpoint{}, fmt.Errorf("port %d not published: %w", port, err)
	}
	host, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return runtime.Endpoint{Host: host}, nil
}

func (r *Runtime) Exec(ctx context.Context, id string, req runtime.ExecRequest) error {
	args := []string{"exec"}
	for _, k := range sortedKeys(req.Env) {
		args = append(args, "-e", k+"="+req.Env[k])
	}
	args = append(args, containerPrefix+id, "bash", "-c", req.Command)
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Stdout, cmd.Stderr = req.Stdout, req.Stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &runtime.ExitError{Code: exitErr.ExitCode()}
	}
	return err
}

func (r *Runtime) ShellCommand(id string) ([]string, error) {
	return []string{r.bin, "exec", "-it", containerPrefix + id, "bash", "-l"}, nil
}

func (r *Runtime) run(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// dockerMemory converts Kubernetes-style sizes ("4Gi", "512Mi") to Docker's ("4g", "512m").
func dockerMemory(s string) string {
	for k8s, d := range map[string]string{"Gi": "g", "Mi": "m", "Ki": "k"} {
		if strings.HasSuffix(s, k8s) {
			return strings.TrimSuffix(s, k8s) + d
		}
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func newID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var _ runtime.Files = (*Runtime)(nil)

// Upload writes src to path inside the container.
func (r *Runtime) Upload(ctx context.Context, id, path string, src io.Reader) error {
	cmd := exec.CommandContext(ctx, r.bin, "exec", "-i", containerPrefix+id, "sh", "-c", `cat > "$1"`, "sh", path)
	cmd.Stdin = src
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("upload %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Download copies a file out of the container.
func (r *Runtime) Download(ctx context.Context, id, path string, w io.Writer) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.bin, "exec", containerPrefix+id, "cat", path)
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("download %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
