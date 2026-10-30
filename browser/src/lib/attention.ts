// Bell state stored per browser

export type Severity = "critical" | "warning" | "info";

export interface AttentionItem {
  id: string;
  kind: string;
  severity: Severity;
  title: string;
  detail: string;
  href: string;
}

export const SEEN_PREFIX = "obstor.seen.";
export const BANNER_PREFIX = "obstor.banner.";

export function seenKey(accessKey: string): string {
  return SEEN_PREFIX + accessKey;
}

// Bad stored values mean nothing seen
export function parseSeen(raw: string | null): string[] {
  try {
    const v: unknown = JSON.parse(raw ?? "[]");
    return Array.isArray(v)
      ? [...new Set(v.filter((x): x is string => typeof x === "string"))]
      : [];
  } catch {
    return [];
  }
}

// Remove cleared IDs so recurrence shows unread
export function pruneSeen(seen: string[], items: AttentionItem[]): string[] {
  const live = new Set(items.map((i) => i.id));
  return seen.filter((id) => live.has(id));
}

export function markSeen(seen: string[], ids: string[]): string[] {
  return [...new Set([...seen, ...ids])];
}

export function unseenItems(items: AttentionItem[], seen: string[]): AttentionItem[] {
  const done = new Set(seen);
  return items.filter((i) => !done.has(i.id));
}

export function bellLabel(unseen: number): string {
  return unseen > 0 ? `Notifications, ${unseen} unread` : "Notifications";
}

// Critical set determines key
export function bannerState(
  items: AttentionItem[],
): { first: AttentionItem; more: number; key: string } | null {
  const critical = items.filter((i) => i.severity === "critical");
  if (critical.length === 0) return null;
  const ids = critical.map((i) => i.id).sort();
  return { first: critical[0], more: critical.length - 1, key: BANNER_PREFIX + ids.join(",") };
}
