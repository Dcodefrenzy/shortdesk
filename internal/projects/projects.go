// Package projects reports and merges desks' work on code projects in the shared
// ~/Developer folder. Each desk works on its own git branch (desk-project in the desk
// image); merging into the project's main branch happens here, locally. Nothing is
// pushed to remote repositories.
package projects

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

//go:embed desk.sh
var deskScript string

// Project is one git project as seen from one desk.
type Project struct {
	Name        string `json:"name"`
	Main        string `json:"main"`
	Branch      string `json:"branch"`      // this desk's branch ("" if it never worked on it)
	Ahead       int    `json:"ahead"`       // commits on the branch not in main
	Behind      int    `json:"behind"`      // commits in main not on the branch
	Uncommitted int    `json:"uncommitted"` // changed files not committed yet
	LastCommit  string `json:"lastCommit"`
}

// Errors callers can check for.
var (
	ErrConflict = errors.New("merge conflict")
	ErrNotClean = errors.New("project main copy has uncommitted changes")
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Manager runs project operations inside desks.
type Manager struct{ rt runtime.Runtime }

// New returns a Manager.
func New(rt runtime.Runtime) *Manager { return &Manager{rt: rt} }

// List returns the projects in ~/Developer and this desk's work on each.
func (m *Manager) List(ctx context.Context, deskID string) ([]Project, error) {
	var out bytes.Buffer
	if err := m.run(ctx, deskID, &out, "status"); err != nil {
		return nil, err
	}
	projects := []Project{}
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		var p Project
		if json.Unmarshal(sc.Bytes(), &p) == nil && p.Name != "" {
			projects = append(projects, p)
		}
	}
	return projects, nil
}

// Diff returns this desk's committed changes to a project, against main.
func (m *Manager) Diff(ctx context.Context, deskID, name string) (string, error) {
	if !valid(name) {
		return "", fmt.Errorf("invalid project name %q", name)
	}
	var out bytes.Buffer
	if err := m.run(ctx, deskID, &out, "diff", name); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Merge merges this desk's branch into the project's main branch. On a conflict
// nothing changes and the error lists the conflicting files.
func (m *Manager) Merge(ctx context.Context, deskID, name string) error {
	if !valid(name) {
		return fmt.Errorf("invalid project name %q", name)
	}
	err := m.run(ctx, deskID, nil, "merge", name)
	var exit *runtime.ExitError
	if errors.As(err, &exit) {
		switch exit.Code {
		case 4:
			return fmt.Errorf("%w: %s", ErrConflict, err)
		case 3:
			return ErrNotClean
		}
	}
	return err
}

// SetDeskName tells the desk its name, which names its branches.
func (m *Manager) SetDeskName(ctx context.Context, deskID, name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid desk name %q", name)
	}
	return m.rt.Exec(ctx, deskID, runtime.ExecRequest{
		Command: `printf %s "$SHORTDESK_DESK_NAME" > /home/desk/.shortdesk-desk`,
		Env:     map[string]string{"SHORTDESK_DESK_NAME": name},
	})
}

func valid(name string) bool { return nameRe.MatchString(name) && !strings.HasPrefix(name, ".") }

func (m *Manager) run(ctx context.Context, deskID string, stdout *bytes.Buffer, args ...string) error {
	var stderr bytes.Buffer
	req := runtime.ExecRequest{
		Command: `bash -c "$SHORTDESK_PROJECTS" projects ` + strings.Join(args, " "),
		Env:     map[string]string{"SHORTDESK_PROJECTS": deskScript, "HOME": "/home/desk"},
		Stderr:  &stderr,
	}
	if stdout != nil {
		req.Stdout = stdout
	}
	if err := m.rt.Exec(ctx, deskID, req); err != nil {
		msg := strings.TrimSpace(stderr.String())
		var exit *runtime.ExitError
		if errors.As(err, &exit) && msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
