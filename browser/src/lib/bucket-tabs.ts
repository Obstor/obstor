// Bucket tabs and leave guard click filter

export type BucketTab = "objects" | "settings" | "access" | "activity";

export const BUCKET_TABS: readonly { id: BucketTab; label: string }[] = [
  { id: "objects", label: "Objects" },
  { id: "settings", label: "Settings" },
  { id: "access", label: "Access" },
  { id: "activity", label: "Activity" },
];

// Exact tab ID, unknown or repeated params open Objects
export function parseTab(raw: string | string[] | undefined): BucketTab {
  if (typeof raw !== "string") return "objects";
  return BUCKET_TABS.find((t) => t.id === raw)?.id ?? "objects";
}

// Match objectsHref so switching tabs keeps folder
export function tabHref(bucket: string, tab: BucketTab, prefix = ""): string {
  const query: string[] = [];
  if (prefix) query.push(`prefix=${encodeURIComponent(prefix)}`);
  if (tab !== "objects") query.push(`tab=${tab}`);
  const base = `/buckets/${encodeURIComponent(bucket)}`;
  return query.length > 0 ? `${base}?${query.join("&")}` : base;
}

// Keyboard focus index, null keeps current focus
export function rovingIndex(key: string, current: number, count: number): number | null {
  if (count < 1) return null;
  switch (key) {
    case "ArrowRight":
      return (current + 1) % count;
    case "ArrowLeft":
      return (current - 1 + count) % count;
    case "Home":
      return 0;
    case "End":
      return count - 1;
    default:
      return null;
  }
}

export interface ClickInfo {
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  defaultPrevented: boolean;
}

export interface LinkInfo {
  href: string;
  target: string;
  download: boolean;
}

// Plain left click to another page on same origin
export function guardsClick(click: ClickInfo, link: LinkInfo, here: string): boolean {
  if (click.defaultPrevented || click.button !== 0) return false;
  if (click.metaKey || click.ctrlKey || click.shiftKey || click.altKey) return false;
  if ((link.target !== "" && link.target !== "_self") || link.download) return false;
  let to: URL;
  let from: URL;
  try {
    to = new URL(link.href, here);
    from = new URL(here);
  } catch {
    return false;
  }
  if (to.origin !== from.origin) return false;
  to.hash = "";
  from.hash = "";
  return to.href !== from.href;
}
