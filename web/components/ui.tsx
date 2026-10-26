// Small shared UI pieces.
import type { ButtonHTMLAttributes, ReactNode } from "react";
import type { AgentState } from "@/lib/api";

type Variant = "primary" | "secondary" | "danger" | "ghost";

const variants: Record<Variant, string> = {
  primary: "bg-accent text-accent-foreground hover:opacity-90",
  secondary: "border border-border bg-surface hover:bg-background",
  danger: "border border-border bg-surface text-danger hover:bg-background",
  ghost: "text-muted hover:bg-background hover:text-foreground",
};

export function Button({
  variant = "secondary",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }) {
  return (
    <button
      className={`inline-flex items-center justify-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50 ${variants[variant]} ${className}`}
      {...props}
    />
  );
}

const agentTone: Record<AgentState, { label: string; dot: string }> = {
  working: { label: "Agent working", dot: "bg-ok animate-pulse" },
  paused: { label: "You're in control", dot: "bg-warn" },
  idle: { label: "Idle", dot: "bg-muted/50" },
};

export function AgentDot({ state }: { state: AgentState }) {
  return <span className={`size-2 shrink-0 rounded-full ${agentTone[state].dot}`} title={agentTone[state].label} />;
}

export function AgentBadge({ state }: { state: AgentState }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted">
      <AgentDot state={state} />
      {agentTone[state].label}
    </span>
  );
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-lg border border-border bg-surface ${className}`}>{children}</div>;
}

export function Spinner() {
  return <span className="size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" aria-hidden />;
}

export function ErrorNote({ message }: { message: string }) {
  return (
    <p role="alert" className="rounded-md border border-danger/30 bg-danger/5 px-3 py-2 text-sm text-danger">
      {message}
    </p>
  );
}
