'use client';

import React from 'react';
import { Provider } from 'react-redux';
import { PersistGate } from 'redux-persist/integration/react';
import { ThemeProvider as NextThemesProvider } from 'next-themes';
import { store, persistor } from '@/lib/store';
import { ServerProvider } from '@/lib/server-context';
import { ServerStatusBanner } from '@/components/server-status-banner';
import { DebugConsoleWrapper } from '@/components/debug-console-wrapper';

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <Provider store={store}>
      <PersistGate loading={null} persistor={persistor}>
        <NextThemesProvider
          attribute="class"
          defaultTheme="dark"
          enableSystem
          disableTransitionOnChange
        >
          <ServerProvider>
            <ServerStatusBanner />
            {children}
            <DebugConsoleWrapper />
          </ServerProvider>
        </NextThemesProvider>
      </PersistGate>
    </Provider>
  );
}
