"use client";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";

export function AppLayout({ children }: { children: React.ReactNode }) {
    return (
        <SidebarProvider>
            <div className="flex h-screen w-full overflow-hidden bg-background">
                <AppSidebar />
                <main className="flex-1 flex flex-col overflow-hidden">
                    <div className="flex items-center gap-2 border-b bg-background px-4 py-2 shrink-0">
                        <SidebarTrigger className="cursor-pointer" />
                    </div>
                    <div className="flex-1 overflow-y-auto">
                        {children}
                    </div>
                </main>
            </div>
        </SidebarProvider>
    );
}
