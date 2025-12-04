import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

/**
 * Opens an external URL in the system's default browser.
 * Works in both Tauri (desktop) and web contexts.
 */
export async function openExternalUrl(url: string): Promise<void> {
    if (typeof window === "undefined") {
        return;
    }

    // Prefer Tauri shell when available (desktop app)
    try {
        const { open } = await import("@tauri-apps/plugin-shell");
        await open(url);
        return;
    } catch (error) {
        console.error("[openExternalUrl] Failed to open URL with Tauri shell:", error);
    }

    // Fallback for web or when Tauri shell is unavailable
    window.open(url, "_blank", "noopener,noreferrer");
}
