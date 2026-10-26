"use client";

import { useEffect, useState } from "react";
import { api, type AgentEvent, type AgentState } from "./api";

// Streams a desk's chat events (Server-Sent Events). EventSource reconnects on its own
// and resumes after the last event it saw. Mount with key={deskId} to reset per desk.
export function useDeskEvents(deskId: string) {
  const [events, setEvents] = useState<AgentEvent[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const es = new EventSource(api.eventsUrl(deskId));
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (m) => {
      const ev: AgentEvent = JSON.parse(m.data);
      setEvents((prev) => (prev.length && prev[prev.length - 1].seq >= ev.seq ? prev : [...prev, ev]));
    };
    return () => es.close();
  }, [deskId]);

  return { events, connected, agent: agentState(events) };
}

// The agent's state is the latest status event: running → working, paused → paused.
function agentState(events: AgentEvent[]): AgentState {
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i].kind !== "status") continue;
    if (events[i].text === "running") return "working";
    if (events[i].text === "paused") return "paused";
    return "idle";
  }
  return "idle";
}
