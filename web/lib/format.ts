// Human-friendly times for desk lists.

export function timeLeft(iso?: string, now = Date.now()): string {
  if (!iso) return "never";
  const ms = new Date(iso).getTime() - now;
  if (ms <= 0) return "expired";
  return duration(ms);
}

export function age(iso: string, now = Date.now()): string {
  return duration(Math.max(0, now - new Date(iso).getTime()));
}

function duration(ms: number): string {
  const min = Math.floor(ms / 60000);
  if (min < 1) return "<1m";
  if (min < 60) return `${min}m`;
  const h = Math.floor(min / 60);
  return min % 60 ? `${h}h ${min % 60}m` : `${h}h`;
}
