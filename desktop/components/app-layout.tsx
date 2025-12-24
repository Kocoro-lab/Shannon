"use client";

import { AppSidebar } from "@/components/app-sidebar";
import { AuthGuard } from "@/components/auth-guard";
import { SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";

export function AppLayout({ children }: { children: React.ReactNode }) {
    return (
        <AuthGuard>
            <SidebarProvider>
                <div className="flex h-screen w-full overflow-hidden bg-background">
                    <AppSidebar />
                    <main className="flex-1 flex flex-col overflow-y-auto">
                        <div className="flex items-center gap-2 border-b bg-background px-4 py-2 shrink-0 sticky top-0 z-10">
                            <SidebarTrigger className="cursor-pointer" />
                        </div>
                        <div className="flex-1 min-h-0 overflow-hidden">
                            {children}
                        </div>
                    </main>
                </div>
            </SidebarProvider>
        </AuthGuard>
    );
}
