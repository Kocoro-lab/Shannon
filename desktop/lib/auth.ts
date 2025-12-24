"use client";

// =============================================================================
// Token Storage Keys
// =============================================================================

const ACCESS_TOKEN_KEY = "shannon_access_token";
const REFRESH_TOKEN_KEY = "shannon_refresh_token";
const API_KEY_KEY = "shannon_api_key";
const USER_KEY = "shannon_user";

// =============================================================================
// Types
// =============================================================================

export interface StoredUser {
    user_id: string;
    email: string;
    username: string;
    name?: string;
    tier?: string;
}

// =============================================================================
// Token Management
// =============================================================================

export function getAccessToken(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem(ACCESS_TOKEN_KEY);
}

export function getRefreshToken(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem(REFRESH_TOKEN_KEY);
}

export function getAPIKey(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem(API_KEY_KEY);
}

export function getStoredUser(): StoredUser | null {
    if (typeof window === "undefined") return null;
    const userStr = localStorage.getItem(USER_KEY);
    if (!userStr) return null;
    try {
        return JSON.parse(userStr);
    } catch {
        return null;
    }
}

export function storeTokens(
    accessToken: string,
    refreshToken: string,
    user: StoredUser,
    apiKey?: string
): void {
    if (typeof window === "undefined") return;
    localStorage.setItem(ACCESS_TOKEN_KEY, accessToken);
    localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
    localStorage.setItem(USER_KEY, JSON.stringify(user));
    if (apiKey) {
        localStorage.setItem(API_KEY_KEY, apiKey);
    }
}

export function clearTokens(): void {
    if (typeof window === "undefined") return;
    localStorage.removeItem(ACCESS_TOKEN_KEY);
    localStorage.removeItem(REFRESH_TOKEN_KEY);
    localStorage.removeItem(API_KEY_KEY);
    localStorage.removeItem(USER_KEY);
}

export function isAuthenticated(): boolean {
    return !!getAccessToken();
}

// =============================================================================
// Token Refresh
// =============================================================================

let refreshPromise: Promise<boolean> | null = null;

export async function refreshAccessToken(): Promise<boolean> {
    // Prevent concurrent refresh attempts
    if (refreshPromise) {
        return refreshPromise;
    }

    const refreshToken = getRefreshToken();
    if (!refreshToken) {
        return false;
    }

    refreshPromise = (async () => {
        try {
            const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
            const response = await fetch(`${API_BASE_URL}/api/v1/auth/refresh`, {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify({ refresh_token: refreshToken }),
            });

            if (!response.ok) {
                clearTokens();
                return false;
            }

            const data = await response.json();
            const user = getStoredUser();
            if (user) {
                storeTokens(data.access_token, data.refresh_token, user);
            }
            return true;
        } catch (error) {
            console.error("Token refresh failed:", error);
            return false;
        } finally {
            refreshPromise = null;
        }
    })();

    return refreshPromise;
}

// =============================================================================
// Logout
// =============================================================================

export function logout(): void {
    clearTokens();
    // OSS mode: Redirect to run page (no login required)
    if (typeof window !== "undefined") {
        window.location.href = "/run-detail?session_id=new";
    }
}
