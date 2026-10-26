"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { DeskWorkspace } from "@/components/desk-workspace";
import { Sidebar } from "@/components/sidebar";
import { StartDesk } from "@/components/start-desk";
import { ErrorNote } from "@/components/ui";
import { api } from "@/lib/api";
import { useAuthRedirect } from "@/lib/use-auth-redirect";
import { usePoll } from "@/lib/use-poll";
import { FirstStartHint } from "@/components/first-start-hint";

const REFRESH_MS = 5000;

// The dashboard is a static export, so the selected desk lives in the query string
// (/?desk=<id>) rather than in a dynamic route.
export default function Home() {
  return (
    <Suspense fallback={null}>
      <Workspace />
    </Suspense>
  );
}

function Workspace() {
  const params = useSearchParams();
  const selectedId = params.get("desk") ?? "";
  const { data: desks, error, reload } = usePoll(api.desks, REFRESH_MS);
  const loadError = useAuthRedirect(error);
  const desk = desks?.find((d) => d.id === selectedId);

  return (
    <div className="flex min-h-screen flex-col md:flex-row">
      <Sidebar desks={desks} selectedId={selectedId} />
      {desk ? (
        <DeskWorkspace key={desk.id} desk={desk} onRemoved={reload} />
      ) : (
        <main className="flex flex-1 items-start justify-center overflow-y-auto px-4 py-12">
          <div className="w-full max-w-md space-y-4">
            {loadError && <ErrorNote message={loadError} />}
            {selectedId && desks && !desk && <ErrorNote message="That desk no longer exists." />}
            <FirstStartHint />
            <StartDesk onStarted={reload} />
          </div>
        </main>
      )}
    </div>
  );
}
