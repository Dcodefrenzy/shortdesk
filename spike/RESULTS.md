# Spike results

| # | Test | Result | Notes |
|---|---|---|---|
| 1 | Desk image (Debian XFCE + Chromium + Node + Claude Code + Playwright MCP) in the browser | ✅ Pass | http://localhost:3300 · image 4.4 GB · arm64 |
| 1a | Links open in desk Chromium | ✅ Fixed | Default browser pointed at `wrapped-chromium`; plain `chromium` dies with "No usable sandbox!" |
| 1b | Copy/paste Mac ↔ desk | ✅ Pass | `spike/bin/desk paste|copy|type|shell` |
| 2 | Claude Code logged in inside desk + Playwright MCP connected | ✅ Pass | Login via desk Chromium |
| 2b | Claude Code drives Chromium on a real task | ⏸ Blocked | Claude weekly limit reached (resets Oct 29) → retest, or run via FCC/open mode |
| 7 | OpenSandbox runs our desk image | ✅ Pass (with `images/desk`) — webtop failed: | Server, API, egress sidecar and endpoints work. Desk exited: OpenSandbox injects its own `execd` bootstrap as PID 1, but webtop's s6-overlay `/init` must be PID 1 (`s6-overlay-suexec: fatal: can only run as pid 1`). Fix: our own desk image without s6 (simple supervisor start script) |
| 3–6 | Streaming, real task, open mode, handoff | ⏳ Todo | Needs Claude limit reset (Oct 29) or an OpenRouter/Groq key |

## Findings
- **Bot checks:** desk Chromium triggered ~3 human checks. Likely causes: fresh profile, `Linux aarch64` user agent, no GPU (software WebGL), container flags. Expect it to be worse on cloud/datacenter IPs. Mitigations: warm persistent profiles, office machines as nodes, Camoufox or real-Chrome mode for strict sites.
- **Account connectors leak into desks:** logging into Claude in a desk brought in the owner's claude.ai connectors (Gmail). Charters must control which connectors a desk gets.
- **Sandbox UX:** clipboard, link opening and logins inside Linux are awkward. The product needs a clipboard bridge, vault-injected logins (`claude setup-token`) and `shortdesk desk shell`.
- Port 3000 is often taken on dev machines; Selkies uses 8082 inside the desk (clashes with a common FCC port).

- **OpenSandbox findings:** API key auth header `OPEN-SANDBOX-API-KEY`; create = `POST /v1/sandboxes` with `image`, `entrypoint`, `env`, `resourceLimits`, `networkPolicy` (default deny + allowlist), `credentialProxy`, `volumes`; desktop reachable via `GET /v1/sandboxes/{id}/endpoints/{port}`. Hardening defaults: drops NET_ADMIN/SYS_ADMIN/etc., `no-new-privileges`. Our desk image must run as a normal child process under its bootstrap.
- **Own desk image (`images/desk`)**: Debian trixie + XFCE + x11vnc + noVNC (:6080) + Chromium + Claude Code + Playwright MCP, self-starting via `start-desk`, non-root `desk` user. 2.17 GB (webtop was 4.4 GB). Runs under plain Docker **and** OpenSandbox. Egress allowlist verified: example.com 200, google.com and api.anthropic.com blocked.
