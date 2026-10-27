package cli

import (
	"net"
	"strings"
	"testing"
)

func TestExposureWarning(t *testing.T) {
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7070}
	all := &net.TCPAddr{IP: net.IPv4zero, Port: 7070}
	if w := exposureWarning(local, false); w != "" {
		t.Errorf("loopback: %q", w)
	}
	if w := exposureWarning(all, false); !strings.Contains(w, "plain HTTP") {
		t.Errorf("all interfaces: %q", w)
	}
	if w := exposureWarning(all, true); w == "" || strings.Contains(w, "plain HTTP") {
		t.Errorf("secure: %q", w)
	}
}
