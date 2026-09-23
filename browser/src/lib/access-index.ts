// Bucket scope and policies are determined here
import type { IAMUser, NamedPolicy } from "./actions";

export const BUILT_IN_POLICIES: ReadonlySet<string> = new Set([
  "writeonly",
  "readonly",
  "readwrite",
  "diagnostics",
  "consoleAdmin",
]);

export interface IndexedPolicy {
  name: string;
  policy: string;
  buckets: string[];
  missingBuckets: string[];
  catchAll: boolean;
  adminOnly: boolean;
  denyOnly: boolean;
  builtIn: boolean;
  parseError: boolean;
  holders: string[];
}

export interface IndexedUser {
  accessKey: string;
  status: "enabled" | "disabled";
  policies: string[];
  missingPolicies: string[];
  buckets: string[];
  catchAll: boolean;
}

export interface AccessIndex {
  buckets: string[];
  policies: IndexedPolicy[];
  users: IndexedUser[];
}

const ARN_PREFIX = "arn:aws:s3:::";

function arnBucketPattern(arn: string): string | null {
  if (typeof arn !== "string" || !arn.startsWith(ARN_PREFIX)) return null;
  const rest = arn.slice(ARN_PREFIX.length);
  if (!rest) return null;
  const slash = rest.indexOf("/");
  return slash === -1 ? rest : rest.slice(0, slash);
}

function globMatch(pattern: string, s: string): boolean {
  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  const rx = new RegExp(`^${escaped.replace(/\*/g, ".*").replace(/\?/g, ".")}$`);
  return rx.test(s);
}

function statementsOf(policyJSON: string): { ok: boolean; statements: Record<string, unknown>[] } {
  try {
    const parsed = JSON.parse(policyJSON);
    const raw = parsed?.Statement;
    const list = Array.isArray(raw) ? raw : raw ? [raw] : [];
    return { ok: true, statements: list.filter(Boolean) };
  } catch {
    return { ok: false, statements: [] };
  }
}

function resourcesOf(st: Record<string, unknown>): string[] {
  const r = st.Resource;
  if (Array.isArray(r)) return r.filter((x): x is string => typeof x === "string");
  return typeof r === "string" ? [r] : [];
}

function indexPolicy(p: NamedPolicy, liveBuckets: string[]): Omit<IndexedPolicy, "holders"> {
  const { ok, statements } = statementsOf(p.policy);
  const patterns = new Set<string>();
  let anyResource = false;
  let denyCount = 0;

  for (const st of statements) {
    if (st.Effect === "Deny") denyCount += 1;
    for (const arn of resourcesOf(st)) {
      anyResource = true;
      const pat = arnBucketPattern(arn);
      if (pat) patterns.add(pat);
    }
  }

  const catchAll = patterns.has("*");
  const buckets: string[] = [];
  const missingBuckets: string[] = [];

  for (const pat of patterns) {
    if (pat === "*") continue;
    const matched = liveBuckets.filter((b) => globMatch(pat, b));
    if (matched.length > 0) buckets.push(...matched);
    // A literal token naming no live bucket is an orphan
    else if (!pat.includes("*") && !pat.includes("?")) missingBuckets.push(pat);
  }

  return {
    name: p.name,
    policy: p.policy,
    buckets: [...new Set(buckets)].sort(),
    missingBuckets: [...new Set(missingBuckets)].sort(),
    catchAll,
    // Admins are valid but dont match resource keys or buckets
    adminOnly: ok && statements.length > 0 && !anyResource,
    denyOnly: ok && statements.length > 0 && denyCount === statements.length,
    builtIn: BUILT_IN_POLICIES.has(p.name),
    parseError: !ok,
  };
}

export function buildAccessIndex(
  buckets: string[],
  policies: NamedPolicy[],
  users: IAMUser[],
): AccessIndex {
  const liveBuckets = [...buckets].sort();
  const indexed = policies.map((p) => indexPolicy(p, liveBuckets));
  const byName = new Map(indexed.map((p) => [p.name, p]));

  const indexedUsers: IndexedUser[] = users.map((u) => {
    const held = u.policies ?? [];
    const known = held.filter((pn) => byName.has(pn));
    const userBuckets = new Set<string>();
    let catchAll = false;
    for (const pn of known) {
      const p = byName.get(pn);
      if (!p) continue;
      if (p.catchAll) catchAll = true;
      for (const b of p.buckets) userBuckets.add(b);
    }
    return {
      accessKey: u.accessKey,
      status: u.status,
      policies: held,
      missingPolicies: held.filter((pn) => !byName.has(pn)),
      buckets: [...userBuckets].sort(),
      catchAll,
    };
  });

  const holders = new Map<string, string[]>();
  for (const u of indexedUsers) {
    for (const pn of u.policies) {
      const list = holders.get(pn);
      if (list) list.push(u.accessKey);
      else holders.set(pn, [u.accessKey]);
    }
  }

  return {
    buckets: liveBuckets,
    policies: indexed
      .map((p) => ({ ...p, holders: (holders.get(p.name) ?? []).sort() }))
      .sort((a, b) => a.name.localeCompare(b.name)),
    users: indexedUsers.sort((a, b) => a.accessKey.localeCompare(b.accessKey)),
  };
}

// When policy grants the bucket
export function policyGrants(p: IndexedPolicy, bucket: string): boolean {
  return p.catchAll || p.buckets.includes(bucket);
}

// When bucket is the only thing policy grants
export function policyExclusiveTo(p: IndexedPolicy, bucket: string): boolean {
  return (
    !p.catchAll &&
    !p.adminOnly &&
    !p.builtIn &&
    p.missingBuckets.length === 0 &&
    p.buckets.length === 1 &&
    p.buckets[0] === bucket
  );
}
