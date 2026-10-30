// Object listing helpers

// ISO timestamp formatted on client
export interface Entry {
  name: string;
  size: number;
  modified: string;
  etag: string;
}

export type SortField = "name" | "size" | "modified";

export const isFolder = (name: string) => name.endsWith("/");

// Matches server and browser regardless of locale
const compareNames = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

// Folders by name first, files by selected field
export function sortEntries<T extends { name: string; size: number; modified: string }>(
  rows: T[],
  field: SortField,
  asc: boolean,
): T[] {
  const dir = asc ? 1 : -1;
  const byName = (a: T, b: T) => compareNames(a.name, b.name);
  return [...rows].sort((a, b) => {
    const fa = isFolder(a.name);
    const fb = isFolder(b.name);
    if (fa !== fb) return fa ? -1 : 1;
    if (fa) return field === "name" ? dir * byName(a, b) : byName(a, b);
    const cmp =
      field === "size"
        ? a.size - b.size
        : field === "modified"
          ? Date.parse(a.modified) - Date.parse(b.modified)
          : byName(a, b);
    return dir * (cmp || byName(a, b));
  });
}

export function paginate<T>(
  rows: T[],
  page: number,
  perPage: number,
): { rows: T[]; page: number; pages: number } {
  const pages = Math.max(1, Math.ceil(rows.length / perPage));
  const p = Math.min(Math.max(1, Math.floor(page) || 1), pages);
  return { rows: rows.slice((p - 1) * perPage, p * perPage), page: p, pages };
}

// Pagination, up to 5 pages for prev/next
export function pageWindow(page: number, pages: number): number[] {
  const start = Math.max(1, Math.min(page - 2, pages - 4));
  return Array.from({ length: Math.min(5, pages) }, (_, i) => start + i);
}

export function parentPrefix(prefix: string): string {
  const parts = prefix.split("/").filter(Boolean);
  return parts.length > 1 ? `${parts.slice(0, -1).join("/")}/` : "";
}

export function prefixCrumbs(prefix: string): { name: string; prefix: string }[] {
  const parts = prefix.split("/").filter(Boolean);
  return parts.map((name, i) => ({ name, prefix: `${parts.slice(0, i + 1).join("/")}/` }));
}

export function entryName(name: string, prefix: string): string {
  return (name.startsWith(prefix) ? name.slice(prefix.length) : name).replace(/\/$/, "");
}

export function objectsHref(bucket: string, prefix = ""): string {
  const base = `/buckets/${encodeURIComponent(bucket)}`;
  return prefix ? `${base}?prefix=${encodeURIComponent(prefix)}` : base;
}

// Folder marker, null if blank after trimming
export function folderKey(prefix: string, input: string): string | null {
  const name = input.trim().replace(/^\/+|\/+$/g, "");
  return name ? `${prefix}${name}/` : null;
}

// S3 error bodies are XML with a Message element
export function s3ErrorMessage(body: string, status: number): string {
  return /<Message>([^<]+)<\/Message>/.exec(body)?.[1] ?? `HTTP ${status}`;
}

// Display URL without scheme
export function hostPath(url: string): string {
  return url.replace(/^[a-z][a-z0-9+.-]*:\/\//i, "");
}

// Host and last path segment
export function locationLabel(endpoint: string): string {
  const url = /^[a-z][a-z0-9+.-]*:\/\/([^/]*)(.*)$/i.exec(endpoint);
  const path = url ? url[2] : endpoint;
  const last = path.split(/[/\\]/).filter(Boolean).pop() ?? "";
  if (!url) return last || endpoint;
  return last ? `${url[1]}/${last}` : url[1];
}

export interface LocationCell {
  label: string;
  title: string;
}

// Location cells with hidden column if values match
export function locationColumn(
  names: string[],
  locs: Record<string, string[]>,
): { show: boolean; cells: Record<string, LocationCell> } {
  const pairs: [string, LocationCell][] = [];
  for (const name of names) {
    const eps = Object.hasOwn(locs, name) ? locs[name] : [];
    if (eps.length === 0) continue;
    const more = eps.length > 1 ? ` +${eps.length - 1}` : "";
    pairs.push([name, { label: `${locationLabel(eps[0])}${more}`, title: eps.join(", ") }]);
  }
  const labels = new Set(pairs.map(([, c]) => c.label));
  return { show: labels.size > 1, cells: Object.fromEntries(pairs) };
}

export type BucketSortField = "name" | "created" | "size" | "objects";

// Null usage last both ways
export function sortBuckets<
  T extends { name: string; createdAt: string; size: number | null; objects: number | null },
>(rows: T[], field: BucketSortField, asc: boolean): T[] {
  const dir = asc ? 1 : -1;
  const byName = (a: T, b: T) => compareNames(a.name, b.name);
  return [...rows].sort((a, b) => {
    if (field === "name") return dir * byName(a, b);
    if (field === "created") {
      return dir * (Date.parse(a.createdAt) - Date.parse(b.createdAt) || byName(a, b));
    }
    const x = a[field];
    const y = b[field];
    if (x === null || y === null) {
      if (x === y) return byName(a, b);
      return x === null ? 1 : -1;
    }
    return dir * (x - y) || byName(a, b);
  });
}

export type UploadStatus = "queued" | "uploading" | "done" | "failed";

// First unfinished item
export function uploadProgress(statuses: UploadStatus[]): {
  active: boolean;
  current: number;
  total: number;
} {
  const first = statuses.findIndex((s) => s === "queued" || s === "uploading");
  return {
    active: first >= 0,
    current: first >= 0 ? first + 1 : statuses.length,
    total: statuses.length,
  };
}
