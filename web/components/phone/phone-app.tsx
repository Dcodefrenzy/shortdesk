"use client";

import { useCallback } from "react";
import { api, type AgentState, type Desk, type Workspace } from "@/lib/api";
import { timeLeft } from "@/lib/format";
import { useOutputs, useProjects } from "@/lib/use-outputs";
import { usePoll } from "@/lib/use-poll";
import { ArtifactsPanel } from "../artifacts-panel";
import { soonApps, systemApps, type PhoneApp } from "./apps";
import { ComingSoon, HomeScreen, Phone, useMinute, type Notice } from "./phone";
import { ShortApp, type ShortAppInfo } from "./shortapp";
import { SettingsApp, StoreApp, VaultApp } from "./system-apps";

// What the phone needs from whatever shows it (the dashboard, or the phone on its own).
export type PhoneHost = {
  appId: string | null;
  open: (id: string | null) => void;
  watch: (deskId: string) => void; // show a desk's screen
  chat: () => void; // go to the chat with the agent
  fileSel: string | null;
  setFileSel: (p: string | null) => void;
  bare?: boolean;
};

async function loadShortApps(): Promise<ShortAppInfo[]> {
  const res = await fetch("/phone/api/apps", { credentials: "same-origin" });
  if (!res.ok) return [];
  return res.json();
}

const agentWords: Record<AgentState, string> = { idle: "ready", working: "working", paused: "you're in control" };

// The agent's phone: home screen, ShortApps from the Phone Desk, and system apps.
export function AgentPhone(host: PhoneHost) {
  const { data: desks } = usePoll(api.desks, 5000);
  const { data: ws, reload: reloadWs } = usePoll(api.workspace, 15000);
  const { data: shortApps = [] } = usePoll(loadShortApps, 10000);
  const main = desks?.find((d) => d.id === ws?.mainDesk);

  const apps: PhoneApp[] = [
    ...shortApps.map((a) => ({ id: a.id, name: a.name, icon: a.icon, tile: "bg-white", color: a.color, kind: "short" as const, about: a.description })),
    ...soonApps.filter((s) => !shortApps.some((a) => a.id === s.id)),
    ...systemApps,
  ];
  const app = apps.find((a) => a.id === host.appId);
  const notices = useNotices(ws, desks, main, host);

  let screen;
  if (!app) {
    screen = (
      <HomeScreen
        who={main ? `🤖 Your agent · ${agentWords[main.agent]}` : "🤖 Your agent · not started"}
        notices={notices}
        apps={apps}
        onOpen={(a) => host.open(a.id)}
      />
    );
  } else if (app.kind === "short") {
    screen = <ShortApp key={app.id} app={shortApps.find((a) => a.id === app.id)!} />;
  } else if (app.id === "vault") {
    screen = <VaultApp />;
  } else if (app.id === "settings") {
    screen = <SettingsApp ws={ws} onChange={reloadWs} mainDesk={main?.id} />;
  } else if (app.id === "store") {
    screen = <StoreApp apps={apps} />;
  } else if (app.id === "files") {
    screen = main ? (
      <FilesApp desk={main} selected={host.fileSel} onSelect={host.setFileSel} onClose={() => host.open(null)} />
    ) : (
      <p className="p-3 text-sm text-muted">Start your main desk first; what your agent saves shows up here.</p>
    );
  } else {
    screen = <ComingSoon app={app} />;
  }
  return (
    <Phone app={app} onHome={() => host.open(null)} bare={host.bare}>
      {screen}
    </Phone>
  );
}

function FilesApp({
  desk,
  selected,
  onSelect,
  onClose,
}: {
  desk: Desk;
  selected: string | null;
  onSelect: (p: string | null) => void;
  onClose: () => void;
}) {
  const { data: outputs = [] } = useOutputs(desk.id, desk.agent);
  const { data: projects = [], reload } = useProjects(desk.id, desk.agent);
  return (
    <div className="-m-3 h-[calc(100%+24px)]">
      <ArtifactsPanel
        deskId={desk.id}
        outputs={outputs}
        projects={projects}
        selected={selected}
        onSelect={onSelect}
        onClose={onClose}
        onProjectsChanged={reload}
      />
    </div>
  );
}

// What the home screen asks of you: getting started first, then what's happening now.
function useNotices(ws: Workspace | undefined, desks: Desk[] | undefined, main: Desk | undefined, host: PhoneHost): Notice[] {
  const { data: logins } = usePoll(api.logins, 30000);
  const { data: vault } = usePoll(api.vault, 30000);
  const connected = !!vault?.connected;
  const load = useCallback(() => (connected ? api.integrations() : Promise.resolve(undefined)), [connected]);
  const { data: saved } = usePoll(load, 30000);
  const minute = useMinute();
  if (!ws || !desks) return [];
  const out: Notice[] = [];
  const aiReady = ws.connections.some((c) => c.set) || !!logins?.some((l) => l.kind === "app" && l.name === "claude-code");
  if (!aiReady)
    out.push({ icon: "🧠", title: "Connect an AI", text: "Claude, or your own model keys", action: { label: "Set up", onClick: () => host.open("settings") } });
  if (!main) out.push({ icon: "🖥", title: "Start your main desk", text: "Your agent's computer", action: { label: "Start", onClick: host.chat } });
  if (main?.agent === "working")
    out.push({ icon: "⚙️", title: "Your agent is working", text: main.name, action: { label: "Watch", onClick: () => host.watch(main.id) } });
  if (main?.agent === "paused")
    out.push({ icon: "✋", title: "You're in control of the desk", text: "The agent waits for you", action: { label: "Open", onClick: () => host.watch(main.id) } });
  if (connected && saved && saved.length === 0)
    out.push({ icon: "🔐", title: "Add your first login", text: "So your agent can sign in to sites", action: { label: "Vault", onClick: () => host.open("vault") } });
  if (main && main.tasks === 0 && main.agent === "idle")
    out.push({ icon: "💬", title: "Give your agent a job", text: "Type it in the chat", action: { label: "Chat", onClick: host.chat } });
  if (main?.expiresAt && minute > 0 && new Date(main.expiresAt).getTime() - minute * 60_000 < 15 * 60_000)
    out.push({ icon: "⏳", title: "Main desk ends soon", text: `Removed in ${timeLeft(main.expiresAt)}` });
  return out.slice(0, 3);
}
