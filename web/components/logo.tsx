// The Shortdesk mark (a desk with a screen, and a spark for the agent at work) and wordmark.
export function LogoMark({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden className="shrink-0">
      <rect width="32" height="32" rx="7.5" className="fill-accent" />
      <g fill="none" className="stroke-accent-foreground" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
        <rect x="7" y="7.5" width="18" height="12.5" rx="2.2" />
        <path d="M16 20v4.5M9 24.5h14" />
      </g>
      <path
        className="fill-accent-foreground"
        d="M16 10.2c.35 1.75 1.05 2.45 2.8 2.8-1.75.35-2.45 1.05-2.8 2.8-.35-1.75-1.05-2.45-2.8-2.8 1.75-.35 2.45-1.05 2.8-2.8z"
      />
    </svg>
  );
}

export function Logo({ size = 24 }: { size?: number }) {
  return (
    <span className="inline-flex items-center gap-2">
      <LogoMark size={size} />
      <span className="font-semibold tracking-tight">shortdesk</span>
    </span>
  );
}
