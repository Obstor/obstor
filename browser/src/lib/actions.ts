"use server";

import { revalidatePath } from "next/cache";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { buildAccessIndex, policyExclusiveTo, policyGrants } from "./access-index";
import { listBuckets, rpc } from "./rpc";
import { RESERVED_BUCKET_NAMES } from "./safe-name";

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

export interface BucketSettings {
  name: string;
  publicAccess: "private" | "public-read" | "public-read-write";
  versioning: boolean;
  objectLocking: boolean;
  quotaEnabled: boolean;
  quotaType: "hard" | "fifo";
  quotaSize: string;
  quotaUnit: "GB" | "TB" | "PB";
  encryptionEnabled: boolean;
  encryptionType: "SSE-S3" | "SSE-KMS";
  kmsKeyId: string;
  tags: { key: string; value: string }[];
  policies: PolicyRow[];
  users: IAMUser[];
  // Staged removals
  removedPolicies: string[];
  detachedUsers: string[];
  // Read-only context for access tab
  catchAllPolicies: string[];
  attachablePolicies: PolicyRow[];
  attachableUsers: IAMUser[];
  sftpEnabled: boolean;
  s3Enabled: boolean;
  placementStrategy: "smart" | "custom";
  regions: string[];
}

// Auth
export async function loginAction(formData: FormData) {
  const accessKey = formData.get("accessKey") as string;
  const secretKey = formData.get("secretKey") as string;

  try {
    const result = await rpcUnauthed<{ token: string }>("Login", {
      username: accessKey,
      password: secretKey,
    });

    const cookieStore = await cookies();
    cookieStore.set("obstor_token", result.token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      path: "/",
      maxAge: 60 * 60 * 24 * 7,
    });
  } catch {
    return { error: "Invalid access key or secret key" };
  }

  redirect("/");
}

async function rpcUnauthed<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
  const endpoint = process.env.OBSTOR_ENDPOINT || "http://localhost:9000";
  const res = await fetch(`${endpoint}/obstor/webrpc`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "User-Agent": "Mozilla/5.0 Obstor Dashboard" },
    body: JSON.stringify({ id: 1, jsonrpc: "2.0", method: `web.${method}`, params }),
    cache: "no-store",
  });
  const data = await res.json();
  if (data.error) throw new Error(data.error.message);
  return data.result as T;
}

export async function logoutAction() {
  const cookieStore = await cookies();
  cookieStore.delete("obstor_token");
  redirect("/login");
}

// Buckets CRUD
export async function deleteBucketAction(bucketName: string) {
  try {
    await rpc("DeleteBucket", { bucketName });
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to delete bucket" };
  }

  revalidatePath("/", "layout");
  redirect("/");
}

// Bucket Settings
function publicAccessToBackend(policy: BucketSettings["publicAccess"]): string {
  switch (policy) {
    case "public-read":
      return "readonly";
    case "public-read-write":
      return "readwrite";
    default:
      return "none";
  }
}

export async function getBucketSettingsAction(
  bucketName: string,
): Promise<BucketSettings | { error: string }> {
  try {
    let cannedPublic: BucketSettings["publicAccess"] = "private";
    try {
      const res = await rpc<{ policy: string }>("GetBucketPolicy", {
        bucketName,
        prefix: "",
      });
      if (res.policy === "readonly") cannedPublic = "public-read";
      else if (res.policy === "readwrite" || res.policy === "writeonly")
        cannedPublic = "public-read-write";
    } catch {
      // Todo: Add error handling
    }

    // Bucket filters
    const snap = await getAccessSnapshotAction();
    const idx = buildAccessIndex(snap.buckets, snap.policies, snap.users);

    const rows: PolicyRow[] = idx.policies
      .filter((p) => policyGrants(p, bucketName) && !p.catchAll && !p.adminOnly)
      .map((p) => ({
        name: p.name,
        policy: p.policy,
        origin: policyExclusiveTo(p, bucketName) ? "exclusive" : "shared",
      }));
    const manageable = new Set(rows.map((r) => r.name));
    const users: IAMUser[] = idx.users
      .filter((u) => u.policies.some((pn) => manageable.has(pn)))
      .map((u) => ({ accessKey: u.accessKey, status: u.status, policies: u.policies }));
    const shownUsers = new Set(users.map((u) => u.accessKey));

    let s3Enabled = true;
    let sftpEnabled = true;
    try {
      const res = await rpc<{ s3Enabled: boolean; sftpEnabled: boolean }>("GetBucketToggles", {
        bucketName,
      });
      s3Enabled = res.s3Enabled;
      sftpEnabled = res.sftpEnabled;
    } catch {
      // Todo: Add error handling
    }

    return {
      name: bucketName,
      publicAccess: cannedPublic,
      versioning: false,
      objectLocking: false,
      quotaEnabled: false,
      quotaType: "hard",
      quotaSize: "",
      quotaUnit: "GB",
      encryptionEnabled: false,
      encryptionType: "SSE-S3",
      kmsKeyId: "",
      tags: [],
      policies: rows,
      users,
      removedPolicies: [],
      detachedUsers: [],
      catchAllPolicies: idx.policies.filter((p) => p.catchAll).map((p) => p.name),
      attachablePolicies: idx.policies
        .filter((p) => !manageable.has(p.name) && !p.catchAll && !p.adminOnly && !p.parseError)
        .map((p) => ({
          name: p.name,
          policy: p.policy,
          origin: "shared" as const,
        })),
      attachableUsers: idx.users
        .filter((u) => !shownUsers.has(u.accessKey))
        .map((u) => ({ accessKey: u.accessKey, status: u.status, policies: u.policies })),
      sftpEnabled,
      s3Enabled,
      placementStrategy: "smart",
      regions: [],
    };
  } catch (err) {
    return {
      error: err instanceof Error ? err.message : "Failed to load bucket settings",
    };
  }
}

export async function createBucketWithSettingsAction(
  settings: BucketSettings,
): Promise<{ success: true; bucketName: string } | { error: string }> {
  if (RESERVED_BUCKET_NAMES.has(settings.name)) {
    return { error: `${settings.name} is reserved by the console. Pick another name.` };
  }
  try {
    await rpc("MakeBucket", { bucketName: settings.name });
    await applyBucketSettings(settings);
    return { success: true, bucketName: settings.name };
  } catch (err) {
    return {
      error: err instanceof Error ? err.message : "Failed to create bucket",
    };
  } finally {
    revalidatePath("/", "layout");
  }
}

export async function updateBucketSettingsAction(
  settings: BucketSettings,
): Promise<{ success: true } | { error: string }> {
  try {
    await applyBucketSettings(settings);
    return { success: true };
  } catch (err) {
    return {
      error: err instanceof Error ? err.message : "Failed to update bucket settings",
    };
  } finally {
    revalidatePath("/", "layout");
  }
}

async function applyBucketSettings(settings: BucketSettings) {
  await rpc("SetBucketPolicy", {
    bucketName: settings.name,
    prefix: "",
    policy: publicAccessToBackend(settings.publicAccess),
  });

  // Feature Toggles
  await rpc("SetBucketToggles", {
    bucketName: settings.name,
    s3Enabled: settings.s3Enabled,
    sftpEnabled: settings.sftpEnabled,
  });

  // List IAM policies
  const resolveName = (n: string) => n.split("BUCKET_NAME").join(settings.name);

  const [allPolicies, allUsers] = await Promise.all([listPolicies(), listUsers()]);
  const policyNames = new Set(allPolicies.map((p) => p.name));
  const userKeys = new Set(allUsers.map((u) => u.accessKey));
  const heldBy = new Map(allUsers.map((u) => [u.accessKey, u.policies]));

  for (const p of settings.policies) {
    if (p.origin === "shared") continue;
    const name = resolveName(p.name);
    if (!name.trim()) throw new Error("Every policy needs a name.");
    try {
      JSON.parse(p.policy.split("BUCKET_NAME").join(settings.name));
    } catch {
      throw new Error(`Policy ${name} is not valid JSON.`);
    }
    if (p.origin === "new" && policyNames.has(name)) {
      throw new Error(
        `A policy named ${name} already exists. Rename this one, or use Attach existing policy.`,
      );
    }
  }
  for (const u of settings.users) {
    if (u.pendingSecretKey && userKeys.has(u.accessKey)) {
      throw new Error(`Access key ${u.accessKey} already exists. Use Attach existing user.`);
    }
  }
  for (const p of settings.policies) {
    if (p.origin === "shared") continue;
    await rpc("SetCannedPolicy", {
      name: resolveName(p.name),
      policy: p.policy.split("BUCKET_NAME").join(settings.name),
    });
  }

  // Create users that were added on bucket modal
  for (const user of settings.users) {
    if (!user.pendingSecretKey) continue;
    await rpc("AddIAMUser", {
      accessKey: user.accessKey,
      secretKey: user.pendingSecretKey,
      policy: "", // Policies attached later
    });
    if (user.status === "disabled") {
      await rpc("SetIAMUserStatus", { accessKey: user.accessKey, enabled: false });
    }
  }

  // Attach policies
  const modalManaged = new Set<string>([
    ...settings.policies.map((p) => (p.origin === "shared" ? p.name : resolveName(p.name))),
    ...settings.removedPolicies,
  ]);
  const known = new Set<string>([
    ...policyNames,
    ...settings.policies.map((p) => resolveName(p.name)),
  ]);

  const targets = [
    ...settings.users.map((u) => ({
      accessKey: u.accessKey,
      checked: u.policies.map(resolveName),
    })),
    ...settings.detachedUsers.map((accessKey) => ({ accessKey, checked: [] as string[] })),
  ];

  for (const t of targets) {
    const held = heldBy.get(t.accessKey) ?? [];
    // Keep what this modal did not manage
    const keep = held.filter((pn) => !modalManaged.has(pn) && known.has(pn));
    const final = Array.from(new Set([...keep, ...t.checked]));
    if (final.length === held.length && final.every((pn) => held.includes(pn))) continue;
    await rpc("SetIAMUserPolicy", { accessKey: t.accessKey, policies: final.join(",") });
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
    return { error: err instanceof Error ? err.message : "Failed to save policy" };
  }
}

export async function deletePolicyAction(name: string) {
  try {
    await rpc("DeleteCannedPolicy", { name });
    return { success: true };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to delete policy" };
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
    return { error: err instanceof Error ? err.message : "Failed to remove user" };
  }
}

export async function setUserStatusAction(accessKey: string, enabled: boolean) {
  try {
    await rpc("SetIAMUserStatus", { accessKey, enabled });
    return { success: true };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to set user status" };
  }
}

// Objects
export async function deleteObjectAction(bucketName: string, objectName: string) {
  try {
    await rpc("RemoveObject", { bucketname: bucketName, objects: [objectName] });
    return { success: true };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to delete object" };
  }
}

export async function getShareLink(bucketName: string, objectName: string, expiry = 300) {
  try {
    const result = await rpc<{ url: string }>("PresignedGet", {
      host: process.env.OBSTOR_HOST || "localhost:9000",
      bucket: bucketName,
      object: objectName,
      expiry,
    });
    return { url: result.url };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to generate link" };
  }
}

export async function getUploadURL(bucketName: string, prefix: string, objectName: string) {
  try {
    const result = await rpc<{ url: string }>("PresignedPut", {
      host: process.env.OBSTOR_HOST || "localhost:9000",
      bucket: bucketName,
      prefix,
      object: objectName,
    });
    return { url: result.url };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to get upload URL" };
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
    return { error: err instanceof Error ? err.message : "Failed to compute checksums" };
  }
}

export async function getDownloadURL(bucketName: string, objectName: string) {
  try {
    const result = await rpc<{ url: string }>("PresignedGet", {
      host: process.env.OBSTOR_HOST || "localhost:9000",
      bucket: bucketName,
      object: objectName,
      expiry: 300,
    });
    return { url: result.url };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "Failed to get download URL" };
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

const msg = (e: unknown, fallback: string) => (e instanceof Error ? e.message : fallback);

// Partial failure is reported per list
export async function getAccessSnapshotAction(): Promise<AccessSnapshot> {
  const [b, p, u] = await Promise.allSettled([listBuckets(), listPolicies(), listUsers()]);
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
  } catch {
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
      } catch {
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
