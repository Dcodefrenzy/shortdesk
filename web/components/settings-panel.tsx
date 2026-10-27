"use client";

import { useCallback, useState } from "react";
import { api, type DeskLogin, type NetworkRule } from "@/lib/api";
import { age, timeLeft } from "@/lib/format";
import { usePoll } from "@/lib/use-poll";
import { Button, Card, ErrorNote, Spinner } from "./ui";

const REFRESH_MS = 10000;

const sourceLabel: Record<NetworkRule["source"], string> = {
  charter: "from charter",
  settings: "added here",
  chat: "approved in chat",
  models: "for your models",
};

export type SettingsSection = "logins" | "shared" | "desk" | "websites" | "apps" | "integrations";

// A desk's settings. With sections, only those are shown, without the page frame (the
// phone's Settings app shows a few of them).
export function SettingsPanel({ deskId, sections }: { deskId: string; sections?: SettingsSection[] }) {
  const show = (x: SettingsSection) => !sections || sections.includes(x);
  const load = useCallback(() => api.settings(deskId), [deskId]);
  const { data: s, error, reload } = usePoll(load, REFRESH_MS);
  const [host, setHost] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");

  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setActionError("");
    try {
      await fn();
      await reload();
    } catch (err) {
      setActionError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  if (!s) {
    return (
      <div className="p-6 text-sm text-muted">
        {error ? <ErrorNote message={(error as Error).message} /> : "Loading settings…"}
      </div>
    );
  }

  const network = { ...s.network, hosts: s.network.hosts ?? [] };
  const logins = s.logins ?? [];
  const shared = s.shared ?? [];
  const apps = s.apps ?? [];
  const integrations = s.integrations ?? [];
  return (
    <div className={sections ? "" : "h-full overflow-y-auto"}>
      <div className={sections ? "space-y-3" : "mx-auto max-w-3xl space-y-6 px-4 py-6"}>
        {actionError && <ErrorNote message={actionError} />}

        {show("logins") && <Section
          title="Logins"
          hint="Saved automatically when you (or the agent) log in on any desk, and given to every desk. Revoke one to log this desk out of it."
        >
          {logins.length === 0 ? (
            <p className="text-sm text-muted">
              No logins yet. Log into a website or app on any desk; it shows up here within a minute.
            </p>
          ) : (
            <ul className="divide-y divide-border rounded-md border border-border">
              {logins.map((l) => (
                <li key={`${l.kind}/${l.name}`} className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
                  <span className="min-w-0 flex-1">
                    <span className="font-medium">{loginLabel(l)}</span>
                    <span className="block text-xs text-muted">
                      {l.kind === "app" ? "App" : "Website"} · from {l.from} · {ago(l.updatedAt)}
                    </span>
                  </span>
                  <span className={`text-xs ${loginTone[l.status]}`}>{loginStatus[l.status]}</span>
                  {l.status === "revoked" ? (
                    <Button disabled={busy} onClick={() => run(() => api.changeLogin(deskId, l, "allow"))}>
                      Allow
                    </Button>
                  ) : (
                    <Button variant="danger" disabled={busy} onClick={() => run(() => api.changeLogin(deskId, l, "revoke"))}>
                      Revoke
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </Section>}

        {show("shared") && <Section
          title="Shared folders"
          hint="The same files on every desk, kept in Shortdesk's own storage. New desks start with all of them."
        >
          {shared.length === 0 ? (
            <p className="text-sm text-muted">No shared folders.</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {shared.map((f) => (
                <li key={f.path} className="font-mono">
                  {f.path.replace("/home/desk", "~")}
                </li>
              ))}
            </ul>
          )}
          <p className="mt-2 text-xs text-muted">Desks created before shared folders existed don&apos;t have them.</p>
        </Section>}

        {show("desk") && <Section title="Desk">
          <Grid
            rows={[
              ["Charter", s.charter ? `${s.charter.name}${s.charter.version ? ` (v${s.charter.version})` : ""}` : s.desk.charter],
              ["Tenant", s.desk.tenant ?? "—"],
              ["Image", s.charter?.image ?? "—"],
              ["Resources", s.charter ? `${s.charter.cpu} CPU · ${s.charter.memory} memory` : "—"],
              ["Lifetime", s.charter ? `${s.charter.lifetime} · removed in ${timeLeft(s.desk.expiresAt)}` : timeLeft(s.desk.expiresAt)],
              ["State", s.desk.state],
            ]}
          />
        </Section>}

        {show("websites") && <Section
          title="Websites"
          hint="The desk can only reach these. The agent can ask for more; you approve in the chat."
        >
          {network.unrestricted ? (
            <p className="text-sm text-warn">This charter allows every website.</p>
          ) : (
            <>
              {network.editable && (
                <label className="mb-3 flex cursor-pointer items-start gap-3 rounded-md border border-border p-3">
                  <input
                    type="checkbox"
                    role="switch"
                    className="mt-0.5 size-4 accent-[var(--color-accent)]"
                    checked={network.allowAll}
                    disabled={busy}
                    onChange={(e) => {
                      const on = e.target.checked;
                      run(() => api.setAllowAll(deskId, on));
                    }}
                  />
                  <span className="text-sm">
                    <span className="font-medium">Allow all websites</span>
                    <span className="block text-xs text-muted">
                      {network.allowAll
                        ? "This desk can reach any website. Turn off to go back to the list below."
                        : "Let this desk reach any website for now. The list below is kept for when you turn it off."}
                    </span>
                  </span>
                </label>
              )}
              <ul
                className={`divide-y divide-border rounded-md border border-border ${network.allowAll ? "opacity-50" : ""}`}
              >
                {network.hosts.map((r) => (
                  <li key={r.host} className="flex items-center gap-3 px-3 py-2 text-sm">
                    <span className="flex-1 font-mono">{r.host}</span>
                    <span className="text-xs text-muted">{sourceLabel[r.source]}</span>
                    {(r.source === "settings" || r.source === "chat") && network.editable && (
                      <Button variant="ghost" disabled={busy} onClick={() => run(() => api.removeHost(deskId, r.host))}>
                        Remove
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
              {network.editable ? (
                <form
                  className="mt-3 flex flex-wrap gap-2"
                  onSubmit={(e) => {
                    e.preventDefault();
                    const h = host.trim();
                    if (h) run(() => api.allowHost(deskId, h).then(() => setHost("")));
                  }}
                >
                  <input
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                    placeholder="booking.com or *.booking.com"
                    className="min-w-0 flex-1 rounded-md border border-border bg-background px-3 py-1.5 font-mono text-sm outline-none focus:border-accent"
                  />
                  <Button type="submit" variant="primary" disabled={busy || !host.trim()}>
                    {busy ? <Spinner /> : "Allow on this desk"}
                  </Button>
                </form>
              ) : (
                <p className="mt-2 text-xs text-muted">This desk&apos;s runtime can&apos;t change website rules while running.</p>
              )}
              <p className="mt-2 text-xs text-muted">
                Changes apply immediately, to this desk only. To change every desk, edit the charter.
              </p>
            </>
          )}
        </Section>}

        {show("apps") && <Section title="Desk software" hint="What's installed on the desk. More comes with the Store.">
          <ul className="grid gap-2">
            {apps.map((a) => (
              <li
                key={a.name}
                className={`rounded-md border border-border px-3 py-2 text-sm ${a.installed ? "" : "opacity-60"}`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{a.name}</span>
                  <span className={`text-xs ${a.installed ? "text-ok" : "text-muted"}`}>
                    {a.installed ? "Installed" : "Coming soon"}
                  </span>
                </div>
                <p className="text-xs text-muted">{a.description}</p>
              </li>
            ))}
          </ul>
        </Section>}

        {show("integrations") && <Section title="Integrations" hint="Accounts and connections (Slack, Bitbucket, Instagram…).">
          {integrations.length === 0 ? (
            <p className="text-sm text-muted">None connected yet. Integrations arrive with the app catalog and vault.</p>
          ) : (
            <ul className="text-sm">{integrations.map((i) => <li key={i}>{i}</li>)}</ul>
          )}
        </Section>}
      </div>
    </div>
  );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <Card className="p-4">
      <h2 className="font-semibold">{title}</h2>
      {hint && <p className="mt-0.5 mb-3 text-sm text-muted">{hint}</p>}
      {!hint && <div className="mb-3" />}
      {children}
    </Card>
  );
}

function Grid({ rows }: { rows: [string, string][] }) {
  return (
    <dl className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-1.5 text-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted">{k}</dt>
          <dd className="font-mono text-xs leading-5">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function ago(iso?: string) {
  return iso ? `${age(iso)} ago` : "";
}

const appNames: Record<string, string> = { "claude-code": "Claude Code", "github-cli": "GitHub CLI", git: "Git" };

function loginLabel(l: DeskLogin) {
  return l.kind === "app" ? (appNames[l.name] ?? l.name) : l.name;
}

const loginStatus: Record<DeskLogin["status"], string> = {
  active: "● Logged in here",
  revoked: "Revoked on this desk",
  missing: "Not on this desk yet",
};

const loginTone: Record<DeskLogin["status"], string> = {
  active: "text-ok",
  revoked: "text-danger",
  missing: "text-muted",
};
