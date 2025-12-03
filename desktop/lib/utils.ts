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
    // Check if we're running in Tauri
    if (typeof window !== 'undefined' && '__TAURI__' in window) {
        try {
            // Dynamic import to avoid issues when not in Tauri context
            const { open } = await import('@tauri-apps/plugin-shell');
            await open(url);
        } catch (error) {
            console.error('Failed to open URL with Tauri shell:', error);
            // Fallback to window.open
            window.open(url, '_blank', 'noopener,noreferrer');
        }
    } else {
        // Web context - use window.open
        window.open(url, '_blank', 'noopener,noreferrer');
    }
}
