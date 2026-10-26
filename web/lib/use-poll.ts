"use client";

import { useCallback, useEffect, useRef, useState } from "react";

type PollState<T> = { data?: T; error?: unknown };

// Loads data now and every intervalMs, and returns it with a reload() for after mutations.
// State is only set from promise callbacks, never synchronously inside the effect.
export function usePoll<T>(load: () => Promise<T>, intervalMs: number) {
  const [state, setState] = useState<PollState<T>>({});
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  }, [load]);

  const reload = useCallback(
    () =>
      loadRef.current().then(
        (data) => setState({ data }),
        (error) => setState((s) => ({ ...s, error })),
      ),
    [],
  );

  useEffect(() => {
    reload();
    const t = setInterval(reload, intervalMs);
    return () => clearInterval(t);
  }, [reload, intervalMs]);

  return { ...state, reload };
}
