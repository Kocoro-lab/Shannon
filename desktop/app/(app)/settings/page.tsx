"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { getStoredUser, getAPIKey, logout, StoredUser } from "@/lib/auth";
import { getCurrentUser, MeResponse } from "@/lib/shannon/api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
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
    Bot,
    Save,
    Eye,
    EyeOff,
} from "lucide-react";

type Tier = "free" | "pro" | "enterprise" | "";

// Check if running in Tauri
const isTauri = typeof window !== 'undefined' && '__TAURI__' in window;

interface LlmKeyStatus {
    openai_configured: boolean;
    anthropic_configured: boolean;
}

export default function SettingsPage() {
    const router = useRouter();
    const [storedUser, setStoredUser] = useState<StoredUser | null>(null);
    const [userInfo, setUserInfo] = useState<MeResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [copiedKey, setCopiedKey] = useState(false);
    
    // LLM Provider API Keys state
    const [openaiKey, setOpenaiKey] = useState("");
    const [anthropicKey, setAnthropicKey] = useState("");
    const [showOpenaiKey, setShowOpenaiKey] = useState(false);
    const [showAnthropicKey, setShowAnthropicKey] = useState(false);
    const [savingOpenai, setSavingOpenai] = useState(false);
    const [savingAnthropic, setSavingAnthropic] = useState(false);
    const [llmKeyStatus, setLlmKeyStatus] = useState<LlmKeyStatus>({ openai_configured: false, anthropic_configured: false });
    const [saveSuccess, setSaveSuccess] = useState<string | null>(null);

    // Load LLM key status from Tauri
    const loadLlmKeyStatus = async () => {
        if (!isTauri) return;
        
        try {
            const { invoke } = await import('@tauri-apps/api/core');
            const status = await invoke<LlmKeyStatus>('get_api_key_status');
            setLlmKeyStatus(status);
        } catch (err) {
            console.error("Failed to load LLM key status:", err);
        }
    };

    // Save LLM API key via Tauri
    const saveLlmKey = async (provider: 'openai' | 'anthropic', apiKey: string) => {
        if (!isTauri) {
            console.warn("Not running in Tauri - API key saving not available");
            return;
        }
        
        const setSaving = provider === 'openai' ? setSavingOpenai : setSavingAnthropic;
        setSaving(true);
        
        try {
            const { invoke } = await import('@tauri-apps/api/core');
            await invoke('save_api_key', { provider, apiKey });
            
            // Refresh status
            await loadLlmKeyStatus();
            
            // Clear input and show success
            if (provider === 'openai') {
                setOpenaiKey("");
            } else {
                setAnthropicKey("");
            }
            
            setSaveSuccess(`${provider === 'openai' ? 'OpenAI' : 'Anthropic'} API key saved successfully!`);
            setTimeout(() => setSaveSuccess(null), 3000);
        } catch (err) {
            console.error(`Failed to save ${provider} API key:`, err);
            setError(`Failed to save API key: ${err}`);
        } finally {
            setSaving(false);
        }
    };

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
                
                // Load LLM key status if in Tauri
                await loadLlmKeyStatus();
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
        // OSS mode: go back to run page (logout just clears local storage)
        router.push("/run-detail?session_id=new");
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

                {/* LLM Provider API Keys Card - Only show in Tauri/embedded mode */}
                {isTauri && (
                    <Card>
                        <CardHeader>
                            <CardTitle className="text-lg flex items-center gap-2">
                                <Bot className="h-4 w-4" />
                                LLM Provider API Keys
                            </CardTitle>
                            <CardDescription>
                                Configure API keys for AI providers. At least one is required for the app to function.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="space-y-6">
                            {/* Success message */}
                            {saveSuccess && (
                                <div className="flex items-center gap-2 p-3 bg-green-500/10 border border-green-500/20 rounded-lg text-green-600 text-sm">
                                    <Check className="h-4 w-4" />
                                    {saveSuccess}
                                </div>
                            )}
                            
                            {/* OpenAI */}
                            <div className="space-y-3">
                                <div className="flex items-center justify-between">
                                    <Label htmlFor="openai-key" className="text-sm font-medium">
                                        OpenAI API Key
                                    </Label>
                                    {llmKeyStatus.openai_configured && (
                                        <Badge variant="outline" className="bg-green-500/10 text-green-600 border-green-500/20">
                                            <Check className="h-3 w-3 mr-1" />
                                            Configured
                                        </Badge>
                                    )}
                                </div>
                                <div className="flex items-center gap-2">
                                    <div className="relative flex-1">
                                        <Input
                                            id="openai-key"
                                            type={showOpenaiKey ? "text" : "password"}
                                            value={openaiKey}
                                            onChange={(e) => setOpenaiKey(e.target.value)}
                                            placeholder={llmKeyStatus.openai_configured ? "Enter new key to update..." : "sk-..."}
                                            className="font-mono text-sm pr-10"
                                        />
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="icon"
                                            className="absolute right-0 top-0 h-full px-3"
                                            onClick={() => setShowOpenaiKey(!showOpenaiKey)}
                                        >
                                            {showOpenaiKey ? (
                                                <EyeOff className="h-4 w-4 text-muted-foreground" />
                                            ) : (
                                                <Eye className="h-4 w-4 text-muted-foreground" />
                                            )}
                                        </Button>
                                    </div>
                                    <Button
                                        onClick={() => saveLlmKey('openai', openaiKey)}
                                        disabled={!openaiKey || savingOpenai}
                                        size="sm"
                                    >
                                        {savingOpenai ? (
                                            <Loader2 className="h-4 w-4 animate-spin" />
                                        ) : (
                                            <>
                                                <Save className="h-4 w-4 mr-1" />
                                                Save
                                            </>
                                        )}
                                    </Button>
                                </div>
                                <p className="text-xs text-muted-foreground">
                                    Get your API key from{" "}
                                    <a
                                        href="https://platform.openai.com/api-keys"
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="text-primary hover:underline"
                                    >
                                        platform.openai.com
                                    </a>
                                </p>
                            </div>

                            <div className="border-t" />

                            {/* Anthropic */}
                            <div className="space-y-3">
                                <div className="flex items-center justify-between">
                                    <Label htmlFor="anthropic-key" className="text-sm font-medium">
                                        Anthropic API Key
                                    </Label>
                                    {llmKeyStatus.anthropic_configured && (
                                        <Badge variant="outline" className="bg-green-500/10 text-green-600 border-green-500/20">
                                            <Check className="h-3 w-3 mr-1" />
                                            Configured
                                        </Badge>
                                    )}
                                </div>
                                <div className="flex items-center gap-2">
                                    <div className="relative flex-1">
                                        <Input
                                            id="anthropic-key"
                                            type={showAnthropicKey ? "text" : "password"}
                                            value={anthropicKey}
                                            onChange={(e) => setAnthropicKey(e.target.value)}
                                            placeholder={llmKeyStatus.anthropic_configured ? "Enter new key to update..." : "sk-ant-..."}
                                            className="font-mono text-sm pr-10"
                                        />
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="icon"
                                            className="absolute right-0 top-0 h-full px-3"
                                            onClick={() => setShowAnthropicKey(!showAnthropicKey)}
                                        >
                                            {showAnthropicKey ? (
                                                <EyeOff className="h-4 w-4 text-muted-foreground" />
                                            ) : (
                                                <Eye className="h-4 w-4 text-muted-foreground" />
                                            )}
                                        </Button>
                                    </div>
                                    <Button
                                        onClick={() => saveLlmKey('anthropic', anthropicKey)}
                                        disabled={!anthropicKey || savingAnthropic}
                                        size="sm"
                                    >
                                        {savingAnthropic ? (
                                            <Loader2 className="h-4 w-4 animate-spin" />
                                        ) : (
                                            <>
                                                <Save className="h-4 w-4 mr-1" />
                                                Save
                                            </>
                                        )}
                                    </Button>
                                </div>
                                <p className="text-xs text-muted-foreground">
                                    Get your API key from{" "}
                                    <a
                                        href="https://console.anthropic.com/settings/keys"
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="text-primary hover:underline"
                                    >
                                        console.anthropic.com
                                    </a>
                                </p>
                            </div>

                            {/* Warning if no keys configured */}
                            {!llmKeyStatus.openai_configured && !llmKeyStatus.anthropic_configured && (
                                <div className="flex items-start gap-2 p-3 bg-amber-500/10 border border-amber-500/20 rounded-lg text-amber-600 text-sm">
                                    <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
                                    <div>
                                        <p className="font-medium">No API keys configured</p>
                                        <p className="text-xs mt-1">
                                            Please add at least one API key above to use the AI features.
                                        </p>
                                    </div>
                                </div>
                            )}
                        </CardContent>
                    </Card>
                )}

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
