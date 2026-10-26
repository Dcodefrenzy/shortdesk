<p><img src="web/public/logo.svg" alt="Shortdesk" height="40"></p>

# Shortdesk

Desktops for AI agents, governed by charters.

A **desk** is a full Linux desktop (XFCE, Chromium, Claude Code) that runs in an isolated box and is viewed in the browser. A **charter** is a YAML file that says what a desk is and what it may do: which image, how much CPU and memory, which websites it can reach, how long it lives.

Shortdesk decides what desks exist and what happens in them. The boxes themselves come from a **runtime**: [OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) (default, enforces network rules) or plain Docker (development only).

See [VISION.md](VISION.md) for the full design and [docs/TERMINOLOGY.md](docs/TERMINOLOGY.md) for what each term means.

> Status: early. Milestone 1 (desks from charters) works; tasks, the dashboard and the vault are next.

## Quick start (macOS or Linux with Docker)

```bash
# 1. Build the desk image and the CLI
make image build

# 2. Start an OpenSandbox server (see spike/opensandbox/ for a local config)
uvx opensandbox-server --config spike/opensandbox/sandbox.toml

# 3. Start a desk
export SHORTDESK_OPENSANDBOX_API_KEY=...   # the server's api_key
./bin/shortdesk desk up --charter charters/examples/assistant.yaml --open
```

## Commands

| Command | What it does |
|---|---|
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
tenant: ayo
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
