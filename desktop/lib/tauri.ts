// Tauri stub for OSS web-only mode (no desktop app)

export function isTauri(): boolean {
    return false;
}

export async function saveFileDialog(_options?: {
    defaultPath?: string;
    filters?: { name: string; extensions: string[] }[];
}): Promise<string | null> {
    return null;
}
