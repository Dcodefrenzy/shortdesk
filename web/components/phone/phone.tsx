"use client";

import { useState, useSyncExternalStore, type ReactNode } from "react";
import type { PhoneApp } from "./apps";

// The clock ticks client-side only; the static page renders it empty.
function subscribeClock(cb: () => void) {
  const t = setInterval(cb, 10_000);
  return () => clearInterval(t);
}
const timeNow = () => new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
const dateNow = () => new Date().toLocaleDateString([], { weekday: "long", day: "numeric", month: "long" });
// useMinute is the current minute (ms / 60000), updated while the page is open.
export function useMinute() {
  return useSyncExternalStore(subscribeClock, () => Math.floor(Date.now() / 60_000), () => 0);
}

export function useClock() {
  const time = useSyncExternalStore(subscribeClock, timeNow, () => "");
  const date = useSyncExternalStore(subscribeClock, dateNow, () => "");
  return { time, date };
}

// The phone frame: status bar, the screen, and the home bar. app is undefined on the
// home screen.
export function Phone({
  app,
  onHome,
  bare,
  children,
}: {
  app?: PhoneApp;
  onHome: () => void;
  // bare fills the screen without the device frame (on a real phone).
  bare?: boolean;
  children: ReactNode;
}) {
  const { time } = useClock();
  const home = !app;
  return (
    <div className={bare ? "h-dvh w-full" : "h-[min(800px,calc(100vh-32px))] w-full max-w-[370px] rounded-[46px] bg-[#0b0f0e] p-[11px] shadow-2xl"}>
      <div
        className={`flex h-full flex-col overflow-hidden ${bare ? "" : "rounded-[36px]"} ${
          home ? "bg-gradient-to-b from-[#1d7a63] via-[#13684f] to-[#0b4a3c] text-white" : "bg-background text-foreground"
        }`}
      >
        <div className="flex shrink-0 items-center justify-between px-6 pt-3 pb-1.5 text-[13px] font-semibold">
          <span className="w-12 tabular-nums">{time}</span>
          <span className={`h-[26px] w-24 rounded-full ${bare ? "" : "bg-[#0b0f0e]"}`} aria-hidden />
          <span className="w-12 text-right text-[11px]">●●● 5G</span>
        </div>
        {app && (
          <div className="relative flex shrink-0 items-center border-b border-border px-4 pb-2.5">
            <button onClick={onHome} className="py-1 text-sm font-semibold text-accent">
              ‹ Home
            </button>
            <h2 className="pointer-events-none absolute inset-x-0 text-center font-display text-[17px]">{app.name}</h2>
          </div>
        )}
        <div className={`min-h-0 flex-1 overflow-x-hidden overflow-y-auto ${home ? "px-4 pt-1 pb-3" : "px-3 py-3"}`}>{children}</div>
        <div className="grid shrink-0 place-items-center pt-1.5 pb-2.5">
          <button
            onClick={onHome}
            aria-label="Home"
            className={`h-[5px] w-[134px] rounded-full opacity-55 hover:opacity-90 ${home ? "bg-white" : "bg-foreground"}`}
          />
        </div>
      </div>
    </div>
  );
}

export type Notice = {
  icon: string;
  title: string;
  text: string;
  action?: { label: string; onClick: () => void };
};

// The home screen: the time, what needs you, and the apps.
export function HomeScreen({
  who,
  notices,
  apps: all,
  onOpen,
}: {
  who: string;
  notices: Notice[];
  apps: PhoneApp[];
  onOpen: (app: PhoneApp) => void;
}) {
  const { time, date } = useClock();
  const [search, setSearch] = useState("");
  const apps = all.filter((a) => a.name.toLowerCase().includes(search.trim().toLowerCase()));
  return (
    <div className="grid gap-3">
      <div>
        <p className="mt-1 font-display text-[60px] leading-none font-bold tracking-tight tabular-nums">{time || " "}</p>
        <p className="mt-1 text-sm opacity-90">{date}</p>
      </div>
      <span className="justify-self-start rounded-full bg-white/18 px-3 py-1 text-[13px] font-medium">{who}</span>
      {notices.map((n) => (
        <div key={n.title} className="grid grid-cols-[34px_1fr_auto] items-center gap-2.5 rounded-[18px] bg-white/95 px-3 py-2.5 text-[13px] text-[#13201b]">
          <span className="grid size-[34px] place-items-center rounded-[10px] bg-[#f1f4f3] text-[17px]" aria-hidden>
            {n.icon}
          </span>
          <span className="min-w-0">
            <b className="block">{n.title}</b>
            <span className="block text-xs text-[#5c6a65]">{n.text}</span>
          </span>
          {n.action && (
            <button onClick={n.action.onClick} className="rounded-full bg-[#e8742c] px-3 py-1.5 text-xs font-semibold whitespace-nowrap text-white">
              {n.action.label} ›
            </button>
          )}
        </div>
      ))}
      <input
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder="🔍 Search apps"
        aria-label="Search apps"
        className="rounded-[14px] bg-white/20 px-3.5 py-2 text-sm text-white placeholder:text-white/85 outline-none focus:bg-white/28"
      />
      <div className="grid grid-cols-4 gap-x-1.5 gap-y-4 pt-1">
        {apps.map((a) => (
          <button key={a.id} onClick={() => onOpen(a)} className="grid justify-items-center gap-1.5 text-[11.5px] font-medium [text-shadow:0_1px_2px_rgba(0,0,0,.3)]">
            <span
              className={`relative grid size-[58px] place-items-center rounded-[16px] text-[27px] shadow-md [text-shadow:none] ${a.tile}`}
              style={a.color ? { backgroundColor: a.color } : undefined}
            >
              {a.icon}
              {a.kind === "soon" && (
                <span className="absolute -right-1 -bottom-1 rounded-full bg-[#0b4a3c] px-1.5 text-[9px] font-semibold text-white ring-2 ring-[#13684f]">
                  soon
                </span>
              )}
            </span>
            {a.name}
          </button>
        ))}
        {apps.length === 0 && <p className="col-span-4 text-center text-sm opacity-85">No app called that.</p>}
      </div>
    </div>
  );
}

// What a not-yet-built app says when opened.
export function ComingSoon({ app }: { app: PhoneApp }) {
  return (
    <div className="grid justify-items-center gap-3 px-3 pt-10 text-center">
      <span className={`grid size-20 place-items-center rounded-[22px] text-4xl shadow-md ${app.tile}`} style={app.color ? { backgroundColor: app.color } : undefined}>
        {app.icon}
      </span>
      <h3 className="font-display text-xl">{app.name}</h3>
      <p className="text-sm text-muted">{app.about}</p>
      <p className="rounded-full bg-accent/10 px-3 py-1 text-xs font-medium text-accent">Coming with the next update</p>
    </div>
  );
}
