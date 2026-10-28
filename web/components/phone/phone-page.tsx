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

// A phone-sized screen (or a touch tablet held narrow): the phone fills it like a
// normal app. Anything bigger shows the phone in its frame. Follows rotation and
// window resizing.
const phoneScreen = "(max-width: 600px), (pointer: coarse) and (max-width: 900px)";
function subscribeScreen(cb: () => void) {
  const mq = matchMedia(phoneScreen);
  mq.addEventListener("change", cb);
  return () => mq.removeEventListener("change", cb);
}
const isPhoneScreen = () => matchMedia(phoneScreen).matches;

function Standalone() {
  const router = useRouter();
  const params = useSearchParams();
  const embedded = params.get("embed") === "1";
  const small = useSyncExternalStore(subscribeScreen, isPhoneScreen, () => false);
  const [fileSel, setFileSel] = useState<string | null>(params.get("path"));
  const { data: session, error } = usePoll(api.session, 60000);
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
    <main
      className={`flex min-h-dvh items-center justify-center ${small && !embedded ? "bg-[#0b0f0e]" : "p-4"} ${
        embedded ? "bg-transparent" : small ? "" : "bg-background"
      }`}
    >
      <AgentPhone
        appId={params.get("app")}
        open={open}
        watch={(id) => tell({ watch: id })}
        chat={() => tell({ chat: true })}
        fileSel={fileSel}
        setFileSel={setFileSel}
        bare={small && !embedded}
        desk={!!session?.desk}
      />
    </main>
  );
}
