// Package vault keeps integration credentials in Vaultwarden (a self-hosted Bitwarden
// server) that Shortdesk runs on the same machine.
//
// Two containers on a private Docker network:
//   - shortdesk-vault: Vaultwarden, published only on 127.0.0.1 so the owner can open
//     its web vault to create their account and manage items.
//   - shortdesk-vault-cli: Bitwarden's official CLI, which Shortdesk uses to read and
//     write the vault.
//
// Desks never reach the vault. Shortdesk fetches a credential only to fill a login form
// in a desk's browser, and only for the site the credential belongs to.
package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pinned images and the address the owner opens to manage the vault.
const (
	VaultwardenImage = "vaultwarden/server:1.37.4"
	CLIImage         = "shortdesk/vault-cli:dev"
	vaultContainer   = "shortdesk-vault"
	cliContainer     = "shortdesk-vault-cli"
	network          = "shortdesk"
	hostPort         = "8222"
	// URL is the vault's address from this machine.
	URL = "https://localhost:" + hostPort
	// FolderName is the vault folder whose items are Shortdesk integrations.
	FolderName = "Shortdesk"
)

// ErrNotConnected means no vault account is connected yet.
var ErrNotConnected = errors.New("the vault isn't connected: connect your Vaultwarden account in Workspace settings")

// Integration is a login item in the Shortdesk folder. Secrets are never included.
type Integration struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Site     string `json:"site"` // host the login is for, e.g. instagram.com
	Username string `json:"username"`
	HasTOTP  bool   `json:"hasTotp"`
}

// NewIntegration is what the dashboard sends to add one.
type NewIntegration struct {
	Name       string `json:"name"`
	Site       string `json:"site"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
}

// Credential is a login fetched for filling a form.
type Credential struct {
	Username string
	Password string
	TOTP     string // current one-time code, if the item has a TOTP secret
}

// Secrets supplies the vault account's master password (kept write-only by the workspace).
type Secrets interface {
	Secret(name string) string
	SetSecret(name, value string) error
}

// Secret names in the workspace store.
const (
	SecretEmail    = "vault_email"
	SecretPassword = "vault_master_password"
)

// Vault manages the containers and talks to Vaultwarden through the Bitwarden CLI.
type Vault struct {
	dataDir string // ~/.shortdesk/vault
	secrets Secrets

	mu      sync.Mutex
	session string // BW_SESSION after unlock
	folder  string
}

// New returns a Vault keeping its data under dataDir.
func New(dataDir string, secrets Secrets) *Vault {
	return &Vault{dataDir: dataDir, secrets: secrets}
}

// Start makes sure both containers are running, with HTTPS on a certificate only this
// machine trusts (the Bitwarden CLI refuses plain HTTP, even on localhost).
func (v *Vault) Start(ctx context.Context) error {
	for _, d := range []string{"data", "cli"} {
		if err := os.MkdirAll(filepath.Join(v.dataDir, d), 0o700); err != nil {
			return err
		}
	}
	certFile, keyFile := v.certPaths()
	if err := ensureCert(certFile, keyFile); err != nil {
		return fmt.Errorf("vault certificate: %w", err)
	}
	docker(ctx, nil, "network", "create", network) // already exists is fine
	// Recreated on every start so its settings always match this version of Shortdesk;
	// the vault's data lives in the mounted folder.
	docker(ctx, nil, "rm", "-f", vaultContainer)
	if _, err := docker(ctx, nil, "run", "-d", "--name", vaultContainer, "--restart", "unless-stopped",
		"--network", network,
		"-v", filepath.Join(v.dataDir, "data")+":/data",
		"-p", "127.0.0.1:"+hostPort+":80",
		"-e", "DOMAIN="+URL,
		"-e", "SIGNUPS_ALLOWED=true", // only reachable from this machine
		"-e", `ROCKET_TLS={certs="/data/tls/cert.pem",key="/data/tls/key.pem"}`,
		VaultwardenImage); err != nil {
		return fmt.Errorf("start vault: %w", err)
	}
	// The CLI shares the vault container's network, so it reaches Vaultwarden as
	// localhost, and trusts only the vault's certificate.
	docker(ctx, nil, "rm", "-f", cliContainer)
	if _, err := docker(ctx, nil, "run", "-d", "--name", cliContainer, "--restart", "unless-stopped",
		"--network", "container:"+vaultContainer,
		"-v", filepath.Join(v.dataDir, "cli")+":/data",
		"-v", certFile+":/certs/vault.pem:ro",
		"-e", "NODE_EXTRA_CA_CERTS=/certs/vault.pem",
		CLIImage); err != nil {
		return fmt.Errorf("start vault CLI: %w", err)
	}
	return nil
}

func (v *Vault) certPaths() (cert, key string) {
	dir := filepath.Join(v.dataDir, "data", "tls")
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

// httpClient talks to the vault from this machine, trusting only its certificate.
func (v *Vault) httpClient() (*http.Client, error) {
	cert, _ := v.certPaths()
	pem, err := os.ReadFile(cert)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("vault certificate is unreadable")
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}, nil
}

// serverURL is Vaultwarden as the CLI sees it (same network namespace).
const serverURL = "https://localhost:80"

// ensure starts a container (on the shortdesk network) unless it's already running.
func ensure(ctx context.Context, name string, args ...string) error {
	if out, err := docker(ctx, nil, "inspect", "-f", "{{.State.Running}}", name); err == nil {
		if strings.TrimSpace(out) == "true" {
			return nil
		}
		_, err := docker(ctx, nil, "start", name)
		return err
	}
	run := append([]string{"run", "-d", "--name", name, "--network", network, "--restart", "unless-stopped"}, args...)
	_, err := docker(ctx, nil, run...)
	return err
}

// Status says whether the vault is running and connected.
type Status struct {
	URL       string `json:"url"`
	Running   bool   `json:"running"`
	Connected bool   `json:"connected"`
	Email     string `json:"email,omitempty"`
}

// Status reports the vault's state.
func (v *Vault) Status(ctx context.Context) Status {
	out, err := docker(ctx, nil, "inspect", "-f", "{{.State.Running}}", vaultContainer)
	st := Status{URL: URL, Running: err == nil && strings.TrimSpace(out) == "true"}
	st.Email = v.secrets.Secret(SecretEmail)
	// Set up counts as connected: the vault unlocks itself on first use (after a
	// restart there's no session until then).
	v.mu.Lock()
	st.Connected = v.session != "" || v.secrets.Secret(SecretPassword) != ""
	v.mu.Unlock()
	return st
}

// Connect logs the CLI into the owner's vault account and remembers it.
func (v *Vault) Connect(ctx context.Context, email, password string) error {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return errors.New("email and master password are required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bw(ctx, nil, nil, "logout") // switching accounts is fine
	if _, err := v.bw(ctx, nil, nil, "config", "server", serverURL); err != nil {
		return err
	}
	out, err := v.bw(ctx, map[string]string{"BW_PASSWORD": password}, nil, "login", email, "--passwordenv", "BW_PASSWORD", "--raw")
	if err != nil {
		return fmt.Errorf("vault login failed (check the email and master password, and that the account exists at %s): %w", URL, err)
	}
	if err := v.secrets.SetSecret(SecretEmail, email); err != nil {
		return err
	}
	if err := v.secrets.SetSecret(SecretPassword, password); err != nil {
		return err
	}
	v.session, v.folder = strings.TrimSpace(out), ""
	return nil
}

// Disconnect forgets the vault account (the vault and its items stay).
func (v *Vault) Disconnect(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bw(ctx, nil, nil, "logout")
	v.session, v.folder = "", ""
	if err := v.secrets.SetSecret(SecretEmail, ""); err != nil {
		return err
	}
	return v.secrets.SetSecret(SecretPassword, "")
}

// unlockedLocked returns a session, unlocking (or logging in again) if needed.
func (v *Vault) unlockedLocked(ctx context.Context) (string, error) {
	if v.session != "" {
		return v.session, nil
	}
	email, password := v.secrets.Secret(SecretEmail), v.secrets.Secret(SecretPassword)
	if email == "" || password == "" {
		return "", ErrNotConnected
	}
	env := map[string]string{"BW_PASSWORD": password}
	out, err := v.bw(ctx, env, nil, "unlock", "--passwordenv", "BW_PASSWORD", "--raw")
	if err != nil {
		v.bw(ctx, nil, nil, "config", "server", serverURL)
		if out, err = v.bw(ctx, env, nil, "login", email, "--passwordenv", "BW_PASSWORD", "--raw"); err != nil {
			return "", fmt.Errorf("unlock vault: %w", err)
		}
	}
	v.session = strings.TrimSpace(out)
	return v.session, nil
}

// session runs fn with an unlocked, synced session.
func (v *Vault) withSession(ctx context.Context, fn func(env map[string]string) error) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, err := v.unlockedLocked(ctx)
	if err != nil {
		return err
	}
	env := map[string]string{"BW_SESSION": s}
	if _, err := v.bw(ctx, env, nil, "sync"); err != nil {
		v.session = "" // the session may have expired; next call unlocks again
		return err
	}
	return fn(env)
}

func (v *Vault) folderLocked(ctx context.Context, env map[string]string) (string, error) {
	if v.folder != "" {
		return v.folder, nil
	}
	out, err := v.bw(ctx, env, nil, "list", "folders", "--search", FolderName)
	if err != nil {
		return "", err
	}
	var folders []struct{ ID, Name string }
	json.Unmarshal([]byte(out), &folders)
	for _, f := range folders {
		if f.Name == FolderName {
			v.folder = f.ID
			return f.ID, nil
		}
	}
	created, err := v.create(ctx, env, "folder", map[string]any{"name": FolderName})
	if err != nil {
		return "", err
	}
	v.folder = created
	return created, nil
}

type item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Login struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		URIs     []struct {
			URI string `json:"uri"`
		} `json:"uris"`
	} `json:"login"`
}

func (it item) integration() Integration {
	in := Integration{ID: it.ID, Name: it.Name, Username: it.Login.Username, HasTOTP: it.Login.TOTP != ""}
	if len(it.Login.URIs) > 0 {
		in.Site = hostOf(it.Login.URIs[0].URI)
	}
	return in
}

// List returns the integrations (login items in the Shortdesk folder).
func (v *Vault) List(ctx context.Context) ([]Integration, error) {
	var out []Integration
	err := v.withSession(ctx, func(env map[string]string) error {
		folder, err := v.folderLocked(ctx, env)
		if err != nil {
			return err
		}
		raw, err := v.bw(ctx, env, nil, "list", "items", "--folderid", folder)
		if err != nil {
			return err
		}
		var items []item
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return err
		}
		for _, it := range items {
			out = append(out, it.integration())
		}
		return nil
	})
	return out, err
}

// Add stores a new integration in the vault.
func (v *Vault) Add(ctx context.Context, in NewIntegration) (Integration, error) {
	site := hostOf(in.Site)
	if site == "" || in.Username == "" || in.Password == "" {
		return Integration{}, errors.New("site, username and password are required")
	}
	if in.Name == "" {
		in.Name = site
	}
	var res Integration
	err := v.withSession(ctx, func(env map[string]string) error {
		folder, err := v.folderLocked(ctx, env)
		if err != nil {
			return err
		}
		login := map[string]any{
			"username": in.Username, "password": in.Password,
			"uris": []map[string]any{{"uri": "https://" + site, "match": 0}},
		}
		if s := strings.ReplaceAll(strings.TrimSpace(in.TOTPSecret), " ", ""); s != "" {
			login["totp"] = s
		}
		id, err := v.create(ctx, env, "item", map[string]any{
			"type": 1, "name": in.Name, "folderId": folder, "login": login,
		})
		res = Integration{ID: id, Name: in.Name, Site: site, Username: in.Username, HasTOTP: login["totp"] != nil}
		return err
	})
	return res, err
}

// Delete removes an integration from the vault (it goes to the vault's trash).
func (v *Vault) Delete(ctx context.Context, id string) error {
	return v.withSession(ctx, func(env map[string]string) error {
		_, err := v.bw(ctx, env, nil, "delete", "item", id)
		return err
	})
}

// CredentialFor returns the login for site, with a fresh one-time code if it has TOTP.
// It only matches an integration whose site is the same host or a parent of it.
func (v *Vault) CredentialFor(ctx context.Context, site string) (Credential, Integration, error) {
	site = hostOf(site)
	var cred Credential
	var found Integration
	err := v.withSession(ctx, func(env map[string]string) error {
		folder, err := v.folderLocked(ctx, env)
		if err != nil {
			return err
		}
		raw, err := v.bw(ctx, env, nil, "list", "items", "--folderid", folder)
		if err != nil {
			return err
		}
		var items []item
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return err
		}
		for _, it := range items {
			in := it.integration()
			if in.Site == "" || !(site == in.Site || strings.HasSuffix(site, "."+in.Site)) {
				continue
			}
			found = in
			cred = Credential{Username: it.Login.Username, Password: it.Login.Password}
			if it.Login.TOTP != "" {
				code, err := v.bw(ctx, env, nil, "get", "totp", it.ID)
				if err != nil {
					return err
				}
				cred.TOTP = strings.TrimSpace(code)
			}
			return nil
		}
		return fmt.Errorf("no integration for %s: add it on the Integrations page", site)
	})
	return cred, found, err
}

// create makes a vault object from a template-shaped JSON value and returns its ID.
func (v *Vault) create(ctx context.Context, env map[string]string, kind string, obj any) (string, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	out, err := v.bw(ctx, env, strings.NewReader(encoded), "create", kind)
	if err != nil {
		return "", err
	}
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		return "", fmt.Errorf("create %s: unexpected output", kind)
	}
	return created.ID, nil
}

// bw runs the Bitwarden CLI in its container. Secrets go in the environment, never in
// arguments, so they don't appear in process lists.
func (v *Vault) bw(ctx context.Context, env map[string]string, stdin *strings.Reader, args ...string) (string, error) {
	cmd := []string{"exec", "-i"}
	for k, val := range env {
		cmd = append(cmd, "-e", k+"="+val)
	}
	cmd = append(cmd, cliContainer, "bw", "--nointeraction")
	cmd = append(cmd, args...)
	return docker(ctx, stdin, cmd...)
}

func docker(ctx context.Context, stdin *strings.Reader, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, "docker", args...)
	if stdin != nil {
		c.Stdin = stdin
	}
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// hostOf turns "https://www.Instagram.com/login" or "instagram.com" into a host.
func hostOf(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s, _, _ = strings.Cut(s, "/")
	s, _, _ = strings.Cut(s, ":")
	return strings.TrimPrefix(s, "www.")
}
