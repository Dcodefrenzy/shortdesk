"use client";

import { useState } from "react";
import { api, type AccessDecision, type AgentEvent } from "@/lib/api";
import { Button } from "./ui";

const decided: Record<AccessDecision, string> = {
  desk: "Allowed on this desk",
  charter: "Allowed and added to the charter",
  deny: "Denied",
};

// The agent asking to reach a blocked website. Only a human can answer; once answered
// the agent continues by itself.
export function AccessRequestCard({
  deskId,
  ev,
  decision,
}: {
  deskId: string;
  ev: AgentEvent;
  decision?: AccessDecision;
}) {
  const [busy, setBusy] = useState<AccessDecision | null>(null);
  const [error, setError] = useState("");
  const host = ev.data?.host ?? "";
  const requestId = ev.data?.requestId ?? "";

  async function decide(d: AccessDecision) {
    setBusy(d);
    setError("");
    try {
      // A helper's request shown in the main chat is answered on the helper's desk.
      await api.decideAccess(ev.data?.desk || deskId, requestId, d);
    } catch (err) {
      setError((err as Error).message);
      setBusy(null);
    }
  }

  return (
    <div className="rounded-lg border border-warn/40 bg-warn/5 p-3 text-sm">
      <p className="font-medium">
        🔒 The agent asks to visit <span className="font-mono">{host}</span>
      </p>
      {ev.data?.reason && <p className="mt-0.5 text-muted">Reason: {ev.data.reason}</p>}
      {decision ? (
        <p className={`mt-2 text-xs font-medium ${decision === "deny" ? "text-danger" : "text-ok"}`}>
          {decided[decision]}
        </p>
      ) : (
        <div className="mt-3 flex flex-wrap gap-2">
          <Button variant="primary" disabled={!!busy || !requestId} onClick={() => decide("desk")}>
            {busy === "desk" ? "Allowing…" : "Allow for this desk"}
          </Button>
          <Button disabled={!!busy || !requestId} onClick={() => decide("charter")}>
            {busy === "charter" ? "Saving…" : "Allow + add to charter"}
          </Button>
          <Button variant="danger" disabled={!!busy || !requestId} onClick={() => decide("deny")}>
            Deny
          </Button>
        </div>
      )}
      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
    </div>
  );
}
