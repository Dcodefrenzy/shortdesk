"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { UnauthorizedError } from "./api";

// Sends the user to /login when error is a 401, and returns a message for any other error.
export function useAuthRedirect(error: unknown): string {
  const router = useRouter();
  const unauthorized = error instanceof UnauthorizedError;
  useEffect(() => {
    if (unauthorized) router.replace("/login/");
  }, [unauthorized, router]);
  return error && !unauthorized ? String((error as Error).message ?? error) : "";
}
