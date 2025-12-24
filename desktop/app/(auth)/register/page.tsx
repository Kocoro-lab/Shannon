"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { register } from "@/lib/shannon/api";
import { storeTokens } from "@/lib/auth";
import { Loader2, AlertCircle, CheckCircle2, Copy } from "lucide-react";

export default function RegisterPage() {
    const router = useRouter();
    const [formData, setFormData] = useState({
        email: "",
        username: "",
        password: "",
        confirmPassword: "",
        fullName: "",
    });
    const [error, setError] = useState<string | null>(null);
    const [isLoading, setIsLoading] = useState(false);
    const [apiKey, setApiKey] = useState<string | null>(null);
    const [copied, setCopied] = useState(false);

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        setFormData((prev) => ({
            ...prev,
            [e.target.name]: e.target.value,
        }));
    };

    const validateForm = (): string | null => {
        if (!formData.email || !formData.username || !formData.password) {
            return "Email, username, and password are required";
        }
        if (formData.password.length < 8) {
            return "Password must be at least 8 characters";
        }
        if (formData.password !== formData.confirmPassword) {
            return "Passwords do not match";
        }
        if (formData.username.length < 3) {
            return "Username must be at least 3 characters";
        }
        return null;
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);

        const validationError = validateForm();
        if (validationError) {
            setError(validationError);
            return;
        }

        setIsLoading(true);

        try {
            const response = await register(
                formData.email,
                formData.username,
                formData.password,
                formData.fullName || undefined
            );

            storeTokens(
                response.access_token,
                response.refresh_token,
                {
                    user_id: response.user_id,
                    email: response.user.email,
                    username: response.user.username,
                    name: response.user.name || undefined,
                    tier: response.tier,
                },
                response.api_key
            );

            // Show API key if returned (new user)
            if (response.api_key) {
                setApiKey(response.api_key);
            } else {
                router.push("/run-detail");
            }
        } catch (err) {
            if (err instanceof Error) {
                if (err.message.includes("already registered") || err.message.includes("email_exists")) {
                    setError("This email is already registered");
                } else if (err.message.includes("rate_limit")) {
                    setError("Too many attempts. Please try again later.");
                } else {
                    setError(err.message);
                }
            } else {
                setError("Registration failed. Please try again.");
            }
        } finally {
            setIsLoading(false);
        }
    };

    const copyApiKey = () => {
        if (apiKey) {
            navigator.clipboard.writeText(apiKey);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        }
    };

    // Show API key modal after successful registration
    if (apiKey) {
        return (
            <Card className="border-border/50 shadow-lg">
                <CardHeader className="space-y-1 text-center">
                    <div className="mx-auto w-12 h-12 rounded-full bg-green-500/10 flex items-center justify-center mb-2">
                        <CheckCircle2 className="h-6 w-6 text-green-500" />
                    </div>
                    <CardTitle className="text-2xl font-bold tracking-tight">
                        Welcome to Shannon!
                    </CardTitle>
                    <CardDescription>
                        Your account has been created successfully
                    </CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                    <div className="p-4 bg-muted rounded-lg space-y-2">
                        <Label className="text-sm font-medium">Your API Key</Label>
                        <p className="text-xs text-muted-foreground mb-2">
                            Save this key securely. It will only be shown once.
                        </p>
                        <div className="flex items-center gap-2">
                            <code className="flex-1 p-2 bg-background rounded text-xs font-mono break-all">
                                {apiKey}
                            </code>
                            <Button
                                variant="outline"
                                size="icon"
                                onClick={copyApiKey}
                                className="shrink-0"
                            >
                                {copied ? (
                                    <CheckCircle2 className="h-4 w-4 text-green-500" />
                                ) : (
                                    <Copy className="h-4 w-4" />
                                )}
                            </Button>
                        </div>
                    </div>
                </CardContent>
                <CardFooter>
                    <Button
                        className="w-full"
                        onClick={() => router.push("/run-detail")}
                    >
                        Continue to Dashboard
                    </Button>
                </CardFooter>
            </Card>
        );
    }

    return (
        <Card className="border-border/50 shadow-lg">
            <CardHeader className="space-y-1 text-center">
                <CardTitle className="text-2xl font-bold tracking-tight">
                    Create an account
                </CardTitle>
                <CardDescription>
                    Get started with Shannon AI
                </CardDescription>
            </CardHeader>
            <form onSubmit={handleSubmit}>
                <CardContent className="space-y-4">
                    {error && (
                        <div className="flex items-center gap-2 p-3 text-sm text-red-500 bg-red-500/10 rounded-md">
                            <AlertCircle className="h-4 w-4 shrink-0" />
                            <span>{error}</span>
                        </div>
                    )}

                    <div className="space-y-2">
                        <Label htmlFor="email">Email</Label>
                        <Input
                            id="email"
                            name="email"
                            type="email"
                            placeholder="you@example.com"
                            value={formData.email}
                            onChange={handleChange}
                            required
                            disabled={isLoading}
                            autoComplete="email"
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="username">Username</Label>
                        <Input
                            id="username"
                            name="username"
                            type="text"
                            placeholder="johndoe"
                            value={formData.username}
                            onChange={handleChange}
                            required
                            disabled={isLoading}
                            autoComplete="username"
                            minLength={3}
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="fullName">Full Name (optional)</Label>
                        <Input
                            id="fullName"
                            name="fullName"
                            type="text"
                            placeholder="John Doe"
                            value={formData.fullName}
                            onChange={handleChange}
                            disabled={isLoading}
                            autoComplete="name"
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="password">Password</Label>
                        <Input
                            id="password"
                            name="password"
                            type="password"
                            placeholder="At least 8 characters"
                            value={formData.password}
                            onChange={handleChange}
                            required
                            disabled={isLoading}
                            autoComplete="new-password"
                            minLength={8}
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="confirmPassword">Confirm Password</Label>
                        <Input
                            id="confirmPassword"
                            name="confirmPassword"
                            type="password"
                            placeholder="Confirm your password"
                            value={formData.confirmPassword}
                            onChange={handleChange}
                            required
                            disabled={isLoading}
                            autoComplete="new-password"
                        />
                    </div>
                </CardContent>

                <CardFooter className="flex flex-col gap-4">
                    <Button
                        type="submit"
                        className="w-full"
                        disabled={isLoading}
                    >
                        {isLoading ? (
                            <>
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                Creating account...
                            </>
                        ) : (
                            "Create account"
                        )}
                    </Button>

                    <p className="text-sm text-muted-foreground text-center">
                        Already have an account?{" "}
                        <Link
                            href="/login"
                            className="text-primary hover:underline font-medium"
                        >
                            Sign in
                        </Link>
                    </p>
                </CardFooter>
            </form>
        </Card>
    );
}
