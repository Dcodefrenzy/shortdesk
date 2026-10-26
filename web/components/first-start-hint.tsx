"use client";

import { api } from "@/lib/api";
import { usePoll } from "@/lib/use-poll";
import { Card } from "./ui";

// Until something is logged in somewhere, new desks start logged out. Explain once.
export function FirstStartHint() {
  const { data: logins } = usePoll(api.logins, 30000);
  if (!logins || logins.length > 0) return null;
  return (
    <Card className="p-4 text-sm">
      <p className="font-medium">Log in once, every desk is logged in</p>
      <p className="mt-2 text-muted">
        Start a desk and log into what your agents need (websites, apps, Claude Code) in its Desktop tab. Shortdesk
        saves those logins automatically and gives them to every desk, including ones that are already running.
      </p>
    </Card>
  );
}
