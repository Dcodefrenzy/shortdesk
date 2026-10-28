// Turns a desk's raw agent events into what the chat shows: each task as the person's
// message, one collapsible group of the agent's working steps, and the final answer.
import type { AgentEvent } from "./api";

export type Step = {
  seq: number;
  label: string; // "Open page", "Terminal", ...
  detail?: string; // url, command, ...
  result?: AgentEvent; // the tool's output, if any
  kind: "tool" | "note" | "text" | "error";
};

export type TaskView = {
  taskId: string;
  prompt?: AgentEvent;
  steps: Step[];
  answer?: AgentEvent; // the final result
  status: "running" | "paused" | "done" | "failed" | "stopped";
  startedAt?: string;
  endedAt?: string;
  cards: AgentEvent[]; // things that need the person (access requests), shown outside the group
  help?: string; // after a failure: what the person can do about it
  helper?: AgentEvent[]; // news about a job a helper desk does for this desk
};

// Friendly names for tool calls. Hidden tools return null.
export function toolLabel(tool = ""): string | null {
  if (tool === "ToolSearch") return null;
  const browser = tool.match(/^mcp__browser__browser_(.+)$/);
  if (browser) {
    const names: Record<string, string> = {
      navigate: "Open page",
      navigate_back: "Go back",
      snapshot: "Read page",
      click: "Click",
      type: "Type",
      fill_form: "Fill form",
      press_key: "Press key",
      select_option: "Choose option",
      hover: "Hover",
      take_screenshot: "Screenshot",
      evaluate: "Run script on page",
      wait_for: "Wait",
      tabs: "Switch tab",
      close: "Close page",
    };
    return names[browser[1]] ?? `Browser: ${browser[1].replaceAll("_", " ")}`;
  }
  const named: Record<string, string> = {
    Bash: "Terminal",
    Read: "Read file",
    Write: "Write file",
    Edit: "Edit file",
    MultiEdit: "Edit file",
    Glob: "Find files",
    Grep: "Search files",
    WebSearch: "Search the web",
    WebFetch: "Fetch page",
    TodoWrite: "Plan",
    Task: "Helper agent",
  };
  return named[tool] ?? tool.replace(/^mcp__[^_]+__/, "").replaceAll("_", " ");
}

// What a running group shows as its live line, e.g. "Open page · google.com".
export function stepSummary(s: Step): string {
  if (!s.detail) return s.label;
  let d = s.detail;
  try {
    if (/^https?:\/\//.test(d)) d = new URL(d).host;
  } catch {}
  return `${s.label} · ${d.length > 60 ? d.slice(0, 60) + "…" : d}`;
}

export function buildTasks(events: AgentEvent[]): TaskView[] {
  const tasks: TaskView[] = [];
  const byId = new Map<string, TaskView>();
  const task = (ev: AgentEvent) => {
    let t = byId.get(ev.taskId);
    if (!t) {
      t = { taskId: ev.taskId, steps: [], status: "running", cards: [], startedAt: ev.time };
      byId.set(ev.taskId, t);
      tasks.push(t);
    }
    return t;
  };

  for (const ev of events) {
    if (ev.kind === "access_decision") continue; // shown on its request's card
    const t = task(ev);
    switch (ev.kind) {
      case "prompt":
        t.prompt = ev;
        t.startedAt = ev.time;
        break;
      case "tool": {
        const label = toolLabel(ev.tool);
        if (label) t.steps.push({ seq: ev.seq, label, detail: ev.text, kind: "tool" });
        break;
      }
      case "tool_result": {
        const last = [...t.steps].reverse().find((s) => s.kind === "tool" && !s.result);
        if (last) last.result = ev;
        break;
      }
      case "text":
        t.steps.push({ seq: ev.seq, label: ev.text ?? "", kind: "text" });
        break;
      case "log":
        t.steps.push({ seq: ev.seq, label: ev.text ?? "", kind: "note" });
        break;
      case "error":
        t.steps.push({ seq: ev.seq, label: ev.text ?? "", kind: "error" });
        break;
      case "result":
        // An earlier result (e.g. Claude's usage limit before switching models) is a step;
        // only the last one is the answer.
        if (t.answer) {
          t.steps.push({
            seq: t.answer.seq,
            label: t.answer.text ?? "",
            kind: t.answer.isError ? "error" : "text",
          });
        }
        t.answer = ev;
        break;
      case "access_request":
        t.cards.push(ev);
        break;
      case "help":
        t.help = ev.text;
        break;
      case "helper":
        (t.helper ??= []).push(ev);
        break;
      case "status":
        if (ev.text === "running" || ev.text === "paused" || ev.text === "done" || ev.text === "failed" || ev.text === "stopped") {
          t.status = ev.text;
        }
        if (ev.text === "done" || ev.text === "failed" || ev.text === "stopped") t.endedAt = ev.time;
        break;
    }
  }

  // The agent usually says its answer once as text and once as the result: drop the copy.
  for (const t of tasks) {
    const last = t.steps.at(-1);
    if (t.answer && last?.kind === "text" && last.label === t.answer.text) t.steps.pop();
  }
  return tasks;
}

export function duration(from?: string, to?: string): string {
  if (!from) return "";
  const ms = (to ? new Date(to).getTime() : Date.now()) - new Date(from).getTime();
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}
