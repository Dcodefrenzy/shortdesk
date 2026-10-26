// Package charter loads and validates charters: the files that define what a desk is
// and what it is allowed to do.
//
// Unknown fields are rejected on purpose. A misspelled or not-yet-supported rule must
// fail loudly rather than be silently ignored, because an ignored permission rule is
// a governance hole.
package charter

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults applied to fields a charter leaves empty.
const (
	DefaultImage    = "shortdesk/desk:dev"
	DefaultCPU      = "2"
	DefaultMemory   = "4Gi"
	DefaultLifetime = 2 * time.Hour
	MinLifetime     = time.Minute
)

// AllowAll is the network allowlist entry meaning "no outbound restriction".
const AllowAll = "*"

// Charter defines a desk: what it runs and what it may do.
type Charter struct {
	Name        string      `yaml:"charter"`
	Tenant      string      `yaml:"tenant"`
	Version     int         `yaml:"version"`
	Desk        Desk        `yaml:"desk"`
	Permissions Permissions `yaml:"permissions"`
	Limits      Limits      `yaml:"limits"`

	// Path is the file the charter was loaded from ("" when parsed from bytes).
	Path string `yaml:"-"`
}

// Desk describes the computer itself.
type Desk struct {
	Image     string            `yaml:"image"`
	Resources Resources         `yaml:"resources"`
	Env       map[string]string `yaml:"env"`
}

// Resources are hard caps for a desk.
type Resources struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
}

// Permissions is what a desk may do beyond its own box.
type Permissions struct {
	Network Network `yaml:"network"`
}

// Network controls outbound traffic.
type Network struct {
	// Allow lists hosts the desk may reach: exact ("claude.ai") or wildcard ("*.claude.ai").
	// ["*"] means unrestricted. Required, so "no rules" is always a deliberate choice.
	Allow []string `yaml:"allow"`
}

// Limits bound a desk's cost and lifetime.
type Limits struct {
	DeskLifetime Duration `yaml:"desk_lifetime"`
}

// Duration is a time.Duration written as "90m" or "2h" in YAML.
type Duration time.Duration

// UnmarshalYAML parses Go duration strings.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	parsed, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q (use e.g. 90m, 2h)", n.Line, n.Value)
	}
	*d = Duration(parsed)
	return nil
}

// Unrestricted reports whether outbound traffic is unrestricted.
func (n Network) Unrestricted() bool {
	for _, h := range n.Allow {
		if h == AllowAll {
			return true
		}
	}
	return false
}

// Load reads, defaults and validates a charter file.
func Load(path string) (*Charter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

// Parse decodes, defaults and validates a charter.
func Parse(data []byte) (*Charter, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Charter
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Charter) applyDefaults() {
	if c.Desk.Image == "" {
		c.Desk.Image = DefaultImage
	}
	if c.Desk.Resources.CPU == "" {
		c.Desk.Resources.CPU = DefaultCPU
	}
	if c.Desk.Resources.Memory == "" {
		c.Desk.Resources.Memory = DefaultMemory
	}
	if c.Limits.DeskLifetime == 0 {
		c.Limits.DeskLifetime = Duration(DefaultLifetime)
	}
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	hostRe = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

// ValidHost reports whether h is a host ("claude.ai") or wildcard host ("*.claude.ai").
func ValidHost(h string) bool { return hostRe.MatchString(strings.ToLower(h)) }

// Validate checks a charter after defaults are applied.
func (c *Charter) Validate() error {
	var errs []error
	if !nameRe.MatchString(c.Name) {
		errs = append(errs, fmt.Errorf("charter: %q must be 1-40 lowercase letters, digits or dashes", c.Name))
	}
	if c.Tenant != "" && !nameRe.MatchString(c.Tenant) {
		errs = append(errs, fmt.Errorf("tenant: %q must be 1-40 lowercase letters, digits or dashes", c.Tenant))
	}
	if len(c.Permissions.Network.Allow) == 0 {
		errs = append(errs, errors.New(`permissions.network.allow is required (list hosts, or ["*"] for unrestricted)`))
	}
	for _, h := range c.Permissions.Network.Allow {
		if h != AllowAll && !ValidHost(h) {
			errs = append(errs, fmt.Errorf("permissions.network.allow: %q is not a host or *.host", h))
		}
	}
	if c.Permissions.Network.Unrestricted() && len(c.Permissions.Network.Allow) > 1 {
		errs = append(errs, errors.New(`permissions.network.allow: "*" cannot be combined with other hosts`))
	}
	if time.Duration(c.Limits.DeskLifetime) < MinLifetime {
		errs = append(errs, fmt.Errorf("limits.desk_lifetime must be at least %s", MinLifetime))
	}
	return errors.Join(errs...)
}

// LoadDir loads every *.yaml / *.yml charter under dir, keyed by charter name.
// Any invalid file or duplicate name fails the whole load, so a broken charter is
// noticed instead of quietly missing from the list.
func LoadDir(dir string) (map[string]*Charter, error) {
	out := map[string]*Charter{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		c, err := Load(path)
		if err != nil {
			return err
		}
		if _, dup := out[c.Name]; dup {
			return fmt.Errorf("%s: charter %q is defined more than once", path, c.Name)
		}
		out[c.Name] = c
		return nil
	})
	return out, err
}
