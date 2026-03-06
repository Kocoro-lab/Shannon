/**
 * Agent color system for swarm visualization.
 *
 * - Lead agent: fixed amber (gold)
 * - Worker agents: HSL golden-angle rotation for maximum visual separation
 * - Colors scoped per workflow run (reset on new swarm task)
 */

export interface AgentColor {
  /** CSS hsl() for backgrounds with alpha */
  bg: string;
  /** CSS hsl() for text */
  text: string;
  /** CSS hsl() for borders with alpha */
  border: string;
  /** CSS hsl() for status dots */
  dot: string;
  /** Raw hue value (for canvas/programmatic use) */
  hue: number;
}

/** Fixed amber color for Lead agent */
export const LEAD_COLOR: AgentColor = {
  bg: "hsl(38 92% 50% / 0.15)",
  text: "hsl(38 92% 65%)",
  border: "hsl(38 92% 50% / 0.3)",
  dot: "hsl(38 92% 55%)",
  hue: 38,
};

/**
 * Generate a unique agent color by index using the golden angle.
 * Golden angle (137.508deg) guarantees maximum hue separation for any N.
 * Offset avoids red (hue 0) for early indices; red zone skip prevents
 * any index from landing on red which looks like an error state.
 */
const HUE_OFFSET = 160; // Start from teal-green, not red

export function generateAgentColor(index: number): AgentColor {
  let hue = Math.round((HUE_OFFSET + index * 137.508) % 360);
  // Skip red zone (345°-15°) — red resembles error/failure in UI
  if (hue > 345 || hue < 15) {
    hue = (hue + 30) % 360;
  }
  return {
    bg: `hsl(${hue} 70% 50% / 0.15)`,
    text: `hsl(${hue} 70% 65%)`,
    border: `hsl(${hue} 70% 50% / 0.3)`,
    dot: `hsl(${hue} 70% 55%)`,
    hue,
  };
}

/**
 * Agent color registry — maps agent names to colors within a workflow run.
 * Call `getOrAssign()` when an agent first appears (AGENT_STARTED event).
 * Call `reset()` when a new swarm workflow starts.
 */
export class AgentColorRegistry {
  private map = new Map<string, AgentColor>();
  private nextIndex = 0;

  /** Get existing color or assign the next one. Lead always gets LEAD_COLOR. */
  getOrAssign(agentId: string): AgentColor {
    if (agentId === "swarm-lead" || agentId === "swarm-supervisor") {
      return LEAD_COLOR;
    }
    const existing = this.map.get(agentId);
    if (existing) return existing;
    const color = generateAgentColor(this.nextIndex++);
    this.map.set(agentId, color);
    return color;
  }

  /** Look up color without assigning. Returns undefined if agent unknown. */
  get(agentId: string): AgentColor | undefined {
    if (agentId === "swarm-lead" || agentId === "swarm-supervisor") {
      return LEAD_COLOR;
    }
    return this.map.get(agentId);
  }

  /** Reset for a new workflow run. */
  reset(): void {
    this.map.clear();
    this.nextIndex = 0;
  }

  /** Get all registered agents and their colors. */
  entries(): [string, AgentColor][] {
    return Array.from(this.map.entries());
  }
}
