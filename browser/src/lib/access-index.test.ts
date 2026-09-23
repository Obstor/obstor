// Run via node --experimental-strip-types src/lib/access-index.test.ts
import assert from "node:assert/strict";
import { buildAccessIndex, policyExclusiveTo, policyGrants } from "./access-index.ts";

const pol = (name: string, statements: unknown[]) => ({
  name,
  policy: JSON.stringify({ Version: "2012-10-17", Statement: statements }),
});
const allow = (resource: string[] | undefined, action = ["s3:*"], effect = "Allow") =>
  resource
    ? { Effect: effect, Action: action, Resource: resource }
    : { Effect: effect, Action: action };

const buckets = ["logs", "logs-2026", "media"];

const policies = [
  pol("media-full", [allow(["arn:aws:s3:::media", "arn:aws:s3:::media/*"])]),
  pol("logs-glob", [allow(["arn:aws:s3:::logs*"])]),
  pol("gone", [allow(["arn:aws:s3:::archive/*"])]),
  pol("consoleAdmin", [allow(["arn:aws:s3:::*"])]),
  pol("admin-only", [allow(undefined, ["admin:ServerInfo"])]),
  pol("deny-media", [allow(["arn:aws:s3:::media"], ["s3:*"], "Deny")]),
  { name: "broken", policy: "{not json" },
];

const users = [
  { accessKey: "anna", status: "enabled" as const, policies: ["media-full"] },
  { accessKey: "orphan", status: "enabled" as const, policies: [] },
  { accessKey: "ghosted", status: "enabled" as const, policies: ["media-full", "vanished"] },
  { accessKey: "root-ish", status: "disabled" as const, policies: ["consoleAdmin"] },
];

const idx = buildAccessIndex(buckets, policies, users);
const P = (n: string) => {
  const found = idx.policies.find((p) => p.name === n);
  if (!found) throw new Error(`no policy ${n}`);
  return found;
};
const U = (n: string) => {
  const found = idx.users.find((u) => u.accessKey === n);
  if (!found) throw new Error(`no user ${n}`);
  return found;
};

// Glob resources
assert.deepEqual(P("logs-glob").buckets, ["logs", "logs-2026"]);
assert.deepEqual(P("media-full").buckets, ["media"]);

// Bucket token with no live bucket is an orphan
assert.deepEqual(P("gone").missingBuckets, ["archive"]);
assert.deepEqual(P("gone").buckets, []);

// Catch-all never exclusive to a bucket
assert.equal(P("consoleAdmin").catchAll, true);
assert.deepEqual(P("consoleAdmin").buckets, []);
assert.equal(P("consoleAdmin").builtIn, true);
assert.equal(policyGrants(P("consoleAdmin"), "media"), true);
assert.equal(policyExclusiveTo(P("consoleAdmin"), "media"), false);

// No Resource key and not read as a bucket policy
assert.equal(P("admin-only").adminOnly, true);
assert.equal(P("admin-only").catchAll, false);
assert.deepEqual(P("admin-only").buckets, []);

assert.equal(P("deny-media").denyOnly, true);
assert.equal(P("broken").parseError, true);

// Edit bucket policy document
assert.equal(policyExclusiveTo(P("media-full"), "media"), true);
assert.equal(policyExclusiveTo(P("logs-glob"), "logs"), false);
assert.equal(policyExclusiveTo(P("gone"), "media"), false);

// Zero-policy and catch-all are all visible
assert.deepEqual(U("orphan").policies, []);
assert.deepEqual(U("orphan").buckets, []);
assert.deepEqual(U("ghosted").missingPolicies, ["vanished"]);
assert.deepEqual(U("ghosted").buckets, ["media"]);
assert.equal(U("root-ish").catchAll, true);
assert.deepEqual(U("root-ish").buckets, []);

// Reverse index the delete confirm
assert.deepEqual(P("media-full").holders, ["anna", "ghosted"]);
assert.deepEqual(P("logs-glob").holders, []);

console.log("access-index: all assertions passed");
