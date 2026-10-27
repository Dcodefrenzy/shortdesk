package agent

import (
	"strings"
	"testing"
)

func TestExplainFailure(t *testing.T) {
	for _, tc := range []struct {
		problems        string
		limited, backup bool
		want            string // substring; "" means no explanation
	}{
		{"API Error: 500 Provider API request failed.\nConnectError: [Errno -2] Name or service not known", false, true, "couldn't reach"},
		{"Invalid API key · Please run /login", false, false, "isn't signed in"},
		{"Error code: 401 - {'error': {'message': 'Incorrect API key provided'}}", false, true, "key was rejected"},
		{"Insufficient Balance", false, true, "out of credit"},
		{"You've hit your weekly limit · resets Oct 29, 11pm", true, false, "no backup model"},
		{"You've hit your weekly limit", true, true, "All your models"},
		{"The agent exited with status 1.", false, false, ""},
	} {
		got := explainFailure(tc.problems, tc.limited, tc.backup)
		if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.problems, got, tc.want)
		}
	}
}
