"use client";

// AI connections and the router: which models every desk's agent uses.
import { useState } from "react";
import { api, type RouterMode, type Workspace } from "@/lib/api";
import { Button, Card, ErrorNote, Spinner } from "./ui";

const connectionInfo: Record<string, { title: string; how: string; placeholder: string }> = {
  claude_oauth_token: {
    title: "Claude subscription",
    how: "Run `claude setup-token` on your own computer and paste the token. Uses your Claude plan's usage.",
    placeholder: "sk-ant-oat01-…",
  },
  anthropic_api_key: {
    title: "Anthropic API key",
    how: "Pay-per-use key from console.anthropic.com. Used when no subscription token is set.",
    placeholder: "sk-ant-api03-…",
  },
  gemini_api_key: {
    title: "Gemini (your key)",
    how: "Free-tier key from aistudio.google.com. Used through Free Claude Code when the router picks your own models.",
    placeholder: "AIza…",
  },
  deepseek_api_key: {
    title: "DeepSeek (your key)",
    how: "Key from platform.deepseek.com, billed by DeepSeek. Used through Free Claude Code.",
    placeholder: "sk-…",
  },
  groq_api_key: {
    title: "Groq (your key)",
    how: "Free-tier key from console.groq.com. Used through Free Claude Code.",
    placeholder: "gsk_…",
  },
  nvidia_nim_api_key: {
    title: "NVIDIA NIM (your key)",
    how: "Free key from build.nvidia.com. Used through Free Claude Code.",
    placeholder: "nvapi-…",
  },
};

export function Connections({ ws, onChange }: { ws: Workspace; onChange: () => void }) {
  return (
    <Card className="p-4">
      <h2 className="font-semibold">AI connections</h2>
      <p className="mt-0.5 mb-4 text-sm text-muted">Every desk&apos;s agent is connected automatically.</p>
      <ul className="space-y-4">
        {ws.connections.map((c) => (
          <ConnectionRow key={c.name} name={c.name} set={c.set} hint={c.hint} onChange={onChange} />
        ))}
      </ul>
    </Card>
  );
}

function ConnectionRow({ name, set, hint, onChange }: { name: string; set: boolean; hint?: string; onChange: () => void }) {
  const info = connectionInfo[name] ?? { title: name, how: "", placeholder: "" };
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(v: string) {
    setBusy(true);
    setError("");
    try {
      await api.setConnection(name, v);
      setValue("");
      onChange();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="font-medium">{info.title}</span>
        {set ? (
          <span className="text-xs text-ok">● Connected {hint}</span>
        ) : (
          <span className="text-xs text-muted">Not connected</span>
        )}
      </div>
      <p className="text-xs text-muted">{info.how}</p>
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (value.trim()) save(value.trim());
        }}
      >
        <input
          type="password"
          autoComplete="off"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={set ? "Paste a new value to replace it" : info.placeholder}
          className="min-w-0 flex-1 basis-40 rounded-md border border-border bg-background px-3 py-1.5 font-mono text-sm outline-none focus:border-accent"
        />
        <Button type="submit" variant="primary" disabled={busy || !value.trim()}>
          {busy ? <Spinner /> : set ? "Replace" : "Connect"}
        </Button>
        {set && (
          <Button variant="danger" disabled={busy} onClick={() => save("")}>
            Remove
          </Button>
        )}
      </form>
      {error && <ErrorNote message={error} />}
    </li>
  );
}

const modes: { value: RouterMode; title: string; detail: string }[] = [
  {
    value: "auto",
    title: "Claude, then your own models",
    detail: "Use Claude. If it hits its usage limit, the task continues on your own models (below).",
  },
  { value: "claude", title: "Claude only", detail: "Never use your own models." },
  { value: "own", title: "Your own models only", detail: "Always use your own models through Free Claude Code." },
];

export function RouterSettings({ ws, onChange }: { ws: Workspace; onChange: () => void }) {
  const [mode, setMode] = useState<RouterMode>(ws.router.mode);
  const [models, setModels] = useState((ws.router.models.length ? ws.router.models : ws.ownModels).join("\n"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.setRouter({ mode, models: models.split("\n").map((m) => m.trim()).filter(Boolean) });
      setSaved(true);
      onChange();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="p-4">
      <h2 className="font-semibold">Router</h2>
      <p className="mt-0.5 mb-4 text-sm text-muted">
        Which models agents use. Your own models run through Free Claude Code with your keys above: no middleman,
        no extra fees. Applies to the next task on every desk.
      </p>
      <form onSubmit={save} className="space-y-4">
        <fieldset className="space-y-2">
          <legend className="sr-only">Mode</legend>
          {modes.map((m) => (
            <label
              key={m.value}
              className={`block cursor-pointer rounded-md border p-3 text-sm ${mode === m.value ? "border-accent" : "border-border"}`}
            >
              <input
                type="radio"
                name="mode"
                className="sr-only"
                checked={mode === m.value}
                onChange={() => {
                  setMode(m.value);
                  setSaved(false);
                }}
              />
              <span className="font-medium">{m.title}</span>
              <span className="mt-0.5 block text-xs text-muted">{m.detail}</span>
            </label>
          ))}
        </fieldset>

        <label className="block space-y-1">
          <span className="text-sm font-medium">Your own models, in order</span>
          <span className="block text-xs text-muted">
            One per line, as provider/model. If the first fails, the next is tried. Leave empty to use one default model
            for each key you added.
          </span>
          <textarea
            value={models}
            onChange={(e) => {
              setModels(e.target.value);
              setSaved(false);
            }}
            rows={4}
            placeholder={"gemini/models/gemini-3.1-flash-lite\ndeepseek/deepseek-chat"}
            className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:border-accent"
          />
        </label>
        {ws.ownModels.length === 0 && (
          <p className="text-xs text-warn">Add at least one model key above to use your own models.</p>
        )}

        <div className="flex items-center gap-3">
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? <Spinner /> : "Save"}
          </Button>
          {saved && <span className="text-xs text-ok">Saved</span>}
        </div>
        {error && <ErrorNote message={error} />}
      </form>
    </Card>
  );
}
