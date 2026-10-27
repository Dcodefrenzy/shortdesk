// The owner's light/dark choice, kept in this browser. "auto" follows the computer.
export type Theme = "auto" | "light" | "dark";

const KEY = "shortdesk-theme";
const listeners = new Set<() => void>();

export function getTheme(): Theme {
  try {
    const t = localStorage.getItem(KEY);
    return t === "light" || t === "dark" ? t : "auto";
  } catch {
    return "auto";
  }
}

export function setTheme(t: Theme) {
  try {
    if (t === "auto") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, t);
  } catch {}
  applyTheme(t);
  listeners.forEach((l) => l());
}

export function subscribeTheme(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

function applyTheme(t: Theme) {
  if (t === "auto") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = t;
}

// Runs before the page paints (see app/layout.tsx), so a saved choice never flashes.
export const themeScript = `try{var t=localStorage.getItem("${KEY}");if(t==="light"||t==="dark")document.documentElement.dataset.theme=t}catch(e){}`;
