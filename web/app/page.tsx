"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { PROJECT } from "@/components/artifacts-panel";
import { ChatPanel } from "@/components/chat-panel";
import { DesksView } from "@/components/desks-view";
import { LogoMark } from "@/components/logo";
import { pending } from "@/components/project-review";
import { StartDesk } from "@/components/start-desk";
import { AgentBadge, ErrorNote } from "@/components/ui";
import { api, type Desk } from "@/lib/api";
import { timeLeft } from "@/lib/format";
import { useAuthRedirect } from "@/lib/use-auth-redirect";
import { useDeskEvents } from "@/lib/use-desk-events";
import { useOutputs, useProjects } from "@/lib/use-outputs";
import { usePoll } from "@/lib/use-poll";

type View = "chat" | "desktop";

// The dashboard: chat with your agent (or watch its desks) on the left, and your
// agent's phone on the right. A link like /?app=tasks opens that app on the phone.
export default function Home() {
  return (
    <Suspense fallback={null}>
      <Dashboard />
    </Suspense>
  );
}

function Dashboard() {
  const params = useSearchParams();
  const appId = params.get("app");
  const { data: desks, error, reload: reloadDesks } = usePoll(api.desks, 5000);
  const { data: ws, reload: reloadWs } = usePoll(api.workspace, 15000);
  const loadError = useAuthRedirect(error);
  const [view, setView] = useState<View>("chat");
  const [deskSel, setDeskSel] = useState("");

  const main = desks?.find((d) => d.id === ws?.mainDesk);
  const reload = useCallback(() => {
    reloadDesks();
    reloadWs();
  }, [reloadDesks, reloadWs]);
  // The phone is its own page (/phone/), served beside the chat; links like /?app=tasks
  // open that app on it.
  const phone = useRef<HTMLIFrameElement>(null);
  const [phoneSrc, setPhoneSrc] = useState(() => `/phone/?embed=1${appId ? `&app=${encodeURIComponent(appId)}` : ""}`);
  const openApp = useCallback((id: string, path?: string) => {
    const q = new URLSearchParams({ embed: "1", app: id });
    if (path) q.set("path", path);
    setPhoneSrc(`/phone/?${q}&t=${Date.now()}`);
  }, []);
  const watch = (id: string) => {
    setDeskSel(id);
    setView("desktop");
  };
  // The phone asks the dashboard to show a desk or the chat.
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.source !== phone.current?.contentWindow || e.origin !== location.origin || e.data?.shortdesk !== 1) return;
      if (typeof e.data.watch === "string") {
        setDeskSel(e.data.watch);
        setView("desktop");
      } else if (e.data.chat) setView("chat");
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

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
                onOpenOutput={(path) => openApp("files", path)}
                onOpenProject={(name) => openApp("files", PROJECT + name)}
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

      <aside className="flex shrink-0 items-center justify-center border-t border-border bg-background/60 lg:w-[420px] lg:border-t-0 lg:border-l">
        <iframe ref={phone} src={phoneSrc} title="Your agent's phone" className="h-[min(840px,100vh)] w-full max-w-[420px] border-0" />
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
