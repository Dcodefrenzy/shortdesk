"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Logo } from "@/components/logo";
import { Button, Card, ErrorNote } from "@/components/ui";
import { api } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

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
        <p className="mt-1 text-sm text-muted">
          Enter the admin token. <code className="font-mono text-xs">shortdesk serve</code> prints where it is kept.
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
