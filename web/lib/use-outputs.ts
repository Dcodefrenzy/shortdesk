"use client";

import { useCallback } from "react";
import { api, type AgentState } from "./api";
import { usePoll } from "./use-poll";

// The desk's output files, refreshed often while the agent works.
export function useOutputs(deskId: string, agent: AgentState) {
  const load = useCallback(() => api.outputs(deskId), [deskId]);
  return usePoll(load, agent === "working" ? 4000 : 20000);
}

// The desk's code projects and its unmerged work on them.
export function useProjects(deskId: string, agent: AgentState) {
  const load = useCallback(() => api.projects(deskId), [deskId]);
  return usePoll(load, agent === "working" ? 6000 : 20000);
}
