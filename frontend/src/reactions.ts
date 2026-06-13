import type { Reaction, StatsTargetType } from './types';

const REACTIONS_KEY = 'seymours-gallery-reactions';

type ReactionsMap = Record<string, Reaction>;

/**
 * Load the user's reactions from localStorage.
 * Key format: "targetType:targetId" -> "like" | "dislike"
 */
export function loadReactions(): ReactionsMap {
  try {
    const raw = localStorage.getItem(REACTIONS_KEY);
    if (!raw) return {};
    return JSON.parse(raw) as ReactionsMap;
  } catch {
    return {};
  }
}

function saveReactions(map: ReactionsMap): void {
  localStorage.setItem(REACTIONS_KEY, JSON.stringify(map));
}

/**
 * Get the current reaction for a given target.
 * Returns null if the user hasn't reacted.
 */
export function getUserReaction(targetType: StatsTargetType, targetId: string): Reaction | null {
  const map = loadReactions();
  return map[reactionKey(targetType, targetId)] ?? null;
}

/**
 * Toggle or set a reaction.
 * - If the user already has the same reaction, remove it (toggle off).
 * - Otherwise, set the new reaction (replacing any previous).
 * Returns the new reaction state or null if toggled off.
 */
export function toggleReaction(
  targetType: StatsTargetType,
  targetId: string,
  reaction: Reaction
): Reaction | null {
  const map = loadReactions();
  const key = reactionKey(targetType, targetId);
  const existing = map[key];
  if (existing === reaction) {
    // Toggle off
    delete map[key];
    saveReactions(map);
    return null;
  }
  // Set new reaction
  map[key] = reaction;
  saveReactions(map);
  return reaction;
}

function reactionKey(targetType: StatsTargetType, targetId: string): string {
  return `${targetType}:${targetId}`;
}
