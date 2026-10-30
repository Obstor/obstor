// Pinned buckets stored per browser

export const PINS_KEY = "obstor.pins";

export function parsePins(raw: string | null): string[] {
  try {
    const v: unknown = JSON.parse(raw ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

// Toggle also removes missing buckets
export function togglePin(pins: string[], name: string, live: ReadonlySet<string>): string[] {
  const kept = pins.filter((p) => live.has(p));
  return kept.includes(name) ? kept.filter((p) => p !== name) : [...kept, name];
}

// Existing pinned buckets, otherwise five newest
export function shortcutBuckets(
  buckets: { name: string; creationDate: string }[],
  pins: string[],
): { pinned: boolean; names: string[] } {
  const live = new Set(buckets.map((b) => b.name));
  const names = pins.filter((p) => live.has(p));
  if (names.length > 0) return { pinned: true, names };
  const newest = [...buckets].sort(
    (a, b) => Date.parse(b.creationDate) - Date.parse(a.creationDate),
  );
  return { pinned: false, names: newest.slice(0, 5).map((b) => b.name) };
}
