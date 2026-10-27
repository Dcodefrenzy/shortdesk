package phone

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Who may call the phone host. Shortdesk and the host share a secret; every key is
// signed with it, so the host keeps no list of keys and a desk can't make one up.
//
//	owner.<sig>                       Shortdesk, acting for the person
//	agent.<desk>.<expires>.<sig>      an agent working on a desk, for one task
//	app.<id>.<expires>.<sig>          opening one app's files (in a URL path)

const (
	roleOwner = "owner"
	roleAgent = "agent"
)

func sign(secret []byte, parts ...string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(m.Sum(nil))[:40]
}

// OwnerKey is the key Shortdesk uses for the person's requests.
func OwnerKey(secret []byte) string { return "owner." + sign(secret, "owner") }

// AgentKey lets an agent on deskName use apps until ttl passes.
func AgentKey(secret []byte, deskName string, ttl time.Duration, now time.Time) string {
	exp := strconv.FormatInt(now.Add(ttl).Unix(), 10)
	return "agent." + deskName + "." + exp + "." + sign(secret, "agent", deskName, exp)
}

// appToken lets a browser load one app's files until it expires.
func appToken(secret []byte, appID string, ttl time.Duration, now time.Time) string {
	exp := strconv.FormatInt(now.Add(ttl).Unix(), 10)
	return appID + "." + exp + "." + sign(secret, "app", appID, exp)
}

func checkAppToken(secret []byte, appID, token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != appID {
		return false
	}
	return fresh(parts[1], now) && hmac.Equal([]byte(parts[2]), []byte(sign(secret, "app", appID, parts[1])))
}

// caller is who a request comes from.
type caller struct {
	role  string
	actor string // shown in history: "you" or "agent on <desk>"
}

func parseKey(secret []byte, key string, now time.Time) (caller, error) {
	if key == OwnerKey(secret) {
		return caller{role: roleOwner, actor: "you"}, nil
	}
	parts := strings.Split(key, ".")
	if len(parts) == 4 && parts[0] == "agent" && validDesk(parts[1]) && fresh(parts[2], now) &&
		hmac.Equal([]byte(parts[3]), []byte(sign(secret, "agent", parts[1], parts[2]))) {
		return caller{role: roleAgent, actor: "agent on " + parts[1]}, nil
	}
	return caller{}, fmt.Errorf("not allowed")
}

func fresh(exp string, now time.Time) bool {
	n, err := strconv.ParseInt(exp, 10, 64)
	return err == nil && now.Unix() < n
}

func validDesk(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// SecretEnv names the variable that gives the Phone Desk its shared secret.
const SecretEnv = "SHORTDESK_PHONE_SECRET"
