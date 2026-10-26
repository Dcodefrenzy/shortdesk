"use client";

import Link from "next/link";
import type { VaultStatus } from "@/lib/api";
import { Card, ErrorNote } from "./ui";

// Status of the vault that holds credentials. Shortdesk sets it up and manages it.
export function VaultCard({ vault }: { vault: VaultStatus }) {
  return (
    <Card className="p-4">
      <h2 className="font-semibold">Vault</h2>
      <p className="mt-0.5 mb-3 text-sm text-muted">
        Credentials are kept in an encrypted vault (Vaultwarden, a self-hosted Bitwarden) on this machine. Shortdesk
        sets it up and manages it; agents never see the passwords.
      </p>
      {!vault.running ? (
        <ErrorNote message="The vault isn't running. Restart shortdesk serve (Docker must be running)." />
      ) : vault.connected ? (
        <p className="text-sm">
          <span className="text-ok">● Ready</span>{" "}
          <span className="text-muted">
            · manage logins on the{" "}
            <Link href="/credentials/" className="underline">
              Credentials
            </Link>{" "}
            page
          </span>
        </p>
      ) : (
        <ErrorNote message="The vault is running but not set up yet. Restart shortdesk serve to finish setting it up." />
      )}
    </Card>
  );
}
