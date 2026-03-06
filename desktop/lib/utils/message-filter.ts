/**
 * Utility functions for filtering status/diagnostic messages
 * Used to determine if a message should be displayed in conversation vs timeline-only
 */

/**
 * Check if a message is a short status/completion message that should be filtered
 * from conversation display (these appear in timeline only)
 *
 * @param content - The message content to check
 * @returns true if the message is a status message that should be filtered
 */
export function isStatusMessage(content: unknown): boolean {
    if (!content || typeof content !== 'string') return false;

    const trimmed = content.trim();
    const lower = trimmed.toLowerCase();

    // Only filter short messages (< 100 chars) to avoid dropping real content
    if (trimmed.length >= 100) return false;

    // Exact matches
    if (lower === 'done' || lower === 'completed' || lower === 'success' || lower === 'all done') {
        return true;
    }

    // Pattern matches for short status messages
    if (/^(task\s+)?(completed|done|success)\.?$/i.test(lower)) {
        return true;
    }

    // Status phrases
    if (lower.includes('task completed') ||
        lower.includes('task done') ||
        lower.includes('workflow completed') ||
        lower.includes('successfully completed') ||
        lower.includes('successfully')) {
        return true;
    }

    return false;
}

/**
 * Check if a message is a diagnostic/system message that should always be filtered
 * from conversation display regardless of length
 *
 * @param content - The message content to check
 * @returns true if the message is a diagnostic message that should be filtered
 */
export function isDiagnosticMessage(content: unknown): boolean {
    if (!content || typeof content !== 'string') return false;

    return content.startsWith('[Incomplete response:') ||
           content.includes('Task budget at');
}

/**
 * Combined check: should this message be filtered from conversation?
 * Returns true for both status and diagnostic messages
 */
export function shouldFilterFromConversation(content: unknown): boolean {
    return isDiagnosticMessage(content) || isStatusMessage(content);
}

/**
 * Maximum number of processed delta sequences to track per message
 * Prevents unbounded memory growth during long streaming sessions
 */
export const MAX_PROCESSED_DELTAS = 100;

/**
 * Cap an array of processed delta sequences to prevent memory growth
 * Keeps the most recent entries
 */
export function capProcessedDeltas(deltas: number[], newSeq?: number): number[] {
    const result = newSeq !== undefined ? [...deltas, newSeq] : deltas;
    if (result.length > MAX_PROCESSED_DELTAS) {
        return result.slice(-MAX_PROCESSED_DELTAS);
    }
    return result;
}
