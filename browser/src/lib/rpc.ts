import { cookies } from "next/headers";
import { redirect, unstable_rethrow } from "next/navigation";
import { cache } from "react";
import type { AttentionItem } from "./attention";
import { sessionExpired } from "./login";
import { parseRPCResponse, RPCError } from "./rpc-response";

export { RPCError } from "./rpc-response";

const RPC_ENDPOINT = process.env.OBSTOR_ENDPOINT || "http://localhost:9000";

export interface BucketEntry {
  name: string;
  creationDate: string;
  size: number | null;
  objects: number | null;
}

export interface BucketList {
  buckets: BucketEntry[];
  usageAsOf: string;
}

// Size and objects stay null until first scan
export const bucketList = cache(async (): Promise<BucketList> => {
  const r = await rpc<{ buckets: BucketEntry[] | null; usageAsOf?: string }>("ListBuckets");
  return { buckets: r.buckets ?? [], usageAsOf: r.usageAsOf ?? "" };
});

export const listBuckets = cache(async (): Promise<BucketEntry[]> => (await bucketList()).buckets);

export async function rpc<T = unknown>(
  method: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  const cookieStore = await cookies();
  const token = cookieStore.get("obstor_token")?.value;
  if (!token) throw new RPCError("Unauthorized", "WebTokenMissing");

  const res = await fetch(`${RPC_ENDPOINT}/obstor/webrpc`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "User-Agent": "Mozilla/5.0 Obstor Dashboard",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify({
      id: 1,
      jsonrpc: "2.0",
      method: `web.${method}`,
      params,
    }),
    cache: "no-store",
  });

  return parseRPCResponse<T>(res.status, await res.text());
}

// Keep framework redirects and wipe rejected sessions before login
export function rethrowSessionError(err: unknown): void {
  unstable_rethrow(err);
  if (err instanceof RPCError && sessionExpired(err.code)) redirect("/sign-out");
}

// Rejected session ends the request
export async function rpcSettled<T extends readonly unknown[] | []>(calls: T) {
  const results = await Promise.allSettled(calls);
  for (const result of results) {
    if (result.status === "rejected") rethrowSessionError(result.reason);
  }
  return results;
}

export interface ServerInfo {
  ObstorVersion: string;
  releaseTag: string;
  canSeeFleet: boolean;
  canSeeSettings: boolean;
  region: string;
  mode: "fs" | "erasure" | "backend";
  sftpEnabled: boolean;
  account: "root" | "temporary" | "service" | "iam";
  metrics: boolean;
  usage: boolean;
  // Settings tier only
  sftpAddress: string;
  ObstorPlatform: string;
  ObstorRuntime: string;
  commit: string;
  startedAt: string;
  kms: boolean;
  autoEncryption: boolean;
  tls: boolean;
  trustedProxies: boolean;
}

// Failed call hides fleet controls
export const serverInfo = cache(async (): Promise<ServerInfo | null> => {
  try {
    return await rpc<ServerInfo>("ServerInfo");
  } catch (err) {
    rethrowSessionError(err);
    return null;
  }
});

// One Attention call per request
export const attention = cache(async (): Promise<AttentionItem[]> => {
  try {
    return (await rpc<{ items: AttentionItem[] | null }>("Attention")).items ?? [];
  } catch (err) {
    rethrowSessionError(err);
    return [];
  }
});

export type AccessLabel = "Private" | "Public read" | "Public read/write" | "Public write";

const ACCESS_LABELS: Record<string, AccessLabel> = {
  none: "Private",
  readonly: "Public read",
  readwrite: "Public read/write",
  writeonly: "Public write",
};

// One GetBucketPolicy per bucket
export async function bucketAccess(names: string[]): Promise<Record<string, AccessLabel | null>> {
  const res = await rpcSettled(
    names.map((bucketName) =>
      rpc<{ policy: string }>("GetBucketPolicy", { bucketName, prefix: "" }),
    ),
  );
  return Object.fromEntries(
    names.map((name, i) => {
      const r = res[i];
      return [
        name,
        r.status === "fulfilled" ? (ACCESS_LABELS[r.value.policy || "none"] ?? null) : null,
      ];
    }),
  );
}

// Public S3 host or RPC_ENDPOINT for internal
export function publicHost(): string {
  return process.env.OBSTOR_HOST || "localhost:9000";
}

export function publicURL(): string {
  const host = publicHost();
  return host.includes("://") ? host : `${new URL(RPC_ENDPOINT).protocol}//${host}`;
}

export function rpcEndpoint(): string {
  return RPC_ENDPOINT;
}

// Display-only since server verifies token on every RPC
export async function sessionAccessKey(): Promise<string> {
  const token = (await cookies()).get("obstor_token")?.value ?? "";
  try {
    const claims: unknown = JSON.parse(
      Buffer.from(token.split(".")[1] ?? "", "base64url").toString("utf8"),
    );
    return typeof claims === "object" &&
      claims !== null &&
      "accessKey" in claims &&
      typeof claims.accessKey === "string"
      ? claims.accessKey
      : "";
  } catch {
    return "";
  }
}
