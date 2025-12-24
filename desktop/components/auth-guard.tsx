"use client";

interface AuthGuardProps {
    children: React.ReactNode;
}

// OSS mode: No authentication required, pass through directly
export function AuthGuard({ children }: AuthGuardProps) {
    return <>{children}</>;
}
