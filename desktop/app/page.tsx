"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { isAuthenticated } from "@/lib/auth";
import { Loader2 } from "lucide-react";

export default function Home() {
    const router = useRouter();

    useEffect(() => {
        // Allow bypass with NEXT_PUBLIC_USER_ID for local dev
        const devUserId = process.env.NEXT_PUBLIC_USER_ID;

        if (devUserId || isAuthenticated()) {
            router.replace("/run-detail?session_id=new");
        } else {
            router.replace("/login");
        }
    }, [router]);

    return (
        <div className="min-h-screen flex items-center justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
    );
}
