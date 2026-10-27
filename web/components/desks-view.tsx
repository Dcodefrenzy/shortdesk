"use client";

import { useState } from "react";
import { api, type Desk } from "@/lib/api";
import { timeLeft } from "@/lib/format";
import { DesktopPanel } from "./desktop-panel";
import { StartDesk } from "./start-desk";
import { AgentDot, Button, ErrorNote } from "./ui";

// The Desktop tab: pick a desk, watch its screen, take over. The main desk is the one
// you chat with; the others are extra desks.
export function DesksView({
  desks,
  mainId,
  selectedId,
  onSelect,
  onChanged,
}: {
  desks: Desk[];
  mainId: string;
  selectedId: string;
  onSelect: (id: string) => void;
  onChanged: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const desk = desks.find((d) => d.id === selectedId) ?? desks.find((d) => d.id === mainId) ?? desks[0];

  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      onChanged();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-2" role="tablist" aria-label="Desks">
        {desks.map((d) => (
          <button
            key={d.id}
            role="tab"
            aria-selected={d.id === desk?.id}
            onClick={() => {
              setAdding(false);
              onSelect(d.id);
            }}
            className={`inline-flex items-center gap-2 rounded-full border px-3 py-1 text-sm ${
              d.id === desk?.id && !adding ? "border-accent bg-accent text-accent-foreground" : "border-border hover:border-accent"
            }`}
          >
            <AgentDot state={d.agent} />
            {d.name}
            {d.id === mainId && <span className="text-[10px] font-semibold uppercase opacity-80">main</span>}
          </button>
        ))}
        <button
          onClick={() => setAdding(!adding)}
          className={`rounded-full border border-dashed px-3 py-1 text-sm ${adding ? "border-accent text-accent" : "border-border text-muted hover:text-foreground"}`}
        >
          + New desk
        </button>
      </div>
      {error && <ErrorNote message={error} />}

      {adding ? (
        <div className="max-w-md">
          <StartDesk
            onStarted={(d) => {
              setAdding(false);
              onChanged();
              onSelect(d.id);
            }}
          />
        </div>
      ) : desk ? (
        <>
          <div className="min-h-[320px] flex-1 overflow-hidden rounded-lg border border-border">
            <DesktopPanel key={desk.id} desk={desk} agent={desk.agent} visible />
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
            <span className="text-muted">
              <b className="text-foreground">{desk.name}</b> · {desk.charter} · removed in {timeLeft(desk.expiresAt)}
              {desk.id === mainId ? " · you chat with this desk" : ""}
            </span>
            <span className="flex-1" />
            <a href={desk.viewerUrl} target="_blank" rel="noopener" className="text-muted underline hover:text-foreground">
              Pop out
            </a>
            {desk.id !== mainId && (
              <Button disabled={busy} onClick={() => run(() => api.setMainDesk(desk.id))}>
                Make main desk
              </Button>
            )}
            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                if (confirm(`Stop and remove desk "${desk.name}"? Anything on it will be lost.`)) run(() => api.stopDesk(desk.id));
              }}
            >
              Remove desk
            </Button>
          </div>
        </>
      ) : (
        <p className="text-sm text-muted">No desks yet.</p>
      )}
    </div>
  );
}
