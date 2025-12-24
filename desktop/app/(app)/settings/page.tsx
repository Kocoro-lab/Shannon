"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { getStoredUser, getAPIKey, logout, StoredUser } from "@/lib/auth";
import { getCurrentUser, MeResponse } from "@/lib/shannon/api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
    Key,
    Copy,
    Check,
    Activity,
    User,
    Loader2,
    LogOut,
    Shield,
    Zap,
    Crown,
    AlertCircle,
} from "lucide-react";

type Tier = "free" | "pro" | "enterprise" | "";

export default function SettingsPage() {
    const router = useRouter();
    const [storedUser, setStoredUser] = useState<StoredUser | null>(null);
    const [userInfo, setUserInfo] = useState<MeResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [copiedKey, setCopiedKey] = useState(false);

    useEffect(() => {
        async function loadUserData() {
            try {
                // Get locally stored user info
                const user = getStoredUser();
                setStoredUser(user);

                // Try to fetch fresh user info from backend
                const apiKey = getAPIKey();
                if (apiKey || process.env.NEXT_PUBLIC_USER_ID) {
                    try {
                        const data = await getCurrentUser();
                        setUserInfo(data);
                    } catch (err) {
                        console.error("Failed to fetch user info from backend:", err);
                        // Continue with stored user info
                    }
                }
            } catch (err) {
                console.error("Failed to load user data:", err);
                setError("Failed to load settings");
            } finally {
                setLoading(false);
            }
        }

        loadUserData();
    }, []);

    const apiKey = getAPIKey();
    const maskedApiKey = apiKey
        ? `${apiKey.substring(0, 8)}...${apiKey.substring(apiKey.length - 4)}`
        : null;

    const copyApiKey = async () => {
        if (apiKey) {
            await navigator.clipboard.writeText(apiKey);
            setCopiedKey(true);
            setTimeout(() => setCopiedKey(false), 2000);
        }
    };

    const handleLogout = () => {
        logout();
        router.push("/login");
    };

    const getTierInfo = (t: Tier) => {
        switch (t) {
            case "enterprise":
                return {
                    label: "Enterprise",
                    icon: Crown,
                    color: "bg-amber-500/10 text-amber-500 border-amber-500/20",
                };
            case "pro":
                return {
                    label: "Pro",
                    icon: Zap,
                    color: "bg-violet-500/10 text-violet-500 border-violet-500/20",
                };
            default:
                return {
                    label: "Free",
                    icon: Shield,
                    color: "bg-muted text-muted-foreground border-border",
                };
        }
    };

    const tier: Tier = (userInfo?.tier || storedUser?.tier || "free") as Tier;
    const tierInfo = getTierInfo(tier);
    const TierIcon = tierInfo.icon;

    if (loading) {
        return (
            <div className="flex-1 flex items-center justify-center">
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
        );
    }

    if (error) {
        return (
            <div className="flex-1 flex items-center justify-center p-6">
                <Card className="max-w-md w-full">
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2 text-destructive">
                            <AlertCircle className="h-5 w-5" />
                            Error
                        </CardTitle>
                        <CardDescription>{error}</CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Button
                            onClick={() => window.location.reload()}
                            className="w-full"
                        >
                            Try Again
                        </Button>
                    </CardContent>
                </Card>
            </div>
        );
    }

    const displayName = userInfo?.name || storedUser?.name || userInfo?.username || storedUser?.username || "User";
    const displayEmail = userInfo?.email || storedUser?.email || "";

    return (
        <div className="flex-1 overflow-auto p-6">
            <div className="max-w-2xl mx-auto space-y-6">
                <div>
                    <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
                    <p className="text-muted-foreground">
                        Manage your account and API access.
                    </p>
                </div>

                {/* User Profile Card */}
                <Card>
                    <CardHeader className="pb-3">
                        <div className="flex items-center gap-4">
                            <div className="h-12 w-12 rounded-full bg-muted flex items-center justify-center">
                                <User className="h-6 w-6 text-muted-foreground" />
                            </div>
                            <div className="flex-1">
                                <CardTitle className="text-lg">{displayName}</CardTitle>
                                <CardDescription>{displayEmail}</CardDescription>
                            </div>
                            <div className="flex items-center gap-2">
                                <Badge variant="outline" className={tierInfo.color}>
                                    <TierIcon className="h-3 w-3 mr-1" />
                                    {tierInfo.label}
                                </Badge>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    onClick={handleLogout}
                                    className="text-muted-foreground hover:text-destructive"
                                >
                                    <LogOut className="h-4 w-4" />
                                </Button>
                            </div>
                        </div>
                    </CardHeader>
                </Card>

                {/* API Key Card */}
                <Card>
                    <CardHeader>
                        <CardTitle className="text-lg flex items-center gap-2">
                            <Key className="h-4 w-4" />
                            API Key
                        </CardTitle>
                        <CardDescription>
                            Your API key for authenticating requests.
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        {maskedApiKey ? (
                            <div className="flex items-center gap-2">
                                <Input
                                    value={maskedApiKey}
                                    readOnly
                                    className="font-mono text-sm"
                                />
                                <Button
                                    variant="outline"
                                    size="icon"
                                    onClick={copyApiKey}
                                >
                                    {copiedKey ? (
                                        <Check className="h-4 w-4 text-green-500" />
                                    ) : (
                                        <Copy className="h-4 w-4" />
                                    )}
                                </Button>
                            </div>
                        ) : (
                            <p className="text-sm text-muted-foreground">
                                No API key found. Please log out and register again to get a new API key.
                            </p>
                        )}
                    </CardContent>
                </Card>

                {/* Rate Limits Card */}
                {userInfo?.rate_limits && (
                    <Card>
                        <CardHeader>
                            <CardTitle className="text-lg flex items-center gap-2">
                                <Activity className="h-4 w-4" />
                                Rate Limits
                            </CardTitle>
                            <CardDescription>
                                Request limits to ensure fair usage.
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            <div className="grid grid-cols-2 gap-4">
                                <div className="space-y-1">
                                    <div className="text-sm text-muted-foreground">Per Minute</div>
                                    <div className="text-2xl font-semibold">
                                        {userInfo.rate_limits.minute?.remaining ?? 0}
                                        <span className="text-base font-normal text-muted-foreground">
                                            {" "}/ {userInfo.rate_limits.minute?.limit ?? 0}
                                        </span>
                                    </div>
                                </div>
                                <div className="space-y-1">
                                    <div className="text-sm text-muted-foreground">Per Hour</div>
                                    <div className="text-2xl font-semibold">
                                        {userInfo.rate_limits.hour?.remaining ?? 0}
                                        <span className="text-base font-normal text-muted-foreground">
                                            {" "}/ {userInfo.rate_limits.hour?.limit ?? 0}
                                        </span>
                                    </div>
                                </div>
                            </div>
                        </CardContent>
                    </Card>
                )}
            </div>
        </div>
    );
}
