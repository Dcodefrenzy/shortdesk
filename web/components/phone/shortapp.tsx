"use client";

import { useEffect, useRef, useState } from "react";
import { ErrorNote, Spinner } from "../ui";

// A ShortApp from the Phone Desk. Its page runs walled off (no network, no cookies) and
// asks this frame for its data through shortapps.js; every call is checked here and
// sent to the Phone Desk for this app only.
export type ShortAppInfo = {
  id: string;
  name: string;
  icon: string;
  color?: string;
  description: string;
  version: string;
  collections: string[];
  builtin: boolean;
  copiedFrom?: string;
};

const POLL_MS = 4000;
const themeVars = ["--background", "--surface", "--foreground", "--muted", "--border", "--accent", "--accent-foreground", "--ok", "--warn", "--danger"];

function theme() {
  const cs = getComputedStyle(document.documentElement);
  const vars: Record<string, string> = {};
  for (const v of themeVars) vars[v] = cs.getPropertyValue(v).trim();
  return { vars, dark: cs.colorScheme.includes("dark") };
}

async function call(path: string, init?: RequestInit) {
  const res = await fetch(`/phone/api/apps/${path}`, { credentials: "same-origin", ...init,
    headers: init?.body ? { "Content-Type": "application/json" } : undefined });
  const body = res.status === 204 ? null : await res.json().catch(() => null);
  if (!res.ok) throw new Error(body?.error ?? `Request failed (${res.status})`);
  return body;
}

const enc = encodeURIComponent;

// handle runs one call from an app's page against that app's own data.
async function handle(app: ShortAppInfo, method: string, args: unknown[]) {
  const [coll, a, b, c] = args as [string, unknown, unknown, unknown];
  const base = `${enc(app.id)}/data/${enc(coll)}`;
  switch (method) {
    case "list":
      return call(`${base}/query`, { method: "POST", body: JSON.stringify(a ?? {}) });
    case "get":
      return call(`${base}/${enc(String(a))}`);
    case "add":
      return call(base, { method: "POST", body: JSON.stringify({ data: a, id: b || "" }) });
    case "update":
      return call(`${base}/${enc(String(a))}`, { method: "PATCH", body: JSON.stringify({ data: b, version: Number(c) || 0 }) });
    case "remove":
      return call(`${base}/${enc(String(a))}?version=${Number(b) || 0}`, { method: "DELETE" });
    case "history":
      return call(`${enc(app.id)}/history?coll=${enc(coll || "")}&id=${enc(String(a || ""))}&limit=100`);
  }
  throw new Error(`unknown call ${method}`);
}

export function ShortApp({ app }: { app: ShortAppInfo }) {
  const frame = useRef<HTMLIFrameElement>(null);
  const [src, setSrc] = useState<string>();
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    call(`${enc(app.id)}/open`, { method: "POST" }).then(
      (o: { path: string }) => live && setSrc(`/phone/${o.path}`),
      (e: Error) => live && setError(e.message),
    );
    return () => {
      live = false;
    };
  }, [app.id]);

  useEffect(() => {
    if (!src) return;
    const win = () => frame.current?.contentWindow;
    const post = (m: object) => win()?.postMessage({ shortapps: 1, ...m }, "*");
    const onMessage = async (e: MessageEvent) => {
      if (e.source !== win() || e.data?.shortapps !== 1) return;
      const { id, method, args } = e.data as { id?: number; method: string; args?: unknown[] };
      if (method === "hello") {
        post({ event: "init", app, theme: theme() });
        return;
      }
      try {
        post({ id, result: await handle(app, method, args ?? []) });
        seen.current = ""; // our own change: the next poll refreshes other views, not this one
      } catch (err) {
        post({ id, error: (err as Error).message });
      }
    };
    window.addEventListener("message", onMessage);

    // Tell the page when someone else (the agent, another tab) changed its data.
    const seen = { current: "" };
    const poll = setInterval(async () => {
      try {
        const h = (await call(`${enc(app.id)}/history?limit=1`)) as { seq: number }[];
        const latest = String(h[0]?.seq ?? 0);
        if (seen.current && latest !== seen.current) post({ event: "change" });
        seen.current = latest;
      } catch {}
    }, POLL_MS);

    // Follow light/dark changes.
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const onTheme = () => post({ event: "theme", theme: theme() });
    mq.addEventListener("change", onTheme);
    const mo = new MutationObserver(onTheme);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    return () => {
      window.removeEventListener("message", onMessage);
      clearInterval(poll);
      mq.removeEventListener("change", onTheme);
      mo.disconnect();
    };
  }, [src, app]);

  if (error) return <div className="p-3"><ErrorNote message={error} /></div>;
  if (!src)
    return (
      <p className="flex items-center gap-2 p-4 text-sm text-muted">
        <Spinner /> Opening {app.name}…
      </p>
    );
  return (
    <iframe
      ref={frame}
      src={src}
      title={app.name}
      sandbox="allow-scripts allow-forms allow-popups allow-modals"
      className="-m-3 block h-[calc(100%+24px)] w-[calc(100%+24px)] border-0 bg-background"
    />
  );
}
