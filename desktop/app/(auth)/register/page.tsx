"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";

// OSS mode: Skip registration, redirect to run page
export default function RegisterPage() {
    const router = useRouter();

    useEffect(() => {
        router.replace("/run-detail?session_id=new");
    }, [router]);

    return (
        <div className="min-h-screen flex items-center justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
    );
}
