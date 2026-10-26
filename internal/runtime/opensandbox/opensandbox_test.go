package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

func newTestRuntime(t *testing.T, h http.HandlerFunc) *Runtime {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	r, err := New(Config{URL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreateSendsPolicyAndLimits(t *testing.T) {
	var got createRequest
	r := newTestRuntime(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("OPEN-SANDBOX-API-KEY") != "k" {
			t.Error("missing API key header")
		}
		if req.Method != http.MethodPost || req.URL.Path != "/v1/sandboxes" {
			t.Errorf("unexpected %s %s", req.Method, req.URL.Path)
		}
		json.NewDecoder(req.Body).Decode(&got)
		io.WriteString(w, `{"id":"abc","status":{"state":"Running"},"metadata":{"a":"b"},"createdAt":"2026-10-26T14:00:00Z"}`)
	})

	box, err := r.Create(context.Background(), runtime.Spec{
		Image: "img", CPU: "2", Memory: "4Gi", Lifetime: 30 * time.Second,
		Labels: map[string]string{"a": "b"}, AllowedHosts: []string{"claude.ai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if box.ID != "abc" || box.State != "Running" {
		t.Errorf("box = %+v", box)
	}
	if got.Timeout != 60 {
		t.Errorf("timeout = %d, want minimum 60", got.Timeout)
	}
	if got.NetworkPolicy == nil || got.NetworkPolicy.DefaultAction != "deny" ||
		len(got.NetworkPolicy.Egress) != 1 || got.NetworkPolicy.Egress[0].Target != "claude.ai" {
		t.Errorf("policy = %+v", got.NetworkPolicy)
	}
	if got.ResourceLimits["memory"] != "4Gi" {
		t.Errorf("limits = %v", got.ResourceLimits)
	}
}

func TestCreateWithoutAllowlistSendsNoPolicy(t *testing.T) {
	var raw map[string]any
	r := newTestRuntime(t, func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&raw)
		io.WriteString(w, `{"id":"abc","status":{"state":"Running"}}`)
	})
	if _, err := r.Create(context.Background(), runtime.Spec{Image: "img"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["networkPolicy"]; ok {
		t.Error("networkPolicy sent for unrestricted spec")
	}
}

func TestListFiltersByLabels(t *testing.T) {
	r := newTestRuntime(t, func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"items":[
			{"id":"1","metadata":{"shortdesk.managed":"true"}},
			{"id":"2","metadata":{"other":"x"}}]}`)
	})
	boxes, err := r.List(context.Background(), map[string]string{"shortdesk.managed": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 1 || boxes[0].ID != "1" {
		t.Errorf("boxes = %+v", boxes)
	}
}

func TestNotFound(t *testing.T) {
	r := newTestRuntime(t, func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	if _, err := r.Get(context.Background(), "x"); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestEndpointSplitsPrefix(t *testing.T) {
	r := newTestRuntime(t, func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"endpoint":"127.0.0.1:56109/proxy/6080"}`)
	})
	ep, err := r.Endpoint(context.Background(), "x", 6080)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Host != "127.0.0.1:56109" || ep.PathPrefix != "/proxy/6080" {
		t.Errorf("endpoint = %+v", ep)
	}
	if ep.URL() != "http://127.0.0.1:56109/proxy/6080" {
		t.Errorf("url = %s", ep.URL())
	}
}

func TestReadExecStream(t *testing.T) {
	stream := `{"type":"init","text":"id"}

{"type":"stdout","text":"hello"}

{"type":"stderr","text":"warn"}

{"type":"execution_complete"}
`
	var out, errOut strings.Builder
	if err := readExecStream(strings.NewReader(stream), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello\n" || errOut.String() != "warn\n" {
		t.Errorf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestReadExecStreamExitCode(t *testing.T) {
	stream := `{"type":"error","error":{"ename":"CommandExecError","evalue":"3"}}`
	err := readExecStream(strings.NewReader(stream), nil, nil)
	var exit *runtime.ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Errorf("err = %v, want exit code 3", err)
	}
}

func TestReadExecStreamTruncated(t *testing.T) {
	if err := readExecStream(strings.NewReader(`{"type":"stdout","text":"x"}`), nil, nil); err == nil {
		t.Error("expected error for stream without completion")
	}
}
