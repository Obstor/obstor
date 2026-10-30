import assert from "node:assert/strict";
import { addBucketToPolicy, fullBucketPolicy } from "./policy-templates.ts";

const policy = JSON.parse(fullBucketPolicy("tenant-bucket"));
assert.deepEqual(policy, {
  Version: "2012-10-17",
  Statement: [
    {
      Effect: "Allow",
      Action: ["s3:*"],
      Resource: ["arn:aws:s3:::tenant-bucket", "arn:aws:s3:::tenant-bucket/*"],
    },
  ],
});

// Bucket roles never grant instance administration
assert.equal(
  policy.Statement.some((s: { Action: string[] }) =>
    s.Action.some((action) => action.startsWith("admin:")),
  ),
  false,
);

const admin = { Effect: "Allow", Action: ["admin:ServerInfo", "admin:GetBucketQuota"] };
const scopedAdmin = { Effect: "Allow", Action: "admin:Heal", Resource: "arn:aws:s3:::original" };
const deny = {
  Effect: "Deny",
  Action: "s3:DeleteObject",
  Resource: "arn:aws:s3:::original/protected/*",
};
const original = {
  Version: "2012-10-17",
  Statement: [
    admin,
    scopedAdmin,
    deny,
    { Effect: "Allow", Action: "s3:GetObject", Resource: "arn:aws:s3:::original/*" },
  ],
};
const widened = JSON.parse(addBucketToPolicy(JSON.stringify(original), "second"));
assert.deepEqual(widened.Statement.slice(0, 3), [admin, scopedAdmin, deny]);
assert.deepEqual(widened.Statement[3].Resource, [
  "arn:aws:s3:::original/*",
  "arn:aws:s3:::second",
  "arn:aws:s3:::second/*",
]);
assert.equal(
  addBucketToPolicy(JSON.stringify(widened), "second"),
  JSON.stringify(widened, null, 2),
);
assert.throws(
  () =>
    addBucketToPolicy(
      JSON.stringify({ Statement: { Effect: "Allow", Action: ["s3:*", "admin:ServerInfo"] } }),
      "second",
    ),
  /must be edited directly/,
);
assert.throws(
  () => addBucketToPolicy(JSON.stringify({ Statement: admin }), "second"),
  /no S3 Allow statement/,
);
