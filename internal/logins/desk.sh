#!/usr/bin/env bash
# Runs inside a desk (sent by Shortdesk, not installed in the image). Finds, exports,
# imports and removes logins.
#
#   list                          one JSON line per login: {"kind","name","hash"}
#   export <kind> <name> <file>   pack one login into <file>
#   import <kind> <name> <file>   add a login exported from another desk
#   remove <kind> <name>          log this desk out of one login
#
# Kinds:
#   app   files in the home folder (see app_files)
#   site  a website's cookies in the desk's browser. These are read from and written to
#         the running browser through its local remote-control port (the same one the
#         agent uses), so they include cookies Chromium hasn't saved to disk yet, and
#         changes apply immediately without restarting the browser.
set -euo pipefail

home=/home/desk
history="$home/.config/shortdesk-browser/Default/History"

app_files() {
  case "$1" in
    claude-code) echo ".claude/.credentials.json .claude.json" ;;
    github-cli) echo ".config/gh/hosts.yml" ;;
    git) echo ".git-credentials" ;;
    *) return 1 ;;
  esac
}
APPS="claude-code github-cli git"

hash_of() { sha256sum | cut -c1-16; }

browser_running() { curl -fs -m 2 http://127.0.0.1:9222/json/version >/dev/null; }

# cdp <command> [site] [file]: talk to the browser (Node 20 needs the WebSocket flag).
cdp() {
  local js=/tmp/shortdesk-cdp.mjs
  cat >"$js" <<'JS'
const [cmd, site, file] = process.argv.slice(2);
const fs = await import("node:fs");
const version = await (await fetch("http://127.0.0.1:9222/json/version")).json();
const ws = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = fail; });
let id = 0;
const pending = new Map();
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
};
const call = (method, params = {}) => new Promise((ok, fail) => {
  const n = ++id;
  pending.set(n, (msg) => (msg.error ? fail(new Error(msg.error.message)) : ok(msg.result)));
  ws.send(JSON.stringify({ id: n, method, params }));
});
// A cookie belongs to a site if its domain is the site or a subdomain of it.
const ofSite = (c, s) => { const d = c.domain.replace(/^\./, ""); return d === s || d.endsWith("." + s); };
// The site a host belongs to: www.instagram.com -> instagram.com, x.bbc.co.uk -> bbc.co.uk.
const siteOf = (host) => {
  const p = host.replace(/^\./, "").split(".");
  if (p.length >= 3 && p.at(-1).length === 2 && /^(co|com|org|net|ac|gov|edu)$/.test(p.at(-2))) return p.slice(-3).join(".");
  return p.slice(-2).join(".");
};
// Only what Storage.setCookies accepts.
const param = (c) => {
  const out = { name: c.name, value: c.value, domain: c.domain, path: c.path, secure: c.secure,
    httpOnly: c.httpOnly, priority: c.priority, sourceScheme: c.sourceScheme, sourcePort: c.sourcePort };
  if (c.sameSite) out.sameSite = c.sameSite;
  if (!c.session) out.expires = c.expires;
  if (c.partitionKey) out.partitionKey = c.partitionKey;
  return out;
};
const { cookies } = await call("Storage.getCookies");
if (cmd === "sites") {
  for (const s of [...new Set(cookies.map((c) => siteOf(c.domain)))].sort()) console.log(s);
} else if (cmd === "export") {
  const mine = cookies.filter((c) => ofSite(c, site)).sort((a, b) => (a.domain + a.name + a.path).localeCompare(b.domain + b.name + b.path));
  console.log(JSON.stringify(mine.map(param)));
} else if (cmd === "import") {
  await call("Storage.setCookies", { cookies: JSON.parse(fs.readFileSync(file, "utf8")) });
} else if (cmd === "remove") {
  const gone = cookies.filter((c) => ofSite(c, site)).map((c) => ({ ...param(c), expires: 1 }));
  if (gone.length) await call("Storage.setCookies", { cookies: gone });
}
ws.close();
process.exit(0);
JS
  node --experimental-websocket --no-warnings "$js" "$@"
}

# Sites the browser actually visited (not ad/tracker domains that only set cookies):
# its saved history plus the tabs open now.
visited_sites() {
  {
    if [ -f "$history" ] && command -v sqlite3 >/dev/null; then
      sqlite3 "file:$history?mode=ro&immutable=1" "SELECT url FROM urls" 2>/dev/null || true
    fi
    curl -fs -m 2 http://127.0.0.1:9222/json/list | jq -r '.[].url' 2>/dev/null || true
  } | sed -nE 's#^https?://([^/:]+).*#\1#p' | awk -F. '{
      n = split($0, p, ".");
      if (n >= 3 && length(p[n]) == 2 && p[n-1] ~ /^(co|com|org|net|ac|gov|edu)$/) print p[n-2]"."p[n-1]"."p[n];
      else if (n >= 2) print p[n-1]"."p[n];
    }' | sort -u
}

list() {
  for app in $APPS; do
    read -r first _ <<<"$(app_files "$app")"
    if [ -s "$home/$first" ]; then
      printf '{"kind":"app","name":"%s","hash":"%s"}\n' "$app" "$(hash_of <"$home/$first")"
    fi
  done
  browser_running || return 0
  local visited
  visited=$(visited_sites)
  for site in $(cdp sites); do
    grep -qx "$site" <<<"$visited" || continue
    # Hash names and values only, so routine expiry refreshes don't count as changes.
    local digest
    digest=$(cdp export "$site" | jq -c '[.[] | {domain, name, path, value}]' | hash_of)
    printf '{"kind":"site","name":"%s","hash":"%s"}\n' "$site" "$digest"
  done
}

export_login() {
  local kind=$1 name=$2 out=$3
  case "$kind" in
    app)
      local files=()
      for f in $(app_files "$name"); do [ -e "$home/$f" ] && files+=("$f"); done
      tar -czf "$out" -C "$home" "${files[@]}"
      ;;
    site)
      browser_running || { echo "the desk's browser is not running" >&2; exit 1; }
      cdp export "$name" >"$out"
      ;;
    *) echo "unknown kind $kind" >&2; exit 2 ;;
  esac
}

import_login() {
  local kind=$1 name=$2 in=$3
  case "$kind" in
    app) tar -xzf "$in" -C "$home" ;;
    site)
      DISPLAY="${DISPLAY:-:0}" desk-browser-ready
      cdp import "$name" "$in"
      ;;
    *) echo "unknown kind $kind" >&2; exit 2 ;;
  esac
}

remove_login() {
  local kind=$1 name=$2
  case "$kind" in
    app) for f in $(app_files "$name"); do rm -f "$home/$f"; done ;;
    site)
      browser_running || return 0
      cdp remove "$name"
      ;;
    *) echo "unknown kind $kind" >&2; exit 2 ;;
  esac
}

case "${1:-}" in
  list) list ;;
  export) export_login "$2" "$3" "$4" ;;
  import) import_login "$2" "$3" "$4" ;;
  remove) remove_login "$2" "$3" ;;
  *) echo "usage: list | export|import <kind> <name> <file> | remove <kind> <name>" >&2; exit 2 ;;
esac
