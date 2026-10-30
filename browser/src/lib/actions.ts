"use server";

import { revalidatePath } from "next/cache";
import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";
import { buildAccessIndex, newUserError, policyExclusiveTo, policyGrants } from "./access-index";
import type { ActivityCursor, ActivityEvent, ActivityGroup, ActivityPage } from "./activity";
import type { AttentionItem } from "./attention";
import {
  type BucketConfig,
  type BucketFeatures,
  type ConfigChange,
  configChanges,
  iamSettingsError,
  parseBucketConfig,
  policyWrites,
  type RawBucketConfig,
  type Readable,
  sectionConfig,
} from "./bucket-config";
import { classifyLoginResponse, cookieSecure, type LoginResult, loginErrorMessage } from "./login";
import {
  listBuckets,
  publicHost,
  RPCError,
  rethrowSessionError,
  rpc,
  rpcEndpoint,
  rpcSettled,
} from "./rpc";

// Types
export interface NamedPolicy {
  name: string;
  policy: string;
}

// New means staged, exclusive is one bucket, shared is multiple buckets
export type PolicyOrigin = "new" | "exclusive" | "shared";

export interface PolicyRow extends NamedPolicy {
  origin: PolicyOrigin;
}

export interface IAMUser {
  accessKey: string;
  status: "enabled" | "disabled";
  policies: string[];
  pendingSecretKey?: string;
}

export interface AccessDraft extends BucketConfig {
  name: string;
  policies: PolicyRow[];
  users: IAMUser[];
  // Staged removals
  removedPolicies: string[];
  detachedUsers: string[];
  // Read-only context for access tab
  catchAllPolicies: string[];
  attachablePolicies: PolicyRow[];
  attachableUsers: IAMUser[];
}

// IAM editors need both policy and user listings
export interface BucketAccessLoad {
  draft: AccessDraft;
  readable: Readable;
  iam: boolean;
}

const msg = (e: unknown, fallback: string) => {
  rethrowSessionError(e);
  return e instanceof Error ? e.message : fallback;
};

// Auth
export async function loginAction(formData: FormData): Promise<{ error: string }> {
  const accessKey = String(formData.get("accessKey") ?? "").trim();
  const secretKey = String(formData.get("secretKey") ?? "");

  const res = await rpcUnauthed("Login", { username: accessKey, password: secretKey });
  if (!res.ok) return { error: loginErrorMessage(res.failure, rpcEndpoint()) };

  (await cookies()).set("obstor_token", res.token, {
    httpOnly: true,
    secure: cookieSecure((await headers()).get("origin"), process.env.OBSTOR_CONSOLE_SECURE_COOKIE),
    sameSite: "lax",
    path: "/",
    maxAge: 60 * 60 * 24,
  });
  redirect("/");
}

// Login doesn't need session, fail is LoginFailure
async function rpcUnauthed(method: string, params: Record<string, unknown>): Promise<LoginResult> {
  try {
    const res = await fetch(`${rpcEndpoint()}/obstor/webrpc`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "User-Agent": "Mozilla/5.0 Obstor Dashboard" },
      body: JSON.stringify({ id: 1, jsonrpc: "2.0", method: `web.${method}`, params }),
      cache: "no-store",
      signal: AbortSignal.timeout(10_000),
    });
    return classifyLoginResponse(res.status, res.headers.get("content-type"), await res.text());
  } catch {
    return { ok: false, failure: { kind: "network" } };
  }
}

export async function logoutAction() {
  const cookieStore = await cookies();
  cookieStore.delete("obstor_token");
  redirect("/login");
}

// Buckets CRUD
export async function deleteBucketAction(
  bucketName: string,
): Promise<{ success: true } | { error: string }> {
  try {
    await rpc("DeleteBucket", { bucketName });
  } catch (err) {
    return { error: msg(err, "Failed to delete bucket") };
  }
  revalidatePath("/", "layout");
  return { success: true };
}

// Only create bucket, other settings use bucket tabs
export async function createBucketAction(
  bucketName: string,
  objectLock: boolean,
): Promise<{ success: true } | { error: string }> {
  try {
    await rpc("MakeBucket", { bucketName, lockEnabled: objectLock });
  } catch (err) {
    return { error: msg(err, "Failed to create bucket") };
  }
  revalidatePath("/", "layout");
  return { success: true };
}

// Bucket Settings
export async function getBucketFeaturesAction(): Promise<BucketFeatures | { error: string }> {
  try {
    return await rpc<BucketFeatures>("BucketFeatures");
  } catch (err) {
    return { error: msg(err, "Failed to load server features") };
  }
}

// Features and read perm settings
export interface BucketConfigLoad {
  config: BucketConfig;
  features: BucketFeatures | null;
  readable: Readable;
}

async function readBucketConfig(bucketName: string): Promise<BucketConfigLoad> {
  const [policy, raw, toggles, features] = await rpcSettled([
    rpc<{ policy: string }>("GetBucketPolicy", { bucketName, prefix: "" }),
    rpc<RawBucketConfig>("GetBucketConfig", { bucketName }),
    rpc<{ s3Enabled: boolean; sftpEnabled: boolean }>("GetBucketToggles", { bucketName }),
    rpc<BucketFeatures>("BucketFeatures"),
  ]);
  if (raw.status === "rejected") {
    throw new Error(msg(raw.reason, "Failed to load bucket settings"));
  }
  const { config, readable } = parseBucketConfig(
    raw.value,
    policy.status === "fulfilled" ? policy.value.policy : null,
    toggles.status === "fulfilled" ? toggles.value : null,
  );
  return { config, features: features.status === "fulfilled" ? features.value : null, readable };
}

export async function getBucketConfigAction(
  bucketName: string,
): Promise<BucketConfigLoad | { error: string }> {
  try {
    return await readBucketConfig(bucketName);
  } catch (err) {
    return { error: msg(err, "Failed to load bucket settings") };
  }
}

// Save changed settings
export async function saveBucketConfigAction(
  bucketName: string,
  loaded: BucketConfig,
  next: BucketConfig,
): Promise<{ error: string; load: BucketConfigLoad | null }> {
  const plan = configChanges(loaded, sectionConfig(loaded, next, "settings"));
  if ("error" in plan) return { error: plan.error, load: null };
  try {
    let error = "";
    for (const change of plan.changes) {
      try {
        await rpc(change.rpc, { bucketName, ...change.params });
      } catch (err) {
        error = `${change.label}: ${msg(err, "request failed")}`;
        break;
      }
    }
    const load = await readBucketConfig(bucketName).catch((err) => {
      rethrowSessionError(err);
      return null;
    });
    return { error, load };
  } finally {
    revalidatePath("/", "layout");
  }
}

// Bucket policies and users from IAM
function buildAccessDraft(
  bucketName: string,
  config: BucketConfig,
  snap: AccessSnapshot,
): AccessDraft {
  const idx = buildAccessIndex(snap.buckets, snap.policies, snap.users);
  const policies: PolicyRow[] = idx.policies
    .filter((p) => policyGrants(p, bucketName) && !p.catchAll && !p.adminOnly)
    .map((p) => ({
      name: p.name,
      policy: p.policy,
      origin: policyExclusiveTo(p, bucketName) ? "exclusive" : "shared",
    }));
  const manageable = new Set(policies.map((r) => r.name));
  const users: IAMUser[] = idx.users
    .filter((u) => u.policies.some((pn) => manageable.has(pn)))
    .map((u) => ({ accessKey: u.accessKey, status: u.status, policies: u.policies }));
  const shownUsers = new Set(users.map((u) => u.accessKey));
  return {
    ...config,
    name: bucketName,
    policies,
    users,
    removedPolicies: [],
    detachedUsers: [],
    catchAllPolicies: idx.policies.filter((p) => p.catchAll).map((p) => p.name),
    attachablePolicies: idx.policies
      .filter((p) => !manageable.has(p.name) && !p.catchAll && !p.adminOnly && !p.parseError)
      .map((p) => ({ name: p.name, policy: p.policy, origin: "shared" as const })),
    attachableUsers: idx.users
      .filter((u) => !shownUsers.has(u.accessKey))
      .map((u) => ({ accessKey: u.accessKey, status: u.status, policies: u.policies })),
  };
}

export async function getBucketAccessAction(
  bucketName: string,
): Promise<BucketAccessLoad | { error: string }> {
  try {
    const [{ config, readable }, snap] = await Promise.all([
      readBucketConfig(bucketName),
      getAccessSnapshotAction(),
    ]);
    return {
      draft: buildAccessDraft(bucketName, config, snap),
      readable,
      iam: !snap.policiesError && !snap.usersError,
    };
  } catch (err) {
    return { error: msg(err, "Failed to load bucket access") };
  }
}

// Save public access and IAM edits
export async function saveBucketAccessAction(
  draft: AccessDraft,
  loaded: AccessDraft,
  iam: boolean,
): Promise<{ error: string; load: BucketAccessLoad | null; keepDraft: boolean }> {
  const plan = configChanges(loaded, sectionConfig(loaded, draft, "access"));
  if ("error" in plan) return { error: plan.error, load: null, keepDraft: true };
  let listing: IAMListing | null = null;
  if (iam) {
    try {
      listing = await checkIAMSettings(draft);
    } catch (err) {
      return { error: msg(err, "Failed to check users and policies"), load: null, keepDraft: true };
    }
  }
  const { error, iamWritten } = await applyBucketSettings(draft, loaded, plan.changes, listing);
  try {
    const load = await getBucketAccessAction(draft.name);
    return { error, load: "error" in load ? null : load, keepDraft: !iamWritten };
  } finally {
    revalidatePath("/", "layout");
  }
}

interface IAMListing {
  policyNames: Set<string>;
  heldBy: Map<string, string[]>;
}

// Load policies and users before save
async function checkIAMSettings(settings: AccessDraft): Promise<IAMListing> {
  const [allPolicies, allUsers] = await Promise.all([listPolicies(), listUsers()]);
  const policyNames = new Set(allPolicies.map((p) => p.name));
  const userKeys = new Set(allUsers.map((u) => u.accessKey));
  const err = iamSettingsError(
    settings.name,
    settings.policies,
    settings.users,
    policyNames,
    userKeys,
  );
  if (err) throw new Error(err);
  return { policyNames, heldBy: new Map(allUsers.map((u) => [u.accessKey, u.policies])) };
}

type IAMWrite = (label: string, method: string, params: Record<string, unknown>) => Promise<void>;

// Save config in order
async function applyBucketSettings(
  draft: AccessDraft,
  loaded: AccessDraft,
  changes: ConfigChange[],
  iam: IAMListing | null,
): Promise<{ error: string; iamWritten: boolean }> {
  let iamWritten = false;
  const write: IAMWrite = async (label, method, params) => {
    try {
      await rpc(method, params);
    } catch (err) {
      throw new Error(`${label}: ${msg(err, "request failed")}`);
    }
    iamWritten = true;
  };
  try {
    for (const change of changes) {
      try {
        await rpc(change.rpc, { bucketName: draft.name, ...change.params });
      } catch (err) {
        throw new Error(`${change.label}: ${msg(err, "request failed")}`);
      }
    }
    // No IAM listings means no IAM editors or writes
    if (iam) await applyIAM(draft, loaded, iam, write);
    return { error: "", iamWritten };
  } catch (err) {
    return { error: msg(err, "Failed to save access settings"), iamWritten };
  }
}

async function applyIAM(draft: AccessDraft, loaded: AccessDraft, iam: IAMListing, write: IAMWrite) {
  const resolveName = (n: string) => n.split("BUCKET_NAME").join(draft.name);
  const { policyNames, heldBy } = iam;

  for (const p of policyWrites(draft.name, loaded.policies, draft.policies)) {
    await write(`Policy ${p.name}`, "SetCannedPolicy", { ...p });
  }

  // Create users that were added on bucket modal
  for (const user of draft.users) {
    if (!user.pendingSecretKey) continue;
    const label = `User ${user.accessKey}`;
    await write(label, "AddIAMUser", {
      accessKey: user.accessKey,
      secretKey: user.pendingSecretKey,
      policy: "", // Policies attached later
    });
    if (user.status === "disabled") {
      await write(label, "SetIAMUserStatus", { accessKey: user.accessKey, enabled: false });
    }
  }

  // Attach policies
  const tabManaged = new Set<string>([
    ...draft.policies.map((p) => (p.origin === "shared" ? p.name : resolveName(p.name))),
    ...draft.removedPolicies,
  ]);
  const known = new Set<string>([
    ...policyNames,
    ...draft.policies.map((p) => resolveName(p.name)),
  ]);

  const targets = [
    ...draft.users.map((u) => ({
      accessKey: u.accessKey,
      checked: u.policies.map(resolveName),
    })),
    ...draft.detachedUsers.map((accessKey) => ({ accessKey, checked: [] as string[] })),
  ];

  for (const t of targets) {
    const held = heldBy.get(t.accessKey) ?? [];
    // Keep what this tab did not manage
    const keep = held.filter((pn) => !tabManaged.has(pn) && known.has(pn));
    const final = Array.from(new Set([...keep, ...t.checked]));
    if (final.length === held.length && final.every((pn) => held.includes(pn))) continue;
    await write(`User ${t.accessKey}`, "SetIAMUserPolicy", {
      accessKey: t.accessKey,
      policies: final.join(","),
    });
  }
}

// List IAM canned policies
async function listPolicies(): Promise<NamedPolicy[]> {
  try {
    const res = await rpc<{ policies?: NamedPolicy[] }>("ListCannedPolicies", {});
    return res.policies || [];
  } catch (err) {
    throw err instanceof Error ? err : new Error("Failed to list policies");
  }
}

export async function savePolicyAction(name: string, policyJSON: string) {
  try {
    await rpc("SetCannedPolicy", { name, policy: policyJSON });
    return { success: true };
  } catch (err) {
    return { error: msg(err, "Failed to save policy") };
  }
}

export async function deletePolicyAction(name: string) {
  try {
    await rpc("DeleteCannedPolicy", { name });
    return { success: true };
  } catch (err) {
    return { error: msg(err, "Failed to delete policy") };
  }
}

// List IAM users
async function listUsers(): Promise<IAMUser[]> {
  try {
    const res = await rpc<{ users?: IAMUser[] }>("ListIAMUsers", {});
    return (res.users || []).map((u) => ({ ...u, policies: u.policies ?? [] }));
  } catch (err) {
    throw err instanceof Error ? err : new Error("Failed to list users");
  }
}

export async function deleteUserAction(accessKey: string) {
  try {
    await rpc("RemoveIAMUser", { accessKey });
    return { success: true };
  } catch (err) {
    return { error: msg(err, "Failed to remove user") };
  }
}

export async function setUserStatusAction(accessKey: string, enabled: boolean) {
  try {
    await rpc("SetIAMUserStatus", { accessKey, enabled });
    return { success: true };
  } catch (err) {
    return { error: msg(err, "Failed to set user status") };
  }
}

// Objects
export async function deleteObjectsAction(
  bucketName: string,
  names: string[],
): Promise<{ success: true } | { error: string }> {
  try {
    await rpc("RemoveObject", { bucketname: bucketName, objects: names });
    return { success: true };
  } catch (err) {
    return { error: msg(err, "Failed to delete") };
  }
}

export async function getShareLink(bucketName: string, objectName: string, expiry = 300) {
  try {
    const result = await rpc<{ url: string }>("PresignedGet", {
      host: publicHost(),
      bucket: bucketName,
      object: objectName,
      expiry,
    });
    return { url: result.url };
  } catch (err) {
    return { error: msg(err, "Failed to generate link") };
  }
}

export async function getUploadURL(bucketName: string, prefix: string, objectName: string) {
  try {
    const result = await rpc<{ url: string }>("PresignedPut", {
      host: publicHost(),
      bucket: bucketName,
      prefix,
      object: objectName,
    });
    return { url: result.url };
  } catch (err) {
    return { error: msg(err, "Failed to get upload URL") };
  }
}

export async function getObjectChecksums(bucketName: string, objectName: string) {
  try {
    const result = await rpc<{ md5: string; sha1: string; sha256: string; sha512: string }>(
      "GetObjectChecksums",
      { bucketName, objectName },
    );
    return result;
  } catch (err) {
    return { error: msg(err, "Failed to compute checksums") };
  }
}

export async function getDownloadURL(bucketName: string, objectName: string) {
  try {
    const result = await rpc<{ url: string }>("PresignedGet", {
      host: publicHost(),
      bucket: bucketName,
      object: objectName,
      expiry: 300,
    });
    return { url: result.url };
  } catch (err) {
    return { error: msg(err, "Failed to get download URL") };
  }
}

// Drive endpoints per object
export async function getObjectLocationsAction(
  bucketName: string,
  objects: string[],
): Promise<Record<string, string[]> | { error: string }> {
  if (objects.length === 0) return {};
  try {
    const r = await rpc<{ objects: { name: string; endpoints: string[] | null }[] | null }>(
      "GetObjectLocations",
      { bucketName, objects },
    );
    return Object.fromEntries((r.objects ?? []).map((o) => [o.name, o.endpoints ?? []]));
  } catch (err) {
    return { error: msg(err, "Failed to load object locations") };
  }
}

// Access page
export interface AccessSnapshot {
  buckets: string[];
  policies: NamedPolicy[];
  users: IAMUser[];
  policiesError?: string;
  usersError?: string;
}

// Partial failure is reported per list
export async function getAccessSnapshotAction(): Promise<AccessSnapshot> {
  const [b, p, u] = await rpcSettled([listBuckets(), listPolicies(), listUsers()]);
  return {
    buckets: b.status === "fulfilled" ? b.value.map((x) => x.name) : [],
    policies: p.status === "fulfilled" ? p.value : [],
    users: u.status === "fulfilled" ? u.value : [],
    policiesError: p.status === "rejected" ? msg(p.reason, "Failed to list policies") : undefined,
    usersError: u.status === "rejected" ? msg(u.reason, "Failed to list users") : undefined,
  };
}

// One listing, which doubles as the capability probe
export async function getAccessBadgeAction(): Promise<{ canManage: boolean; noPolicy: number }> {
  try {
    const users = await listUsers();
    return { canManage: true, noPolicy: users.filter((u) => u.policies.length === 0).length };
  } catch (err) {
    rethrowSessionError(err);
    return { canManage: false, noPolicy: 0 };
  }
}

export async function setUserPoliciesAction(accessKey: string, policies: string[]) {
  try {
    await rpc("SetIAMUserPolicy", { accessKey, policies: policies.join(",") });
    revalidatePath("/access");
    return { success: true as const };
  } catch (err) {
    return { error: msg(err, "Failed to update policies") };
  }
}

// Composed from three RPCs because the server has no rename verb
export async function renamePolicyAction(oldName: string, newName: string, policyJSON: string) {
  if (!newName.trim()) return { error: "The new name cannot be empty." };
  if (oldName === newName) return { error: "That is already the policy name." };
  try {
    const existing = await listPolicies();
    if (existing.some((p) => p.name === newName)) {
      return { error: `A policy named ${newName} already exists.` };
    }
    await rpc("SetCannedPolicy", { name: newName, policy: policyJSON });

    const holders = (await listUsers()).filter((u) => u.policies.includes(oldName));
    const stuck: string[] = [];
    for (const u of holders) {
      const next = u.policies.map((pn) => (pn === oldName ? newName : pn));
      try {
        await rpc("SetIAMUserPolicy", { accessKey: u.accessKey, policies: next.join(",") });
      } catch (err) {
        rethrowSessionError(err);
        stuck.push(u.accessKey);
      }
    }
    if (stuck.length > 0) {
      return {
        error: `Renamed to ${newName} but ${stuck.length} of ${holders.length} holders were not moved. Both names exist. Retry from this dialog.`,
      };
    }

    await rpc("DeleteCannedPolicy", { name: oldName });
    revalidatePath("/access");
    return { success: true as const };
  } catch (err) {
    return { error: msg(err, "Failed to rename policy") };
  }
}

// AddIAMUser on an existing access key replaces the secret
export async function rotateSecretAction(
  accessKey: string,
  wasDisabled: boolean,
): Promise<{ secretKey: string } | { error: string }> {
  try {
    const res = await rpc<{ accessKey: string; secretKey: string }>("AddIAMUser", {
      accessKey,
      secretKey: "",
      policy: "",
    });
    if (wasDisabled) {
      await rpc("SetIAMUserStatus", { accessKey, enabled: false });
    }
    revalidatePath("/access");
    return { secretKey: res.secretKey };
  } catch (err) {
    return { error: msg(err, "Failed to rotate secret key") };
  }
}

// Check fresh user listing
export async function createUserAction(
  accessKey: string,
  policies: string[],
): Promise<{ accessKey: string; secretKey: string } | { error: string }> {
  try {
    const existing = (await listUsers()).map((u) => u.accessKey);
    const invalid = newUserError(accessKey, existing);
    if (invalid) return { error: invalid };
    const res = await rpc<{ accessKey: string; secretKey: string }>("AddIAMUser", {
      accessKey: accessKey.trim(),
      secretKey: "",
      policy: policies.join(","),
    });
    return { accessKey: res.accessKey, secretKey: res.secretKey };
  } catch (err) {
    return { error: msg(err, "Failed to create user") };
  } finally {
    revalidatePath("/access");
  }
}

// Notifications
export async function getAttentionAction(): Promise<AttentionItem[] | { error: string }> {
  try {
    return (await rpc<{ items: AttentionItem[] | null }>("Attention")).items ?? [];
  } catch (err) {
    return { error: msg(err, "Failed to load notifications") };
  }
}

// Returns token, caller signs out
export async function changeSecretAction(
  currentSecretKey: string,
  newSecretKey: string,
): Promise<{ ok: true } | { error: string }> {
  try {
    await rpc("SetAuth", { currentSecretKey, newSecretKey });
    return { ok: true };
  } catch (err) {
    if (err instanceof RPCError && err.code === "InvalidCurrentSecret") {
      return { error: "Current secret key is incorrect." };
    }
    return { error: msg(err, "Failed to change secret key") };
  }
}

// Activity
export async function listActivityAction(args: {
  bucket?: string;
  accessKey?: string;
  group?: ActivityGroup | "";
  before?: ActivityCursor | null;
  limit?: number;
}): Promise<ActivityPage | { error: string }> {
  try {
    const res = await rpc<Omit<ActivityPage, "events"> & { events: ActivityEvent[] | null }>(
      "ListActivity",
      {
        bucket: args.bucket ?? "",
        accessKey: args.accessKey ?? "",
        api: args.group ?? "",
        before: args.before ?? null,
        limit: args.limit ?? 50,
      },
    );
    return { ...res, events: res.events ?? [] };
  } catch (err) {
    return { error: msg(err, "Could not load activity") };
  }
}
