package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsAreWriteOnlyAndPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret(AnthropicKey, "  sk-ant-abcdefgh1234  "); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret("nope", "x"); err == nil {
		t.Error("unknown connection accepted")
	}
	for _, st := range s.Status() {
		if st.Name == AnthropicKey && (!st.Set || st.Hint != "…1234") {
			t.Errorf("status = %+v", st)
		}
		if strings.Contains(st.Hint, "abcdefgh") {
			t.Error("status leaks the secret")
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Secret(AnthropicKey) != "sk-ant-abcdefgh1234" {
		t.Error("secret not persisted (or not trimmed)")
	}
	reopened.SetSecret(AnthropicKey, "")
	if reopened.Secret(AnthropicKey) != "" {
		t.Error("empty value should clear the secret")
	}
}

func TestVaultSecretsAreStoredButNotListed(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "ws.json"))
	if err := s.SetSecret("vault_master_password", "pw"); err != nil {
		t.Fatal(err)
	}
	for _, st := range s.Status() {
		if st.Name == "vault_master_password" {
			t.Error("vault secret listed as an AI connection")
		}
	}
}

func TestDroppedConnectionsAreRemovedOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	os.WriteFile(path, []byte(`{"secrets":{"openrouter_api_key":"sk-or-1","anthropic_api_key":"k"}}`), 0o600)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Secret("openrouter_api_key") != "" || s.Secret(AnthropicKey) != "k" {
		t.Error("unsupported connection kept, or supported one lost")
	}
}

func TestClaudeEnvPrefersSubscription(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "ws.json"))
	if env := s.ClaudeEnv(); len(env) != 0 {
		t.Errorf("no credentials should rely on the desk's own login: %v", env)
	}
	s.SetSecret(AnthropicKey, "sk-ant-1")
	if env := s.ClaudeEnv(); env["ANTHROPIC_API_KEY"] != "sk-ant-1" {
		t.Errorf("api key env = %v", env)
	}
	s.SetSecret(ClaudeToken, "oauth-1")
	if env := s.ClaudeEnv(); env["CLAUDE_CODE_OAUTH_TOKEN"] != "oauth-1" || env["ANTHROPIC_API_KEY"] != "" {
		t.Errorf("subscription token should win: %v", env)
	}
	ps, _ := s.Providers()
	if len(ps) != 1 || ps[0].Name != "Claude" {
		t.Errorf("providers = %+v", ps)
	}
}

func TestRouterUsesOwnModelsThroughFCC(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "ws.json"))
	names := func() string {
		ps, err := s.Providers()
		if err != nil {
			return "error"
		}
		var n []string
		for _, p := range ps {
			n = append(n, p.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(); got != "Claude" {
		t.Errorf("auto with no model keys = %s", got)
	}
	s.SetSecret(GeminiKey, "g-key")
	s.SetSecret(DeepSeekKey, "d-key")
	if got := names(); got != "Claude,your own models (gemini/models/gemini-3.1-flash-lite → deepseek/deepseek-chat)" {
		t.Errorf("auto with keys = %s", got)
	}
	ps, _ := s.Providers()
	env := ps[1].Env
	if env["MODEL"] != "gemini/models/gemini-3.1-flash-lite" || env["MODEL_FALLBACKS"] != "deepseek/deepseek-chat" {
		t.Errorf("default models = %q / %q", env["MODEL"], env["MODEL_FALLBACKS"])
	}
	if env["GEMINI_API_KEY"] != "g-key" || env["DEEPSEEK_API_KEY"] != "d-key" || env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8082" {
		t.Errorf("fcc env = %v", env)
	}
	if v, ok := env["ANTHROPIC_API_KEY"]; !ok || v != "" {
		t.Error("ANTHROPIC_API_KEY must be set empty so Claude Code talks to FCC")
	}

	if err := s.SetRouter(Router{Mode: ModeOwn, Models: []string{" deepseek/deepseek-chat ", "", "gemini/models/gemini-3.1-flash-lite"}}); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.Providers()
	if len(ps) != 1 || ps[0].Env["MODEL"] != "deepseek/deepseek-chat" || ps[0].Env["MODEL_FALLBACKS"] != "gemini/models/gemini-3.1-flash-lite" {
		t.Errorf("custom order = %+v", ps)
	}
	if err := s.SetRouter(Router{Mode: ModeAuto, Models: []string{"no-slash"}}); err == nil {
		t.Error("bad model accepted")
	}
	if err := s.SetRouter(Router{Mode: "fast"}); err == nil {
		t.Error("bad mode accepted")
	}

	s.SetRouter(Router{Mode: ModeOwn})
	s.SetSecret(GeminiKey, "")
	s.SetSecret(DeepSeekKey, "")
	if _, err := s.Providers(); err == nil {
		t.Error("own-models mode without keys should explain what's missing")
	}
}
