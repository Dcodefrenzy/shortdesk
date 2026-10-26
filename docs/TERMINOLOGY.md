# Shortdesk terminology

Plain-language definitions of the words used in Shortdesk's code, docs and conversations.

Status: ✅ built · 🔜 planned

---

## The big picture in one paragraph

**Shortdesk** gives AI agents their own computers, called **desks**. Each desk is created from a **desk image** (the template) inside a **box** that a **runtime** (usually **OpenSandbox**) starts and guards. A **charter** says what the desk is and what it may do. **Tasks** are sent to desks, where an **agent** (like Claude Code) does the work, with a **model** (like Claude) making the decisions. Humans watch through the **viewer** and step in through **help links**.

```
You ──task──► Shortdesk ──"make a desk with this charter"──► OpenSandbox ──► Box ──► Desk (from desk image)
                 │                                                                    │
                 └──────────── routes the task, streams progress, sends help links ◄──┘
```

---

## Core

### Shortdesk ✅
The product: open-source software anyone can download and run on their own machines. It decides **what** happens: which desks exist, which rules they follow, which tasks they run, which AI model answers. It doesn't do the work itself; agents inside desks do.
*Analogy:* the company's management and IT department.

### Desk ✅
One full Linux desktop computer for an agent (or a person) to work at: screen, Chromium browser, terminal, files, Claude Code. Viewed in a web browser. Every desk is separate: what happens in one can't affect another.
*Analogy:* one laptop handed to one worker.

### Desk image ✅
The **template** every desk is made from: the operating system, desktop, apps and settings, saved once. Making a new desk copies the template; deleting a desk leaves the template untouched. Lives in `images/desk/`.
*Analogy:* the master setup IT copies onto every new laptop.
*Not to be confused with:* a desk (a running copy of the template).

### Charter ✅
A YAML file that defines a desk and its rules: which image, how much CPU and memory, **which websites it may visit**, and how long it lives. Shortdesk enforces it; the agent can't change its own charter. Unknown or misspelled rules are rejected, never silently ignored.
*Analogy:* the company policy a laptop is set up with.
*Example:* `charters/examples/assistant.yaml`

```yaml
charter: assistant
permissions:
  network:
    allow: [claude.ai, "*.anthropic.com"]   # everything else is blocked
limits:
  desk_lifetime: 2h
```

### Tenant ✅ (label only) · 🔜 (full)
A person, team or company using Shortdesk, e.g. `personal`, `acme`, `my-team`. Tenants never see each other's desks, accounts, keys or bills. Today it's a label on desks; later it controls access, budgets and the vault.
*Analogy:* separate companies renting space in the same building.

---

## Boxes and runtimes

### Box (sandbox) ✅
An isolated, locked container that a desk runs inside. On its own a box is an empty room; it becomes a desk because we put the desk image in it. "Sandbox" = a safe place to play where nothing breaks outside it.
*Analogy:* a locked office room.

### Runtime ✅
The thing that creates, runs and deletes boxes. Shortdesk talks to any runtime through one interface, so runtimes are swappable.
- **OpenSandbox runtime** (default): enforces website rules, can hold keys, can pause desks.
- **Docker runtime**: for development only. It **can't** enforce website rules, so it refuses charters that have them.

### OpenSandbox ✅
Free, open-source software (Apache 2.0) that Shortdesk uses as its default runtime. It **spins desks up and down** and **guards them**: blocks websites not in the charter, injects keys without the desk seeing them, gives the link to each desk's screen.
*Analogy:* the building manager and security guard. Shortdesk is management; OpenSandbox runs the building.
*Not to be confused with:* Shortdesk. OpenSandbox handles the boxes; Shortdesk handles the work.

### OpenSandbox client ✅
The part of Shortdesk's code that talks to OpenSandbox ("create a desk", "run this in the desk", "delete it"). In `internal/runtime/opensandbox/`. Not a separate tool.

### execd ✅
A small program OpenSandbox puts inside every box so Shortdesk can run commands in it (used for copy/paste today, tasks later). ⚠️ Currently accepts commands without a password; fine on one machine, must be secured before shared use.

### Endpoint ✅
The address for reaching something inside a box, e.g. `127.0.0.1:56109/proxy/6080` for a desk's screen. Every box gets its own. Shortdesk turns these into the viewer links.

### Network allowlist (egress rules) ✅
The list of websites a desk may reach, from the charter's `permissions.network.allow`. Everything else is blocked by the runtime. `["*"]` means unrestricted, and has to be written explicitly.
*Analogy:* the guard's list of approved visitors, except it controls who the desk can visit.

### Lifetime / expiry ✅
How long a desk lives before it's removed automatically (`limits.desk_lifetime`). Stops forgotten desks running up costs. Enforced by OpenSandbox; the Docker runtime doesn't expire desks.

### Labels ✅
Tags Shortdesk puts on every box (`shortdesk.name`, `shortdesk.charter`, `shortdesk.tenant`) so it can find and list its desks.

### Snapshot 🔜
A saved copy of a box's whole state, used to start new desks already set up (e.g. logged in). Supported by OpenSandbox; not used yet.

### Warm desk 🔜
A desk kept running (or paused) and ready, so a task can start instantly instead of waiting for a new desk.

---

## Seeing and using a desk

### Viewer ✅
The web page showing a desk's live screen. You can watch or use it with your mouse and keyboard. Built on **KasmVNC**, which also syncs copy/paste with your computer (Chrome, Edge, Brave).

### KasmVNC ✅
The open-source program inside the desk image that streams the desktop to the browser. Replaced noVNC because it has automatic clipboard sync and better picture quality.

### Clipboard sync / desk paste & copy ✅
Moving text between your computer and a desk: automatic in the viewer, or with `shortdesk desk paste` / `shortdesk desk copy`.

### Shell ✅
A desk's terminal opened in your own terminal (`shortdesk desk shell`), with your normal copy/paste.

### Help link (handoff, `ask_human`) 🔜
When an agent hits something only a human should do (CAPTCHA, login, payment), it pauses and sends you a link. You open the desk, do it, and reply "done". The agent continues. A task waiting on you is `waiting_on_human`, not failed.

### Take over / hand back 🔜
Manually pausing the agent to use a desk yourself, then giving control back with an optional note.

---

## Tasks and agents

### Task 🔜
A unit of work sent to a desk: "research flights to Lagos", "find where to buy this item", "fix this bug". Shortdesk queues it, picks a desk, runs it, streams progress and returns the result.
*Analogy:* a ticket assigned to a worker.

### Agent ✅ (installed) · 🔜 (driven by tasks)
The program in the desk that does the work by using tools (terminal, files, browser). Default: **Claude Code**. Others: Codex, Gemini CLI, OpenCode, Aider. The agent is the **tool caller**: it carries out decisions, it doesn't make them.
*Analogy:* the assistant at the computer, following instructions.

### Model 🔜
The AI that decides what to do next (Claude, DeepSeek, Gemini...). It only reads and writes text and never touches the computer. The agent sends it the situation, it replies with the next step, the agent does it, and so on until the task is done.
*Analogy:* the boss on the phone giving instructions.

### Normal mode / Open mode 🔜
Which model a task uses, decided by simple if/else rules in Shortdesk:
- **Normal mode:** Claude Code talks directly to Claude, using your Claude subscription.
- **Open mode:** Claude Code talks to cheaper or free models through FCC and LiteLLM. Used when the Claude limit is reached, or for cheap tasks.

### Free Claude Code (FCC) 🔜
Open-source tool (AGPL-3.0) that lets Claude Code use non-Claude models, with a different model per tier (opus/sonnet/haiku) and fallbacks. Runs inside each desk in open mode.

### LiteLLM 🔜
Open-source model router. One central place for real API keys, budgets and spend tracking. In open mode, model requests go through it.

### OpenRouter 🔜
A paid service giving access to many AI models with one key. One of the providers behind LiteLLM.

### Claude subscription vs API key
Two ways to pay for Claude:
- **Subscription:** monthly plan, logged in with your account. Works only directly with Anthropic, has usage limits, and is for the owner's own use.
- **API key:** pay per use, a key any tool (including LiteLLM) can use. Other tenants bring their own.

### Streaming 🔜
Sending a task's progress to you live (each step the agent takes), to the dashboard or Telegram/Slack, instead of only at the end.

### MCP (Model Context Protocol)
A standard way to give agents extra tools (Gmail, Slack, Notion, databases...). Shortdesk's integration standard: a charter lists which MCP servers a desk gets. ⚠️ Claude account connectors (like Gmail) follow your Claude login into a desk; charters must control which ones a desk gets.

---

## Running Shortdesk

### Node 🔜
Any machine running desks for Shortdesk: your laptop, an office Mac mini, a cloud server. Office machines on home/office internet trigger fewer bot checks than cloud servers.

### Control plane 🔜
The central part of Shortdesk (dashboard, task queue, charters, vault, audit log) that all nodes connect to. On one machine, it runs on the same machine as the desks.

### Dashboard 🔜
The web app (Next.js) for managing everything: start/stop desks, watch them live, pick apps and integrations, manage logins and keys, send tasks.

### Catalog: apps and integrations 🔜
What you can tick in the dashboard:
- **Apps:** software installed into the desk image (Chrome, VS Code, Claude Code...)
- **Integrations:** accounts and connections added when a desk starts (Instagram login, Bitbucket, Slack...)

### Vault 🔜
Where logins and keys are stored, encrypted and write-only (can be replaced, never viewed). Built-in, or connected to Bitwarden / 1Password. Rule: **the AI never sees a secret**; Shortdesk uses it on the agent's behalf.

### Saved browser profile 🔜
A desk browser's saved logins and cookies, stored so future desks start already signed in (e.g. `pollen-ig-1`). Used by one desk at a time.

### Worker desk vs assistant desk 🔜
- **Worker desk:** short-lived, for one-off jobs (find a source, check prices, fix a bug), then deleted.
- **Assistant desk:** long-lived, with its own identity (email, calendar, memory, saved logins), for personal tasks.

---

## Project words

### Spike
A quick experiment to prove something works before building it properly. Kept in `spike/`, with results in `spike/RESULTS.md`.

### Milestone
One working, tested step of the build:
- **Milestone 1** ✅: desks from charters, via the CLI
- **Milestone 2** 🔜: web API + dashboard
