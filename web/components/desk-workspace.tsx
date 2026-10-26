"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, type Desk } from "@/lib/api";
import { timeLeft } from "@/lib/format";
import { useDeskEvents } from "@/lib/use-desk-events";
import { useOutputs, useProjects } from "@/lib/use-outputs";
import { ArtifactsPanel, PROJECT } from "./artifacts-panel";
import { pending } from "./project-review";
import { ChatPanel } from "./chat-panel";
import { DesktopPanel } from "./desktop-panel";
import { SettingsPanel } from "./settings-panel";
import { AgentBadge, Button } from "./ui";

type Tab = "chat" | "desktop" | "settings";

// One desk: Chat (tasks and the agent's steps), Desktop (the live screen) and Settings.
// Chat and Desktop stay mounted so the screen connection and chat survive tab switches.
export function DeskWorkspace({ desk, onRemoved }: { desk: Desk; onRemoved: () => void }) {
  const router = useRouter();
  const { events, agent } = useDeskEvents(desk.id);
  const [tab, setTab] = useState<Tab>("chat");
  const [removing, setRemoving] = useState(false);
  const { data: outputs = [] } = useOutputs(desk.id, agent);
  const { data: projects = [], reload: reloadProjects } = useProjects(desk.id, agent);
  const toMerge = projects.filter(pending);
  const [artifactsOpen, setArtifactsOpen] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);

  function openArtifact(path: string | null) {
    setSelected(path);
    setArtifactsOpen(true);
    setTab("chat");
  }

  async function removeDesk() {
    if (!confirm(`Stop and remove desk "${desk.name}"? Anything on it will be lost.`)) return;
    setRemoving(true);
    try {
      await api.stopDesk(desk.id);
      onRemoved();
      router.replace("/");
    } catch (err) {
      alert((err as Error).message);
      setRemoving(false);
    }
  }

  return (
    <div className="flex h-screen min-w-0 flex-1 flex-col">
      <header className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border bg-surface px-4 py-2">
        <div className="min-w-0">
          <h1 className="truncate font-medium">{desk.name}</h1>
          <p className="truncate text-xs text-muted">
            {desk.charter} · removed in {timeLeft(desk.expiresAt)}
          </p>
        </div>
        <AgentBadge state={agent} />

        <div className="mx-auto flex rounded-lg border border-border p-0.5 text-sm" role="tablist">
          {(["chat", "desktop", "settings"] as const).map((t) => (
            <button
              key={t}
              role="tab"
              aria-selected={tab === t}
              onClick={() => setTab(t)}
              className={`rounded-md px-4 py-1 capitalize ${tab === t ? "bg-background font-medium" : "text-muted hover:text-foreground"}`}
            >
              {t}
            </button>
          ))}
        </div>

        <div className="flex gap-2">
          <Button
            variant={artifactsOpen ? "primary" : "secondary"}
            onClick={() => (artifactsOpen ? setArtifactsOpen(false) : openArtifact(null))}
          >
            Artifacts{outputs.length + toMerge.length > 0 && ` (${outputs.length + toMerge.length})`}
          </Button>
          <a
            href={desk.viewerUrl}
            target="_blank"
            rel="noopener"
            className="rounded-md px-3 py-1.5 text-sm text-muted hover:bg-background hover:text-foreground"
          >
            Pop out
          </a>
          <Button variant="danger" onClick={removeDesk} disabled={removing}>
            {removing ? "Removing…" : "Remove desk"}
          </Button>
        </div>
      </header>

      <div className="relative flex-1">
        <section className={`absolute inset-0 flex ${tab === "chat" ? "" : "invisible"}`} aria-hidden={tab !== "chat"}>
          <div className="min-w-0 flex-1">
            <ChatPanel
              deskId={desk.id}
              events={events}
              agent={agent}
              outputs={outputs}
              toMerge={toMerge}
              onOpenOutput={openArtifact}
              onOpenProject={(name) => openArtifact(PROJECT + name)}
              onTakeOver={() => setTab("desktop")}
            />
          </div>
          {artifactsOpen && (
            <div className="w-full max-w-[min(520px,50%)] shrink-0">
              <ArtifactsPanel
                deskId={desk.id}
                outputs={outputs}
                projects={projects}
                selected={selected}
                onSelect={setSelected}
                onClose={() => setArtifactsOpen(false)}
                onProjectsChanged={reloadProjects}
              />
            </div>
          )}
        </section>
        <section className={`absolute inset-0 ${tab === "desktop" ? "" : "invisible"}`} aria-hidden={tab !== "desktop"}>
          <DesktopPanel desk={desk} agent={agent} visible={tab === "desktop"} />
        </section>
        {tab === "settings" && (
          <section className="absolute inset-0">
            <SettingsPanel deskId={desk.id} />
          </section>
        )}
      </div>
    </div>
  );
}
