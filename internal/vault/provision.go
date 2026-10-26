package vault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ownerEmail is the vault account Shortdesk creates for itself. It is never emailed:
// Vaultwarden runs without a mail server and only on this machine.
const ownerEmail = "owner@shortdesk.local"

// kdfIterations is Bitwarden's default PBKDF2-SHA256 work factor.
const kdfIterations = 600000

// Provision creates Shortdesk's vault account and connects to it, unless already
// connected. The owner never has to visit the vault: the master password is generated
// and kept with the workspace's other secrets.
func (v *Vault) Provision(ctx context.Context) error {
	v.mu.Lock()
	connected := v.session != ""
	v.mu.Unlock()
	if connected {
		return nil
	}
	client, err := v.httpClient()
	if err != nil {
		return err
	}
	if err := waitAlive(ctx, client); err != nil {
		return err
	}
	// Reuse a password saved by an earlier attempt; otherwise generate and save one
	// before the account exists, so a failure later never leaves a locked account.
	password := v.secrets.Secret(SecretPassword)
	if password == "" || v.secrets.Secret(SecretEmail) != ownerEmail {
		if password, err = randomPassword(); err != nil {
			return err
		}
		if err := v.secrets.SetSecret(SecretEmail, ownerEmail); err != nil {
			return err
		}
		if err := v.secrets.SetSecret(SecretPassword, password); err != nil {
			return err
		}
		if err := register(ctx, client, URL, ownerEmail, password); err != nil {
			return fmt.Errorf("create vault account: %w", err)
		}
	}
	return v.Connect(ctx, ownerEmail, password)
}

func waitAlive(ctx context.Context, client *http.Client) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, URL+"/alive", nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("vault at %s did not become ready", URL)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func randomPassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// accountKeys is what a Bitwarden client sends to create an account. All encryption
// happens here; the server only ever sees encrypted keys and a password hash.
type accountKeys struct {
	MasterPasswordHash  string
	UserSymmetricKey    string // the vault key, encrypted with the stretched master key
	PublicKey           string
	EncryptedPrivateKey string // the RSA private key, encrypted with the vault key
}

// deriveAccountKeys follows Bitwarden's key model (KDF type 0, PBKDF2-SHA256):
//
//	masterKey  = PBKDF2(password, salt=email, iterations)
//	hash       = PBKDF2(masterKey, salt=password, 1)          (sent to the server)
//	stretched  = HKDF-Expand(masterKey, "enc") || HKDF-Expand(masterKey, "mac")
//	vault key  = 64 random bytes, stored as EncString(vault key, stretched)
func deriveAccountKeys(email, password string) (accountKeys, error) {
	salt := strings.ToLower(strings.TrimSpace(email))
	masterKey, err := pbkdf2.Key(sha256.New, password, []byte(salt), kdfIterations, 32)
	if err != nil {
		return accountKeys{}, err
	}
	hash, err := pbkdf2.Key(sha256.New, string(masterKey), []byte(password), 1, 32)
	if err != nil {
		return accountKeys{}, err
	}
	stretched, err := stretch(masterKey)
	if err != nil {
		return accountKeys{}, err
	}
	vaultKey := make([]byte, 64)
	if _, err := rand.Read(vaultKey); err != nil {
		return accountKeys{}, err
	}
	protected, err := encString(vaultKey, stretched)
	if err != nil {
		return accountKeys{}, err
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return accountKeys{}, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return accountKeys{}, err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return accountKeys{}, err
	}
	encPriv, err := encString(privDER, vaultKey)
	if err != nil {
		return accountKeys{}, err
	}
	return accountKeys{
		MasterPasswordHash:  base64.StdEncoding.EncodeToString(hash),
		UserSymmetricKey:    protected,
		PublicKey:           base64.StdEncoding.EncodeToString(pub),
		EncryptedPrivateKey: encPriv,
	}, nil
}

// stretch turns a 32-byte master key into Bitwarden's 64-byte enc||mac key.
func stretch(masterKey []byte) ([]byte, error) {
	enc, err := hkdf.Expand(sha256.New, masterKey, "enc", 32)
	if err != nil {
		return nil, err
	}
	mac, err := hkdf.Expand(sha256.New, masterKey, "mac", 32)
	if err != nil {
		return nil, err
	}
	return append(enc, mac...), nil
}

// encString encrypts plain as a Bitwarden type-2 EncString (AES-256-CBC + HMAC-SHA256):
// "2.<iv>|<ciphertext>|<mac>", with key = 32-byte encryption key || 32-byte MAC key.
func encString(plain, key []byte) (string, error) {
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return "", err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	m := hmac.New(sha256.New, key[32:])
	m.Write(iv)
	m.Write(ct)
	b64 := base64.StdEncoding.EncodeToString
	return "2." + b64(iv) + "|" + b64(ct) + "|" + b64(m.Sum(nil)), nil
}

// decString reverses encString (used to check keys round-trip).
func decString(s string, key []byte) ([]byte, error) {
	parts := strings.Split(strings.TrimPrefix(s, "2."), "|")
	if !strings.HasPrefix(s, "2.") || len(parts) != 3 {
		return nil, fmt.Errorf("not a type-2 EncString")
	}
	var raw [3][]byte
	for i, p := range parts {
		b, err := base64.StdEncoding.DecodeString(p)
		if err != nil {
			return nil, err
		}
		raw[i] = b
	}
	iv, ct, mac := raw[0], raw[1], raw[2]
	m := hmac.New(sha256.New, key[32:])
	m.Write(iv)
	m.Write(ct)
	if !hmac.Equal(m.Sum(nil), mac) {
		return nil, fmt.Errorf("MAC mismatch")
	}
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return nil, err
	}
	if len(ct)%aes.BlockSize != 0 || len(ct) == 0 {
		return nil, fmt.Errorf("bad ciphertext length")
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ct)
	pad := int(out[len(out)-1])
	if pad == 0 || pad > aes.BlockSize {
		return nil, fmt.Errorf("bad padding")
	}
	return out[:len(out)-pad], nil
}

// register creates an account with Vaultwarden's two-step flow. Without a mail server,
// step one returns the verification token directly.
func register(ctx context.Context, client *http.Client, base, email, password string) error {
	token, err := postJSON(ctx, client, base+"/identity/accounts/register/send-verification-email",
		map[string]any{"email": email, "name": "Shortdesk", "receiveMarketingEmails": false})
	if err != nil {
		return err
	}
	token = strings.Trim(strings.TrimSpace(token), `"`)
	keys, err := deriveAccountKeys(email, password)
	if err != nil {
		return err
	}
	_, err = postJSON(ctx, client, base+"/identity/accounts/register/finish", map[string]any{
		"email":                  email,
		"emailVerificationToken": token,
		"masterPasswordHash":     keys.MasterPasswordHash,
		"masterPasswordHint":     "",
		"userSymmetricKey":       keys.UserSymmetricKey,
		"userAsymmetricKeys": map[string]string{
			"publicKey":           keys.PublicKey,
			"encryptedPrivateKey": keys.EncryptedPrivateKey,
		},
		"kdf":           0,
		"kdfIterations": kdfIterations,
	})
	return err
}

func postJSON(ctx context.Context, client *http.Client, url string, body any) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		msg := string(out)
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(out, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", fmt.Errorf("%s: HTTP %d: %s", url[strings.LastIndex(url, "/")+1:], resp.StatusCode, msg)
	}
	return string(out), nil
}
