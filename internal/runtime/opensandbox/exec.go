package opensandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

// execEvent is one line of execd's streamed /command response.
type execEvent struct {
	Type  string `json:"type"` // init, ping, stdout, stderr, error, execution_complete
	Text  string `json:"text"`
	Error *struct {
		Name  string `json:"ename"`
		Value string `json:"evalue"`
	} `json:"error"`
}

// Exec runs a command through the sandbox's execd daemon and streams its output.
// execd sends each output line as its own event without the line ending, so one
// newline is written back per event.
func (r *Runtime) Exec(ctx context.Context, id string, req runtime.ExecRequest) error {
	ep, err := r.Endpoint(ctx, id, execdPort)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"command": req.Command, "envs": req.Env})
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL()+"/command", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	// Command streams can outlive the lifecycle client's timeout; rely on ctx instead.
	resp, err := (&http.Client{Transport: r.http.Transport}).Do(hreq)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &apiError{Status: resp.StatusCode, Body: string(b)}
	}
	return readExecStream(resp.Body, req.Stdout, req.Stderr)
}

func readExecStream(r io.Reader, stdout, stderr io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev execEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("exec: bad event %q: %w", line, err)
		}
		switch ev.Type {
		case "stdout":
			writeLine(stdout, ev.Text)
		case "stderr":
			writeLine(stderr, ev.Text)
		case "error":
			return execError(ev)
		case "execution_complete":
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return errors.New("exec: stream ended before the command completed")
}

func writeLine(w io.Writer, text string) {
	if w == nil {
		return
	}
	io.WriteString(w, text)
	if !strings.HasSuffix(text, "\n") {
		io.WriteString(w, "\n")
	}
}

// execError maps execd's error event to an ExitError when the command exited non-zero.
func execError(ev execEvent) error {
	if ev.Error == nil {
		return errors.New("exec: unknown error")
	}
	if ev.Error.Name == "CommandExecError" {
		if code, err := strconv.Atoi(ev.Error.Value); err == nil {
			return &runtime.ExitError{Code: code}
		}
	}
	return fmt.Errorf("exec: %s: %s", ev.Error.Name, ev.Error.Value)
}
