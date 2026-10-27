package agent

import "regexp"

// Common reasons a task fails, and what the person can do about each. The agent's own
// error is still shown; this adds the next step in plain words.
var (
	notSignedInRe = regexp.MustCompile(`(?i)not logged in|please run /login|oauth token (has )?expired|invalid bearer token`)
	noCreditRe    = regexp.MustCompile(`(?i)insufficient[ _](balance|quota|credit)|payment required|credit balance is too low|\b402\b`)
	badKeyRe      = regexp.MustCompile(`(?i)invalid[ _-]?(x-)?api[ _-]?key|incorrect api key|authentication_error|api key not valid|\b401\b|unauthorized`)
	unreachableRe = regexp.MustCompile(`(?i)name or service not known|gaierror|getaddrinfo|could not resolve|enotfound|eai_again|connection (error|refused)|connecterror`)
)

// explainFailure returns what to do about a failed task, or "" when the cause isn't one
// Shortdesk recognises. limited means the last model tried was out of usage; hadBackup
// means the router had more than one to try.
func explainFailure(problems string, limited, hadBackup bool) string {
	switch {
	case notSignedInRe.MatchString(problems):
		return "Claude isn't signed in. Open this desk's **Desktop** tab, run `claude` in the terminal and sign in once; " +
			"every desk gets the login. Or add a Claude token in [Workspace settings](/workspace/)."
	case noCreditRe.MatchString(problems):
		return "Your account with this AI provider is out of credit. Top it up on the provider's website, " +
			"or add a different model key in [Workspace settings](/workspace/)."
	case badKeyRe.MatchString(problems):
		return "An AI key was rejected. Check it and paste it again in [Workspace settings](/workspace/) → AI connections."
	case unreachableRe.MatchString(problems):
		return "The desk couldn't reach the AI service. Check this computer's internet connection. If the desk uses a " +
			"website list, make sure the service is allowed: this desk's **Settings → Websites**, or turn on **Allow all websites**."
	case limited && !hadBackup:
		return "Claude's usage limit is reached and there's no backup model. Add a model key in " +
			"[Workspace settings](/workspace/) (Gemini and Groq have free tiers) and tasks switch to it automatically, " +
			"or try again after the limit resets."
	case limited:
		return "All your models are out of usage right now. Try again after a limit resets, or add another model key in " +
			"[Workspace settings](/workspace/)."
	}
	return ""
}
