// Package workspace holds settings that apply to every desk: how agents reach Claude,
// the owner's own model keys, the router between them, and the vault account. Every
// new task reads them, so a change applies to all desks at once.
//
// Secrets are write-only through the API: they can be set or cleared, never read
// back. They are kept in a file only the owner can read (0600).
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/shortdesk/shortdesk/internal/agent"
)

// Connection names.
const (
	ClaudeToken  = "claude_oauth_token" // from `claude setup-token`; uses the Claude subscription
	AnthropicKey = "anthropic_api_key"  // pay-per-use Anthropic API key

	// The owner's own model keys, used through Free Claude Code (FCC).
	DeepSeekKey = "deepseek_api_key"
	GeminiKey   = "gemini_api_key"
	GroqKey     = "groq_api_key"
	NvidiaKey   = "nvidia_nim_api_key"
)

// Connections lists the AI connections shown in Workspace settings, in display order.
var Connections = []string{ClaudeToken, AnthropicKey, GeminiKey, DeepSeekKey, GroqKey, NvidiaKey}

// providerHosts are the API hosts FCC calls for each provider (its default base URLs).
var providerHosts = map[string]string{
	"gemini":     "generativelanguage.googleapis.com",
	"deepseek":   "api.deepseek.com",
	"groq":       "api.groq.com",
	"nvidia_nim": "integrate.api.nvidia.com",
}

// fccKeys maps model-key connections to the variable FCC reads, and gives each provider
// a default model (FCC's "<provider>/<model-id>" format) for when the owner picks none.
var fccKeys = []struct{ Secret, Env, DefaultModel string }{
	{GeminiKey, "GEMINI_API_KEY", "gemini/models/gemini-3.1-flash-lite"},
	{DeepSeekKey, "DEEPSEEK_API_KEY", "deepseek/deepseek-chat"},
	{GroqKey, "GROQ_API_KEY", "groq/llama-3.3-70b-versatile"},
	{NvidiaKey, "NVIDIA_NIM_API_KEY", "nvidia_nim/nvidia/nemotron-3-ultra-550b-a55b"},
}

// Mode is how the router picks between Claude and the owner's own models.
type Mode string

const (
	ModeAuto   Mode = "auto"   // Claude; if it hits a usage limit, the task continues on your own models
	ModeClaude Mode = "claude" // Claude only
	ModeOwn    Mode = "own"    // your own models only
)

// Router is the workspace's routing configuration.
type Router struct {
	Mode Mode `json:"mode"`
	// Models are FCC model strings in the order to try, e.g. "gemini/models/gemini-3.1-flash-lite".
	// Empty means one default model per provider key that is set.
	Models []string `json:"models"`
}

// otherSecrets are stored here for other parts of Shortdesk but not shown as AI connections.
var otherSecrets = []string{"vault_email", "vault_master_password"}

type file struct {
	Secrets map[string]string `json:"secrets"`
	Router  Router            `json:"router"`
	// MainDesk is the ID of the desk the owner chats with.
	MainDesk string `json:"mainDesk,omitempty"`
}

// Store persists workspace settings in one JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	data file
}

// Open loads the store at path, creating an empty one if missing.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: file{Secrets: map[string]string{}, Router: Router{Mode: ModeAuto}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.data.Secrets == nil {
		s.data.Secrets = map[string]string{}
	}
	if s.data.Router.Mode == "" {
		s.data.Router.Mode = ModeAuto
	}
	// Connections that are no longer supported are dropped.
	for name := range s.data.Secrets {
		if !known(name) {
			delete(s.data.Secrets, name)
		}
	}
	return s, nil
}

// ConnectionStatus says whether a connection is set, without revealing it.
type ConnectionStatus struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
	// Hint is the last 4 characters, to tell keys apart.
	Hint string `json:"hint,omitempty"`
}

// Status lists the AI connections.
func (s *Store) Status() []ConnectionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConnectionStatus, 0, len(Connections))
	for _, n := range Connections {
		v := s.data.Secrets[n]
		st := ConnectionStatus{Name: n, Set: v != ""}
		if len(v) >= 8 {
			st.Hint = "…" + v[len(v)-4:]
		}
		out = append(out, st)
	}
	return out
}

// SetSecret stores (or, with an empty value, removes) a secret.
func (s *Store) SetSecret(name, value string) error {
	if !known(name) {
		return fmt.Errorf("unknown connection %q", name)
	}
	value = strings.TrimSpace(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.data.Secrets, name)
	} else {
		s.data.Secrets[name] = value
	}
	return s.saveLocked()
}

// Secret returns a secret's value, for use inside Shortdesk only.
func (s *Store) Secret(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Secrets[name]
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func known(name string) bool {
	return slices.Contains(Connections, name) || slices.Contains(otherSecrets, name)
}

// ClaudeEnv returns the environment an agent run needs to reach Claude: the
// subscription token if set, else the API key, else nothing (the desk may be logged
// in already, e.g. through a saved login).
func (s *Store) ClaudeEnv() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.data.Secrets[ClaudeToken] != "":
		return map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": s.data.Secrets[ClaudeToken]}
	case s.data.Secrets[AnthropicKey] != "":
		return map[string]string{"ANTHROPIC_API_KEY": s.data.Secrets[AnthropicKey]}
	}
	return map[string]string{}
}

// Router returns the routing configuration.
func (s *Store) Router() Router {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Router
}

// MainDesk returns the ID of the desk the owner chats with ("" when none was picked).
func (s *Store) MainDesk() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.MainDesk
}

// SetMainDesk records which desk the owner chats with.
func (s *Store) SetMainDesk(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.MainDesk = id
	return s.saveLocked()
}

// SetRouter replaces the routing configuration.
func (s *Store) SetRouter(r Router) error {
	switch r.Mode {
	case ModeAuto, ModeClaude, ModeOwn:
	default:
		return fmt.Errorf("mode must be auto, claude or own, not %q", r.Mode)
	}
	models := r.Models[:0:0]
	for _, m := range r.Models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !strings.Contains(m, "/") || strings.ContainsAny(m, " ,\n") {
			return fmt.Errorf("%q is not a model like provider/model-id", m)
		}
		models = append(models, m)
	}
	r.Models = models
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Router = r
	return s.saveLocked()
}

// OwnModels returns the models the router tries after Claude, in order: the owner's
// list, or one default model for each provider key that is set.
func (s *Store) OwnModels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data.Router.Models) > 0 {
		return append([]string(nil), s.data.Router.Models...)
	}
	var out []string
	for _, k := range fccKeys {
		if s.data.Secrets[k.Secret] != "" {
			out = append(out, k.DefaultModel)
		}
	}
	return out
}

// ModelHosts returns the API hosts the router's own models need, so desks with a
// website allowlist can still reach them. None when the router uses Claude only.
func (s *Store) ModelHosts() []string {
	if s.Router().Mode == ModeClaude {
		return nil
	}
	var hosts []string
	for _, m := range s.OwnModels() {
		provider, _, _ := strings.Cut(m, "/")
		if h, ok := providerHosts[provider]; ok && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// ownModelsEnv is what the agent run needs to use the owner's models through FCC:
// FCC's settings (models, keys) plus Claude Code pointed at FCC inside the desk.
func (s *Store) ownModelsEnv() (map[string]string, bool) {
	models := s.OwnModels()
	if len(models) == 0 {
		return nil, false
	}
	env := map[string]string{
		"SHORTDESK_FCC":           "1",
		"MODEL":                   models[0],
		"ANTHROPIC_BASE_URL":      "http://127.0.0.1:8082",
		"ANTHROPIC_AUTH_TOKEN":    "freecc", // FCC's default; it only listens inside the desk
		"ANTHROPIC_API_KEY":       "",
		"CLAUDE_CODE_OAUTH_TOKEN": "",
	}
	if len(models) > 1 {
		env["MODEL_FALLBACKS"] = strings.Join(models[1:], ",")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range fccKeys {
		if v := s.data.Secrets[k.Secret]; v != "" {
			env[k.Env] = v
		}
	}
	return env, true
}

// Providers returns the providers a task tries, in order, for the router's mode.
func (s *Store) Providers() ([]agent.Provider, error) {
	claude := agent.Provider{Name: "Claude", Env: s.ClaudeEnv()}
	ownEnv, hasOwn := s.ownModelsEnv()
	models := s.OwnModels()
	own := agent.Provider{
		Name: "your own models (" + strings.Join(models, " → ") + ")",
		Env:  ownEnv,
		Note: "Important: you are not Claude and not made by Anthropic. You are the model " + strings.Join(models, ", then ") +
			", reached through Free Claude Code, a proxy that lets Claude Code use other models. If asked what you are, say that.",
		HideCost: true,
	}
	switch s.Router().Mode {
	case ModeClaude:
		return []agent.Provider{claude}, nil
	case ModeOwn:
		if !hasOwn {
			return nil, errors.New("no model keys set: add a Gemini, DeepSeek, Groq or NVIDIA key in Workspace settings")
		}
		return []agent.Provider{own}, nil
	default:
		if hasOwn {
			return []agent.Provider{claude, own}, nil
		}
		return []agent.Provider{claude}, nil
	}
}
