"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useState } from "react";
import { ArtifactsPanel, PROJECT } from "@/components/artifacts-panel";
import { ChatPanel } from "@/components/chat-panel";
import { DesksView } from "@/components/desks-view";
import { LogoMark } from "@/components/logo";
import { appById, type AppId } from "@/components/phone/apps";
import { ComingSoon, HomeScreen, Phone, useMinute, type Notice } from "@/components/phone/phone";
import { SettingsApp, StoreApp, VaultApp } from "@/components/phone/system-apps";
import { pending } from "@/components/project-review";
import { StartDesk } from "@/components/start-desk";
import { AgentBadge, ErrorNote } from "@/components/ui";
import { api, type AgentState, type Desk, type Workspace } from "@/lib/api";
import { timeLeft } from "@/lib/format";
import { useAuthRedirect } from "@/lib/use-auth-redirect";
import { useDeskEvents } from "@/lib/use-desk-events";
import { useOutputs, useProjects } from "@/lib/use-outputs";
import { usePoll } from "@/lib/use-poll";

type View = "chat" | "desktop";

// The dashboard: chat with your agent (or watch its desks) on the left, the phone with
// your apps on the right. The open app is in the address (/?app=vault), so links from
// the agent's messages can open one.
export default function Home() {
  return (
    <Suspense fallback={null}>
      <Dashboard />
    </Suspense>
  );
}

function Dashboard() {
  const router = useRouter();
  const params = useSearchParams();
  const appId = params.get("app");
  const { data: desks, error, reload: reloadDesks } = usePoll(api.desks, 5000);
  const { data: ws, reload: reloadWs } = usePoll(api.workspace, 15000);
  const loadError = useAuthRedirect(error);
  const [view, setView] = useState<View>("chat");
  const [deskSel, setDeskSel] = useState("");
  const [fileSel, setFileSel] = useState<string | null>(null);

  const main = desks?.find((d) => d.id === ws?.mainDesk);
  const reload = useCallback(() => {
    reloadDesks();
    reloadWs();
  }, [reloadDesks, reloadWs]);
  const openApp = useCallback((id: AppId | null) => router.replace(id ? `/?app=${id}` : "/"), [router]);
  const watch = (id: string) => {
    setDeskSel(id);
    setView("desktop");
  };

  return (
    <div className="flex min-h-screen flex-col lg:h-screen lg:flex-row">
      <section className="flex h-screen min-w-0 flex-1 flex-col lg:h-auto">
        <header className="flex flex-wrap items-center gap-3 border-b border-border bg-surface px-4 py-2.5">
          <LogoMark size={26} />
          <div className="min-w-0">
            <h1 className="font-display text-base leading-tight">Your agent</h1>
            <p className="truncate text-xs text-muted">
              {main ? `${main.name} · removed in ${timeLeft(main.expiresAt)}` : desks ? "No main desk yet" : "Loading…"}
            </p>
          </div>
          {main && <AgentBadge state={main.agent} />}
          <div className="ml-auto flex rounded-[10px] border border-border bg-background p-[3px] text-sm" role="tablist">
            {(["chat", "desktop"] as const).map((v) => (
              <button
                key={v}
                role="tab"
                aria-selected={view === v}
                onClick={() => setView(v)}
                className={`rounded-[7px] px-3.5 py-1 font-medium ${view === v ? "bg-surface shadow-sm" : "text-muted hover:text-foreground"}`}
              >
                {v === "chat" ? "💬 Chat" : "🖥 Desktop"}
              </button>
            ))}
          </div>
        </header>
        {loadError && (
          <div className="p-4">
            <ErrorNote message={loadError} />
          </div>
        )}
        <div className="relative min-h-0 flex-1">
          <div className={`absolute inset-0 ${view === "chat" ? "" : "invisible"}`}>
            {main ? (
              <MainChat
                key={main.id}
                desk={main}
                onOpenOutput={(path) => {
                  setFileSel(path);
                  openApp("files");
                }}
                onOpenProject={(name) => {
                  setFileSel(PROJECT + name);
                  openApp("files");
                }}
                onTakeOver={() => watch(main.id)}
              />
            ) : (
              desks && (
                <div className="flex h-full items-start justify-center overflow-y-auto px-4 py-10">
                  <div className="w-full max-w-md space-y-4">
                    <div>
                      <h2 className="font-display text-2xl">Start your main desk</h2>
                      <p className="mt-1 text-sm text-muted">
                        Your agent lives on its main desk: a computer with a browser and Claude Code. You chat with it
                        here, and it runs your apps. Starting takes about a minute.
                      </p>
                    </div>
                    <StartDesk
                      title="Pick its charter"
                      onStarted={async (d) => {
                        await api.setMainDesk(d.id).catch(() => {});
                        reload();
                      }}
                    />
                  </div>
                </div>
              )
            )}
          </div>
          {view === "desktop" && (
            <div className="absolute inset-0">
              {desks && (
                <DesksView desks={desks} mainId={ws?.mainDesk ?? ""} selectedId={deskSel} onSelect={setDeskSel} onChanged={reload} />
              )}
            </div>
          )}
        </div>
      </section>

      <aside className="flex shrink-0 items-center justify-center border-t border-border bg-background/60 p-4 lg:w-[420px] lg:border-t-0 lg:border-l">
        <PhonePanel
          appId={appId}
          openApp={openApp}
          ws={ws}
          desks={desks}
          main={main}
          fileSel={fileSel}
          setFileSel={setFileSel}
          onWatch={watch}
          onChat={() => setView("chat")}
          reload={reload}
        />
      </aside>
    </div>
  );
}

// The chat with the main desk.
function MainChat({
  desk,
  onOpenOutput,
  onOpenProject,
  onTakeOver,
}: {
  desk: Desk;
  onOpenOutput: (path: string) => void;
  onOpenProject: (name: string) => void;
  onTakeOver: () => void;
}) {
  const { events, agent } = useDeskEvents(desk.id);
  const { data: outputs = [] } = useOutputs(desk.id, agent);
  const { data: projects = [] } = useProjects(desk.id, agent);
  return (
    <ChatPanel
      deskId={desk.id}
      events={events}
      agent={agent}
      outputs={outputs}
      toMerge={projects.filter(pending)}
      onOpenOutput={onOpenOutput}
      onOpenProject={onOpenProject}
      onTakeOver={onTakeOver}
    />
  );
}

function PhonePanel({
  appId,
  openApp,
  ws,
  desks,
  main,
  fileSel,
  setFileSel,
  onWatch,
  onChat,
  reload,
}: {
  appId: string | null;
  openApp: (id: AppId | null) => void;
  ws?: Workspace;
  desks?: Desk[];
  main?: Desk;
  fileSel: string | null;
  setFileSel: (p: string | null) => void;
  onWatch: (id: string) => void;
  onChat: () => void;
  reload: () => void;
}) {
  const app = appById(appId);
  const notices = useNotices(ws, desks, main, openApp, onWatch, onChat);
  return (
    <Phone app={app} onHome={() => openApp(null)}>
      {!app ? (
        <HomeScreen
          who={main ? `🤖 Your agent · ${agentWords[main.agent]}` : "🤖 Your agent · not started"}
          notices={notices}
          onOpen={(a) => openApp(a.id)}
        />
      ) : app.id === "vault" ? (
        <VaultApp />
      ) : app.id === "settings" ? (
        <SettingsApp ws={ws} onChange={reload} mainDesk={main?.id} />
      ) : app.id === "store" ? (
        <StoreApp />
      ) : app.id === "files" ? (
        main ? (
          <FilesApp desk={main} selected={fileSel} onSelect={setFileSel} onClose={() => openApp(null)} />
        ) : (
          <p className="p-3 text-sm text-muted">Start your main desk first; what your agent saves shows up here.</p>
        )
      ) : (
        <ComingSoon app={app} />
      )}
    </Phone>
  );
}

const agentWords: Record<AgentState, string> = { idle: "ready", working: "working", paused: "you're in control" };

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
function useNotices(
  ws: Workspace | undefined,
  desks: Desk[] | undefined,
  main: Desk | undefined,
  openApp: (id: AppId | null) => void,
  onWatch: (id: string) => void,
  onChat: () => void,
): Notice[] {
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
    out.push({ icon: "🧠", title: "Connect an AI", text: "Claude, or your own model keys", action: { label: "Set up", onClick: () => openApp("settings") } });
  if (!main) out.push({ icon: "🖥", title: "Start your main desk", text: "Your agent's computer", action: { label: "Start", onClick: onChat } });
  if (main?.agent === "working")
    out.push({ icon: "⚙️", title: "Your agent is working", text: main.name, action: { label: "Watch", onClick: () => onWatch(main.id) } });
  if (main?.agent === "paused")
    out.push({ icon: "✋", title: "You're in control of the desk", text: "The agent waits for you", action: { label: "Open", onClick: () => onWatch(main.id) } });
  if (connected && saved && saved.length === 0)
    out.push({ icon: "🔐", title: "Add your first login", text: "So your agent can sign in to sites", action: { label: "Vault", onClick: () => openApp("vault") } });
  if (main && main.tasks === 0 && main.agent === "idle")
    out.push({ icon: "💬", title: "Give your agent a job", text: "Type it in the chat", action: { label: "Chat", onClick: onChat } });
  if (main?.expiresAt && minute > 0 && new Date(main.expiresAt).getTime() - minute * 60_000 < 15 * 60_000)
    out.push({ icon: "⏳", title: "Main desk ends soon", text: `Removed in ${timeLeft(main.expiresAt)}` });
  return out.slice(0, 3);
}
