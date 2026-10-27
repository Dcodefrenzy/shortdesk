"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

// Settings moved into the Settings app on the phone. Old links still work.
export default function Redirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/?app=settings");
  }, [router]);
  return null;
}
