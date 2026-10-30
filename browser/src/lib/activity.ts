// Activity log helpers

export type ActivitySource = "s3" | "console" | "sftp" | "admin";

export interface ActivityEvent {
  time: string;
  node: string;
  seq: number;
  accessKey: string;
  source: ActivitySource;
  api: string;
  bucket: string;
  object: string;
  count: number;
  status: number;
}

export interface ActivityCursor {
  time: string;
  node: string;
  seq: number;
}

export interface ActivityPage {
  events: ActivityEvent[];
  next: ActivityCursor | null;
  partial: boolean;
  missingNodes: number;
  denied: number;
  dropped: number;
}

export type ActivityGroup = "write" | "delete" | "bucket" | "iam" | "policy" | "service" | "config";

export const ACTIVITY_GROUPS: readonly { id: ActivityGroup; label: string }[] = [
  { id: "write", label: "Writes" },
  { id: "delete", label: "Deletes" },
  { id: "bucket", label: "Bucket changes" },
  { id: "iam", label: "Users" },
  { id: "policy", label: "Policies" },
  { id: "service", label: "Service" },
  { id: "config", label: "Configuration" },
];

// One label per activityAPIs name in cmd/activity.go
const LABELS: Record<string, string> = {
  PutObject: "Put object",
  PutObjectExtract: "Extract archive",
  PostPolicyBucket: "Form upload",
  CopyObject: "Copy object",
  CompleteMultipartUpload: "Complete multipart upload",
  WebUpload: "Upload object",
  RenameObject: "Rename object",
  DeleteObject: "Delete object",
  DeleteMultipleObjects: "Delete objects",
  RemoveObject: "Delete objects",
  PutBucket: "Create bucket",
  DeleteBucket: "Delete bucket",
  PutBucketObjectLockConfig: "Set object lock",
  PutBucketTagging: "Set bucket tags",
  DeleteBucketTagging: "Remove bucket tags",
  PutBucketReplicationConfig: "Set replication",
  DeleteBucketReplicationConfig: "Remove replication",
  PutBucketACL: "Set bucket ACL",
  PutBucketEncryption: "Set encryption",
  DeleteBucketEncryption: "Remove encryption",
  PutBucketLifecycle: "Set lifecycle",
  DeleteBucketLifecycle: "Remove lifecycle",
  PutBucketNotification: "Set notifications",
  PutBucketPolicy: "Set bucket policy",
  DeleteBucketPolicy: "Remove bucket policy",
  PutBucketVersioning: "Set versioning",
  PutBucketQuotaConfig: "Set quota",
  SetBucketTarget: "Set replication target",
  RemoveBucketTarget: "Remove replication target",
  MakeBucket: "Create bucket",
  SetBucketPolicy: "Set bucket policy",
  SetBucketPolicyDoc: "Set bucket policy",
  SetBucketToggles: "Set protocol access",
  SetBucketVersioning: "Set versioning",
  SetBucketQuota: "Set quota",
  SetBucketEncryption: "Set encryption",
  SetBucketTags: "Set bucket tags",
  AddUser: "Create user",
  RemoveUser: "Delete user",
  SetUserStatus: "Set user status",
  UpdateGroupMembers: "Update group members",
  SetGroupStatus: "Set group status",
  AddServiceAccount: "Create service account",
  UpdateServiceAccount: "Update service account",
  DeleteServiceAccount: "Delete service account",
  SetPolicyForUserOrGroup: "Attach policy",
  AddIAMUser: "Create user",
  RemoveIAMUser: "Delete user",
  SetIAMUserStatus: "Set user status",
  SetIAMUserPolicy: "Attach policy",
  SetAuth: "Change secret key",
  AddCannedPolicy: "Save policy",
  RemoveCannedPolicy: "Delete policy",
  SetCannedPolicy: "Save policy",
  DeleteCannedPolicy: "Delete policy",
  Service: "Service action",
  ServerUpdate: "Server update",
  SetConfigKV: "Set config",
  DeleteConfigKV: "Delete config",
  SetConfig: "Replace config",
  RestoreConfigHistoryKV: "Restore config",
  ClearConfigHistoryKV: "Clear config history",
  KMSCreateKey: "Create KMS key",
};

export function activityLabel(api: string): string {
  return Object.hasOwn(LABELS, api) ? LABELS[api] : api;
}

export type ActivityTarget =
  | { kind: "object"; bucket: string; prefix: string; name: string }
  | { kind: "objects"; bucket: string; count: number }
  | { kind: "bucket"; bucket: string }
  | { kind: "subject"; name: string }
  | { kind: "none" };

export function activityTarget(e: ActivityEvent): ActivityTarget {
  if (!e.bucket && !e.object) return { kind: "none" };
  if (!e.bucket) return { kind: "subject", name: e.object };
  if (e.count > 0) return { kind: "objects", bucket: e.bucket, count: e.count };
  if (!e.object) return { kind: "bucket", bucket: e.bucket };
  const prefix = e.object.endsWith("/")
    ? e.object
    : e.object.slice(0, e.object.lastIndexOf("/") + 1);
  return { kind: "object", bucket: e.bucket, prefix, name: e.object };
}

export function activityWho(e: ActivityEvent): string {
  return e.accessKey || "another user";
}

const SOURCE_LABELS: Record<ActivitySource, string> = {
  s3: "S3",
  console: "Console",
  sftp: "SFTP",
  admin: "Admin API",
};

export function activitySourceLabel(s: ActivitySource): string {
  return SOURCE_LABELS[s] ?? s;
}

export function activityFailed(status: number): boolean {
  return status >= 400;
}

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// Empty past 30 days, caller shows exact time
export function relativeTime(iso: string, now: number): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const diff = now - t;
  if (diff < MINUTE) return "just now";
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)} min ago`;
  if (diff < DAY) return `${Math.floor(diff / HOUR)} h ago`;
  if (diff < 30 * DAY) return `${Math.floor(diff / DAY)} d ago`;
  return "";
}

const param = (v: string | string[] | undefined) => (typeof v === "string" ? v.trim() : "");

export function parseActivityParams(sp: {
  bucket?: string | string[];
  accessKey?: string | string[];
  action?: string | string[];
}): { bucket: string; accessKey: string; group: ActivityGroup | "" } {
  const action = param(sp.action);
  const group = ACTIVITY_GROUPS.find((g) => g.id === action)?.id ?? "";
  return { bucket: param(sp.bucket), accessKey: param(sp.accessKey), group };
}
