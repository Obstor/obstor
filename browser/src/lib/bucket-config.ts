// Shared bucket settings and save plans

export type PublicAccess = "private" | "public-read" | "public-read-write";
export type QuotaType = "hard" | "fifo";
export type QuotaUnit = "GB" | "TB" | "PB";

export interface Tag {
  key: string;
  value: string;
}

// Editable settings, save only changed fields
export interface BucketConfig {
  publicAccess: PublicAccess;
  versioning: boolean;
  objectLock: boolean;
  quotaEnabled: boolean;
  quotaType: QuotaType;
  quotaSize: string;
  quotaUnit: QuotaUnit;
  encryption: boolean;
  tags: Tag[];
  s3Enabled: boolean;
  sftpEnabled: boolean;
}

// Settings this server mode can store
export interface BucketFeatures {
  versioning: boolean;
  objectLock: boolean;
  quota: boolean;
  encryption: boolean;
  autoEncryption: boolean;
  tags: boolean;
  backend: boolean;
}

// Bucket config reply, null for unreadable
export interface RawBucketConfig {
  versioning: string | null;
  objectLock: boolean | null;
  quota: { type: string; bytes: number } | null;
  encryption: boolean | null;
  tags: Tag[] | null;
}

export type Control =
  | "policy"
  | "versioning"
  | "objectLock"
  | "quota"
  | "encryption"
  | "tags"
  | "toggles";

// Read permission per control
export type Readable = Record<Control, boolean>;

export const ALL_READABLE: Readable = {
  policy: true,
  versioning: true,
  objectLock: true,
  quota: true,
  encryption: true,
  tags: true,
  toggles: true,
};

export interface ControlContext {
  features: BucketFeatures | null;
  readable: Readable;
  config: BucketConfig;
  isEdit: boolean;
}

export interface ConfigChange {
  label: string;
  rpc:
    | "SetBucketPolicy"
    | "SetBucketVersioning"
    | "SetBucketQuota"
    | "SetBucketEncryption"
    | "SetBucketTags"
    | "SetBucketToggles";
  params: Record<string, unknown>;
}

// Protocol settings use separate metadata
export const MAX_USER_TAGS = 50;
export const RESERVED_TAG_PREFIX = "__obstor_";
// Largest JS byte count
export const MAX_QUOTA_BYTES = 2 ** 53;

const UNIT_BYTES: Record<QuotaUnit, number> = { GB: 1024 ** 3, TB: 1024 ** 4, PB: 1024 ** 5 };

// Same characters as server tags package
const TAG_CHARS = /^[\w+.:/@ =-]+$/;

export function publicAccessFromPolicy(policy: string): PublicAccess {
  if (policy === "readonly") return "public-read";
  if (policy === "readwrite" || policy === "writeonly") return "public-read-write";
  return "private";
}

export function policyFromPublicAccess(access: PublicAccess): string {
  if (access === "public-read") return "readonly";
  if (access === "public-read-write") return "readwrite";
  return "none";
}

// Binary units null for empty
export function quotaBytes(size: string, unit: QuotaUnit): number | null {
  if (size.trim() === "") return null;
  const n = Number(size);
  if (!Number.isFinite(n) || n <= 0) return null;
  const bytes = Math.round(n * UNIT_BYTES[unit]);
  return bytes >= 1 && bytes <= MAX_QUOTA_BYTES ? bytes : null;
}

// Largest unit rounded to two decimals
export function quotaFields(bytes: number): { quotaSize: string; quotaUnit: QuotaUnit } {
  const quotaUnit: QuotaUnit = bytes >= UNIT_BYTES.PB ? "PB" : bytes >= UNIT_BYTES.TB ? "TB" : "GB";
  return { quotaSize: String(Number((bytes / UNIT_BYTES[quotaUnit]).toFixed(2))), quotaUnit };
}

// Ignore rows with both key and value blank
export function cleanTags(tags: Tag[]): Tag[] {
  return tags.filter((t) => t.key !== "" || t.value !== "");
}

// Server tag rules null when valid
export function tagError(tags: Tag[]): string | null {
  if (tags.length > MAX_USER_TAGS) return `A bucket can have at most ${MAX_USER_TAGS} tags.`;
  const seen = new Set<string>();
  for (const { key, value } of tags) {
    if (key === "") return "Every tag needs a key.";
    if (key.startsWith(RESERVED_TAG_PREFIX)) {
      return `Tag keys starting with ${RESERVED_TAG_PREFIX} are reserved.`;
    }
    if (key.length > 128) return "Tag keys can be at most 128 characters.";
    if (value.length > 256) return "Tag values can be at most 256 characters.";
    if (!TAG_CHARS.test(key) || (value !== "" && !TAG_CHARS.test(value))) {
      return "Tags can use letters, digits, spaces and + - . _ : / @ = only.";
    }
    if (seen.has(key)) return `Tag key "${key}" is used twice.`;
    seen.add(key);
  }
  return null;
}

const tagSignature = (tags: Tag[]) =>
  JSON.stringify(
    tags.map((t) => [t.key, t.value]).sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0)),
  );

// Row order does not count as a change
export function tagsEqual(a: Tag[], b: Tag[]): boolean {
  return tagSignature(a) === tagSignature(b);
}

// Settings after creating bucket
export function createdConfig(objectLock: boolean): BucketConfig {
  return {
    publicAccess: "private",
    versioning: objectLock,
    objectLock,
    quotaEnabled: false,
    quotaType: "hard",
    quotaSize: "",
    quotaUnit: "GB",
    encryption: false,
    tags: [],
    s3Enabled: true,
    sftpEnabled: false,
  };
}

// Form values and read permissions, null policy or toggles means failed read
export function parseBucketConfig(
  raw: RawBucketConfig,
  policy: string | null,
  toggles: { s3Enabled: boolean; sftpEnabled: boolean } | null,
): { config: BucketConfig; readable: Readable } {
  const base = createdConfig(false);
  const bytes = raw.quota?.bytes ?? 0;
  return {
    config: {
      ...base,
      ...(bytes > 0 ? quotaFields(bytes) : {}),
      publicAccess: policy === null ? base.publicAccess : publicAccessFromPolicy(policy),
      versioning: raw.versioning === "Enabled",
      objectLock: raw.objectLock === true,
      quotaEnabled: bytes > 0,
      quotaType: raw.quota?.type === "fifo" ? "fifo" : "hard",
      encryption: raw.encryption === true,
      tags: raw.tags ?? [],
      s3Enabled: toggles?.s3Enabled ?? base.s3Enabled,
      sftpEnabled: toggles?.sftpEnabled ?? base.sftpEnabled,
    },
    readable: {
      policy: policy !== null,
      versioning: raw.versioning !== null,
      objectLock: raw.objectLock !== null,
      quota: raw.quota !== null,
      encryption: raw.encryption !== null,
      tags: raw.tags !== null,
      toggles: toggles !== null,
    },
  };
}

const quotaKey = (c: BucketConfig) =>
  c.quotaEnabled ? `${c.quotaType}:${quotaBytes(c.quotaSize, c.quotaUnit)}` : "off";

// Changed fields in save order
export function configChanges(
  loaded: BucketConfig,
  next: BucketConfig,
): { changes: ConfigChange[] } | { error: string } {
  const changes: ConfigChange[] = [];
  if (next.publicAccess !== loaded.publicAccess) {
    changes.push({
      label: "Public access",
      rpc: "SetBucketPolicy",
      params: { prefix: "", policy: policyFromPublicAccess(next.publicAccess) },
    });
  }
  if (next.versioning !== loaded.versioning) {
    changes.push({
      label: "Versioning",
      rpc: "SetBucketVersioning",
      params: { enabled: next.versioning },
    });
  }
  if (quotaKey(next) !== quotaKey(loaded)) {
    const bytes = next.quotaEnabled ? quotaBytes(next.quotaSize, next.quotaUnit) : 0;
    if (bytes === null) return { error: "Enter a quota size above 0 and at most 8 PB." };
    changes.push({
      label: "Quota",
      rpc: "SetBucketQuota",
      params: { type: next.quotaType, bytes },
    });
  }
  if (next.encryption !== loaded.encryption) {
    changes.push({
      label: "Encryption",
      rpc: "SetBucketEncryption",
      params: { enabled: next.encryption },
    });
  }
  const tags = cleanTags(next.tags);
  if (!tagsEqual(tags, loaded.tags)) {
    const problem = tagError(tags);
    if (problem) return { error: problem };
    changes.push({ label: "Tags", rpc: "SetBucketTags", params: { tags } });
  }
  if (next.s3Enabled !== loaded.s3Enabled || next.sftpEnabled !== loaded.sftpEnabled) {
    changes.push({
      label: "Protocol access",
      rpc: "SetBucketToggles",
      params: { s3Enabled: next.s3Enabled, sftpEnabled: next.sftpEnabled },
    });
  }
  return { changes };
}

export type SettingsSection = "settings" | "access";

// This tab's edits with other fields from base
export function sectionConfig(
  base: BucketConfig,
  edited: BucketConfig,
  section: SettingsSection,
): BucketConfig {
  return section === "access"
    ? { ...base, publicAccess: edited.publicAccess }
    : { ...edited, publicAccess: base.publicAccess };
}

// Dirty when save would write or edits are invalid
export function sectionDirty(
  loaded: BucketConfig,
  next: BucketConfig,
  section: SettingsSection,
): boolean {
  const plan = configChanges(loaded, sectionConfig(loaded, next, section));
  return "error" in plan || plan.changes.length > 0;
}

// Access tab IAM draft
export interface IAMDraft {
  policies: { name: string; policy: string; origin: "new" | "exclusive" | "shared" }[];
  users: {
    accessKey: string;
    status: "enabled" | "disabled";
    policies: string[];
    pendingSecretKey?: string;
  }[];
  removedPolicies: string[];
  detachedUsers: string[];
}

const byKey = <T>(rows: T[], key: (row: T) => string): T[] =>
  [...rows].sort((a, b) => (key(a) < key(b) ? -1 : key(a) > key(b) ? 1 : 0));

// Shared policy text stays untouched, existing user status saves separately
const iamSignature = (d: IAMDraft) =>
  JSON.stringify([
    byKey(d.policies, (p) => p.name).map((p) => [
      p.name,
      p.origin,
      p.origin === "shared" ? "" : p.policy,
    ]),
    byKey(d.users, (u) => u.accessKey).map((u) => [
      u.accessKey,
      [...u.policies].sort(),
      u.pendingSecretKey ?? "",
      u.pendingSecretKey ? u.status : "",
    ]),
    [...d.removedPolicies].sort(),
    [...d.detachedUsers].sort(),
  ]);

// Row and chip order do not count as changes
export function iamDirty(loaded: IAMDraft, next: IAMDraft): boolean {
  return iamSignature(loaded) !== iamSignature(next);
}

const BACKEND = "Not available in backend mode.";
const ERASURE = "Needs an erasure-coded server (4 or more drives).";
const needs = (permission: string) => `Needs the ${permission} permission to view.`;

// Disabled reason, null when editable
export function controlReason(control: Control, ctx: ControlContext): string | null {
  const { features: f, readable: r, config, isEdit } = ctx;
  if (control === "policy") return r.policy ? null : needs("s3:GetBucketPolicy");
  if (control === "objectLock" && isEdit) {
    return r.objectLock
      ? "Set when the bucket is created. It cannot change."
      : needs("s3:GetBucketObjectLockConfiguration");
  }
  if (!f) return "Server features did not load. Close and reopen this dialog.";
  switch (control) {
    case "versioning":
      if (!f.versioning) return f.backend ? BACKEND : ERASURE;
      if (!r.versioning) return needs("s3:GetBucketVersioning");
      return config.objectLock ? "Object locking keeps versioning on." : null;
    case "objectLock":
      if (f.objectLock) return null;
      return f.backend ? BACKEND : ERASURE;
    case "quota":
      if (!f.quota) return BACKEND;
      return r.quota ? null : needs("admin:GetBucketQuota");
    case "encryption":
      if (f.autoEncryption) {
        return "OBSTOR_KMS_AUTO_ENCRYPTION is on, so every new object is encrypted.";
      }
      if (f.backend) return BACKEND;
      if (!f.encryption) {
        return "No KMS is configured. Set OBSTOR_KMS_SECRET_KEY and restart the server.";
      }
      return r.encryption ? null : needs("s3:GetEncryptionConfiguration");
    case "tags":
      if (!f.tags) return BACKEND;
      return r.tags ? null : needs("s3:GetBucketTagging");
    case "toggles":
      // Protocol settings use tag IAM actions
      if (!f.tags) return BACKEND;
      return r.toggles ? null : needs("s3:GetBucketTagging");
  }
}

// First IAM validation error before any writes
export function iamSettingsError(
  bucket: string,
  policies: { name: string; policy: string; origin: "new" | "exclusive" | "shared" }[],
  users: { accessKey: string; pendingSecretKey?: string }[],
  policyNames: Set<string>,
  userKeys: Set<string>,
): string | null {
  const seen = new Set<string>();
  for (const p of policies) {
    if (p.origin === "shared") continue;
    const name = p.name.split("BUCKET_NAME").join(bucket);
    if (!name.trim()) return "Every policy needs a name.";
    if (seen.has(name)) return `Two policies are named ${name}.`;
    seen.add(name);
    try {
      JSON.parse(p.policy.split("BUCKET_NAME").join(bucket));
    } catch {
      return `Policy ${name} is not valid JSON.`;
    }
    if (p.origin === "new" && policyNames.has(name)) {
      return `A policy named ${name} already exists. Rename this one, or use Attach existing policy.`;
    }
  }
  for (const u of users) {
    if (u.pendingSecretKey && userKeys.has(u.accessKey)) {
      return `Access key ${u.accessKey} already exists. Use Attach existing user.`;
    }
  }
  return null;
}

// New and edited exclusive policies, BUCKET_NAME resolved
export function policyWrites(
  bucket: string,
  loaded: IAMDraft["policies"],
  draft: IAMDraft["policies"],
): { name: string; policy: string }[] {
  const resolve = (s: string) => s.split("BUCKET_NAME").join(bucket);
  const before = new Map(
    loaded.filter((p) => p.origin !== "shared").map((p) => [p.name, p.policy]),
  );
  return draft
    .filter((p) => p.origin !== "shared" && (p.origin === "new" || before.get(p.name) !== p.policy))
    .map((p) => ({ name: resolve(p.name), policy: resolve(p.policy) }));
}
