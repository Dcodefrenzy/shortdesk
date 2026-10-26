<p><img src="web/public/logo.svg" alt="Shortdesk" height="40"></p>

# Shortdesk

Self-hosted sandboxes for AI agents. Each agent gets its own Linux desktop with a browser, terminal and Claude Code, and you watch, approve and control everything it does.

- **Desks:** full Linux desktops (XFCE, Chromium, Firefox, Claude Code) in isolated sandboxes, viewed live in your browser. Take over any time, then hand back.
- **Dashboard:** chat with each desk, watch it work, review its files, tables and project changes.
- **Charters:** YAML rules for what a desk may do: which websites it can reach, resources, lifetime. Agents ask; you approve.
- **Logins and credentials:** log in once and every desk is logged in; revoke per desk. Passwords live in a private vault that Shortdesk runs for you.
- **Your models:** your Claude subscription first, then your own keys (DeepSeek, Gemini, Groq, NVIDIA) when it runs out.
- **Projects:** each desk works on its own git branch; you review and merge locally. Nothing is pushed anywhere unless you choose.

Everything runs on your machine. See [VISION.md](VISION.md) for the design and [docs/TERMINOLOGY.md](docs/TERMINOLOGY.md) for the terms.

> Status: early, but it works end to end. Expect rough edges.

## Install

You need **Docker**: [Docker Desktop](https://docs.docker.com/get-docker/) on Mac or Windows, Docker Engine on Linux. Start it, then:

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/Dcodefrenzy/shortdesk/main/scripts/install.sh | sh
shortdesk up
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/Dcodefrenzy/shortdesk/main/scripts/install.ps1 | iex
shortdesk up
```

`shortdesk up` downloads the desk images the first time (a few GB, once), starts everything and opens the dashboard. Log in with the token it prints, then create a desk. Stop with Ctrl+C; run `shortdesk up` again any time to carry on where you left off. Your settings, chats and logins live in `~/.shortdesk`.

Prefer to download by hand? Every [release](https://github.com/Dcodefrenzy/shortdesk/releases) has the binary for each system.

### First steps

1. **Workspace → Connections:** sign in to Claude in a desk, or add your own model keys (DeepSeek, Gemini, Groq, NVIDIA).
2. **New desk** in the sidebar, pick a charter (`assistant` is restricted to an allowlist of websites; `open` can go anywhere).
3. Type a task in the chat. Watch it on the **Desktop** tab; results appear under **Artifacts**.

## Build from source

Needs Go and Node 22.

```bash
make all            # dashboard + binary in bin/shortdesk
./bin/shortdesk up  # builds the desk images locally the first time (20+ minutes)
make check          # vet and tests
```

Releases are published by pushing a version tag (`git tag v0.1.0 && git push origin v0.1.0`); [the release workflow](.github/workflows/release.yml) builds the images and binaries.

## Commands

| Command | What it does |
|---|---|
| `shortdesk up` | Start everything and open the dashboard |
| `shortdesk down` | Stop the background services (desks, files and logins are kept) |
| `shortdesk desk up -c <charter> [--name n] [--open]` | Start a desk and wait until its screen is ready |
| `shortdesk desk list` | List desks with charter, tenant, state and expiry |
| `shortdesk desk open [desk]` | Open the desk's screen in the browser |
| `shortdesk desk url [desk]` | Print the screen link |
| `shortdesk desk shell [desk]` | Open the desk's terminal in yours |
| `shortdesk desk paste [desk] [--text t]` | Send your clipboard (or text) to the desk |
| `shortdesk desk copy [desk]` | Copy the desk's clipboard to yours |
| `shortdesk desk down [desk]` | Remove a desk |

`[desk]` is a name or ID, and can be left out when only one desk is running. In the browser view, clipboard sync with your computer is automatic in Chrome, Edge and Brave.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `SHORTDESK_RUNTIME` | `opensandbox` | `opensandbox` or `docker` |
| `SHORTDESK_OPENSANDBOX_URL` | `http://127.0.0.1:8090` | OpenSandbox lifecycle API |
| `SHORTDESK_OPENSANDBOX_API_KEY` | | OpenSandbox API key |
| `SHORTDESK_OPENSANDBOX_LOCAL_DOCKER` | `true` | Server runs on this machine (enables `desk shell`) |

The Docker runtime refuses charters with a network allowlist, because it cannot enforce one.

## Charters

```yaml
charter: assistant
tenant: personal
desk:
  image: shortdesk/desk:dev
  resources: {cpu: "2", memory: 4Gi}
  env: {TZ: Europe/London}
permissions:
  network:
    allow: [claude.ai, "*.claude.ai", "*.anthropic.com"]   # or ["*"] for unrestricted
limits:
  desk_lifetime: 2h
```

Unknown fields are rejected, so a misspelled or unsupported rule fails loudly instead of being ignored.

## Layout

```
cmd/shortdesk/          CLI entry point
internal/charter/       charter parsing and validation
internal/runtime/       box interface + opensandbox/ and docker/ implementations
internal/desk/          charter → desk, viewer links, clipboard
internal/cli/           commands
internal/config/        environment → runtime
images/desk/            the desk image
charters/examples/      example charters
spike/                  early experiments and their results
```

## Development

```bash
make check   # gofmt, go vet, tests
```

## License

Apache 2.0
