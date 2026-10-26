"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, type Desk } from "@/lib/api";
import { Logo } from "./logo";
import { AgentDot, Button } from "./ui";

export function Sidebar({ desks, selectedId }: { desks: Desk[] | undefined; selectedId: string }) {
  const router = useRouter();

  async function logout() {
    await api.logout().catch(() => {});
    router.replace("/login/");
  }

  return (
    <nav className="flex w-full shrink-0 flex-col border-b border-border bg-surface md:h-screen md:w-64 md:border-r md:border-b-0">
      <div className="flex items-center justify-between px-4 py-3">
        <Link href="/" aria-label="Shortdesk home">
          <Logo />
        </Link>
      </div>
      <div className="px-3">
        <Link
          href="/?new=1"
          className="flex w-full items-center justify-center gap-2 rounded-md border border-border px-3 py-2 text-sm font-medium hover:bg-background"
        >
          + New desk
        </Link>
      </div>

      <p className="px-4 pt-5 pb-1 text-xs font-medium text-muted">Desks</p>
      <ul className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-2">
        {desks === undefined && <li className="px-2 py-1.5 text-sm text-muted">Loading…</li>}
        {desks?.length === 0 && <li className="px-2 py-1.5 text-sm text-muted">No desks yet</li>}
        {desks?.map((d) => (
          <li key={d.id}>
            <Link
              href={`/?desk=${encodeURIComponent(d.id)}`}
              className={`flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm ${
                d.id === selectedId ? "bg-background font-medium" : "hover:bg-background"
              }`}
            >
              <AgentDot state={d.agent} />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{d.name}</span>
                <span className="block truncate text-xs text-muted">{d.charter}</span>
              </span>
            </Link>
          </li>
        ))}
      </ul>

      <div className="space-y-0.5 border-t border-border p-2">
        <Link
          href="/credentials/"
          className="flex w-full items-center rounded-md px-3 py-1.5 text-sm font-medium text-muted hover:bg-background hover:text-foreground"
        >
          Credentials
        </Link>
        <Link
          href="/workspace/"
          className="flex w-full items-center rounded-md px-3 py-1.5 text-sm font-medium text-muted hover:bg-background hover:text-foreground"
        >
          Workspace settings
        </Link>
        <Button variant="ghost" className="w-full justify-start" onClick={logout}>
          Log out
        </Button>
      </div>
    </nav>
  );
}
