// Package agent runs an AI agent (Claude Code) inside a desk for a task, turns its
// output into chat events, and lets a human pause, resume or stop it.
package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind is what an event shows in the chat.
type Kind string

const (
	KindPrompt     Kind = "prompt"      // the user's task
	KindText       Kind = "text"        // the agent talking
	KindTool       Kind = "tool"        // the agent using a tool
	KindToolResult Kind = "tool_result" // what the tool returned
	KindResult     Kind = "result"      // the task finished
	KindError      Kind = "error"
	KindStatus     Kind = "status" // the task's status changed
	KindLog        Kind = "log"    // other output
	KindHelp       Kind = "help"   // after a failure: what the person can do about it

	// KindAccessRequest is the agent asking to reach a blocked website.
	// Data: requestId, host, reason.
	KindAccessRequest Kind = "access_request"
	// KindAccessDecision is a human answering a request. Data: requestId, host, decision.
	KindAccessDecision Kind = "access_decision"

	// KindHelper is news about a job a helper desk is doing for this desk.
	// Data: job, title, state (started, done, failed), desk (the helper's ID).
	KindHelper Kind = "helper"
)

// accessRequestMarker starts the line printed by the desk's shortdesk-request-access.
const accessRequestMarker = "SHORTDESK_ACCESS_REQUEST "

// Event is one entry in a desk's chat.
type Event struct {
	Seq     int       `json:"seq"`
	TaskID  string    `json:"taskId"`
	Time    time.Time `json:"time"`
	Kind    Kind      `json:"kind"`
	Text    string    `json:"text,omitempty"`
	Tool    string    `json:"tool,omitempty"`
	IsError bool      `json:"isError,omitempty"`
	CostUSD float64   `json:"costUsd,omitempty"`
	// Data carries kind-specific fields (see the Kind constants).
	Data map[string]string `json:"data,omitempty"`
}

const (
	maxToolSummary = 300
	maxToolResult  = 2000
)

// claudeLine is the subset of Claude Code's --output-format stream-json we use.
type claudeLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Input   json.RawMessage `json:"input"`
			Content json.RawMessage `json:"content"`
			IsError bool            `json:"is_error"`
		} `json:"content"`
	} `json:"message"`
	Result  string  `json:"result"`
	IsError bool    `json:"is_error"`
	CostUSD float64 `json:"total_cost_usd"`
	Model   string  `json:"model"`
}

// ParseClaudeLine turns one stdout line from `claude -p --output-format stream-json`
// into chat events. Lines that are not JSON (e.g. "You've hit your limit") become
// log events, so nothing the agent prints is lost.
func ParseClaudeLine(line string) []Event {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var cl claudeLine
	if line[0] != '{' || json.Unmarshal([]byte(line), &cl) != nil {
		return []Event{{Kind: KindLog, Text: line}}
	}
	switch cl.Type {
	case "system":
		if cl.Subtype == "init" {
			return []Event{{Kind: KindLog, Text: "Agent started"}}
		}
	case "assistant":
		var out []Event
		for _, c := range cl.Message.Content {
			switch c.Type {
			case "text":
				if t := strings.TrimSpace(c.Text); t != "" {
					out = append(out, Event{Kind: KindText, Text: t})
				}
			case "tool_use":
				out = append(out, Event{Kind: KindTool, Tool: c.Name, Text: truncate(summarizeInput(c.Input), maxToolSummary)})
			}
		}
		return out
	case "user":
		var out []Event
		for _, c := range cl.Message.Content {
			if c.Type != "tool_result" {
				continue
			}
			text := resultText(c.Content)
			out = append(out, Event{Kind: KindToolResult, Text: truncate(text, maxToolResult), IsError: c.IsError})
			if req, ok := parseAccessRequest(text); ok {
				out = append(out, req)
			}
		}
		return out
	case "result":
		text := strings.TrimSpace(cl.Result)
		if text == "" {
			text = "Finished (" + cl.Subtype + ")"
		}
		return []Event{{Kind: KindResult, Text: text, IsError: cl.IsError, CostUSD: cl.CostUSD}}
	}
	return nil
}

// parseAccessRequest finds a shortdesk-request-access line in a tool's output.
func parseAccessRequest(text string) (Event, bool) {
	for _, line := range strings.Split(text, "\n") {
		body, ok := strings.CutPrefix(strings.TrimSpace(line), accessRequestMarker)
		if !ok {
			continue
		}
		var req struct{ Host, Reason string }
		if json.Unmarshal([]byte(body), &req) != nil {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(req.Host))
		if host == "" {
			continue
		}
		return Event{
			Kind: KindAccessRequest,
			Text: "Access requested: " + host,
			Data: map[string]string{"host": host, "reason": req.Reason},
		}, true
	}
	return Event{}, false
}

// summarizeInput picks the most telling field of a tool's input (a URL, a command, ...).
func summarizeInput(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return string(raw)
	}
	for _, k := range []string{"url", "command", "query", "file_path", "path", "pattern", "text", "element", "description"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return strings.Join(parts, " ")
}

// resultText flattens a tool_result's content, which is a string or a list of blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			b.WriteString(bl.Text)
			b.WriteString("\n")
		case "image":
			b.WriteString("[image]\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
