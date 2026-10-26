// Package stack starts everything Shortdesk runs on, so `shortdesk up` works on a fresh
// machine with only Docker installed: the images, OpenSandbox (the runtime that creates
// and guards desks) and its configuration. Nothing is set up by hand.
package stack

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OpenSandbox release Shortdesk is tested with, and the execd/egress images it uses.
const (
	OpenSandboxImage = "opensandbox/server:release-1.1.1"
	openSandboxName  = "shortdesk-opensandbox"
	openSandboxPort  = "8090"
)

// Stack is what the rest of Shortdesk needs to reach the runtime.
type Stack struct {
	OpenSandboxURL string
	APIKey         string
	// EndpointHost is how OpenSandbox names the machine in desk addresses from inside its
	// container ("host.docker.internal" on Docker Desktop). Shortdesk, running on the
	// machine itself, replaces it with 127.0.0.1.
	EndpointHost string
}

// Options configure Up.
type Options struct {
	Dir           string // ~/.shortdesk
	RebuildImages bool
	Log           func(format string, args ...any)
}

// Up checks Docker, builds missing images and (re)starts OpenSandbox.
func Up(ctx context.Context, o Options) (Stack, error) {
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	osName, err := docker(ctx, "info", "--format", "{{.OperatingSystem}}")
	if err != nil {
		return Stack{}, errors.New("Docker isn't running. Install Docker (Docker Desktop on Mac/Windows) and start it, then run shortdesk up again")
	}
	desktop := strings.Contains(osName, "Docker Desktop")

	if err := ensureImages(ctx, o.RebuildImages, o.Log); err != nil {
		return Stack{}, err
	}

	dir := filepath.Join(o.Dir, "opensandbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Stack{}, err
	}
	key, err := apiKey(filepath.Join(dir, "api-key"))
	if err != nil {
		return Stack{}, err
	}
	st := Stack{OpenSandboxURL: "http://127.0.0.1:" + openSandboxPort, APIKey: key}

	// Docker Desktop (Mac/Windows) can't share the machine's network with a container, so
	// OpenSandbox reaches desks through host.docker.internal. On Linux it simply shares
	// the machine's network. Either way desk ports are published on 127.0.0.1 only.
	run := []string{"run", "-d", "--name", openSandboxName, "--restart", "unless-stopped",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", filepath.Join(dir, "config.toml") + ":/etc/opensandbox/config.toml:ro",
		"-v", dir + ":/data"}
	listen, hostIP := "0.0.0.0:8080", ""
	if desktop {
		run = append(run, "-p", "127.0.0.1:"+openSandboxPort+":8080")
		hostIP = "host.docker.internal"
		st.EndpointHost = hostIP
	} else {
		run = append(run, "--network", "host")
		listen = "127.0.0.1:" + openSandboxPort
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(openSandboxConfig(listen, key, hostIP)), 0o600); err != nil {
		return Stack{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "endpoint-host"), []byte(hostIP+"\n"), 0o600); err != nil {
		return Stack{}, err
	}
	o.Log("Starting OpenSandbox…")
	docker(ctx, "rm", "-f", openSandboxName)
	if _, err := docker(ctx, append(run, OpenSandboxImage)...); err != nil {
		return Stack{}, fmt.Errorf("start OpenSandbox: %w", err)
	}
	if err := waitHealthy(ctx, st.OpenSandboxURL+"/health"); err != nil {
		logs, _ := docker(ctx, "logs", "--tail", "20", openSandboxName)
		return Stack{}, fmt.Errorf("OpenSandbox didn't start: %w\n%s", err, logs)
	}
	return st, nil
}

// Down stops what Up started: OpenSandbox and the vault. Desks keep running until they
// expire or are removed; their data and the vault's data are kept.
func Down(ctx context.Context) error {
	_, err := docker(ctx, "rm", "-f", openSandboxName, "shortdesk-vault", "shortdesk-vault-cli")
	return err
}

func apiKey(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		return string(bytes.TrimSpace(b)), nil
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := hex.EncodeToString(b)
	return key, os.WriteFile(path, []byte(key+"\n"), 0o600)
}

func openSandboxConfig(listen, key, hostIP string) string {
	host, port, _ := strings.Cut(listen, ":")
	hostLine := ""
	if hostIP != "" {
		hostLine = fmt.Sprintf("host_ip = %q\n", hostIP)
	}
	return fmt.Sprintf(`# Written by shortdesk up. Changes are overwritten on the next start.
[server]
host = %q
port = %s
max_sandbox_timeout_seconds = 86400
api_key = %q

[proxy]
resolve_internal = true

[log]
level = "INFO"

[runtime]
type = "docker"
execd_image = "opensandbox/execd:v1.1.0"

[storage]
allowed_host_paths = ["/nonexistent-shortdesk"]

[store]
type = "sqlite"
path = "/data/opensandbox.db"

[docker]
network_mode = "bridge"
port_range_min = 40000
port_range_max = 60000
publish_host = "127.0.0.1"
%sdrop_capabilities = ["AUDIT_WRITE", "MKNOD", "NET_ADMIN", "NET_RAW", "SYS_ADMIN", "SYS_MODULE", "SYS_PTRACE", "SYS_TIME", "SYS_TTY_CONFIG"]
no_new_privileges = true
apparmor_profile = ""
pids_limit = 4096
seccomp_profile = ""

[ingress]
mode = "direct"

[egress]
image = "opensandbox/egress:v1.1.7"
mode = "dns"
readiness_timeout_seconds = 30.0
`, host, port, key, hostLine)
}

func waitHealthy(ctx context.Context, url string) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func docker(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}
