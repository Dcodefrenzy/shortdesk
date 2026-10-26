package desk

// App is software on a desk.
type App struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

// ImageApps describes images/desk: what every desk has today, and what the app
// catalog will offer. Kept here until the catalog exists and apps come from charters.
var ImageApps = []App{
	{"Chromium", "Web browser, with the agent's browser tool", true},
	{"Firefox", "Second web browser (ESR)", true},
	{"Claude Code", "The agent that works on the desk", true},
	{"Terminal", "XFCE terminal", true},
	{"Files", "Thunar file manager", true},
	{"Git, Node.js, jq", "Developer tools", true},
	{"VS Code", "Code editor", false},
	{"Codex CLI", "OpenAI's coding agent", false},
	{"LibreOffice", "Documents and spreadsheets", false},
	{"Free Claude Code", "Run the agent on cheaper models", false},
}
