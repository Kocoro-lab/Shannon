/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { getAccessToken, getAPIKey } from "@/lib/auth";

// =============================================================================
// Base API URL Helper
// =============================================================================

/**
 * Get the API base URL synchronously.
 * This relies on the ServerProvider to set window.__SHANNON_API_URL when the server is ready.
 */
function getApiBaseUrl(): string {
    const isTauri = typeof window !== 'undefined' && '__TAURI__' in window;

    if (isTauri) {
        return (typeof window !== 'undefined' && window.__SHANNON_API_URL) || "";
    }

    return process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
}

// =============================================================================
// Auth Headers Helper
// =============================================================================

function getAuthHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};

    const apiKey = getAPIKey();
    if (apiKey) {
        headers["X-API-Key"] = apiKey;
        return headers;
    }

    const token = getAccessToken();
    if (token) {
        headers["Authorization"] = `Bearer ${token}`;
        return headers;
    }

    const userId = process.env.NEXT_PUBLIC_USER_ID;
    if (userId) {
        headers["X-User-Id"] = userId;
    }

    return headers;
}

// =============================================================================
// Settings Types
// =============================================================================

/**
 * User setting domain object.
 */
export interface UserSetting {
    user_id: string;
    setting_key: string;
    setting_value: string;
    setting_type: string;
    encrypted: boolean;
    created_at: string;
    updated_at: string;
}

/**
 * Request to set a setting.
 */
export interface SetSettingRequest {
    key: string;
    value: string;
    setting_type?: string;
    encrypted?: boolean;
}

/**
 * API key information (masked).
 */
export interface ApiKeyInfo {
    provider: string;
    is_configured: boolean;
    masked_key: string | null;
    is_active: boolean;
    last_used_at: string | null;
    created_at: string | null;
}

/**
 * Response from setting an API key.
 */
export interface SetApiKeyResponse {
    provider: string;
    masked_key: string;
    message: string;
}

// =============================================================================
// Settings API Functions
// =============================================================================

/**
 * Get all settings for the current user.
 *
 * @returns Array of user settings
 * @throws Error if the request fails
 */
export async function getAllSettings(): Promise<UserSetting[]> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings`, {
        method: "GET",
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to get settings: ${response.statusText}`);
    }

    return response.json();
}

/**
 * Get a single setting by key.
 *
 * @param key - Setting key
 * @returns User setting object
 * @throws Error if the request fails or setting not found
 */
export async function getSetting(key: string): Promise<UserSetting> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings/${encodeURIComponent(key)}`, {
        method: "GET",
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Failed to get setting: ${response.statusText}`);
    }

    return response.json();
}

/**
 * Set a setting value (create or update).
 *
 * @param key - Setting key
 * @param value - Setting value
 * @param type - Setting type: 'string' | 'number' | 'boolean' | 'json'
 * @param encrypted - Whether to encrypt the value
 * @returns Updated user setting object
 * @throws Error if the request fails
 */
export async function setSetting(
    key: string,
    value: string,
    type: string = "string",
    encrypted: boolean = false
): Promise<UserSetting> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            ...getAuthHeaders(),
        },
        body: JSON.stringify({
            key,
            value,
            setting_type: type,
            encrypted,
        }),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Failed to set setting: ${response.statusText}`);
    }

    // Return the result - backend returns success message, but we need to fetch the actual setting
    const result = await response.json();

    // Fetch the updated setting to return consistent data
    try {
        return await getSetting(key);
    } catch {
        // If fetch fails, construct a minimal response
        return {
            user_id: "",
            setting_key: key,
            setting_value: value,
            setting_type: type,
            encrypted,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
        };
    }
}

/**
 * Delete a setting.
 *
 * @param key - Setting key to delete
 * @throws Error if the request fails or setting not found
 */
export async function deleteSetting(key: string): Promise<void> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings/${encodeURIComponent(key)}`, {
        method: "DELETE",
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Failed to delete setting: ${response.statusText}`);
    }
}

// =============================================================================
// API Key Management Functions
// =============================================================================

/**
 * List all API keys (with masked values).
 *
 * @returns Array of API key information
 * @throws Error if the request fails
 */
export async function listApiKeys(): Promise<ApiKeyInfo[]> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings/api-keys`, {
        method: "GET",
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to list API keys: ${response.statusText}`);
    }

    return response.json();
}

/**
 * Set an API key for a provider.
 *
 * @param provider - Provider name (openai, anthropic, google, groq, xai)
 * @param apiKey - API key value
 * @returns Response with masked key
 * @throws Error if the request fails
 */
export async function setApiKey(provider: string, apiKey: string): Promise<SetApiKeyResponse> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings/api-keys/${encodeURIComponent(provider)}`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            ...getAuthHeaders(),
        },
        body: JSON.stringify({
            api_key: apiKey,
        }),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Failed to set API key: ${response.statusText}`);
    }

    return response.json();
}

/**
 * Delete an API key for a provider.
 *
 * @param provider - Provider name to delete key for
 * @throws Error if the request fails or key not found
 */
export async function deleteApiKey(provider: string): Promise<void> {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/settings/api-keys/${encodeURIComponent(provider)}`, {
        method: "DELETE",
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Failed to delete API key: ${response.statusText}`);
    }
}
