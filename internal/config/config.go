// Package config resolves Shortdesk's settings from the environment and picks a runtime.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/internal/runtime/docker"
	"github.com/shortdesk/shortdesk/internal/runtime/opensandbox"
)

// Environment variables Shortdesk reads.
const (
	EnvRuntime         = "SHORTDESK_RUNTIME" // opensandbox (default) | docker
	EnvOpenSandboxURL  = "SHORTDESK_OPENSANDBOX_URL"
	EnvOpenSandboxKey  = "SHORTDESK_OPENSANDBOX_API_KEY"
	EnvOpenSandboxHost = "SHORTDESK_OPENSANDBOX_LOCAL_DOCKER"
)

// Config is Shortdesk's resolved configuration.
type Config struct {
	Runtime     string
	OpenSandbox opensandbox.Config
}

// FromEnv reads configuration from the environment, with local-development defaults.
func FromEnv() (Config, error) {
	c := Config{
		Runtime: envOr(EnvRuntime, "opensandbox"),
		OpenSandbox: opensandbox.Config{
			URL:         envOr(EnvOpenSandboxURL, "http://127.0.0.1:8090"),
			APIKey:      os.Getenv(EnvOpenSandboxKey),
			LocalDocker: true,
		},
	}
	// Written by `shortdesk up`, so other commands work without any setup.
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".shortdesk", "opensandbox")
		if c.OpenSandbox.APIKey == "" {
			if b, err := os.ReadFile(filepath.Join(dir, "api-key")); err == nil {
				c.OpenSandbox.APIKey = strings.TrimSpace(string(b))
			}
		}
		if b, err := os.ReadFile(filepath.Join(dir, "endpoint-host")); err == nil {
			c.OpenSandbox.EndpointHost = strings.TrimSpace(string(b))
		}
	}
	if v := os.Getenv(EnvOpenSandboxHost); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", EnvOpenSandboxHost, err)
		}
		c.OpenSandbox.LocalDocker = b
	}
	return c, nil
}

// NewRuntime builds the configured runtime.
func (c Config) NewRuntime() (runtime.Runtime, error) {
	switch c.Runtime {
	case "opensandbox":
		return opensandbox.New(c.OpenSandbox)
	case "docker":
		return docker.New(), nil
	}
	return nil, fmt.Errorf("%s=%q: want opensandbox or docker", EnvRuntime, c.Runtime)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
