// Run via node --experimental-strip-types src/lib/activity.test.ts
import assert from "node:assert/strict";
import {
  ACTIVITY_GROUPS,
  type ActivityEvent,
  activityFailed,
  activityLabel,
  activitySourceLabel,
  activityTarget,
  activityWho,
  parseActivityParams,
  relativeTime,
} from "./activity.ts";

// Same API names as cmd/activity.go
const GO_APIS = [
  "PutObject",
  "PutObjectExtract",
  "PostPolicyBucket",
  "CopyObject",
  "CompleteMultipartUpload",
  "WebUpload",
  "RenameObject",
  "DeleteObject",
  "DeleteMultipleObjects",
  "RemoveObject",
  "PutBucket",
  "DeleteBucket",
  "PutBucketObjectLockConfig",
  "PutBucketTagging",
  "DeleteBucketTagging",
  "PutBucketReplicationConfig",
  "DeleteBucketReplicationConfig",
  "PutBucketACL",
  "PutBucketEncryption",
  "DeleteBucketEncryption",
  "PutBucketLifecycle",
  "DeleteBucketLifecycle",
  "PutBucketNotification",
  "PutBucketPolicy",
  "DeleteBucketPolicy",
  "PutBucketVersioning",
  "PutBucketQuotaConfig",
  "SetBucketTarget",
  "RemoveBucketTarget",
  "MakeBucket",
  "SetBucketPolicy",
  "SetBucketPolicyDoc",
  "SetBucketToggles",
  "SetBucketVersioning",
  "SetBucketQuota",
  "SetBucketEncryption",
  "SetBucketTags",
  "AddUser",
  "RemoveUser",
  "SetUserStatus",
  "UpdateGroupMembers",
  "SetGroupStatus",
  "AddServiceAccount",
  "UpdateServiceAccount",
  "DeleteServiceAccount",
  "SetPolicyForUserOrGroup",
  "AddIAMUser",
  "RemoveIAMUser",
  "SetIAMUserStatus",
  "SetIAMUserPolicy",
  "SetAuth",
  "AddCannedPolicy",
  "RemoveCannedPolicy",
  "SetCannedPolicy",
  "DeleteCannedPolicy",
  "Service",
  "ServerUpdate",
  "SetConfigKV",
  "DeleteConfigKV",
  "SetConfig",
  "RestoreConfigHistoryKV",
  "ClearConfigHistoryKV",
  "KMSCreateKey",
];

assert.equal(GO_APIS.length, 63);
for (const api of GO_APIS) {
  const label = activityLabel(api);
  assert.notEqual(label, api, `${api} has no label`);
  assert.match(label, /^[A-Z][a-z]* [a-zA-Z ]+$/, `${api} label "${label}" is not sentence case`);
}
assert.equal(activityLabel("PutObject"), "Put object");
assert.equal(activityLabel("WebUpload"), "Upload object");
assert.equal(activityLabel("CopyObject"), "Copy object");
assert.equal(activityLabel("CompleteMultipartUpload"), "Complete multipart upload");
assert.equal(activityLabel("RenameObject"), "Rename object");
assert.equal(activityLabel("DeleteObject"), "Delete object");
assert.equal(activityLabel("RemoveObject"), "Delete objects");
assert.equal(activityLabel("DeleteMultipleObjects"), "Delete objects");
assert.equal(activityLabel("PutBucket"), "Create bucket");
assert.equal(activityLabel("MakeBucket"), "Create bucket");
assert.equal(activityLabel("DeleteBucket"), "Delete bucket");
assert.equal(activityLabel("PutBucketPolicy"), "Set bucket policy");
assert.equal(activityLabel("SetBucketPolicy"), "Set bucket policy");
assert.equal(activityLabel("SetBucketPolicyDoc"), "Set bucket policy");
assert.equal(activityLabel("AddUser"), "Create user");
assert.equal(activityLabel("AddIAMUser"), "Create user");
assert.equal(activityLabel("AddCannedPolicy"), "Save policy");
assert.equal(activityLabel("SetCannedPolicy"), "Save policy");
assert.equal(activityLabel("SetConfigKV"), "Set config");
assert.equal(activityLabel("Service"), "Service action");
assert.equal(activityLabel("GetObject"), "GetObject");
assert.equal(activityLabel("toString"), "toString");
assert.equal(activityLabel(""), "");

assert.deepEqual(
  ACTIVITY_GROUPS.map((g) => g.id),
  ["write", "delete", "bucket", "iam", "policy", "service", "config"],
);
assert.deepEqual(
  ACTIVITY_GROUPS.map((g) => g.label),
  ["Writes", "Deletes", "Bucket changes", "Users", "Policies", "Service", "Configuration"],
);

const ev = (patch: Partial<ActivityEvent>): ActivityEvent => ({
  time: "2026-10-09T12:00:00Z",
  node: "127.0.0.1:9200",
  seq: 1,
  accessKey: "obstoradmin",
  source: "s3",
  api: "PutObject",
  bucket: "alpha",
  object: "",
  count: 0,
  status: 200,
  ...patch,
});

assert.deepEqual(activityTarget(ev({ object: "docs/c.txt" })), {
  kind: "object",
  bucket: "alpha",
  prefix: "docs/",
  name: "docs/c.txt",
});
assert.deepEqual(activityTarget(ev({ object: "a.txt" })), {
  kind: "object",
  bucket: "alpha",
  prefix: "",
  name: "a.txt",
});
assert.deepEqual(activityTarget(ev({ object: "docs/" })), {
  kind: "object",
  bucket: "alpha",
  prefix: "docs/",
  name: "docs/",
});
assert.deepEqual(activityTarget(ev({ object: "a/b/c/d.txt" })), {
  kind: "object",
  bucket: "alpha",
  prefix: "a/b/c/",
  name: "a/b/c/d.txt",
});
assert.deepEqual(activityTarget(ev({ api: "RemoveObject", source: "console", count: 3 })), {
  kind: "objects",
  bucket: "alpha",
  count: 3,
});
assert.deepEqual(activityTarget(ev({ api: "DeleteBucket" })), { kind: "bucket", bucket: "alpha" });
assert.deepEqual(
  activityTarget(ev({ api: "AddIAMUser", source: "console", bucket: "", object: "bob" })),
  { kind: "subject", name: "bob" },
);
assert.deepEqual(activityTarget(ev({ api: "SetConfigKV", source: "admin", bucket: "" })), {
  kind: "none",
});

assert.equal(activityWho(ev({})), "obstoradmin");
assert.equal(activityWho(ev({ accessKey: "" })), "another user");

assert.equal(activitySourceLabel("s3"), "S3");
assert.equal(activitySourceLabel("console"), "Console");
assert.equal(activitySourceLabel("sftp"), "SFTP");
assert.equal(activitySourceLabel("admin"), "Admin API");

assert.equal(activityFailed(200), false);
assert.equal(activityFailed(204), false);
assert.equal(activityFailed(404), true);
assert.equal(activityFailed(500), true);

const now = Date.parse("2026-10-09T12:00:00Z");
const ago = (ms: number) => new Date(now - ms).toISOString();
const sec = 1000;
const min = 60 * sec;
const hour = 60 * min;
const day = 24 * hour;
assert.equal(relativeTime(ago(0), now), "just now");
assert.equal(relativeTime(ago(59 * sec), now), "just now");
assert.equal(relativeTime(ago(60 * sec), now), "1 min ago");
assert.equal(relativeTime(ago(5 * min + 30 * sec), now), "5 min ago");
assert.equal(relativeTime(ago(3 * hour + 59 * min), now), "3 h ago");
assert.equal(relativeTime(ago(2 * day), now), "2 d ago");
assert.equal(relativeTime(ago(30 * day - sec), now), "29 d ago");
assert.equal(relativeTime(ago(30 * day), now), "");
assert.equal(relativeTime(ago(40 * day), now), "");
assert.equal(relativeTime(ago(-5 * min), now), "just now");
assert.equal(relativeTime("nope", now), "");
assert.equal(relativeTime("", now), "");

assert.deepEqual(parseActivityParams({}), { bucket: "", accessKey: "", group: "" });
assert.deepEqual(
  parseActivityParams({ bucket: " alpha ", accessKey: "  bob", action: "delete " }),
  { bucket: "alpha", accessKey: "bob", group: "delete" },
);
assert.deepEqual(
  parseActivityParams({ bucket: ["a", "b"], accessKey: ["x"], action: ["write", "delete"] }),
  { bucket: "", accessKey: "", group: "" },
);
assert.deepEqual(parseActivityParams({ action: "bogus" }), {
  bucket: "",
  accessKey: "",
  group: "",
});
assert.deepEqual(parseActivityParams({ action: "Write" }), {
  bucket: "",
  accessKey: "",
  group: "",
});
assert.deepEqual(parseActivityParams({ action: "iam" }), {
  bucket: "",
  accessKey: "",
  group: "iam",
});

console.log("activity tests passed");
