"use client";

import { SessionProvider } from "next-auth/react";
import { Provider } from "react-redux";
import { PersistGate } from "redux-persist/integration/react";
import { store, persistor } from "@/lib/store";
import { AuthSyncProvider } from "@/components/auth-sync-provider";

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <SessionProvider>
      <Provider store={store}>
        <PersistGate loading={null} persistor={persistor}>
          <AuthSyncProvider>
            {children}
          </AuthSyncProvider>
        </PersistGate>
      </Provider>
    </SessionProvider>
  );
}
