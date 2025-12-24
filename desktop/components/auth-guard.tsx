"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { isAuthenticated } from "@/lib/auth";
import { Loader2 } from "lucide-react";

interface AuthGuardProps {
    children: React.ReactNode;
}

export function AuthGuard({ children }: AuthGuardProps) {
    const router = useRouter();
    const [isLoading, setIsLoading] = useState(true);
    const [isAuthed, setIsAuthed] = useState(false);

    useEffect(() => {
        // Check auth state on mount
        const checkAuth = () => {
            // Allow bypass with NEXT_PUBLIC_USER_ID for local dev
            const devUserId = process.env.NEXT_PUBLIC_USER_ID;
            if (devUserId) {
                setIsAuthed(true);
                setIsLoading(false);
                return;
            }

            if (isAuthenticated()) {
                setIsAuthed(true);
            } else {
                router.replace("/login");
            }
            setIsLoading(false);
        };

        checkAuth();
    }, [router]);

    if (isLoading) {
        return (
            <div className="min-h-screen flex items-center justify-center">
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
        );
    }

    if (!isAuthed) {
        return null;
    }

    return <>{children}</>;
}
