"use client";

// Logins agents can use: saved in the vault (Vaultwarden, a self-hosted Bitwarden on
// this machine). When a site asks an agent to log in, Shortdesk fills the form on that
// site only; the agent never sees the password.
import { useState } from "react";
import { api, type Integration } from "@/lib/api";
import { usePoll } from "@/lib/use-poll";
import { Button, Card, ErrorNote, Spinner } from "./ui";

// Common apps and sites. Any other website can be added too.
const catalog: { name: string; site: string }[] = [
  { name: "Google / Gmail", site: "google.com" },
  { name: "Instagram", site: "instagram.com" },
  { name: "Facebook", site: "facebook.com" },
  { name: "TikTok", site: "tiktok.com" },
  { name: "X (Twitter)", site: "x.com" },
  { name: "LinkedIn", site: "linkedin.com" },
  { name: "Pinterest", site: "pinterest.com" },
  { name: "GitHub", site: "github.com" },
  { name: "Bitbucket", site: "bitbucket.org" },
  { name: "Jira / Atlassian", site: "atlassian.com" },
  { name: "Slack", site: "slack.com" },
  { name: "Notion", site: "notion.so" },
  { name: "Microsoft / Outlook", site: "live.com" },
  { name: "Amazon", site: "amazon.com" },
  { name: "Shopify", site: "shopify.com" },
  { name: "Stripe", site: "stripe.com" },
];

export function LoginCatalog() {
  const { data: saved, error, reload } = usePoll(api.integrations, 30000);
  const [open, setOpen] = useState<string | null>(null);

  if (!saved) {
    return error ? <ErrorNote message={(error as Error).message} /> : <p className="text-sm text-muted">Loading…</p>;
  }
  const bySite = (site: string) => saved.filter((i) => i.site === site || i.site.endsWith("." + site));
  const custom = saved.filter((i) => !catalog.some((c) => i.site === c.site || i.site.endsWith("." + c.site)));

  return (
    <>
      <ul className="grid gap-2">
        {catalog.map((c) => (
          <li key={c.site}>
            <CredentialCard
              name={c.name}
              site={c.site}
              saved={bySite(c.site)}
              open={open === c.site}
              onOpen={() => setOpen(open === c.site ? null : c.site)}
              onChange={() => {
                setOpen(null);
                reload();
              }}
            />
          </li>
        ))}
        {custom.map((i) => (
          <li key={i.id}>
            <CredentialCard name={i.name} site={i.site} saved={[i]} open={false} onOpen={() => {}} onChange={reload} />
          </li>
        ))}
      </ul>
      <Card className="p-3">
        <h2 className="font-semibold">Another website</h2>
        <p className="mt-0.5 mb-3 text-sm text-muted">Any site not listed above.</p>
        <LoginForm onSaved={reload} askSite />
      </Card>
    </>
  );
}

function CredentialCard({
  name,
  site,
  saved,
  open,
  onOpen,
  onChange,
}: {
  name: string;
  site: string;
  saved: Integration[];
  open: boolean;
  onOpen: () => void;
  onChange: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function remove(i: Integration) {
    if (!confirm(`Remove the ${name} login (${i.username})? Agents will no longer be able to log in with it.`)) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteIntegration(i.id);
      onChange();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="h-full p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">{name}</p>
          <p className="font-mono text-xs text-muted">{site}</p>
        </div>
        {saved.length > 0 ? (
          <span className="text-xs text-ok">● Connected</span>
        ) : (
          <Button onClick={onOpen}>{open ? "Cancel" : "Add login"}</Button>
        )}
      </div>
      {saved.map((i) => (
        <div key={i.id} className="mt-3 flex items-center justify-between gap-2 text-sm">
          <span className="truncate text-muted">
            {i.username}
            {i.hasTotp ? " · 2FA" : ""}
          </span>
          <Button variant="ghost" disabled={busy} onClick={() => remove(i)}>
            Remove
          </Button>
        </div>
      ))}
      {open && saved.length === 0 && (
        <div className="mt-3">
          <LoginForm site={site} name={name} onSaved={onChange} />
        </div>
      )}
      {error && (
        <div className="mt-3">
          <ErrorNote message={error} />
        </div>
      )}
    </Card>
  );
}

export function LoginForm({
  site: fixedSite,
  name,
  askSite,
  onSaved,
}: {
  site?: string;
  name?: string;
  askSite?: boolean;
  onSaved: () => void;
}) {
  const [site, setSite] = useState(fixedSite ?? "");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.addIntegration({
        name: name ?? site.trim(),
        site: site.trim(),
        username: username.trim(),
        password,
        totpSecret: totp,
      });
      setSite(fixedSite ?? "");
      setUsername("");
      setPassword("");
      setTotp("");
      onSaved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const input = "w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm outline-none focus:border-accent";
  return (
    <form onSubmit={submit} className="space-y-2">
      {askSite && (
        <input className={`${input} font-mono`} value={site} onChange={(e) => setSite(e.target.value)} placeholder="example.com" />
      )}
      <input
        className={input}
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        placeholder="Username or email"
        autoComplete="off"
      />
      <input
        className={input}
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder="Password"
        autoComplete="new-password"
      />
      <input
        className={`${input} font-mono`}
        value={totp}
        onChange={(e) => setTotp(e.target.value)}
        placeholder="2FA setup key (optional)"
        autoComplete="off"
      />
      <Button type="submit" variant="primary" disabled={busy || !site.trim() || !username.trim() || !password}>
        {busy ? <Spinner /> : "Save"}
      </Button>
      {error && <ErrorNote message={error} />}
    </form>
  );
}
