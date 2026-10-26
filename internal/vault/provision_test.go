package vault

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"testing"
)

func TestEncStringRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 64)
	for _, plain := range [][]byte{{}, []byte("x"), bytes.Repeat([]byte("a"), 16), bytes.Repeat([]byte("b"), 100)} {
		s, err := encString(plain, key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decString(s, key)
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("round trip %q: got %q, %v", plain, got, err)
		}
	}
	s, _ := encString([]byte("secret"), key)
	other := bytes.Repeat([]byte{8}, 64)
	if _, err := decString(s, other); err == nil {
		t.Error("decrypted with the wrong key")
	}
}

// The derived keys must fit together the way a Bitwarden client checks them at login:
// the master password unlocks the vault key, which unlocks the RSA private key.
func TestDeriveAccountKeysAreConsistent(t *testing.T) {
	email, password := "Owner@Shortdesk.local", "correct horse battery staple"
	keys, err := deriveAccountKeys(email, password)
	if err != nil {
		t.Fatal(err)
	}

	masterKey, _ := pbkdf2.Key(sha256.New, password, []byte("owner@shortdesk.local"), kdfIterations, 32)
	wantHash, _ := pbkdf2.Key(sha256.New, string(masterKey), []byte(password), 1, 32)
	if keys.MasterPasswordHash != base64.StdEncoding.EncodeToString(wantHash) {
		t.Error("password hash doesn't match Bitwarden's derivation (salt must be the lowercased email)")
	}

	stretched, _ := stretch(masterKey)
	vaultKey, err := decString(keys.UserSymmetricKey, stretched)
	if err != nil || len(vaultKey) != 64 {
		t.Fatalf("vault key: %v (len %d)", err, len(vaultKey))
	}
	privDER, err := decString(keys.EncryptedPrivateKey, vaultKey)
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	priv, err := x509.ParsePKCS8PrivateKey(privDER)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, _ := base64.StdEncoding.DecodeString(keys.PublicKey)
	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		t.Fatal(err)
	}
	if !pub.(*rsa.PublicKey).Equal(&priv.(*rsa.PrivateKey).PublicKey) {
		t.Error("public key doesn't match the encrypted private key")
	}
}
