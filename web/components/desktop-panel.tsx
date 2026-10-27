"use client";

import { useEffect, useRef, useState } from "react";
import { api, type AgentState, type Desk } from "@/lib/api";
import { Button } from "./ui";

// After taking over, the agent resumes on its own once the person stops using the desk.
const IDLE_RESUME_SECONDS = 60;
const ACTIVITY_EVENTS = ["mousemove", "mousedown", "keydown", "wheel", "touchstart"] as const;

type Props = {
  desk: Desk;
  agent: AgentState;
  visible: boolean;
};

export function DesktopPanel({ desk, agent, visible }: Props) {
  const frame = useRef<HTMLIFrameElement>(null);
  const [error, setError] = useState("");

  async function control(action: "pause" | "resume") {
    setError("");
    try {
      await api.controlAgent(desk.id, action);
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <div className="relative h-full bg-black">
      <iframe
        ref={frame}
        src={desk.viewerUrl}
        title={`Desk ${desk.name}`}
        allow="clipboard-read; clipboard-write; fullscreen"
        className="h-full w-full border-0"
      />

      {agent === "working" && (
        // Locks the desk: clicks and keys never reach it while the agent works.
        <div className="absolute inset-0 flex items-end justify-center bg-black/5 p-4" aria-live="polite">
          <div className="flex items-center gap-3 rounded-full border border-border bg-surface/95 py-1.5 pr-1.5 pl-4 text-sm shadow-lg backdrop-blur">
            <span className="size-2 animate-pulse rounded-full bg-ok" />
            The agent is using this desk
            <Button variant="primary" className="rounded-full" onClick={() => control("pause")}>
              Take over
            </Button>
          </div>
        </div>
      )}

      {agent === "paused" && visible && (
        <IdleResume frame={frame} onResume={() => control("resume")} />
      )}

      {error && (
        <p className="absolute top-3 left-1/2 -translate-x-1/2 rounded-md bg-danger px-3 py-1.5 text-sm text-background">
          {error}
        </p>
      )}
    </div>
  );
}

// Banner shown while a person controls the desk. Any activity inside the desk resets
// the countdown; when it reaches zero the agent continues.
function IdleResume({
  frame,
  onResume,
}: {
  frame: React.RefObject<HTMLIFrameElement | null>;
  onResume: () => void;
}) {
  const [left, setLeft] = useState(IDLE_RESUME_SECONDS);
  const resumed = useRef(false);

  useEffect(() => {
    const reset = () => setLeft(IDLE_RESUME_SECONDS);
    // The viewer is same-origin, so its document's events can be observed.
    let doc: Document | null = null;
    try {
      doc = frame.current?.contentDocument ?? null;
    } catch {
      doc = null;
    }
    ACTIVITY_EVENTS.forEach((e) => doc?.addEventListener(e, reset, true));
    const t = setInterval(() => setLeft((s) => Math.max(0, s - 1)), 1000);
    return () => {
      clearInterval(t);
      ACTIVITY_EVENTS.forEach((e) => doc?.removeEventListener(e, reset, true));
    };
  }, [frame]);

  useEffect(() => {
    if (left === 0 && !resumed.current) {
      resumed.current = true;
      onResume();
    }
  }, [left, onResume]);

  return (
    <div className="absolute top-3 left-1/2 flex -translate-x-1/2 items-center gap-3 rounded-full border border-border bg-surface/95 py-1.5 pr-1.5 pl-4 text-sm shadow-lg backdrop-blur">
      <span className="size-2 rounded-full bg-warn" />
      You&apos;re in control · agent continues in {left}s if you stop
      <Button variant="primary" className="rounded-full" onClick={onResume}>
        Continue agent
      </Button>
    </div>
  );
}
