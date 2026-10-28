// Typed client for the Shortdesk API (internal/server). Same origin, cookie auth.

export type AgentState = "idle" | "working" | "paused";

export type Desk = {
  id: string;
  name: string;
  charter: string;
  tenant?: string;
  state: string;
  createdAt: string;
  expiresAt?: string;
  agent: AgentState;
  viewerUrl: string;
  tasks: number;
};

export type Charter = {
  name: string;
  tenant?: string;
  image: string;
  cpu: string;
  memory: string;
  lifetime: string;
  allowedHosts: string[];
};

export type EventKind =
  | "prompt"
  | "text"
  | "tool"
  | "tool_result"
  | "result"
  | "error"
  | "status"
  | "log"
  | "help"
  | "access_request"
  | "access_decision";

export type AccessDecision = "desk" | "charter" | "deny";

export type NetworkRule = { host: string; source: "charter" | "settings" | "chat" | "models"; addedAt?: string };

export type DeskSettings = {
  desk: Desk;
  charter?: Charter & { version?: number };
  network: { unrestricted: boolean; editable: boolean; allowAll: boolean; hosts: NetworkRule[] };
  apps: { name: string; description: string; installed: boolean }[];
  integrations: string[];
  logins: DeskLogin[];
  shared: { volume: string; path: string }[];
};

// A login saved automatically from a desk (an app's login or a website's cookies).
export type Login = { kind: "app" | "site"; name: string; from: string; updatedAt: string };
// The same login as seen from one desk.
export type DeskLogin = Login & { status: "active" | "revoked" | "missing" };

export type RouterMode = "auto" | "claude" | "own";

export type Workspace = {
  connections: { name: string; set: boolean; hint?: string }[];
  router: { mode: RouterMode; models: string[] };
  // The order actually used after Claude: the router's list, or one default per key set.
  ownModels: string[];
  // The desk you chat with ("" when there are no desks).
  mainDesk: string;
};

// A code project in ~/Developer, and this desk's work on it (its own git branch).
export type Project = {
  name: string;
  main: string;
  branch: string;
  ahead: number;
  behind: number;
  uncommitted: number;
  lastCommit: string;
};

// A file the agent saved in the desk's outputs folder.
export type Output = { path: string; size: number; modified: string; type: string };

export type VaultStatus = { url: string; running: boolean; connected: boolean; email?: string };

// A login stored in the vault's Shortdesk folder. Passwords are never returned.
export type Integration = { id: string; name: string; site: string; username: string; hasTotp: boolean };

// One entry in a desk's chat (internal/agent.Event).
export type AgentEvent = {
  seq: number;
  taskId: string;
  time: string;
  kind: EventKind;
  text?: string;
  tool?: string;
  isError?: boolean;
  costUsd?: number;
  data?: Record<string, string>;
};

export class UnauthorizedError extends Error {
  constructor() {
    super("Login required");
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    ...init,
    credentials: "same-origin",
    headers: init.body ? { "Content-Type": "application/json", ...init.headers } : init.headers,
  });
  if (res.status === 401) throw new UnauthorizedError();
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new Error(body?.error ?? `Request failed (${res.status})`);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

const desk = (id: string) => `/api/desks/${encodeURIComponent(id)}`;

export const api = {
  login: (token: string) => request<void>("/api/login", { method: "POST", body: JSON.stringify({ token }) }),
  logout: () => request<void>("/api/logout", { method: "POST" }),
  // desk is true when the phone is opened in a desk's own browser (no Shortdesk login).
  session: () => request<{ authenticated: boolean; desk?: boolean }>("/api/session"),

  charters: () => request<Charter[]>("/api/charters"),

  desks: () => request<Desk[]>("/api/desks"),
  desk: (id: string) => request<Desk>(desk(id)),
  startDesk: (charter: string, name?: string) =>
    request<Desk>("/api/desks", { method: "POST", body: JSON.stringify(name ? { charter, name } : { charter }) }),
  stopDesk: (id: string) => request<void>(desk(id), { method: "DELETE" }),

  sendTask: (id: string, prompt: string) =>
    request<unknown>(`${desk(id)}/tasks`, { method: "POST", body: JSON.stringify({ prompt }) }),
  controlAgent: (id: string, action: "pause" | "resume" | "stop") =>
    request<void>(`${desk(id)}/agent/${action}`, { method: "POST" }),
  eventsUrl: (id: string) => `${desk(id)}/events`,

  settings: (id: string) => request<DeskSettings>(`${desk(id)}/settings`),
  outputs: (id: string) => request<Output[]>(`${desk(id)}/outputs`),
  projects: (id: string) => request<Project[]>(`${desk(id)}/projects`),
  projectDiff: (id: string, name: string) =>
    request<{ diff: string }>(`${desk(id)}/projects/${encodeURIComponent(name)}/diff`),
  mergeProject: (id: string, name: string) =>
    request<void>(`${desk(id)}/projects/${encodeURIComponent(name)}/merge`, { method: "POST" }),
  outputUrl: (id: string, path: string, mode?: "download" | "preview") =>
    `${desk(id)}/outputs/${path.split("/").map(encodeURIComponent).join("/")}${mode ? `?${mode}=1` : ""}`,
  allowHost: (id: string, host: string) =>
    request<void>(`${desk(id)}/network`, { method: "POST", body: JSON.stringify({ host }) }),
  setAllowAll: (id: string, on: boolean) =>
    request<void>(`${desk(id)}/network/allow-all`, { method: "PUT", body: JSON.stringify({ on }) }),
  removeHost: (id: string, host: string) =>
    request<void>(`${desk(id)}/network/${encodeURIComponent(host)}`, { method: "DELETE" }),
  logins: () => request<Login[]>("/api/logins"),
  changeLogin: (id: string, l: Login, action: "revoke" | "allow") =>
    request<void>(`${desk(id)}/logins/${l.kind}/${encodeURIComponent(l.name)}/${action}`, { method: "POST" }),

  vault: () => request<VaultStatus>("/api/vault"),
  connectVault: (email: string, password: string) =>
    request<void>("/api/vault/connect", { method: "POST", body: JSON.stringify({ email, password }) }),
  disconnectVault: () => request<void>("/api/vault/disconnect", { method: "POST" }),
  integrations: () => request<Integration[]>("/api/integrations"),
  addIntegration: (i: { name: string; site: string; username: string; password: string; totpSecret: string }) =>
    request<Integration>("/api/integrations", { method: "POST", body: JSON.stringify(i) }),
  deleteIntegration: (id: string) => request<void>(`/api/integrations/${encodeURIComponent(id)}`, { method: "DELETE" }),

  workspace: () => request<Workspace>("/api/workspace"),
  setMainDesk: (id: string) =>
    request<void>("/api/workspace/main-desk", { method: "PUT", body: JSON.stringify({ id }) }),
  setRouter: (router: Workspace["router"]) =>
    request<void>("/api/workspace/router", { method: "PUT", body: JSON.stringify(router) }),
  setConnection: (name: string, value: string) =>
    request<void>(`/api/workspace/connections/${encodeURIComponent(name)}`, {
      method: "PUT",
      body: JSON.stringify({ value }),
    }),

  decideAccess: (id: string, requestId: string, decision: AccessDecision) =>
    request<void>(`${desk(id)}/access/${encodeURIComponent(requestId)}`, {
      method: "POST",
      body: JSON.stringify({ decision }),
    }),
};
