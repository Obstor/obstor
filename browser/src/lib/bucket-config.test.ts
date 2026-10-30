// Run via node --experimental-strip-types src/lib/bucket-config.test.ts
import assert from "node:assert/strict";
import {
  ALL_READABLE,
  type BucketConfig,
  cleanTags,
  configChanges,
  controlReason,
  createdConfig,
  type IAMDraft,
  iamDirty,
  iamSettingsError,
  MAX_QUOTA_BYTES,
  parseBucketConfig,
  policyWrites,
  publicAccessFromPolicy,
  quotaBytes,
  quotaFields,
  sectionConfig,
  sectionDirty,
  tagError,
  tagsEqual,
} from "./bucket-config.ts";

const GB = 1024 ** 3;
const TB = 1024 ** 4;
const PB = 1024 ** 5;

// Quota units match humanSize
assert.equal(quotaBytes("1", "GB"), GB);
assert.equal(quotaBytes("1.5", "TB"), 1.5 * TB);
assert.equal(quotaBytes(" 2 ", "GB"), 2 * GB);
assert.equal(quotaBytes("8", "PB"), MAX_QUOTA_BYTES);
assert.equal(quotaBytes("8.01", "PB"), null);
for (const bad of ["", " ", "0", "-1", "abc", "1e-12"]) assert.equal(quotaBytes(bad, "GB"), null);
assert.deepEqual(quotaFields(GB), { quotaSize: "1", quotaUnit: "GB" });
assert.deepEqual(quotaFields(100 * GB), { quotaSize: "100", quotaUnit: "GB" });
assert.deepEqual(quotaFields(GB / 2), { quotaSize: "0.5", quotaUnit: "GB" });
assert.deepEqual(quotaFields(1.5 * TB), { quotaSize: "1.5", quotaUnit: "TB" });
assert.deepEqual(quotaFields(8 * PB), { quotaSize: "8", quotaUnit: "PB" });
for (const b of [GB, 1.5 * TB, 8 * PB]) {
  const f = quotaFields(b);
  assert.equal(quotaBytes(f.quotaSize, f.quotaUnit), b);
}

// Public tags have their own 50-tag limit
assert.equal(
  tagError([
    { key: "team", value: "a" },
    { key: "env", value: "" },
  ]),
  null,
);
assert.equal(tagError([{ key: "a+b-c.d_e:f/g@h i=j", value: "" }]), null);
assert.equal(tagError([{ key: "", value: "x" }]), "Every tag needs a key.");
assert.equal(
  tagError([{ key: "__obstor_s3_enabled", value: "true" }]),
  "Tag keys starting with __obstor_ are reserved.",
);
assert.equal(
  tagError([{ key: "k".repeat(129), value: "" }]),
  "Tag keys can be at most 128 characters.",
);
assert.equal(
  tagError([{ key: "k", value: "v".repeat(257) }]),
  "Tag values can be at most 256 characters.",
);
const charsetError = "Tags can use letters, digits, spaces and + - . _ : / @ = only.";
assert.equal(tagError([{ key: "bad*key", value: "" }]), charsetError);
assert.equal(tagError([{ key: "ok", value: "caf\u00e9" }]), charsetError);
assert.equal(
  tagError([
    { key: "a", value: "1" },
    { key: "a", value: "2" },
  ]),
  'Tag key "a" is used twice.',
);
const many = Array.from({ length: 51 }, (_, i) => ({ key: `k${i}`, value: "v" }));
assert.equal(tagError(many), "A bucket can have at most 50 tags.");
assert.equal(tagError(many.slice(0, 50)), null);
assert.deepEqual(
  cleanTags([
    { key: "", value: "" },
    { key: "a", value: "" },
  ]),
  [{ key: "a", value: "" }],
);
assert.ok(
  tagsEqual(
    [
      { key: "a", value: "1" },
      { key: "b", value: "2" },
    ],
    [
      { key: "b", value: "2" },
      { key: "a", value: "1" },
    ],
  ),
);
assert.ok(!tagsEqual([{ key: "a", value: "1" }], [{ key: "a", value: "2" }]));

// Unchanged settings send no writes
const base = createdConfig(false);
assert.deepEqual(configChanges(base, { ...base }), { changes: [] });
const loaded = {
  ...base,
  versioning: true,
  quotaEnabled: true,
  quotaSize: "1.5",
  quotaUnit: "TB" as const,
  tags: [{ key: "a", value: "1" }],
};
assert.deepEqual(
  configChanges(loaded, {
    ...loaded,
    quotaSize: "1.50",
    tags: [
      { key: "", value: "" },
      { key: "a", value: "1" },
    ],
  }),
  { changes: [] },
);
assert.deepEqual(configChanges(loaded, { ...loaded, versioning: false }), {
  changes: [{ label: "Versioning", rpc: "SetBucketVersioning", params: { enabled: false } }],
});
assert.deepEqual(configChanges(loaded, { ...loaded, quotaEnabled: false }), {
  changes: [{ label: "Quota", rpc: "SetBucketQuota", params: { type: "hard", bytes: 0 } }],
});
assert.deepEqual(
  configChanges(base, {
    ...base,
    quotaEnabled: true,
    quotaType: "fifo",
    quotaSize: "2",
    quotaUnit: "GB",
  }),
  { changes: [{ label: "Quota", rpc: "SetBucketQuota", params: { type: "fifo", bytes: 2 * GB } }] },
);
assert.deepEqual(configChanges(base, { ...base, quotaEnabled: true, quotaSize: "0" }), {
  error: "Enter a quota size above 0 and at most 8 PB.",
});
assert.deepEqual(configChanges(base, { ...base, tags: [{ key: "__obstor_x", value: "" }] }), {
  error: "Tag keys starting with __obstor_ are reserved.",
});
assert.deepEqual(
  configChanges(base, {
    ...base,
    publicAccess: "public-read",
    encryption: true,
    sftpEnabled: true,
  }),
  {
    changes: [
      {
        label: "Public access",
        rpc: "SetBucketPolicy",
        params: { prefix: "", policy: "readonly" },
      },
      { label: "Encryption", rpc: "SetBucketEncryption", params: { enabled: true } },
      {
        label: "Protocol access",
        rpc: "SetBucketToggles",
        params: { s3Enabled: true, sftpEnabled: true },
      },
    ],
  },
);
// Locked bucket already has versioning, skip versioning write
const locked = createdConfig(true);
assert.equal(locked.versioning, true);
assert.deepEqual(configChanges(locked, { ...locked }), { changes: [] });

// Unreadable fields use disabled defaults and never write
const parsed = parseBucketConfig(
  {
    versioning: "Suspended",
    objectLock: false,
    quota: { type: "fifo", bytes: 1.5 * TB },
    encryption: true,
    tags: [{ key: "team", value: "a" }],
  },
  "writeonly",
  { s3Enabled: false, sftpEnabled: true },
);
assert.deepEqual(parsed.config, {
  ...createdConfig(false),
  publicAccess: "public-read-write",
  quotaEnabled: true,
  quotaType: "fifo",
  quotaSize: "1.5",
  quotaUnit: "TB",
  encryption: true,
  tags: [{ key: "team", value: "a" }],
  s3Enabled: false,
  sftpEnabled: true,
});
assert.deepEqual(parsed.readable, ALL_READABLE);
const hidden = parseBucketConfig(
  { versioning: null, objectLock: null, quota: null, encryption: null, tags: null },
  null,
  null,
);
assert.deepEqual(hidden.config, createdConfig(false));
assert.ok(Object.values(hidden.readable).every((v) => v === false));
assert.equal(publicAccessFromPolicy("none"), "private");

// Disabled reasons, first match wins
const fsMode = {
  versioning: false,
  objectLock: false,
  quota: true,
  encryption: false,
  autoEncryption: false,
  tags: true,
  backend: false,
};
const erasure = { ...fsMode, versioning: true, objectLock: true, encryption: true };
const backend = { ...fsMode, quota: false, tags: false, backend: true };
const ctx = (over: object = {}) => ({
  features: fsMode,
  readable: ALL_READABLE,
  config: base,
  isEdit: false,
  ...over,
});
const ERASURE = "Needs an erasure-coded server (4 or more drives).";
assert.equal(controlReason("versioning", ctx()), ERASURE);
assert.equal(controlReason("objectLock", ctx()), ERASURE);
assert.equal(controlReason("quota", ctx()), null);
assert.equal(controlReason("tags", ctx()), null);
assert.equal(
  controlReason("encryption", ctx()),
  "No KMS is configured. Set OBSTOR_KMS_SECRET_KEY and restart the server.",
);
assert.equal(controlReason("encryption", ctx({ features: erasure })), null);
assert.equal(
  controlReason(
    "encryption",
    ctx({ features: erasure, readable: { ...ALL_READABLE, encryption: false } }),
  ),
  "Needs the s3:GetEncryptionConfiguration permission to view.",
);
assert.equal(controlReason("versioning", ctx({ features: erasure })), null);
assert.equal(
  controlReason("versioning", ctx({ features: erasure, config: { ...base, objectLock: true } })),
  "Object locking keeps versioning on.",
);
assert.equal(
  controlReason("objectLock", ctx({ features: erasure, isEdit: true })),
  "Set when the bucket is created. It cannot change.",
);
assert.equal(
  controlReason(
    "objectLock",
    ctx({ isEdit: true, readable: { ...ALL_READABLE, objectLock: false } }),
  ),
  "Needs the s3:GetBucketObjectLockConfiguration permission to view.",
);
for (const c of ["versioning", "objectLock", "quota", "encryption", "tags", "toggles"] as const) {
  assert.equal(controlReason(c, ctx({ features: backend })), "Not available in backend mode.");
}
assert.equal(
  controlReason("encryption", ctx({ features: { ...erasure, autoEncryption: true } })),
  "OBSTOR_KMS_AUTO_ENCRYPTION is on, so every new object is encrypted.",
);
assert.equal(
  controlReason("quota", ctx({ readable: { ...ALL_READABLE, quota: false } })),
  "Needs the admin:GetBucketQuota permission to view.",
);
assert.equal(
  controlReason("policy", ctx({ features: null, readable: { ...ALL_READABLE, policy: false } })),
  "Needs the s3:GetBucketPolicy permission to view.",
);
assert.equal(
  controlReason("tags", ctx({ features: null })),
  "Server features did not load. Close and reopen this dialog.",
);

// Check IAM before MakeBucket so name collision cannot leave a partial create
const full = {
  name: "BUCKET_NAME-full",
  policy: '{"Resource":"arn:aws:s3:::BUCKET_NAME/*"}',
  origin: "new" as const,
};
assert.equal(iamSettingsError("foo", [full], [], new Set(), new Set()), null);
assert.equal(
  iamSettingsError("foo", [full], [], new Set(["foo-full"]), new Set()),
  "A policy named foo-full already exists. Rename this one, or use Attach existing policy.",
);
assert.equal(
  iamSettingsError("foo", [{ ...full, origin: "exclusive" }], [], new Set(["foo-full"]), new Set()),
  null,
);
assert.equal(
  iamSettingsError(
    "foo",
    [{ ...full, origin: "shared", policy: "not json" }],
    [],
    new Set(),
    new Set(),
  ),
  null,
);
assert.equal(
  iamSettingsError("foo", [{ ...full, name: " " }], [], new Set(), new Set()),
  "Every policy needs a name.",
);
assert.equal(
  iamSettingsError("foo", [{ ...full, policy: "{" }], [], new Set(), new Set()),
  "Policy foo-full is not valid JSON.",
);
assert.equal(
  iamSettingsError(
    "foo",
    [],
    [{ accessKey: "ak", pendingSecretKey: "s" }],
    new Set(),
    new Set(["ak"]),
  ),
  "Access key ak already exists. Use Attach existing user.",
);
assert.equal(iamSettingsError("foo", [], [{ accessKey: "ak" }], new Set(), new Set(["ak"])), null);

// Each tab saves its own fields
const readOnly: BucketConfig = { ...createdConfig(false), publicAccess: "public-read" };
const mixed: BucketConfig = {
  ...readOnly,
  publicAccess: "private",
  tags: [{ key: "team", value: "a" }],
  quotaEnabled: true,
  quotaSize: "5",
};
assert.deepEqual(configChanges(readOnly, sectionConfig(readOnly, mixed, "settings")), {
  changes: [
    { label: "Quota", rpc: "SetBucketQuota", params: { type: "hard", bytes: 5 * GB } },
    { label: "Tags", rpc: "SetBucketTags", params: { tags: [{ key: "team", value: "a" }] } },
  ],
});
assert.deepEqual(configChanges(readOnly, sectionConfig(readOnly, mixed, "access")), {
  changes: [
    { label: "Public access", rpc: "SetBucketPolicy", params: { prefix: "", policy: "none" } },
  ],
});

// Canceled edits leave tab clean, invalid edits stay dirty
const tagged: BucketConfig = {
  ...readOnly,
  tags: [
    { key: "a", value: "1" },
    { key: "b", value: "2" },
  ],
};
const once: BucketConfig = { ...tagged, versioning: !tagged.versioning };
const twice: BucketConfig = { ...once, versioning: !once.versioning };
assert.equal(sectionDirty(tagged, { ...tagged }, "settings"), false);
assert.equal(sectionDirty(tagged, twice, "settings"), false);
assert.equal(
  sectionDirty(tagged, { ...tagged, tags: [...tagged.tags, { key: "", value: "" }] }, "settings"),
  false,
);
assert.equal(
  sectionDirty(tagged, { ...tagged, tags: [...tagged.tags].reverse() }, "settings"),
  false,
);
assert.equal(
  sectionDirty(tagged, { ...tagged, quotaEnabled: false, quotaSize: "9" }, "settings"),
  false,
);
assert.equal(sectionDirty(tagged, { ...tagged, publicAccess: "private" }, "settings"), false);
assert.equal(sectionDirty(tagged, once, "settings"), true);
assert.equal(sectionDirty(tagged, once, "access"), false);
assert.equal(sectionDirty(tagged, { ...tagged, publicAccess: "private" }, "access"), true);
assert.equal(
  sectionDirty(tagged, { ...tagged, quotaEnabled: true, quotaSize: "" }, "settings"),
  true,
);

// IAM order does not matter, existing user status saves separately
const iam: IAMDraft = {
  policies: [
    { name: "alpha-rw", policy: '{"Statement":[]}', origin: "exclusive" },
    { name: "team", policy: '{"Statement":[1]}', origin: "shared" },
  ],
  users: [{ accessKey: "ann", status: "enabled", policies: ["alpha-rw", "team"] }],
  removedPolicies: [],
  detachedUsers: [],
};
const [ann] = iam.users;
const [own, team] = iam.policies;
assert.equal(iamDirty(iam, structuredClone(iam)), false);
assert.equal(iamDirty(iam, { ...iam, users: [{ ...ann, status: "disabled" }] }), false);
assert.equal(iamDirty(iam, { ...iam, users: [{ ...ann, policies: ["team", "alpha-rw"] }] }), false);
assert.equal(iamDirty(iam, { ...iam, policies: [team, own] }), false);
assert.equal(iamDirty(iam, { ...iam, policies: [own, { ...team, policy: "{}" }] }), false);
assert.equal(iamDirty(iam, { ...iam, policies: [{ ...own, policy: "{}" }, team] }), true);
assert.equal(
  iamDirty(iam, {
    ...iam,
    policies: [...iam.policies, { name: "alpha-ro", policy: "{}", origin: "new" }],
  }),
  true,
);
assert.equal(
  iamDirty(iam, {
    ...iam,
    users: [...iam.users, { accessKey: "bob", status: "enabled", policies: ["alpha-rw"] }],
  }),
  true,
);
assert.equal(iamDirty(iam, { ...iam, removedPolicies: ["alpha-rw"] }), true);
assert.equal(iamDirty(iam, { ...iam, detachedUsers: ["ann"] }), true);
const pending: IAMDraft = {
  ...iam,
  users: [{ ...ann, accessKey: "cara", pendingSecretKey: "cara-secret" }],
};
assert.equal(
  iamDirty(pending, { ...pending, users: [{ ...pending.users[0], status: "disabled" }] }),
  true,
);

// Reject staged names that would overwrite each other
assert.equal(
  iamSettingsError("foo", [full, { ...full, policy: "{}" }], [], new Set(), new Set()),
  "Two policies are named foo-full.",
);
assert.equal(
  iamSettingsError("foo", [full, { ...full, name: "foo-full" }], [], new Set(), new Set()),
  "Two policies are named foo-full.",
);

// Write only new or edited policies
const kept = { name: "foo-ro", policy: "{}", origin: "exclusive" as const };
const sharedRow = { name: "all-ro", policy: "{}", origin: "shared" as const };
assert.deepEqual(policyWrites("foo", [kept, sharedRow], [kept, sharedRow]), []);
assert.deepEqual(
  policyWrites(
    "foo",
    [kept, sharedRow],
    [{ ...kept, policy: '{"a":1}' }, { ...sharedRow, policy: "x" }, full],
  ),
  [
    { name: "foo-ro", policy: '{"a":1}' },
    { name: "foo-full", policy: '{"Resource":"arn:aws:s3:::foo/*"}' },
  ],
);

console.log("bucket-config tests passed");
