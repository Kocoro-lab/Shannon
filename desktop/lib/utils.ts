import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

export function isSafeUrl(url: string): boolean {
    try {
        const parsed = new URL(url);
        return parsed.protocol === "http:" || parsed.protocol === "https:";
    } catch {
        return false;
    }
}

/**
 * Opens an external URL in the system's default browser.
 * Works in both Tauri (desktop) and web contexts.
 */
export async function openExternalUrl(url: string): Promise<void> {
    if (typeof window === "undefined") {
        return;
    }

    if (!isSafeUrl(url)) {
        console.warn("[openExternalUrl] Blocked unsafe URL:", url);
        return;
    }

    // Fallback for web
    window.open(url, "_blank", "noopener,noreferrer");
}

export function safeNumber(value: unknown): number {
    if (typeof value === "number" && !isNaN(value)) return value;
    const n = Number(value);
    return isNaN(n) ? 0 : n;
}

export function safeToFixed(value: unknown, digits: number): string {
    return safeNumber(value).toFixed(digits);
}
