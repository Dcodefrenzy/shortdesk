// Package clipboard reads and writes this machine's clipboard.
package clipboard

import (
	"errors"
	"os/exec"
	goruntime "runtime"
	"strings"
)

// Read returns the clipboard's text.
func Read() (string, error) {
	name, args, err := readCmd()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// Write replaces the clipboard's text.
func Write(text string) error {
	name, args, err := writeCmd()
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func readCmd() (string, []string, error) {
	switch goruntime.GOOS {
	case "darwin":
		return "pbpaste", nil, nil
	case "windows":
		return "powershell", []string{"-NoProfile", "-Command", "Get-Clipboard -Raw"}, nil
	case "linux":
		return linuxCmd([]string{"wl-paste", "--no-newline"}, []string{"xclip", "-selection", "clipboard", "-o"})
	}
	return "", nil, errUnsupported
}

func writeCmd() (string, []string, error) {
	switch goruntime.GOOS {
	case "darwin":
		return "pbcopy", nil, nil
	case "windows":
		return "clip", nil, nil
	case "linux":
		return linuxCmd([]string{"wl-copy"}, []string{"xclip", "-selection", "clipboard", "-i"})
	}
	return "", nil, errUnsupported
}

var errUnsupported = errors.New("clipboard: unsupported OS (use --text)")

// linuxCmd prefers Wayland's tool, then X11's.
func linuxCmd(candidates ...[]string) (string, []string, error) {
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			return c[0], c[1:], nil
		}
	}
	return "", nil, errors.New("clipboard: install wl-clipboard or xclip (or use --text)")
}
