"use client";

import { useRouter } from "next/navigation";
import { useState, useSyncExternalStore } from "react";
import { Logo } from "@/components/logo";
import { Button, Card, ErrorNote } from "@/components/ui";
import { api } from "@/lib/api";

const noSubscribe = () => () => {};

export default function LoginPage() {
  const router = useRouter();
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const expired = useSyncExternalStore(
    noSubscribe,
    () => new URLSearchParams(window.location.search).get("link") === "expired",
    () => false,
  );

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.login(token.trim());
      router.replace("/");
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <main className="flex flex-1 items-center justify-center px-4">
      <Card className="w-full max-w-sm p-6">
        <h1 className="text-lg">
          <Logo size={28} />
        </h1>
        {expired && (
          <p className="mt-3 rounded-md border border-border bg-background px-3 py-2 text-sm">
            That login link was already used or has expired. Log in with the token instead.
          </p>
        )}
        <p className="mt-3 text-sm text-muted">
          Paste the admin token. <code className="font-mono text-xs">shortdesk up</code> prints it in the terminal, and
          it&apos;s saved in <code className="font-mono text-xs">~/.shortdesk/admin-token</code>.
        </p>
        <form onSubmit={submit} className="mt-5 space-y-3">
          <input
            type="password"
            autoFocus
            autoComplete="current-password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Admin token"
            className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:border-accent"
          />
          {error && <ErrorNote message={error} />}
          <Button type="submit" variant="primary" className="w-full" disabled={busy || !token.trim()}>
            {busy ? "Logging in…" : "Log in"}
          </Button>
        </form>
      </Card>
    </main>
  );
}
