"use client";

import { useEffect, useState, useCallback } from "react";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Loader2, Eye, EyeOff, Check, X } from "lucide-react";
import { listApiKeys, setApiKey, deleteApiKey, ApiKeyInfo } from "@/lib/shannon/settings";
import { toast } from "sonner";

interface ProviderConfig {
    id: string;
    name: string;
    description: string;
    placeholderPrefix: string;
}

const PROVIDERS: ProviderConfig[] = [
    {
        id: "openai",
        name: "OpenAI",
        description: "GPT-4, GPT-3.5 and other OpenAI models",
        placeholderPrefix: "sk-",
    },
    {
        id: "anthropic",
        name: "Anthropic",
        description: "Claude 3 models",
        placeholderPrefix: "sk-ant-",
    },
    {
        id: "google",
        name: "Google",
        description: "Gemini and other Google AI models",
        placeholderPrefix: "AI",
    },
    {
        id: "groq",
        name: "Groq",
        description: "Fast inference with open-source models",
        placeholderPrefix: "gsk_",
    },
    {
        id: "xai",
        name: "xAI",
        description: "Grok and other xAI models",
        placeholderPrefix: "xai-",
    },
];

export default function ApiKeysPage() {
    const [keys, setKeys] = useState<Record<string, ApiKeyInfo>>({});
    const [inputValues, setInputValues] = useState<Record<string, string>>({});
    const [showKeys, setShowKeys] = useState<Record<string, boolean>>({});
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState<Record<string, boolean>>({});
    const [deleting, setDeleting] = useState<Record<string, boolean>>({});

    const loadKeys = useCallback(async () => {
        try {
            setLoading(true);
            const apiKeys = await listApiKeys();
            const keysMap: Record<string, ApiKeyInfo> = {};
            apiKeys.forEach(key => {
                keysMap[key.provider] = key;
            });
            setKeys(keysMap);
        } catch (error) {
            console.error("Failed to load API keys:", error);
            toast.error("Failed to load API keys");
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        loadKeys();
    }, [loadKeys]);

    const handleSave = async (providerId: string) => {
        const value = inputValues[providerId]?.trim();
        if (!value) {
            toast.error("Please enter an API key");
            return;
        }

        setSaving(prev => ({ ...prev, [providerId]: true }));
        try {
            const response = await setApiKey(providerId, value);
            toast.success(response.message);

            // Clear input and reload keys
            setInputValues(prev => ({ ...prev, [providerId]: "" }));
            setShowKeys(prev => ({ ...prev, [providerId]: false }));
            await loadKeys();
        } catch (error) {
            console.error(`Failed to save API key for ${providerId}:`, error);
            toast.error(error instanceof Error ? error.message : "Failed to save API key");
        } finally {
            setSaving(prev => ({ ...prev, [providerId]: false }));
        }
    };

    const handleDelete = async (providerId: string) => {
        setDeleting(prev => ({ ...prev, [providerId]: true }));
        try {
            await deleteApiKey(providerId);
            toast.success(`${PROVIDERS.find(p => p.id === providerId)?.name} API key deleted`);
            await loadKeys();
        } catch (error) {
            console.error(`Failed to delete API key for ${providerId}:`, error);
            toast.error(error instanceof Error ? error.message : "Failed to delete API key");
        } finally {
            setDeleting(prev => ({ ...prev, [providerId]: false }));
        }
    };

    const toggleShowKey = (providerId: string) => {
        setShowKeys(prev => ({ ...prev, [providerId]: !prev[providerId] }));
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight">API Keys</h2>
                <p className="text-muted-foreground">
                    Configure your LLM provider API keys. Keys are encrypted before storage.
                </p>
            </div>

            <div className="grid gap-4">
                {PROVIDERS.map((provider) => {
                    const keyInfo = keys[provider.id];
                    const isConfigured = keyInfo?.is_configured;
                    const isSaving = saving[provider.id];
                    const isDeleting = deleting[provider.id];
                    const showKey = showKeys[provider.id];

                    return (
                        <Card key={provider.id} className="p-6">
                            <div className="space-y-4">
                                <div className="flex items-start justify-between">
                                    <div>
                                        <div className="flex items-center gap-2">
                                            <h3 className="text-lg font-semibold">{provider.name}</h3>
                                            {isConfigured && (
                                                <span className="inline-flex items-center gap-1 rounded-full bg-green-50 dark:bg-green-900/20 px-2 py-0.5 text-xs font-medium text-green-700 dark:text-green-300">
                                                    <Check className="h-3 w-3" />
                                                    Configured
                                                </span>
                                            )}
                                        </div>
                                        <p className="text-sm text-muted-foreground mt-1">
                                            {provider.description}
                                        </p>
                                    </div>
                                </div>

                                {isConfigured && keyInfo.masked_key && (
                                    <div className="text-sm">
                                        <Label className="text-xs text-muted-foreground">Current Key</Label>
                                        <div className="mt-1 font-mono text-sm bg-muted px-3 py-2 rounded-md">
                                            {keyInfo.masked_key}
                                        </div>
                                    </div>
                                )}

                                <div className="space-y-2">
                                    <Label htmlFor={`${provider.id}-key`}>
                                        {isConfigured ? "Update API Key" : "API Key"}
                                    </Label>
                                    <div className="flex gap-2">
                                        <div className="relative flex-1">
                                            <Input
                                                id={`${provider.id}-key`}
                                                type={showKey ? "text" : "password"}
                                                placeholder={`${provider.placeholderPrefix}...`}
                                                value={inputValues[provider.id] || ""}
                                                onChange={(e) =>
                                                    setInputValues(prev => ({
                                                        ...prev,
                                                        [provider.id]: e.target.value
                                                    }))
                                                }
                                                disabled={isSaving || isDeleting}
                                                className="pr-10"
                                            />
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="icon"
                                                className="absolute right-0 top-0 h-full px-3"
                                                onClick={() => toggleShowKey(provider.id)}
                                                disabled={isSaving || isDeleting}
                                            >
                                                {showKey ? (
                                                    <EyeOff className="h-4 w-4" />
                                                ) : (
                                                    <Eye className="h-4 w-4" />
                                                )}
                                            </Button>
                                        </div>
                                        <Button
                                            onClick={() => handleSave(provider.id)}
                                            disabled={isSaving || isDeleting || !inputValues[provider.id]?.trim()}
                                        >
                                            {isSaving ? (
                                                <>
                                                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                                    Saving...
                                                </>
                                            ) : (
                                                "Save"
                                            )}
                                        </Button>
                                        {isConfigured && (
                                            <Button
                                                variant="destructive"
                                                onClick={() => handleDelete(provider.id)}
                                                disabled={isSaving || isDeleting}
                                            >
                                                {isDeleting ? (
                                                    <>
                                                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                                        Deleting...
                                                    </>
                                                ) : (
                                                    <>
                                                        <X className="mr-2 h-4 w-4" />
                                                        Delete
                                                    </>
                                                )}
                                            </Button>
                                        )}
                                    </div>
                                </div>

                                {isConfigured && keyInfo.last_used_at && (
                                    <p className="text-xs text-muted-foreground">
                                        Last used: {new Date(keyInfo.last_used_at).toLocaleString()}
                                    </p>
                                )}
                            </div>
                        </Card>
                    );
                })}
            </div>

            <Card className="p-4 bg-muted/50">
                <div className="text-sm space-y-2">
                    <p className="font-medium">Security Notes:</p>
                    <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                        <li>API keys are encrypted using AES-256-GCM before storage</li>
                        <li>Keys are stored locally in your SQLite database</li>
                        <li>Never share your API keys or commit them to version control</li>
                        <li>Each provider uses its own key - configure at least one to use Shannon</li>
                    </ul>
                </div>
            </Card>
        </div>
    );
}
