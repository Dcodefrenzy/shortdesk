"use client";

import { useState } from "react";
import { api, type Desk } from "@/lib/api";
import { useAuthRedirect } from "@/lib/use-auth-redirect";
import { usePoll } from "@/lib/use-poll";
import { Button, Card, ErrorNote, Spinner } from "./ui";

// Charters are re-read every minute so files added on the server show up.
const CHARTERS_REFRESH_MS = 60000;

export function StartDesk({ title = "Start a desk", onStarted }: { title?: string; onStarted: (d: Desk) => void }) {
  const { data: charters, error: loadErr } = usePoll(api.charters, CHARTERS_REFRESH_MS);
  const loadError = useAuthRedirect(loadErr);
  const [picked, setPicked] = useState("");
  const [name, setName] = useState("");
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");

  const selected = picked || charters?.[0]?.name || "";
  const charter = charters?.find((c) => c.name === selected);

  async function start(e: React.FormEvent) {
    e.preventDefault();
    setStarting(true);
    setError("");
    try {
      const d = await api.startDesk(selected, name.trim() || undefined);
      setStarting(false);
      onStarted(d);
    } catch (err) {
      setError((err as Error).message);
      setStarting(false);
    }
  }

  const message = error || loadError;

  return (
    <Card className="p-4">
      <h2 className="font-semibold">{title}</h2>
      <p className="mt-1 text-sm text-muted">Pick a charter. It decides what the desk can reach and how long it lives.</p>

      {charters === undefined && !loadError && <p className="mt-4 text-sm text-muted">Loading charters…</p>}
      {charters?.length === 0 && (
        <p className="mt-4 text-sm text-muted">No charters found. Add YAML files to the server&apos;s charters folder.</p>
      )}

      {charters && charters.length > 0 && (
        <form onSubmit={start} className="mt-4 space-y-4">
          <fieldset className="space-y-2">
            <legend className="sr-only">Charter</legend>
            {charters.map((c) => (
              <label
                key={c.name}
                className={`block cursor-pointer rounded-md border p-3 text-sm ${
                  c.name === selected ? "border-accent" : "border-border"
                }`}
              >
                <input
                  type="radio"
                  name="charter"
                  value={c.name}
                  checked={c.name === selected}
                  onChange={() => setPicked(c.name)}
                  className="sr-only"
                />
                <span className="font-medium">{c.name}</span>
                {c.tenant && <span className="text-muted"> · {c.tenant}</span>}
                <span className="mt-0.5 block text-xs text-muted">
                  {c.cpu} CPU · {c.memory} · lives {c.lifetime}
                </span>
              </label>
            ))}
          </fieldset>

          {charter && (
            <div className="text-xs">
              <p className="font-medium">Can reach</p>
              {charter.allowedHosts.includes("*") ? (
                <p className="mt-1 text-warn">Any website (unrestricted)</p>
              ) : (
                <ul className="mt-1 flex flex-wrap gap-1">
                  {charter.allowedHosts.map((h) => (
                    <li key={h} className="rounded bg-background px-1.5 py-0.5 font-mono text-muted">
                      {h}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          <label className="block text-sm">
            <span className="font-medium">Name</span> <span className="text-muted">(optional)</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={`${selected}-xxxx`}
              pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
              title="Lowercase letters, digits and dashes"
              className="mt-1 w-full rounded-md border border-border bg-background px-2 py-1.5 font-mono text-sm outline-none focus:border-accent"
            />
          </label>

          <Button type="submit" variant="primary" className="w-full" disabled={starting || !selected}>
            {starting ? (
              <>
                <Spinner /> Starting… (about 15s)
              </>
            ) : (
              "Start desk"
            )}
          </Button>
        </form>
      )}
      {message && (
        <div className="mt-4">
          <ErrorNote message={message} />
        </div>
      )}
    </Card>
  );
}
