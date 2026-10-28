"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useState, useSyncExternalStore } from "react";
import { AgentPhone } from "@/components/phone/phone-app";
import { useAuthRedirect } from "@/lib/use-auth-redirect";
import { api } from "@/lib/api";
import { usePoll } from "@/lib/use-poll";

// The agent's phone, as the Phone Desk serves it (at /phone/, through Shortdesk): what a
// link opens (/phone/?app=tasks), and what the dashboard shows beside the chat
// (/phone/?embed=1). On a real phone it fills the screen; on a big screen it shows as a
// phone. Routes here are relative to /phone (the build's base path).
export function PhonePage() {
  return (
    <Suspense fallback={null}>
      <Standalone />
    </Suspense>
  );
}

const noSubscribe = () => () => {};
const isSmall = () => window.innerWidth < 520;

function Standalone() {
  const router = useRouter();
  const params = useSearchParams();
  const embedded = params.get("embed") === "1";
  const small = useSyncExternalStore(noSubscribe, isSmall, () => false);
  const [fileSel, setFileSel] = useState<string | null>(params.get("path"));
  const { error } = usePoll(api.session, 60000);
  useAuthRedirect(error);

  const open = useCallback(
    (id: string | null) => {
      const q = new URLSearchParams();
      if (embedded) q.set("embed", "1");
      if (id) q.set("app", id);
      router.replace(`/${q.size ? "?" + q : ""}`);
    },
    [router, embedded],
  );
  // Inside the dashboard, desk and chat actions go to it; on their own they open it.
  const tell = (msg: object) => {
    if (embedded && window.parent !== window) window.parent.postMessage({ shortdesk: 1, ...msg }, location.origin);
    // The dashboard is outside the phone's base path, so this is a full page load.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    else window.location.assign("/");
  };

  return (
    <main className={`flex min-h-dvh items-center justify-center ${small && !embedded ? "" : "p-4"} ${embedded ? "bg-transparent" : "bg-background"}`}>
      <AgentPhone
        appId={params.get("app")}
        open={open}
        watch={(id) => tell({ watch: id })}
        chat={() => tell({ chat: true })}
        fileSel={fileSel}
        setFileSel={setFileSel}
        bare={small && !embedded}
      />
    </main>
  );
}
