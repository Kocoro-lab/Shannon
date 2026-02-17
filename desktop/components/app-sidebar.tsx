"use client";

import Link from "next/link";
import Image from "next/image";
import { usePathname, useSearchParams } from "next/navigation";
import { Plus, History, Sparkles, Microscope, Bot, CalendarClock, Settings, LogOut } from "lucide-react";
import { logout, getStoredUser } from "@/lib/auth";
import { ThemeToggle } from "@/components/theme-toggle";
import { useEffect, useState, Suspense, useCallback, useRef } from "react";
import { useSelector } from "react-redux";
import { RootState } from "@/lib/store";
import { listSessions, Session } from "@/lib/shannon/api";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  useSidebar,
} from "@/components/ui/sidebar";

function SidebarInner() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const currentSessionId = searchParams.get("session_id");
  const [recentSessions, setRecentSessions] = useState<Session[]>([]);
  const lastKnownSessionIdRef = useRef<string | null>(null);
  const prevStatusRef = useRef<string | null>(null);
  const { isMobile, setOpenMobile } = useSidebar();
  
  // Subscribe to run status for auto-refresh on task completion
  const runStatus = useSelector((state: RootState) => state.run.status);
  // Subscribe to session title from streaming events (title now generated at start of task)
  const streamingTitle = useSelector((state: RootState) => state.run.sessionTitle);

  // Close sidebar on mobile after navigation
  const handleNavClick = useCallback(() => {
    if (isMobile) {
      setOpenMobile(false);
    }
  }, [isMobile, setOpenMobile]);

  // Fetch recent sessions
  const fetchRecent = useCallback(async () => {
    try {
      const data = await listSessions(10, 0);
      setRecentSessions(data.sessions || []);
    } catch (error) {
      console.error("Failed to fetch recent sessions:", error);
    }
  }, []);

  // Fetch on mount
  useEffect(() => {
    fetchRecent();
  }, [fetchRecent]);

  // Refresh when navigating to a new session (e.g., after creating a task)
  // This detects when currentSessionId changes to a value not in our list
  useEffect(() => {
    if (!currentSessionId || currentSessionId === "new") return;
    if (currentSessionId === lastKnownSessionIdRef.current) return;
    
    lastKnownSessionIdRef.current = currentSessionId;
    
    // Check if this session is already in our list
    const sessionExists = recentSessions.some(s => s.session_id === currentSessionId);
    if (!sessionExists) {
      // New session detected, refresh the list after a short delay
      // (give the backend time to persist the session)
      const timer = setTimeout(() => fetchRecent(), 1000);
      return () => clearTimeout(timer);
    }
  }, [currentSessionId, recentSessions, fetchRecent]);

  // Auto-refresh when a task completes (to update title)
  useEffect(() => {
    // Detect transition from running to completed
    if (prevStatusRef.current === "running" && runStatus === "completed") {
      // Delay refresh to allow backend to update session title
      const timer = setTimeout(() => fetchRecent(), 1500);
      return () => clearTimeout(timer);
    }
    prevStatusRef.current = runStatus;
  }, [runStatus, fetchRecent]);

  // Update sidebar immediately when streaming title arrives (title now generated at task start)
  // Re-run when recentSessions changes to handle case where title arrives before session is loaded
  useEffect(() => {
    if (!streamingTitle || !currentSessionId || currentSessionId === "new") return;
    
    // Check if current session exists and needs title update
    const session = recentSessions.find(s => s.session_id === currentSessionId);
    if (session && !session.title) {
      // Update the title in local state for immediate UI feedback
      setRecentSessions(prev => prev.map(s => 
        s.session_id === currentSessionId
          ? { ...s, title: streamingTitle }
          : s
      ));
    }
  }, [streamingTitle, currentSessionId, recentSessions]);

  const routes = [
    {
      label: "New Task",
      icon: Plus,
      href: "/run-detail?session_id=new",
      active: pathname.startsWith("/run-detail") && currentSessionId === "new",
    },
    {
      label: "My Agents",
      icon: Bot,
      href: "/agents",
      active: pathname.startsWith("/agents"),
    },
    {
      label: "Schedules",
      icon: CalendarClock,
      href: "/schedules",
      active: pathname.startsWith("/schedules"),
    },
    {
      label: "Settings",
      icon: Settings,
      href: "/settings",
      active: pathname.startsWith("/settings"),
    },
  ];

  return (
    <Sidebar>
      <SidebarHeader>
        <Link href="/run-detail?session_id=new" onClick={handleNavClick} className="flex items-center gap-2 px-2 py-2 hover:opacity-80 transition-opacity">
          <Image 
            src="/app-icon.png" 
            alt="Shannon Agents" 
            width={28} 
            height={28}
            className="rounded-md"
            onError={(e) => {
              // Hide image on error, text will show
              e.currentTarget.style.display = 'none';
            }}
          />
          <h2 className="text-lg font-semibold tracking-tight">
            Shannon
          </h2>
        </Link>
      </SidebarHeader>
      <SidebarContent>
        <SidebarMenu>
          {routes.map((route) => (
            <SidebarMenuItem key={route.href}>
              <SidebarMenuButton asChild isActive={route.active}>
                <Link href={route.href} onClick={handleNavClick}>
                  <route.icon />
                  <span>{route.label}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>

        {recentSessions.length > 0 && (
          <SidebarGroup className="mt-4">
            <div className="flex items-center justify-between px-2">
              <SidebarGroupLabel className="text-xs text-muted-foreground p-0">
                Recents
              </SidebarGroupLabel>
              <Link
                href="/runs"
                onClick={handleNavClick}
                className="h-5 w-5 flex items-center justify-center rounded-md hover:bg-muted transition-colors"
                title="View all sessions"
              >
                <History className="h-3 w-3 text-muted-foreground" />
              </Link>
            </div>
            <SidebarGroupContent>
              <SidebarMenu>
                {recentSessions.map((session) => {
                  const isActive = currentSessionId === session.session_id;
                  const isResearch = session.is_research_session;
                  // Friendly display: prefer title, else truncated query, else "New task..."
                  const truncatedQuery = session.latest_task_query 
                    ? (session.latest_task_query.length > 30 
                        ? session.latest_task_query.slice(0, 30) + "..." 
                        : session.latest_task_query)
                    : null;
                  const displayTitle = session.title || truncatedQuery || "New task...";
                  return (
                    <SidebarMenuItem key={session.session_id}>
                      <SidebarMenuButton asChild isActive={isActive} className="h-auto py-1.5">
                        <Link href={`/run-detail?session_id=${session.session_id}`} onClick={handleNavClick}>
                          {isResearch ? (
                            <Microscope className="h-3.5 w-3.5 text-violet-500 shrink-0" />
                          ) : (
                            <Sparkles className="h-3.5 w-3.5 text-amber-500 shrink-0" />
                          )}
                          <span className={`truncate text-sm ${!session.title ? 'text-muted-foreground' : ''}`}>
                            {displayTitle}
                          </span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>
      <SidebarFooter>
        <div className="space-y-2">
          <div className="flex items-center justify-between px-2 py-2">
            <span className="text-sm">Theme</span>
            <ThemeToggle />
          </div>
          {/* Show logout only if authenticated (not using dev X-User-Id) */}
          {!process.env.NEXT_PUBLIC_USER_ID && getStoredUser() && (
            <button
              onClick={logout}
              className="flex w-full items-center gap-2 px-2 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-muted rounded-md transition-colors"
            >
              <LogOut className="h-4 w-4" />
              <span>Sign out</span>
            </button>
          )}
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}

export function AppSidebar() {
  return (
    <Suspense fallback={<Sidebar><SidebarContent /></Sidebar>}>
      <SidebarInner />
    </Suspense>
  );
}
