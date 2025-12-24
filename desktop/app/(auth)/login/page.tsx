"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { login } from "@/lib/shannon/api";
import { storeTokens } from "@/lib/auth";
import { Loader2, AlertCircle } from "lucide-react";

export default function LoginPage() {
    const router = useRouter();
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [error, setError] = useState<string | null>(null);
    const [isLoading, setIsLoading] = useState(false);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);
        setIsLoading(true);

        try {
            const response = await login(email, password);

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

            router.push("/run-detail");
        } catch (err) {
            if (err instanceof Error) {
                if (err.message.includes("Invalid email or password") || err.message.includes("invalid_credentials")) {
                    setError("Invalid email or password");
                } else if (err.message.includes("rate_limit")) {
                    setError("Too many attempts. Please try again later.");
                } else {
                    setError(err.message);
                }
            } else {
                setError("Login failed. Please try again.");
            }
        } finally {
            setIsLoading(false);
        }
    };

    return (
        <Card className="border-border/50 shadow-lg">
            <CardHeader className="space-y-1 text-center">
                <CardTitle className="text-2xl font-bold tracking-tight">
                    Welcome back
                </CardTitle>
                <CardDescription>
                    Sign in to your Shannon account
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
                            type="email"
                            placeholder="you@example.com"
                            value={email}
                            onChange={(e) => setEmail(e.target.value)}
                            required
                            disabled={isLoading}
                            autoComplete="email"
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="password">Password</Label>
                        <Input
                            id="password"
                            type="password"
                            placeholder="Enter your password"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            required
                            disabled={isLoading}
                            autoComplete="current-password"
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
                                Signing in...
                            </>
                        ) : (
                            "Sign in"
                        )}
                    </Button>

                    <p className="text-sm text-muted-foreground text-center">
                        Don&apos;t have an account?{" "}
                        <Link
                            href="/register"
                            className="text-primary hover:underline font-medium"
                        >
                            Create one
                        </Link>
                    </p>
                </CardFooter>
            </form>
        </Card>
    );
}
