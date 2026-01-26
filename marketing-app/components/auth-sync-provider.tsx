"use client";

import { useAuthSync } from "@/lib/hooks/useAuthSync";

export function AuthSyncProvider({ children }: { children: React.ReactNode }) {
  useAuthSync();
  return <>{children}</>;
}
