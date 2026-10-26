# Shortdesk

> A developer's desk for AI agents: a full computer (terminal, IDE, browser, apps, logins) that agents work at, controlled by a charter, runnable on any machine.

Status: **design draft**. Nothing is built yet. This document defines *what* we are building before we write code.

---

## 1. The idea in one paragraph

I work on a laptop: a terminal, VS Code, Chrome logged into Bitbucket, Slack and dashboards, git, Docker, Claude Code. Shortdesk gives that same setup to AI agents. Each agent gets a **desk**, a containerised desktop computer with the same apps and settings a developer has. You send it a task ("fix this bug and open a PR"), and the agent does the whole job the way a developer would. It can drive the browser, run tests and push branches. Humans review the result. Desks run anywhere: a laptop, an office Mac mini, or a cloud VM. Every desk is governed by a **charter** that states what it has, what it may do and what it may spend.

Shortdesk is **open source** and general-purpose: not tied to one company, model vendor or git host. **Anyone can download it, run it on their own machines, and get their own dashboard.** Their data, keys and accounts never pass through us.

```bash
curl -fsSL https://shortdesk.dev/install.sh | sh
shortdesk up        # installs and runs everything: dashboard, sandbox runtime, queue, DB, model gateway
```

Then they open their dashboard (e.g. `http://localhost:8080`, or on their server) and:
1. Create their workspace / company.
2. Pick apps and integrations from the catalog (§6.1).
3. Add their logins and AI keys to the vault (Claude subscription, OpenRouter, ...) (§6.3).
4. Add their machines (laptop, Mac mini, cloud VM).
5. Send tasks and watch their desks work.

Users only ever see Shortdesk. Components like the sandbox runtime, Redis, Postgres and LiteLLM are bundled and configured by `shortdesk up`. A hosted version (shortdesk.cloud) may come later for people who don't want to run their own server; self-hosted stays the main product. Pollen and Perto (§16) are example users, not special cases.

---

## 2. Principles

1. **Like a real developer laptop.** If a developer can do it on their machine, an agent should be able to do it at a desk.
2. **Any model, any agent.** Claude Code, Codex, Gemini CLI, OpenCode, Aider; Anthropic, OpenAI, Google, OpenRouter, local models. Nothing is hard-wired.
3. **Governance is code, not trust.** The charter is enforced by the platform. The agent never grants itself permissions.
4. **Runs anywhere.** One binary on any machine joins the fleet. No machine is special.
5. **Humans approve what matters.** Agents work on branches and open PRs; merging stays human by default.
6. **Easy setup.** One command for a single-machine setup. A fleet should take minutes, not days.
7. **Prove locally first.** Each phase works on one laptop before it is distributed.

---

## 3. Core concepts

| Concept | What it is | Laptop analogy |
|---|---|---|
| **Desk** | A running containerised desktop (Linux + apps + settings + persistent home) | Your laptop |
| **Charter** | YAML file defining a desk: apps, settings, models, permissions, limits, approvals | Company IT policy + your setup |
| **Catalog** | Reusable app packages (Chrome, VS Code, Claude Code, ...) that charters reference | The software center |
| **Task** | A unit of work sent to a desk ("fix bug X in repo Y") | A ticket assigned to you |
| **Node** | Any machine running the Shortdesk agent that hosts desks | A desk in the office |
| **Control plane** | Central service: queue, orchestrator, charters, audit, dashboard | Management / IT department |
| **Tenant** | A person, team or company with its own charters, keys, budgets and audit trail | An organisation |

---

## 4. What's on a desk

A desk aims to match a developer laptop:

- **Desktop:** Ubuntu + XFCE, viewed in the browser (watch an agent live, or take over).
- **Browser:** Chrome with a persistent profile (logins survive restarts), extensions preinstalled, including Claude in Chrome so agents can operate the browser.
- **Editor:** VS Code (or the browser-based code-server).
- **Terminal tools:** git, node, python, go, docker CLI, database clients, `gh` / Bitbucket CLI.
- **Agent CLIs:** Claude Code, Codex CLI, Gemini CLI, OpenCode, Aider (whichever the charter enables).
- **Model router:** free-claude-code proxy, so those agents can run on cheaper or free models (§7.3).
- **Browser automation for agents:** Claude in Chrome, Playwright MCP, Chrome DevTools MCP.
- **MCP servers:** declared in the charter (Slack, Linear, Jira, Sentry, DBs, ...).
- **Services:** test databases or Redis as sidecar containers declared in the charter, so the agent can run the project's tests.
- **Persistent home volume:** shell history, Chrome profile, CLI logins and caches.

### 4.1 Two kinds of desk

| | Worker desk | Assistant desk |
|---|---|---|
| Purpose | Any one-off job: find a product's source, check prices, fix a bug, run a report | Personal tasks that need an identity (research, booking, email, admin) |
| Lifetime | Fresh per task, destroyed after | Long-lived, persistent |
| Identity | None (bot git account only) | Own email, calendar, browser logins, memory, virtual card |
| Example | "Where can I buy this item + current prices?" / "Fix the redis bug" | "Research flights to Lagos next year" |

An assistant desk "has a life". Its identity is declared in the charter:

```yaml
charter: my-assistant
desk:
  persistent: true
  apps: [chrome, claude-code, libreoffice]
identity:
  name: "Ayo's Desk"
  email: desk@yourdomain.com        # own mailbox, logged in once in Chrome
  calendar: shared-with-owner
  memory: enabled                   # ~/memory: preferences, past tasks
  phone: optional                   # e.g. Twilio number for 2FA; many sites block virtual numbers
  payment: { card: virtual-1, limit_per_tx_usd: 1500 }
approvals:
  human_required_for: [payment, send_email_to_new_contact, account_signup, delete_anything]
```

Risks specific to assistant desks:
- **Prompt injection:** a desk that reads email and web pages can be tricked by hidden instructions. Payments, new contacts and deletions **always** require human approval, enforced by the charter and never left to the agent.
- **Bot detection:** booking and flight sites show CAPTCHAs. Use the handoff (§8.3), or APIs (e.g. Duffel for flights) where possible.
- **Terms of service:** use accounts we own; expect occasional blocks.

---

## 5. Architecture

```
┌──────────────────────────── CONTROL PLANE (online, one place) ───────────────────────────┐
│                                                                                          │
│  Inbox ──► Orchestrator ──► Queue (Redis)        Charters + Catalog     Model Gateway    │
│  (CLI, web,  • dedupe          ready tasks       (versioned, git)       (LiteLLM:        │
│   Slack,     • plan/split                                               keys, budgets,   │
│   Telegram)  • locks                             Audit log + Dashboard   routing)        │
│              • approvals                         (Postgres)                              │
└───────────────────────────────────────┬──────────────────────────────────────────────────┘
                                        │  nodes connect OUT (no inbound ports)
            ┌───────────────────────────┼────────────────────────────┐
            ▼                           ▼                            ▼
   Node: laptop                 Node: Mac mini                Node: cloud VM
   shortdesk-node               shortdesk-node                shortdesk-node
   ├ desk (charter A)           ├ desk (charter A)            ├ desk (charter C)
   └ desk (charter B)           ├ desk (charter B)            └ ...
                                └ ...
                                        │
                     ┌──────────────────┴───────────────────┐
                     ▼                                      ▼
          Git hosts (Bitbucket, GitHub, GitLab)     Cloud agent offload
          branches + PRs → human review             (Claude Code cloud / Codex cloud)
                                                    for small tasks
```

### Single-machine mode
`shortdesk up` runs the control plane **and** a node on one machine. This is how everyone starts, and it is all Phase 1 needs.

### Fleet mode
The control plane is deployed once (a small VPS is enough). Each extra machine runs `shortdesk node join <token>`.

---

## 6. The charter (governance contract)

The charter is the heart of governance. It is versioned in git, so every change is reviewed and auditable.

```yaml
charter: backend-bugfixer
tenant: perto
version: 3

desk:
  base: shortdesk/desktop-ubuntu:24.04
  apps: [chrome, vscode, claude-code, codex, opencode, postgres-client]
  settings:
    chrome:
      extensions: [claude-in-chrome]
      homepage: https://bitbucket.org
  services:
    - postgres:16          # sidecar for tests
    - redis:7
  resources: { cpu: 2, memory: 6GB }
  max_desks: 5

models:
  default: { agent: claude-code, model: claude-sonnet-5-5 }
  allowed:
    - { agent: claude-code, model: claude-opus-5-5 }
    - { agent: codex,       model: gpt-* }
    - { agent: opencode,    model: openrouter/* }
  router: free-claude-code                    # see §7.3, per-tier cheap models
  tiers:                                      # Claude Code tier → actual model
    opus:   anthropic/claude-opus-5-5
    sonnet: openrouter/deepseek/deepseek-chat
    haiku:  groq/llama-3.3-70b
  fallbacks: [openrouter/qwen/qwen3-coder, ollama/qwen2.5-coder]

permissions:
  network:
    allow: [bitbucket.org, api.anthropic.com, openrouter.ai, registry.npmjs.org, github.com]
  git:
    repos: [perto/perto-iota-be-new]
    push: ["agent/*"]                         # never main/develop
  secrets: [BITBUCKET_TOKEN]                  # injected at runtime, never baked in
  browser:
    allow_sites: [bitbucket.org, localhost]

limits:
  task_timeout: 30m
  max_turns: 200
  budget: { per_task_usd: 5, monthly_usd: 300 }

approvals:
  human_required_for: [merge, new_domain, new_secret, install_app]

audit:
  record: [commands, network, model_calls, screenshots]
  retain: 90d
```

**Who enforces what**

| Charter section | Enforced by |
|---|---|
| `desk` | Image builder (catalog → Docker image) + node runtime |
| `models` | Model gateway (only allowed models/keys reachable) |
| `permissions.network` | Egress proxy per desk (allowlist) |
| `permissions.git` | Scoped git token + branch restrictions on the git host |
| `permissions.secrets` | Node injects only listed secrets at runtime |
| `limits` | Node (timeouts) + gateway (budget) |
| `approvals` | Orchestrator pauses the task and asks a human |
| `audit` | Node + gateway write to the audit log |

Policy decisions (e.g. "may this desk reach this domain?") are evaluated with **OPA**, so tenants can also write custom rules.

### 6.1 Installing apps and integrations from the dashboard

Nobody writes YAML by hand unless they want to. Each tenant picks apps and integrations in the **dashboard**, and Shortdesk generates the charter and installs everything.

```
┌─ Shortdesk dashboard › Pollen › Desk template "source-finder" ─────────────┐
│ APPS                                 INTEGRATIONS                          │
│ [x] Chrome + Claude in Chrome        [x] Instagram  (2 accounts)           │
│ [ ] VS Code                          [x] TikTok     (1 account)            │
│ [x] Claude Code                      [x] Pinterest                         │
│ [x] Free Claude Code (cheap models)  [ ] Bitbucket                         │
│ [ ] Codex                            [x] Pollen API (results webhook)      │
│ [ ] LibreOffice                      [ ] Slack                             │
│                                                                            │
│ Models: Claude first → cheap fallback      Max desks: 10                   │
│                                              [ Save & roll out ]           │
└────────────────────────────────────────────────────────────────────────────┘
```

**Two kinds of things you can select:**

| | Apps | Integrations |
|---|---|---|
| What | Software installed on the desk | Accounts / connections the desk can use |
| Examples | Chrome, VS Code, Claude Code, FCC, Codex, LibreOffice | Instagram/TikTok logins, Bitbucket, Slack, Gmail, Pollen API, MCP servers |
| Installed | **Baked into the desk image** (build time) | **Injected when a desk starts** (runtime): secrets, MCP config, logged-in browser profiles |
| Changes need | Image rebuild (cached, usually fast) | Nothing; next desk picks it up |

**Each catalog entry is a small package with a manifest:**

```yaml
# catalog/instagram/manifest.yaml
name: instagram
kind: integration
needs:
  apps: [chrome]
  accounts: { type: browser-login, site: instagram.com }   # login once via ask_human, kept in Chrome profile
grants:
  network: [instagram.com, cdninstagram.com]               # added to the desk's allowlist automatically
limits:
  one_account_per_desk: true
```

```yaml
# catalog/claude-code/manifest.yaml
name: claude-code
kind: app
install: install.sh            # runs in the image build
settings: settings.json.tmpl   # filled from the charter (model, mode, MCP servers)
grants:
  network: [api.anthropic.com]
```

**What "Save & roll out" does:**
1. Dashboard writes the tenant's charter (new version, kept in history).
2. Image builder: base image + selected apps → tenant image. Apps are separate cached layers, so adding one app is quick.
3. Integrations are stored as secrets / account logins, never inside the image.
4. Running desks finish their current task, then restart on the new version. New desks start on it immediately.
5. New accounts (e.g. an Instagram login) trigger `ask_human` once: someone logs in through the desk view and the session is saved.

**Each tenant gets its own setup:**

| | Pollen | Perto |
|---|---|---|
| Desk template | `source-finder` | `backend-bugfixer` |
| Apps | Chrome + Claude in Chrome, Claude Code, FCC | Chrome, VS Code, Claude Code, Codex, DB clients |
| Integrations | Instagram, TikTok, Pinterest, Pollen API | Bitbucket, Slack, test Postgres/Redis |
| Desk type | Short-lived, many in parallel | Short-lived, one per task |
| Models | Mostly cheap (high volume) | Claude first (quality) |
| Output | JSON results to Pollen's API | Bitbucket PR |

A tenant can have several templates (e.g. Pollen: `source-finder` + `price-checker`). Tenants never see each other's desks, accounts or secrets.

### 6.2 Keeping passwords and keys from leaking

**Core rule: the AI never sees a secret.** Anything the agent can read can end up in the model provider's logs, in a report, or be sent out by a prompt-injected web page. So secrets are *used* by the platform, never *handed* to the agent.

| Secret | How it's used without the agent seeing it |
|---|---|
| Website logins (Instagram, TikTok, ...) | A human logs in once via `ask_human`; only the **session cookie** lives in the Chrome profile. The agent never types or reads a password |
| Passwords that must be filled | Password-manager extension (Bitwarden / 1Password) autofills. The agent clicks "fill", never sees the value |
| API keys (models, OpenRouter) | Held by LiteLLM in the control plane. The desk gets a scoped, revocable gateway key, not the real one |
| API tokens (Bitbucket, Pollen API, ...) | **Injected by the egress proxy** into outgoing requests to that domain. The desk has no token to leak |
| Git push | Short-lived token per task, scoped to one repo and `agent/*` branches, expires when the task ends |
| Payment cards | Virtual cards with limits; payment always goes through `ask_human` |

**Layers of protection:**
1. **Network allowlist:** a desk can only reach domains in its charter, so even a leaked value has nowhere to go.
2. **Redaction:** the node scans every streamed event, log line and report for known secret values and masks them before they leave the desk.
3. **Agent permissions:** Claude Code's settings deny reading secret files and dumping env vars (`env`, `printenv`, `~/.config/...`).
4. **Encryption at rest:** secrets store (Infisical / Vault / SOPS) with per-tenant keys. Chrome profiles hold session cookies, so their volumes are encrypted and per-tenant.
5. **Isolation:** tenants never share desks, volumes, accounts or networks.
6. **Rotation + audit:** short-lived tokens, easy revocation from the dashboard, and every secret use is logged (which desk, which domain, when).
7. **Approvals:** sending email to new contacts, payments and signups always need a human. This blocks the main prompt-injection payoffs.

### 6.3 The vault: where logins and keys are stored

Every workspace has a **Logins & Keys** page in the dashboard. It's the only place secrets are entered, and they can never be read back in plain text, not even by admins after saving.

```
┌─ Shortdesk › Workspace › Logins & Keys ─────────────────────────────────────┐
│ AI PROVIDERS        Claude subscription ●  OpenRouter ●  Gemini ○  [+ Add]  │
│ WEBSITE LOGINS      instagram (3 accounts) ●  tiktok (1) ●       [+ Add]    │
│ API TOKENS          Bitbucket ●  Slack ●  Pollen API ●            [+ Add]   │
│ CARDS               Virtual card ••4321 (limit £1,500) ●          [+ Add]   │
│ STORAGE BACKEND     Built-in vault ▾  (Built-in / 1Password / Bitwarden /   │
│                                        HashiCorp Vault / Infisical)         │
└─────────────────────────────────────────────────────────────────────────────┘
```

**What can be stored:**

| Type | Example | How desks use it (agent never sees the value, §6.2) |
|---|---|---|
| AI provider keys | Anthropic API key, OpenRouter key, Claude subscription token | Loaded into LiteLLM / passed to Claude Code by the node; desk only gets a scoped key |
| Website logins | Instagram username + password + 2FA (TOTP) secret | Autofilled into Chrome by the password-manager extension, or a human logs in once and the session is saved |
| Saved browser sessions | Logged-in Chrome profile cookies | Encrypted volume mounted only into that workspace's desks |
| API tokens / OAuth | Bitbucket, Slack, Gmail, Pollen API | Injected by the egress proxy / credential vault into requests to that domain |
| Cards | Virtual card details | Used only at payment steps, always behind `ask_human` |

**Storage backends: users pick one.**

| Backend | For whom | Notes |
|---|---|---|
| **Built-in vault (default)** | Everyone; works out of the box | Bundled with `shortdesk up`. Not home-made crypto: we embed a proven open-source secret store (**OpenBao**, the open-source fork of HashiCorp Vault, or **Infisical**), or at minimum envelope encryption with standard libraries (AES-256-GCM, per-workspace keys). The master key comes from a passphrase set at install, the OS keychain, or a cloud KMS |
| **1Password** | Teams already on 1Password | Via 1Password Service Accounts / Connect. Secrets stay in 1Password; Shortdesk fetches at runtime. Desk Chrome can also run the 1Password extension for autofill |
| **Bitwarden / Vaultwarden** | Open-source fans, self-hosters | Bitwarden Secrets Manager for keys; Bitwarden extension for website logins. Vaultwarden is free and self-hostable |
| **HashiCorp Vault / OpenBao / Infisical** | Companies with existing secret infra | Point Shortdesk at their instance |

**Rules for every backend:**
- Write-only in the dashboard: secrets can be replaced, never viewed.
- Scoped: each secret is assigned to specific desk templates (Pollen's IG accounts only go to `source-finder` desks).
- Fetched just in time when a desk/task starts, never baked into images, never written to logs (§6.2 redaction).
- Every read is audited (which desk, which task, when). Revoke and rotate from the dashboard.
- Backups are encrypted. Losing the master key means losing the vault, so this is stated clearly at install.

**Decision:** we do **not** invent our own cryptography. "Our own secured way" means a built-in vault that wraps a proven open-source store, plus connectors to 1Password and Bitwarden.

---

## 7. Models and agents

### 7.1 Agent CLIs (run inside desks)
| Agent | Models | Notes |
|---|---|---|
| Claude Code | Claude (Anthropic API or subscription token); any model via a gateway | Strongest default; headless via `claude -p`; operates Chrome via Claude in Chrome |
| Codex CLI | OpenAI | Headless via `codex exec` |
| Gemini CLI | Google Gemini | Open source |
| OpenCode | Any (75+ providers incl. OpenRouter, local) | Open source, model-agnostic |
| Aider | Any | Open source, git-native |

### 7.2 Model gateway: routing to the right model
All model traffic from desks goes through **one gateway** in the control plane:

- **LiteLLM** (open source, self-hosted): a single endpoint that speaks both the Anthropic and OpenAI APIs and routes to Anthropic, OpenAI, Gemini, OpenRouter, Bedrock, Vertex or local models (Ollama). It provides per-tenant virtual keys, budgets, rate limits and spend logs. **This is where model governance happens.**
- **OpenRouter**: one of the upstreams behind the gateway, giving access to many models with one key.
- Desks never see real provider keys, only a gateway key scoped to their charter.

Claude Code is pointed at the gateway with `ANTHROPIC_BASE_URL`, so it can use non-Claude models when the charter allows. Quality varies a lot by model for agentic tool use, so Claude stays the default for coding tasks.

### 7.3 Cheap models for small tasks: Free Claude Code (FCC)
[free-claude-code](https://github.com/alishahryar1/free-claude-code) (FCC) is an open-source local proxy that lets Claude Code, Codex, OpenCode, Aider, Cline and others run on **cheaper, free or local models**. It supports about 60 providers, including NVIDIA NIM, OpenRouter, Groq, DeepSeek, Gemini, Mistral, Ollama and LM Studio. We keep the Claude Code experience but stop paying Claude prices for every task.

What we use from it:
- **Per-tier routing:** Claude Code's opus / sonnet / haiku tiers each map to a different backend (`MODEL_OPUS`, `MODEL_SONNET`, `MODEL_HAIKU`). Hard reasoning goes to a strong model; small tasks and sub-agents go to cheap or free ones.
- **Fallback chain:** if a provider fails or hits a rate limit, FCC moves to the next model, so long-running agent tasks keep going.
- **Launchers:** `fcc-claude`, `fcc-codex`, `fcc-opencode`, `fcc-aider`, and others start the agent already pointed at the proxy.
- **Telegram/Discord bots:** optional. These could become an early task inbox.

How it fits Shortdesk:
- FCC must run **on the same machine and user as the agent**, so it runs **inside each desk** as a catalog app, not centrally. The charter's `models.tiers` / `fallbacks` generate its config.
- **FCC (in the desk)** handles *which model answers this request*. **LiteLLM (central)** handles keys, budgets and spend. To be verified: whether FCC can use the LiteLLM gateway as an upstream (any OpenAI-compatible endpoint). If it can, the desk never holds real provider keys and governance stays central.
- Orchestrator rule (charter-configurable): **small/simple task → cheap tiers via FCC; hard task → strong model (e.g. Claude Opus).**

Caveats:
- **License: AGPL-3.0.** Ship it unmodified, as a separate process installed from upstream, rather than copying its code into Shortdesk. This matters if Shortdesk is Apache 2.0 (see §15).
- It's written in Python (installed via `uv`). That's fine inside the desk image.
- Free tiers change without notice, and one failed request can consume quota on several providers in turn.
- Cheap and local models vary widely at agentic tool use. Expect to test and tune per task type.

### 7.4 Routing rules: normal vs open mode

Routing is **plain if/else in Shortdesk**, with no AI and no extra cost. Shortdesk decides the mode *before* starting Claude Code by setting one environment variable:

```bash
# Normal mode: Claude Code → Anthropic directly (owner's Claude subscription)
claude -p "<task>"

# Open mode: Claude Code → FCC → LiteLLM → DeepSeek / Gemini / Groq / ...
ANTHROPIC_BASE_URL=http://localhost:8082 claude -p "<task>"
```

```go
if charter.ForceCheap || claudeLimitReached {
    runOpenMode(task)
} else {
    runNormalMode(task)
}
// Claude Code exits with "usage limit reached" mid-task
// → restart in open mode and resume the same session.
```

| Decision | Made by |
|---|---|
| Normal Claude or open mode? | Shortdesk (charter rules) |
| Which cheap model, in open mode? | FCC tiers + LiteLLM routing/fallbacks |

**Who pays what:** normal mode is covered by the existing Claude subscription. Open mode costs only the chosen provider's usage (cents per task via OpenRouter, or free tiers). FCC and LiteLLM are free open-source software.

**Subscription vs API key:** a Claude *subscription* is a login, and only works directly between Claude Code and Anthropic, so LiteLLM can't route it. With a pay-per-use Anthropic *API key*, all traffic, Claude included, can go through LiteLLM with one set of fallback rules. Plan: the owner uses their subscription (two decision points). Other tenants bring their own API keys (LiteLLM handles everything).

### 7.5 Optional: cloud offload
Separately, small repo-only tasks can be sent to **Claude Code on the web / routines** or **Codex cloud**, which run on the vendor's infrastructure. This is only for the owner's own subscription. Using a personal subscription to serve *other* tenants likely violates the provider's terms. Low priority compared with FCC.

---

## 8. Task lifecycle

```
1. Submit      shortdesk task "fix redis reconnect bug" --repo perto/be --charter backend-bugfixer
2. Triage      orchestrator: duplicate? scope? small → cloud offload : local desk
3. Lock        lock repo/area so two desks don't collide
4. Assign      a node with capacity pulls the task, starts a fresh desk from the charter
5. Work        agent clones repo → branch agent/<task-id> → edits, runs tests, uses Chrome if needed
6. Deliver     push branch → open PR (draft if tests fail) → summary + PR link to inbox
7. Review      humans review; PR comments can create follow-up tasks on the same branch
8. Close       desk destroyed (or kept warm), lock released, audit + cost recorded
```

Collision prevention:
- **Queue claims are atomic.** One task goes to exactly one desk.
- **Scope locks:** v1 locks per repo, v2 per folder/module.
- **Dedupe + planner:** an LLM step flags duplicates and splits big tasks into non-overlapping subtasks. Rules (locks, budgets, permissions) are always enforced in code, never left to an LLM.

### 8.1 How a task actually runs inside a desk

Shortdesk does not do the work. **Claude Code, running in the desk's terminal, does the work**, the same way it works on a developer laptop.

| Part | Role | Analogy |
|---|---|---|
| **Model** (Claude, DeepSeek, ...) | Decides what to do next. Only reads and writes text; never touches the computer | The boss on the phone |
| **Claude Code** | Tool caller: carries out each decision on the desk (terminal, files, Chrome, MCP tools) and reports back what it sees | The assistant at the computer |
| **Shortdesk** | Provides the desk, starts Claude Code with the task + settings + mode, enforces the charter, streams progress | The office and management |

The loop, repeated until done:
1. Claude Code sends the model the task, the list of tools available, and everything seen so far.
2. The model replies with the next action, e.g. "open Google Flights and search London → Lagos, March 2027".
3. Claude Code performs it on the desk (drives the real Chrome via Claude in Chrome / Playwright MCP, runs a command, writes a file).
4. Claude Code sends the result (page content, command output) back to the model → step 1.

The model chooses tools based on the task. A bug fix uses terminal + files. Flight research uses Chrome or web search, or a flight API tool if the charter provides one. Shortdesk launches it in a `tmux` session so it keeps running unattended, and a human can also attach and talk to it interactively.

### 8.2 Streaming progress back to the user

```bash
claude -p "<task>" --output-format stream-json --verbose
```

Claude Code emits one JSON event per line as it works (messages, tool calls, tool results, final result with cost). This is identical in normal and open mode.

```
Desk: Claude Code ──lines──► Node worker ──► Redis (live channel) ──► Dashboard (WebSocket, full live feed)
                              + Postgres (history, replay)        └─► Telegram/Slack (one message, edited
                                                                       in place: "🔍 Checking Skyscanner…" → "✅ Done")
```

Messages can also go *in* while a task runs (`--input-format stream-json`), e.g. "also check Emirates".

### 8.3 Desktop view and human handoff

Each desk runs a VNC server (KasmVNC / noVNC). From the dashboard, the owner can **watch** the desk's real screen or **control** it with their own mouse and keyboard, from a laptop or phone.

To avoid the agent and a human fighting over the mouse, control is handed off explicitly. The main path is **the agent asks**:

```
1. Agent hits a CAPTCHA / login / 2FA / payment page
2. Agent calls the ask_human tool: "Payment page for BA £612, please complete"
   → agent WAITS and does not touch the screen
3. Owner gets a message (Telegram/Slack/email): "🙋 Desk needs you → [Open desk]"
4. Owner opens the link → the desk is on that exact page
5. Owner does what's needed (solve, log in, pay, or decide not to)
6. Owner replies "done" (optionally with a note: "done, use Virgin instead" / "skip this site")
7. Agent reads the reply and continues
```

There's also a manual path: a **Take over** button pauses the agent, and **Hand back** resumes it with an optional note.

Rules:
- A task waiting on a human is in state **`waiting_on_human`**, not failed. Reminders are sent; the timeout is set per charter (e.g. 24h).
- Desk links require login. They give full control of a desk that may hold logins and payment details.
- Every handoff is audited: who, when, how long, and the note.

### 8.4 Example: "Research flights to Lagos next year"

1. Request arrives via Telegram → orchestrator: not a code task → owner's **assistant desk** (§4.1), normal mode.
2. Claude Code reads `~/memory` (home airport, preferred airlines, budget).
3. The model directs Chrome through Google Flights, Skyscanner and airline sites; it compares dates, prices, stops and baggage, and notes cheaper months.
4. Writes `~/reports/lagos-flights.md` and sends a summary + report via Telegram and the desk's own email.
5. Optional: schedules a weekly price check and alerts on drops.
6. Owner: "book the March 12 BA option" → desk fills everything up to payment → `ask_human` → owner pays via the link or approves the virtual card → "done" → confirmation lands in the desk's mailbox and the trip goes on the shared calendar.

---

## 9. Tech stack

| Part | Choice | Why |
|---|---|---|
| Control plane + node agent | **Go** | Same language as Docker/containerd/OPA/Tailscale; single binary per machine |
| Dashboard / web inbox | **TypeScript (Next.js)** | Fast UI work |
| Charter format | **YAML + JSON Schema** | Readable by people, validated by tools |
| Policy | **OPA / Rego** | Standard policy engine |
| Catalog packages | Dockerfile fragments + Bash | Normal for installing software |
| Rust | Later, only if needed | e.g. a high-performance egress filter |

---

## 10. External tools we build on

| Need | Tool | Open source | Setup ease | Notes |
|---|---|---|---|---|
| Container runtime | Docker (Linux), OrbStack/Colima (Mac) | Mostly | ⭐⭐⭐ easy | Already on most dev machines |
| Desktop in the browser | linuxserver/webtop or KasmVNC | ✅ | ⭐⭐⭐ easy | `docker run` and open a URL |
| Multi-arch images | Docker buildx | ✅ | ⭐⭐ | Needed: Mac minis are arm64, cloud is often amd64 |
| Queue | Redis + Asynq (Go) | ✅ | ⭐⭐⭐ | Postgres-based River is an alternative to drop Redis |
| Database | Postgres | ✅ | ⭐⭐⭐ | Tasks, tenants, audit |
| Model gateway | LiteLLM | ✅ | ⭐⭐ | One Docker container + config |
| Multi-model access | OpenRouter | ❌ (service) | ⭐⭐⭐ | One API key |
| Cheap models in Claude Code etc. | free-claude-code (FCC) | ✅ AGPL-3.0 | ⭐⭐⭐ | `curl \| sh` install; runs inside each desk; per-tier routing + fallbacks |
| Policy engine | OPA | ✅ | ⭐⭐ | Embedded as a Go library |
| Egress allowlist | Squid or Smokescreen proxy per desk | ✅ | ⭐⭐ | Desk network → proxy only |
| Private networking | Tailscale (or Headscale, self-hosted) | Client ✅ / Headscale ✅ | ⭐⭐⭐ | Nodes reach control plane privately |
| Secrets vault | Built-in (OpenBao or Infisical, bundled) + connectors: 1Password, Bitwarden/Vaultwarden, HashiCorp Vault | ✅ / mixed | ⭐⭐ | Never in images; see §6.3 |
| Docker inside desks | Sidecar services (v1); Sysbox runtime (later, Linux only) | ✅ | ⭐ harder | Agents need DBs for tests; sidecars cover most cases |
| Browser control | Claude in Chrome, Playwright MCP, Chrome DevTools MCP | Mixed | ⭐⭐ | Verify Claude in Chrome support on Linux desks |
| Cloud offload (optional) | Claude Code web/routines, Codex cloud | ❌ (service) | ⭐⭐ | Owner subscription only, see §7.5 |
| Git hosts | Bitbucket, GitHub, GitLab APIs | n/a | ⭐⭐ | Adapter per host |
| Inbox | CLI, web, Slack, Telegram | n/a | ⭐⭐ | CLI + web first |

**Comparable projects** (to learn from, not to compete with on day one): Kasm Workspaces (desktops + catalog), Coder (workspaces as code), E2B / Daytona (agent sandboxes), OpenHands (agent + sandbox). Shortdesk's distinct angle: **full developer desktop + any agent/model + charter-based governance + bring-your-own machines**.

---

## 11. Setup experience (target)

```bash
# Single machine (laptop / Mac mini)
curl -fsSL https://shortdesk.dev/install.sh | sh
shortdesk init            # creates ~/.shortdesk, asks for model keys, writes a starter charter
shortdesk up              # starts control plane + node + gateway locally
shortdesk desk open dev   # opens a desk in the browser
shortdesk task "fix the failing test in auth.ts" --repo ./my-app

# Add another machine to the fleet
shortdesk node join <join-token>
```

Goal: **from zero to an agent working at a desk in under 10 minutes** on a Mac or Linux machine with Docker installed.

---

## 12. Repo layout (planned)

```
shortdesk/
├── cmd/
│   ├── shortdesk/        # CLI (init, up, task, desk, node)
│   ├── controlplane/     # orchestrator + API
│   └── node/             # node agent
├── internal/             # charter parsing, policy, queue, git adapters, ...
├── catalog/              # app packages: chrome/, vscode/, claude-code/, codex/, ...
├── images/               # base desktop image(s)
├── charters/examples/    # example charters
├── schema/charter.json   # charter JSON Schema
├── web/                  # dashboard (Next.js)
├── deploy/               # docker-compose for control plane
└── docs/
```

---

## 13. Roadmap

| Phase | Deliverable | Done when |
|---|---|---|
| **0. Spec** | This doc + charter schema v0 | We agree on scope |
| **1. One desk** | Base image + catalog (Chrome, VS Code, Claude Code, Codex, OpenCode) | A desk opens in the browser on a Mac; Claude Code operates Chrome inside it |
| **2. One task** | `shortdesk task` runs headless: clone → agent → push → PR | A real bug becomes a real Bitbucket PR, with no human touching the desk |
| **3. Gateway** | LiteLLM in front of all models, charter-scoped keys + budgets | Switch models per task; spend is visible |
| **4. Governance** | Egress proxy, secret injection, OPA policies, approvals, audit log | A desk cannot reach non-allowlisted domains; every action is logged |
| **5. Queue + many desks** | Redis queue, orchestrator, locks, parallel desks | 10 tasks submitted → no collisions |
| **6. Fleet** | Node join, Tailscale, central control plane | A Mac mini and a cloud VM share one queue |
| **7. Cost routing** | FCC tiers + fallbacks driven by the charter; optional cloud offload | Small tasks run on cheap/free models automatically; hard ones on strong models |
| **8. Dashboard + inbox** | Web UI, Slack/Telegram | Non-technical people can submit and watch tasks |
| **9. Open source launch** | Docs, quickstart, examples, license | An outsider reaches a working desk in 10 minutes |

---

## 14. Non-goals (for now)

- macOS or Windows desktops (Linux desks only; macOS VMs via Tart may come later)
- Kubernetes (Docker on plain machines first)
- Agents merging to main branches without human approval
- Building our own model or agent. We orchestrate existing ones.

---

## 15. Open questions

1. **License:** Apache 2.0 (max adoption) or AGPL (protects against hosted clones, and matches FCC)? If Apache, FCC stays a separate, unmodified component.
2. **FCC + LiteLLM:** can FCC use the central LiteLLM gateway as its upstream? If not, do desks hold provider keys directly (weaker governance), or do we skip LiteLLM in v1?
3. **Claude in Chrome on Linux desks:** verify support; fall back to Playwright/DevTools MCP if needed.
4. **Docker inside desks:** are sidecar services enough, or do agents need full Docker (Sysbox)?
5. **Persistent vs fresh desks:** a fresh desk per task (clean, safe) or long-lived desks with logins (convenient)? Probably both, set per charter.
6. **Human takeover:** can a human jump into a running desk mid-task and hand it back?
7. **Name/domain:** check `shortdesk` on GitHub, domains (.dev/.sh/.io), Docker Hub, trademarks.
8. **Pollen integration:** what Pollen users can trigger, consent model for sending screenshots to a desk, and whether Pollen runs its own desks or uses a hosted Shortdesk (§16).

---

## 16. First use cases

1. **Developer tasks (Perto):** "fix bug X" → worker desk → Bitbucket PR → human review.
2. **Personal assistant:** research, bookings, admin via an assistant desk (§8.4).
3. **Pollen: the browsing engine behind screenshots.** [Pollen](https://joinpollen.app/): a user sends a screenshot, and Pollen figures out the actual source (where the product, recipe, place or video comes from and where to get it), then does price checking across sellers. Shortdesk fits in three ways:
   - **Powering what Pollen already does:** Pollen's backend sends Shortdesk a task ("find where this comes from + where to buy it + current prices"). The desk's agent uses **Chrome through the extension (Claude in Chrome)**: a real Chrome with real logged-in sessions that sees pages exactly as a person does. It returns structured results (source URL, sellers, prices, stock).
     - **Example, an Instagram screenshot:** the desk's Chrome is logged into an Instagram account. The agent finds the post or account from the screenshot (username, caption text, image), opens it, reads the caption, tagged products, shop link and link-in-bio, follows them to the seller, and returns the source + where to buy + price.
     - The same approach covers TikTok, Pinterest, X, YouTube, and any site that needs a login or only works in a real browser.
     - **Accounts:** use Pollen's existing platform accounts (never users' personal accounts). Each account is logged in once in a desk's persistent Chrome profile and stays logged in. Assign each account to one desk at a time, since one account active in several browsers at once looks suspicious. Browse at human pace, and use `ask_human` when a platform asks for a login check or 2FA. No coding involved: the desk only browses and reports back. These are short-lived desks with no user identity, shared across requests and scaled by the queue.
   - **Ongoing price tracking:** scheduled tasks re-check prices and alert the user on drops. Cheap tasks run in open mode (FCC + cheap models) to keep the cost per check very low.
   - **Acting on it (next step):** a "Do it" button: buy at the best price, book the restaurant, add the event to the calendar. This runs on a per-user assistant desk with `ask_human` links for payment and logins, and streams progress back into Pollen.
   - **Requirements:** a task API with structured (JSON) results, not only CLI/Telegram; high-volume, low-cost runs (pool of warm desks, cheap models); user consent before anything acts on their behalf; strict per-user isolation for assistant desks.
