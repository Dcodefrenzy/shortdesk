"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { api, type AccessDecision, type AgentEvent, type AgentState, type Output, type Project } from "@/lib/api";
import { buildTasks, duration, stepSummary, type Step, type TaskView } from "@/lib/chat-items";
import { AccessRequestCard } from "./access-request-card";
import { OutputIcon } from "./artifacts-panel";
import { Markdown } from "./markdown";
import { Button, ErrorNote, Spinner } from "./ui";

type Props = {
  deskId: string;
  events: AgentEvent[];
  agent: AgentState;
  outputs: Output[];
  toMerge: Project[];
  onOpenOutput: (path: string) => void;
  onOpenProject: (name: string) => void;
  onTakeOver: () => void;
  // Messages waiting for the agent to finish its current task.
  waiting?: string[];
};

export function ChatPanel({ deskId, events, agent, outputs, toMerge, onOpenOutput, onOpenProject, onTakeOver, waiting = [] }: Props) {
  const [prompt, setPrompt] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const bottom = useRef<HTMLDivElement>(null);

  // Answers to access requests, by request id, so each card shows its outcome.
  const decisions = useMemo(() => {
    const m: Record<string, AccessDecision> = {};
    for (const ev of events) {
      if (ev.kind === "access_decision" && ev.data?.requestId) m[ev.data.requestId] = ev.data.decision as AccessDecision;
    }
    return m;
  }, [events]);

  const tasks = useMemo(() => buildTasks(events), [events]);

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end" });
  }, [events.length]);

  async function send() {
    const text = prompt.trim();
    if (!text) return;
    setSending(true);
    setError("");
    try {
      await api.sendTask(deskId, text);
      setPrompt("");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSending(false);
    }
  }

  async function control(action: "pause" | "resume" | "stop") {
    setError("");
    try {
      await api.controlAgent(deskId, action);
      if (action === "pause") onTakeOver();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-3 px-4 py-6">
          {events.length === 0 && (
            <div className="py-16 text-center text-sm text-muted">
              <p className="text-base text-foreground">What should this desk do?</p>
              <p className="mt-1">The agent works on the desk&apos;s computer. Watch it in the Desktop tab.</p>
            </div>
          )}
          {tasks.map((t) => (
            <TaskBlock
              key={t.taskId}
              task={t}
              deskId={deskId}
              decisions={decisions}
              files={filesFor(t, outputs)}
              onOpenOutput={onOpenOutput}
            />
          ))}
          {agent === "idle" && toMerge.length > 0 && (
            <div className="rounded-lg border border-warn/40 bg-warn/5 p-3 text-sm">
              <p className="font-medium">Changes ready to merge</p>
              <ul className="mt-1 space-y-1">
                {toMerge.map((p) => (
                  <li key={p.name} className="flex flex-wrap items-center gap-2">
                    <span className="font-mono">{p.name}</span>
                    <span className="text-xs text-muted">
                      {p.ahead} {p.ahead === 1 ? "commit" : "commits"}
                      {p.uncommitted > 0 && ` · ${p.uncommitted} uncommitted`}
                    </span>
                    <Button onClick={() => onOpenProject(p.name)}>Review</Button>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {waiting.map((w, i) => (
            <div key={i} className="flex justify-end">
              <div className="max-w-[85%] text-right">
                <p className="rounded-2xl border border-dashed border-accent px-4 py-2 text-left text-sm whitespace-pre-wrap">{w}</p>
                <span className="text-xs text-muted">waiting · goes next when the agent finishes</span>
              </div>
            </div>
          ))}
          <div ref={bottom} />
        </div>
      </div>

      <div className="border-t border-border bg-surface">
        <div className="mx-auto max-w-3xl space-y-2 px-4 py-3">
          {error && <ErrorNote message={error} />}
          {agent !== "idle" && (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              {agent === "working" ? (
                <>
                  <span className="text-muted">The agent is working.</span>
                  <Button onClick={() => control("pause")}>Take over</Button>
                </>
              ) : (
                <>
                  <span className="text-muted">You&apos;re in control of the desk.</span>
                  <Button variant="primary" onClick={() => control("resume")}>
                    Continue agent
                  </Button>
                </>
              )}
              <Button variant="danger" onClick={() => control("stop")}>
                Stop task
              </Button>
            </div>
          )}
          <form
            onSubmit={(e) => {
              e.preventDefault();
              send();
            }}
            className="flex items-end gap-2"
          >
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  send();
                }
              }}
              rows={2}
              placeholder={
                agent === "idle"
                  ? "Give the agent a task…  (Enter to send, Shift+Enter for a new line)"
                  : "The agent is working. Send anyway: your message goes next."
              }
              className="min-h-11 flex-1 resize-none rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-accent disabled:opacity-60"
            />
            <Button type="submit" variant="primary" disabled={!prompt.trim() || sending}>
              {sending ? <Spinner /> : "Send"}
            </Button>
          </form>
        </div>
      </div>
    </div>
  );
}

// One task: the person's message, the agent's steps (collapsed), then its answer.
// Files the agent saved while working on a task.
function filesFor(t: TaskView, outputs: Output[]): Output[] {
  if (!t.startedAt) return [];
  const from = new Date(t.startedAt).getTime();
  const to = t.endedAt ? new Date(t.endedAt).getTime() + 10_000 : Infinity;
  return outputs.filter((o) => {
    const m = new Date(o.modified).getTime();
    return m >= from && m <= to;
  });
}

function TaskBlock({
  task: t,
  deskId,
  decisions,
  files,
  onOpenOutput,
}: {
  task: TaskView;
  deskId: string;
  decisions: Record<string, AccessDecision>;
  files: Output[];
  onOpenOutput: (path: string) => void;
}) {
  const failedWithoutAnswer = t.status === "failed" && !t.answer;
  const lastError = [...t.steps].reverse().find((s) => s.kind === "error");
  return (
    <div className="space-y-3 pt-2">
      {t.prompt && (
        <div className="flex justify-end">
          <p className="max-w-[85%] rounded-2xl bg-accent px-4 py-2 text-sm whitespace-pre-wrap text-accent-foreground">
            {t.prompt.text}
          </p>
        </div>
      )}
      {t.steps.length > 0 && <Activity task={t} />}
      {t.cards.map((ev) => (
        <AccessRequestCard key={ev.seq} deskId={deskId} ev={ev} decision={decisions[ev.data?.requestId ?? ""]} />
      ))}
      {t.answer && <Answer ev={t.answer} task={t} />}
      {files.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {files.map((f) => (
            <button
              key={f.path}
              onClick={() => onOpenOutput(f.path)}
              className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 py-1 text-xs hover:border-accent"
            >
              <OutputIcon output={f} />
              <span className="max-w-52 truncate">{f.path}</span>
            </button>
          ))}
        </div>
      )}
      {failedWithoutAnswer && (
        <p className="text-sm text-danger">✕ Task failed{lastError ? `: ${lastError.label}` : ""}</p>
      )}
      {t.help && (
        <div className="rounded-md border border-border bg-surface px-3 py-2 text-sm">
          <p className="text-xs font-medium text-muted">What to do</p>
          <Markdown text={t.help} />
        </div>
      )}
      {t.status === "stopped" && <p className="text-xs text-muted">■ Stopped</p>}
      {t.status === "paused" && <p className="text-xs text-muted">⏸ You took over the desk</p>}
    </div>
  );
}

// The agent's working steps, folded into one line. While it works, the line shows the
// current step.
function Activity({ task: t }: { task: TaskView }) {
  const tools = t.steps.filter((s) => s.kind === "tool");
  const errors = t.steps.filter((s) => s.kind === "error").length;
  const running = t.status === "running";
  const current = [...t.steps].reverse().find((s) => s.kind === "tool" || s.kind === "text");
  const took = duration(t.startedAt, t.endedAt);
  return (
    <details className="group rounded-lg border border-border bg-surface text-sm">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-muted select-none">
        <span className="transition group-open:rotate-90">▸</span>
        {running ? (
          <>
            <Spinner />
            <span className="truncate">{current ? stepSummary(current) : "Starting…"}</span>
          </>
        ) : (
          <span>
            Worked {took && `for ${took}`} · {tools.length} {tools.length === 1 ? "step" : "steps"}
            {errors > 0 && <span className="text-danger"> · {errors} {errors === 1 ? "problem" : "problems"}</span>}
          </span>
        )}
      </summary>
      <ol className="space-y-1.5 border-t border-border px-3 py-2">
        {t.steps.map((s) => (
          <StepRow key={s.seq} step={s} />
        ))}
      </ol>
    </details>
  );
}

function StepRow({ step: s }: { step: Step }) {
  if (s.kind === "text") return <li className="text-sm whitespace-pre-wrap">{s.label}</li>;
  if (s.kind === "note") return <li className="text-xs text-muted">{s.label}</li>;
  if (s.kind === "error") return <li className="text-xs whitespace-pre-wrap text-danger">{s.label}</li>;
  return (
    <li className="text-xs">
      <details>
        <summary className="cursor-pointer truncate text-muted select-none" title={s.detail}>
          <span className="font-medium text-foreground">{s.label}</span>
          {s.detail && <span> · {s.detail}</span>}
          {s.result?.isError && <span className="text-danger"> · failed</span>}
        </summary>
        {s.result?.text && (
          <pre className="mt-1 max-h-60 overflow-auto rounded bg-background p-2 font-mono whitespace-pre-wrap">
            {s.result.text}
          </pre>
        )}
      </details>
    </li>
  );
}

// Known agent failures get a plain-language explanation and what to do next.
function explainFailure(text: string): { title: string; hint: string } | null {
  if (/hit your .*limit|usage limit|rate limit/i.test(text)) {
    return {
      title: "Claude usage limit reached",
      hint: "Claude's usage is used up until the reset. If you added your own model keys in Workspace settings, Shortdesk switches to them and skips Claude until then.",
    };
  }
  if (/not logged in|please run \/login|invalid api key|authentication/i.test(text)) {
    return {
      title: "The agent isn't logged in to Claude",
      hint: "Log in once in this desk's terminal (run claude), or give Shortdesk a Claude token so every desk is logged in.",
    };
  }
  return null;
}

// The task's final answer. Failures with a known cause get a plain explanation.
function Answer({ ev, task }: { ev: AgentEvent; task: TaskView }) {
  if (ev.isError) {
    const known = explainFailure(ev.text ?? "");
    return (
      <div className="rounded-lg border border-danger/40 p-3 text-sm">
        <p className="mb-1 text-xs font-medium text-danger">{known ? `⚠ ${known.title}` : "✕ Task failed"}</p>
        <p className="whitespace-pre-wrap">{ev.text}</p>
        {known && <p className="mt-2 text-xs text-muted">{known.hint}</p>}
      </div>
    );
  }
  return (
    <div>
      <Markdown text={ev.text ?? ""} />
      <p className="mt-2 text-xs text-muted">
        ✓ Done{task.endedAt && ` in ${duration(task.startedAt, task.endedAt)}`}
        {ev.costUsd ? ` · $${ev.costUsd.toFixed(2)}` : ""}
      </p>
    </div>
  );
}
