"use client";

import Link from "next/link";
import { api, type Desk } from "@/lib/api";
import { usePoll } from "@/lib/use-poll";
import { Card } from "./ui";

// Getting started: the three steps to a first result, ticked off from what Shortdesk
// already knows. Hidden once they're all done.
export function FirstStartHint({ desks }: { desks?: Desk[] }) {
  const { data: ws } = usePoll(api.workspace, 15000);
  const { data: logins } = usePoll(api.logins, 30000);
  if (!desks || !ws || !logins) return null;

  const aiReady =
    ws.connections.some((c) => c.set) || logins.some((l) => l.kind === "app" && l.name === "claude-code");
  const hasDesk = desks.length > 0;
  const hasTask = desks.some((d) => d.tasks > 0);
  if (aiReady && hasDesk && hasTask) return null;

  const firstDesk = desks[0];
  return (
    <Card className="p-4">
      <p className="font-medium">Getting started</p>
      <ol className="mt-3 space-y-3 text-sm">
        <Step n={1} done={aiReady} title="Connect an AI">
          Start a desk, open its <b>Desktop</b> tab and run <code className="font-mono text-xs">claude</code> in the
          terminal to sign in with your Claude account. Or add a Claude token or your own model key (Gemini,
          DeepSeek, Groq, NVIDIA) in{" "}
          <Link href="/workspace/" className="underline">
            Workspace settings
          </Link>
          . Every desk gets it automatically.
        </Step>
        <Step n={2} done={hasDesk} title="Start a desk">
          Pick a charter below. <b>assistant</b> can only reach an approved list of websites; <b>open</b> can go
          anywhere. The first desk takes a minute to start.
        </Step>
        <Step n={3} done={hasTask} title="Give it a task">
          {firstDesk ? (
            <>
              Open{" "}
              <Link href={`/?desk=${encodeURIComponent(firstDesk.id)}`} className="underline">
                {firstDesk.name}
              </Link>{" "}
              and type what you need in the chat
            </>
          ) : (
            "Type what you need in the desk's chat"
          )}
          , e.g. &ldquo;find flights from London to Lagos next month and put them in a table&rdquo;. Watch it work in
          the Desktop tab; results appear under Artifacts.
        </Step>
      </ol>
      <p className="mt-4 border-t border-border pt-3 text-xs text-muted">
        Log into a website or app once on any desk and Shortdesk gives that login to every desk.
      </p>
    </Card>
  );
}

function Step({ n, done, title, children }: { n: number; done: boolean; title: string; children: React.ReactNode }) {
  return (
    <li className="flex gap-3">
      <span
        aria-hidden
        className={`mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full text-xs font-medium ${
          done ? "bg-ok text-white" : "border border-border text-muted"
        }`}
      >
        {done ? "✓" : n}
      </span>
      <span className={done ? "text-muted" : ""}>
        <span className={`block font-medium ${done ? "line-through" : ""}`}>{title}</span>
        {!done && <span className="mt-0.5 block text-muted">{children}</span>}
      </span>
    </li>
  );
}
