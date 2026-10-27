"use client";

import { useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { api, type Workspace } from "@/lib/api";
import { age } from "@/lib/format";
import { usePoll } from "@/lib/use-poll";
import { Connections, RouterSettings } from "../ai-settings";
import { SettingsPanel } from "../settings-panel";
import { ThemeSwitch } from "../theme-switch";
import { Button, Card, ErrorNote } from "../ui";
import { LoginCatalog } from "../vault-logins";
import { phoneApps, storeApps } from "./apps";

function Group({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="grid gap-2">
      <h3 className="px-1 pt-2 font-mono text-[11px] tracking-wider text-muted uppercase">{title}</h3>
      {hint && <p className="px-1 text-xs text-muted">{hint}</p>}
      {children}
    </section>
  );
}

// Vault: the logins agents can use, kept in Vaultwarden on this machine.
export function VaultApp() {
  const { data: vault, error } = usePoll(api.vault, 15000);
  const { data: logins } = usePoll(api.logins, 30000);
  return (
    <div className="grid gap-3">
      <p className="px-1 text-xs text-muted">
        Passwords are kept encrypted on this machine, in Vaultwarden (a self-hosted Bitwarden). When a site asks an agent
        to log in, Shortdesk fills the form on that site only. The agent never sees the password.
      </p>
      {error ? <ErrorNote message={(error as Error).message} /> : null}
      {vault && !vault.running && <ErrorNote message="The vault isn't running. Restart Shortdesk (Docker must be running)." />}
      {vault?.running && !vault.connected && (
        <ErrorNote message="The vault is starting up. If this stays, restart Shortdesk." />
      )}
      {vault?.connected && (
        <Group title="Passwords">
          <LoginCatalog />
        </Group>
      )}
      <Group title="Signed in on desks" hint="Saved when you or an agent log in on any desk, and given to every desk.">
        {logins === undefined ? (
          <p className="px-1 text-sm text-muted">Loading…</p>
        ) : logins.length === 0 ? (
          <p className="px-1 text-sm text-muted">Nothing yet. Log into a site on any desk and it shows up here.</p>
        ) : (
          <ul className="divide-y divide-border rounded-lg border border-border bg-surface">
            {logins.map((l) => (
              <li key={`${l.kind}/${l.name}`} className="px-3 py-2 text-sm">
                <span className="font-medium">{l.kind === "app" && l.name === "claude-code" ? "Claude Code" : l.name}</span>
                <span className="block text-xs text-muted">
                  {l.kind === "app" ? "App" : "Website"} · from {l.from} · {age(l.updatedAt)} ago
                </span>
              </li>
            ))}
          </ul>
        )}
      </Group>
    </div>
  );
}

// Settings: what applies to every desk, plus how Shortdesk looks.
export function SettingsApp({ ws, onChange, mainDesk }: { ws?: Workspace; onChange: () => void; mainDesk?: string }) {
  const router = useRouter();
  async function logout() {
    await api.logout().catch(() => {});
    router.replace("/login/");
  }
  return (
    <div className="grid gap-3">
      {ws ? (
        <>
          <Connections ws={ws} onChange={onChange} />
          <RouterSettings key={JSON.stringify(ws.router)} ws={ws} onChange={onChange} />
        </>
      ) : (
        <p className="px-1 text-sm text-muted">Loading…</p>
      )}
      {mainDesk ? (
        <SettingsPanel deskId={mainDesk} sections={["websites", "shared", "apps"]} />
      ) : (
        <p className="px-1 text-sm text-muted">Start your main desk; its websites, folders and software show up here.</p>
      )}
      <Card className="grid gap-3 p-4">
        <h2 className="font-semibold">Appearance</h2>
        <ThemeSwitch />
      </Card>
      <Card className="flex items-center justify-between gap-3 p-4">
        <h2 className="font-semibold">Account</h2>
        <Button variant="danger" onClick={logout}>
          Log out
        </Button>
      </Card>
    </div>
  );
}

// Store: what's on the phone now, what's coming, and what will be installable.
export function StoreApp() {
  const installed = phoneApps.filter((a) => a.ready && a.id !== "store");
  const soon = phoneApps.filter((a) => !a.ready);
  const row = (icon: string, name: string, about: string, right: ReactNode, tile = "bg-background") => (
    <li key={name} className="grid grid-cols-[40px_1fr_auto] items-center gap-3 px-3 py-2.5">
      <span className={`grid size-10 place-items-center rounded-[12px] text-xl ${tile}`}>{icon}</span>
      <span className="min-w-0 text-sm">
        <b className="block">{name}</b>
        <span className="block text-xs text-muted">{about}</span>
      </span>
      {right}
    </li>
  );
  const list = (children: ReactNode) => (
    <ul className="divide-y divide-border rounded-lg border border-border bg-surface">{children}</ul>
  );
  return (
    <div className="grid gap-3">
      <Group title="On your phone">
        {list(installed.map((a) => row(a.icon, a.name, a.about, <span className="text-xs text-ok">Installed</span>, a.tile)))}
      </Group>
      <Group title="Coming to every phone">
        {list(soon.map((a) => row(a.icon, a.name, a.about, <span className="text-xs text-muted">Soon</span>, a.tile)))}
      </Group>
      <Group title="In the Store" hint="Install the ones you want, or copy one someone else made and change it.">
        {list(storeApps.map((a) => row(a.icon, a.name, a.about, <Button disabled>Soon</Button>)))}
      </Group>
    </div>
  );
}
