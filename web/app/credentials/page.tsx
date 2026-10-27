"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

// Credentials moved into the Vault app on the phone. Old links still work.
export default function Redirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/?app=vault");
  }, [router]);
  return null;
}
