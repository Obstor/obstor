"use client";

import { useCallback, useEffect, useReducer, useState } from "react";
import {
  addUserAction,
  type BucketSettings,
  createBucketWithSettingsAction,
  getBucketSettingsAction,
  type IAMUser,
  type NamedPolicy,
  type PolicyRow,
  setUserStatusAction,
  updateBucketSettingsAction,
} from "@/lib/actions";
import { Dialog } from "./Dialog";

interface Props {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  editBucket?: string | null;
}

const TABS = [
  { id: "general", label: "General", icon: "icon-[lucide--settings-2]" },
  { id: "access", label: "Access", icon: "icon-[lucide--shield]" },
  { id: "features", label: "Features", icon: "icon-[lucide--toggle-right]" },
  { id: "quota", label: "Quota", icon: "icon-[lucide--gauge]" },
  { id: "encryption", label: "Encryption", icon: "icon-[lucide--lock]" },
  { id: "tags", label: "Tags", icon: "icon-[lucide--tag]" },
  { id: "region", label: "Region", icon: "icon-[lucide--map-pin]" },
] as const;

type TabId = (typeof TABS)[number]["id"];

const DEFAULT_POLICY_TEMPLATE = (bucketName: string): PolicyRow => ({
  origin: "new",
  name: `${bucketName || "BUCKET_NAME"}-full`,
  policy: JSON.stringify(
    {
      Version: "2012-10-17",
      Statement: [
        {
          Effect: "Allow",
          Action: [
            "admin:DataUsageInfo",
            "admin:GetBucketQuota",
            "admin:GetBucketTarget",
            "admin:Heal",
            "admin:SetBucketTarget",
            "admin:TopLocksInfo",
          ],
          Resource: [
            `arn:aws:s3:::${bucketName || "BUCKET_NAME"}`,
            `arn:aws:s3:::${bucketName || "BUCKET_NAME"}/*`,
          ],
        },
        {
          Effect: "Allow",
          Action: ["s3:*"],
          Resource: [
            `arn:aws:s3:::${bucketName || "BUCKET_NAME"}`,
            `arn:aws:s3:::${bucketName || "BUCKET_NAME"}/*`,
          ],
        },
      ],
    },
    null,
    2,
  ),
});

const READONLY_POLICY_TEMPLATE = (bucketName: string): PolicyRow => ({
  origin: "new",
  name: `${bucketName || "BUCKET_NAME"}-readonly`,
  policy: JSON.stringify(
    {
      Version: "2012-10-17",
      Statement: [
        {
          Effect: "Allow",
          Action: ["s3:GetObject", "s3:ListBucket"],
          Resource: [
            `arn:aws:s3:::${bucketName || "BUCKET_NAME"}`,
            `arn:aws:s3:::${bucketName || "BUCKET_NAME"}/*`,
          ],
        },
      ],
    },
    null,
    2,
  ),
});

const EMPTY_SETTINGS: BucketSettings = {
  name: "",
  publicAccess: "private",
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
  policies: [],
  users: [],
  removedPolicies: [],
  detachedUsers: [],
  catchAllPolicies: [],
  attachablePolicies: [],
  attachableUsers: [],
  sftpEnabled: false,
  s3Enabled: true,
  placementStrategy: "smart",
  regions: [],
};

// Sub-components
function Toggle({
  checked,
  onChange,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors duration-200 ${
        disabled ? "cursor-not-allowed opacity-40" : ""
      } ${checked ? "bg-amber-500" : "bg-surface-overlay"}`}
    >
      <span
        className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-stone-100 shadow-sm transition-transform duration-200 ${
          checked ? "translate-x-6" : "translate-x-1"
        }`}
      />
    </button>
  );
}

function SettingRow({
  label,
  description,
  children,
}: {
  label: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border border-amber-200/5 bg-surface/50 px-4 py-3">
      <div className="min-w-0">
        <p className="font-body text-sm">{label}</p>
        {description && (
          <p className="mt-0.5 font-body text-[11px] text-stone-600 leading-relaxed">
            {description}
          </p>
        )}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-2 font-mono text-[10px] text-stone-600 uppercase tracking-wider">{children}</p>
  );
}

// CSS height-collapse (grid 0fr<->1fr), replaces framer accordion. inert when closed.
function Collapse({
  open,
  className = "",
  children,
}: {
  open: boolean;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className="grid transition-[grid-template-rows] duration-200 ease-out"
      style={{ gridTemplateRows: open ? "1fr" : "0fr" }}
      inert={!open}
    >
      <div
        className={`overflow-hidden transition-opacity duration-200 ${open ? "opacity-100" : "opacity-0"} ${className}`}
      >
        {children}
      </div>
    </div>
  );
}

function GeneralTab({
  settings,
  onChange,
  isEdit,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
  isEdit: boolean;
}) {
  return (
    <div className="space-y-4">
      <div>
        <SectionLabel>Bucket Name</SectionLabel>
        <input
          type="text"
          value={settings.name}
          disabled={isEdit}
          onChange={(e) =>
            onChange({ name: e.target.value.toLowerCase().replace(/[^a-z0-9.-]/g, "") })
          }
          placeholder="my-bucket"
          className="w-full rounded-lg border border-amber-200/5 bg-surface px-4 py-2.5 font-mono text-sm outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500 disabled:cursor-not-allowed disabled:opacity-50"
        />
        {!isEdit && settings.name && (
          <p className="mt-1.5 font-mono text-[10px] text-stone-600">
            arn:aws:s3:::{settings.name}
          </p>
        )}
      </div>

      <div className="space-y-2">
        <SectionLabel>Storage Configuration</SectionLabel>
        <SettingRow
          label="Versioning"
          description="Keep multiple versions of an object in the same bucket"
        >
          <Toggle checked={settings.versioning} onChange={(v) => onChange({ versioning: v })} />
        </SettingRow>
        <SettingRow
          label="Object Locking"
          description="Prevent objects from being deleted. Can only be enabled at bucket creation."
        >
          <Toggle
            checked={settings.objectLocking}
            onChange={(v) => onChange({ objectLocking: v })}
            disabled={isEdit}
          />
        </SettingRow>
      </div>
    </div>
  );
}

function PublicAccessSection({
  settings,
  onChange,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
}) {
  const options = [
    { id: "private", label: "Private", desc: "No public access" },
    { id: "public-read", label: "Public Read", desc: "Anyone can read objects" },
    { id: "public-read-write", label: "Public Read/Write", desc: "Anyone can read and write" },
  ] as const;

  return (
    <div>
      <SectionLabel>Public Access</SectionLabel>
      <div className="grid grid-cols-3 gap-2">
        {options.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => onChange({ publicAccess: p.id })}
            className={`rounded-lg border px-3 py-2.5 text-left transition-all ${
              settings.publicAccess === p.id
                ? "border-amber-500/40 bg-amber-500/10"
                : "border-amber-200/10 bg-surface/50 hover:border-amber-200/20"
            }`}
          >
            <p
              className={`font-body font-medium text-xs ${settings.publicAccess === p.id ? "text-amber-400" : "text-stone-100"}`}
            >
              {p.label}
            </p>
            <p className="mt-0.5 font-body text-[10px] text-stone-600">{p.desc}</p>
          </button>
        ))}
      </div>
    </div>
  );
}

function PoliciesAccordion({
  settings,
  onChange,
  expanded,
  onToggle,
  isEdit,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
  expanded: boolean;
  onToggle: () => void;
  isEdit: boolean;
}) {
  const addPolicy = () => {
    const base =
      settings.policies.length === 0
        ? DEFAULT_POLICY_TEMPLATE(settings.name)
        : READONLY_POLICY_TEMPLATE(settings.name);
    // Check if name is available
    const existing = new Set(settings.policies.map((p) => p.name));
    let name = base.name;
    let i = 2;
    while (existing.has(name)) {
      name = `${base.name}-${i++}`;
    }
    onChange({ policies: [...settings.policies, { ...base, name }] });
  };

  const updatePolicy = (idx: number, patch: Partial<NamedPolicy>) => {
    onChange({
      policies: settings.policies.map((p, i) => (i === idx ? { ...p, ...patch } : p)),
    });
  };

  const removePolicy = (idx: number) => {
    const pol = settings.policies[idx];
    onChange({
      policies: settings.policies.filter((_, i) => i !== idx),
      // No record for empty
      removedPolicies:
        pol.origin === "new" ? settings.removedPolicies : [...settings.removedPolicies, pol.name],
      // Cascade: detach from any user's selection in the modal
      users: settings.users.map((u) => ({
        ...u,
        policies: u.policies.filter((pn) => pn !== pol.name),
      })),
    });
  };

  const [attachPolicy, setAttachPolicy] = useState(false);

  // Client-side ID React keys for policies array length
  const [policyUIDs, setPolicyUIDs] = useState<string[]>([]);
  useEffect(() => {
    setPolicyUIDs((prev) => {
      if (prev.length === settings.policies.length) return prev;
      const next = prev.slice(0, settings.policies.length);
      while (next.length < settings.policies.length) {
        next.push(
          typeof crypto !== "undefined" && crypto.randomUUID
            ? crypto.randomUUID()
            : `${Date.now()}-${Math.random()}`,
        );
      }
      return next;
    });
  }, [settings.policies.length]);

  return (
    <div className="overflow-hidden rounded-lg border border-amber-200/5 bg-surface/30">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition-colors hover:bg-surface/50"
      >
        <div className="flex items-center gap-3">
          <span className="icon-[lucide--file-lock] block text-amber-500 text-sm" />
          <div>
            <p className="font-body font-medium text-sm">Access Policies</p>
            <p className="font-mono text-[10px] text-stone-600">
              {settings.policies.length} polic{settings.policies.length === 1 ? "y" : "ies"} scoped
              to this bucket
            </p>
          </div>
        </div>
        <span
          className={`icon-[lucide--chevron-down] text-sm text-stone-600 transition-transform ${
            expanded ? "rotate-180" : ""
          }`}
        />
      </button>

      <Collapse open={expanded}>
        <div className="space-y-3 border-amber-200/10 border-t px-4 py-4">
          <PublicAccessSection settings={settings} onChange={onChange} />

          {(settings.policies.length > 0 || isEdit) && (
            <div className="border-amber-200/10 border-t pt-3">
              <SectionLabel>Named IAM Policies</SectionLabel>
            </div>
          )}

          {settings.policies.length === 0 && (
            <p className="py-2 text-center font-body text-stone-600 text-xs">
              No named policies defined yet.
            </p>
          )}
          {settings.policies.map((p, idx) => {
            const key = policyUIDs[idx] ?? `fallback-${idx}`;
            return (
              <PolicyEditor
                key={key}
                policy={p}
                onChange={(patch) => updatePolicy(idx, patch)}
                onRemove={() => removePolicy(idx)}
              />
            );
          })}
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={addPolicy}
              className="flex flex-1 items-center justify-center gap-2 rounded-lg border border-amber-200/5 border-dashed py-2.5 font-body text-stone-600 text-xs transition-colors hover:border-amber-500/60 hover:text-amber-500"
            >
              <span className="icon-[lucide--plus] text-[11px]" />
              Create policy
            </button>
            {settings.attachablePolicies.length > 0 && (
              <button
                type="button"
                onClick={() => setAttachPolicy(true)}
                className="shrink-0 font-mono text-[11px] text-stone-600 transition-colors hover:text-stone-400"
              >
                Attach existing policy
              </button>
            )}
          </div>
          {settings.catchAllPolicies.length > 0 && (
            <p className="font-mono text-[10px] text-stone-600">
              Also applies here: {settings.catchAllPolicies.join(", ")}. Manage on the Access page.
            </p>
          )}
          {attachPolicy && (
            <AttachList
              title="Attach existing policy"
              items={settings.attachablePolicies.map((p) => ({
                id: p.name,
                label: p.name,
                meta: "Grants other buckets",
              }))}
              onCancel={() => setAttachPolicy(false)}
              onConfirm={(ids) => {
                const picked = settings.attachablePolicies.filter((p) => ids.includes(p.name));
                onChange({
                  policies: [...settings.policies, ...picked],
                  attachablePolicies: settings.attachablePolicies.filter(
                    (p) => !ids.includes(p.name),
                  ),
                  removedPolicies: settings.removedPolicies.filter((n) => !ids.includes(n)),
                });
                setAttachPolicy(false);
              }}
            />
          )}
        </div>
      </Collapse>
    </div>
  );
}

function PolicyEditor({
  policy,
  onChange,
  onRemove,
}: {
  policy: PolicyRow;
  onChange: (patch: Partial<NamedPolicy>) => void;
  onRemove: () => void;
}) {
  const shared = policy.origin === "shared";
  const format = () => {
    try {
      const formatted = JSON.stringify(JSON.parse(policy.policy), null, 2);
      onChange({ policy: formatted });
    } catch {
      // Todo: Add error handling
    }
  };

  return (
    <div className="rounded-lg border border-amber-200/5 bg-surface/60 p-3">
      <div className="mb-2 flex items-center gap-2">
        <input
          type="text"
          value={policy.name}
          disabled={policy.origin !== "new"}
          title={
            policy.origin === "exclusive" ? "Rename this policy on the Access page." : undefined
          }
          onChange={(e) => onChange({ name: e.target.value.replace(/[^a-zA-Z0-9_-]/g, "") })}
          placeholder="policy-name"
          className="flex-1 rounded-md border border-amber-200/5 bg-surface px-3 py-1.5 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500 disabled:opacity-60"
        />
        {shared && (
          <span className="rounded border border-amber-200/15 px-1.5 py-0.5 font-mono text-[9px] text-stone-500 uppercase">
            Shared
          </span>
        )}
        {!shared && (
          <button
            type="button"
            onClick={format}
            className="rounded-md bg-surface-overlay px-2.5 py-1 font-mono text-[10px] text-stone-600 transition-colors hover:text-stone-400"
          >
            Format
          </button>
        )}
        {!shared && (
          <button
            type="button"
            onClick={onRemove}
            title="Remove policy"
            className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-red-500/10 hover:text-red-400"
          >
            <span className="icon-[lucide--trash-2] block text-[11px]" />
          </button>
        )}
      </div>
      {shared ? (
        <p className="font-body text-stone-600 text-xs">
          Grants other buckets too. Read only here, edit it on the Access page.
        </p>
      ) : (
        <textarea
          value={policy.policy}
          onChange={(e) => onChange({ policy: e.target.value })}
          rows={10}
          spellCheck={false}
          className="w-full resize-none rounded-md border border-amber-200/5 bg-void p-3 font-mono text-[11px] text-stone-400 leading-relaxed outline-none transition-colors focus:border-amber-500"
        />
      )}
    </div>
  );
}

function UsersAccordion({
  settings,
  onChange,
  expanded,
  onToggle,
  isEdit,
  onUserError,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
  expanded: boolean;
  onToggle: () => void;
  isEdit: boolean;
  onUserError: (err: string) => void;
}) {
  const [showAdd, setShowAdd] = useState(false);
  const [attachUser, setAttachUser] = useState(false);
  const [pendingCreds, setPendingCreds] = useState<{ accessKey: string; secretKey: string } | null>(
    null,
  );
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const availablePolicies = settings.policies.map((p) => p.name).filter(Boolean);

  const removeUser = (user: IAMUser) => {
    // Not written yet, skip detaching on save
    if (user.pendingSecretKey || !isEdit) {
      onChange({ users: settings.users.filter((u) => u.accessKey !== user.accessKey) });
      return;
    }
    // On save the account keeps policies that modal didnt change
    onChange({
      users: settings.users.filter((u) => u.accessKey !== user.accessKey),
      detachedUsers: [...settings.detachedUsers, user.accessKey],
    });
  };

  const toggleStatus = async (user: IAMUser) => {
    const next = user.status !== "enabled";
    // Edit pending users
    if (user.pendingSecretKey) {
      onChange({
        users: settings.users.map((u) =>
          u.accessKey === user.accessKey ? { ...u, status: next ? "enabled" : "disabled" } : u,
        ),
      });
      return;
    }
    setBusyKey(user.accessKey);
    const res = await setUserStatusAction(user.accessKey, next);
    setBusyKey(null);
    if ("error" in res && res.error) {
      onUserError(res.error);
      return;
    }
    onChange({
      users: settings.users.map((u) =>
        u.accessKey === user.accessKey ? { ...u, status: next ? "enabled" : "disabled" } : u,
      ),
    });
  };

  const updateUserPolicies = (accessKey: string, policies: string[]) => {
    onChange({
      users: settings.users.map((u) => (u.accessKey === accessKey ? { ...u, policies } : u)),
    });
  };

  return (
    <div className="overflow-hidden rounded-lg border border-amber-200/5 bg-surface/30">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition-colors hover:bg-surface/50"
      >
        <div className="flex items-center gap-3">
          <span className="icon-[lucide--users] block text-amber-500 text-sm" />
          <div>
            <p className="font-body font-medium text-sm">Users</p>
            <p className="font-mono text-[10px] text-stone-600">
              {settings.users.length} user{settings.users.length === 1 ? "" : "s"} with access to
              this bucket
            </p>
          </div>
        </div>
        <span
          className={`icon-[lucide--chevron-down] text-sm text-stone-600 transition-transform ${
            expanded ? "rotate-180" : ""
          }`}
        />
      </button>

      <Collapse open={expanded}>
        <div className="space-y-3 border-amber-200/10 border-t px-4 py-4">
          {pendingCreds && (
            <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3">
              <div className="mb-2 flex items-center gap-2">
                <span className="icon-[lucide--key-round] text-amber-500 text-sm" />
                <p className="font-body font-medium text-amber-400 text-xs">
                  Save these credentials now - the secret won't be shown again.
                </p>
              </div>
              <div className="grid gap-1.5 font-mono text-[11px]">
                <div className="flex items-center justify-between gap-2 rounded bg-void px-2 py-1.5">
                  <span className="text-stone-600">Access Key</span>
                  <span className="truncate">{pendingCreds.accessKey}</span>
                </div>
                <div className="flex items-center justify-between gap-2 rounded bg-void px-2 py-1.5">
                  <span className="text-stone-600">Secret Key</span>
                  <span className="truncate">{pendingCreds.secretKey}</span>
                </div>
              </div>
              <button
                type="button"
                onClick={() => setPendingCreds(null)}
                className="mt-2 font-body text-[11px] text-amber-500 transition-colors hover:text-amber-400"
              >
                Saved credentials, dismiss
              </button>
            </div>
          )}

          {settings.users.length === 0 && !showAdd && (
            <p className="py-2 text-center font-body text-stone-600 text-xs">
              No users attached to this bucket yet.
            </p>
          )}

          {settings.users.map((u) => (
            <div
              key={u.accessKey}
              className="rounded-lg border border-amber-200/5 bg-surface/60 p-3"
            >
              <div className="flex items-center gap-3">
                <span className="icon-[lucide--circle-user] text-base text-stone-600" />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono text-xs">{u.accessKey}</p>
                  <p className="font-mono text-[10px] text-stone-600">
                    {u.status === "enabled" ? "Active" : "Disabled"}
                  </p>
                </div>
                <Toggle
                  checked={u.status === "enabled"}
                  onChange={() => toggleStatus(u)}
                  disabled={busyKey === u.accessKey}
                />
                <button
                  type="button"
                  onClick={() => removeUser(u)}
                  disabled={busyKey === u.accessKey}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-red-500/10 hover:text-red-400 disabled:opacity-40"
                >
                  <span className="icon-[lucide--trash-2] block text-[11px]" />
                </button>
              </div>

              {availablePolicies.length > 0 && (
                <div className="mt-3 border-amber-200/10 border-t pt-3">
                  <p className="mb-1.5 font-mono text-[10px] text-stone-600 uppercase tracking-wider">
                    Member of
                  </p>
                  <div className="flex flex-wrap gap-1.5">
                    {availablePolicies.map((pn) => {
                      const checked = u.policies.includes(pn);
                      return (
                        <button
                          key={pn}
                          type="button"
                          onClick={() =>
                            updateUserPolicies(
                              u.accessKey,
                              checked ? u.policies.filter((x) => x !== pn) : [...u.policies, pn],
                            )
                          }
                          className={`rounded-md border px-2.5 py-1 font-mono text-[10px] transition-colors ${
                            checked
                              ? "border-amber-500/40 bg-amber-500/10 text-amber-400"
                              : "border-amber-200/10 bg-surface text-stone-600 hover:border-amber-200/20 hover:text-stone-400"
                          }`}
                        >
                          {pn}
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          ))}

          {showAdd ? (
            <AddUserForm
              isEdit={isEdit}
              availablePolicies={availablePolicies}
              onCancel={() => setShowAdd(false)}
              onCreated={(user, creds) => {
                onChange({ users: [...settings.users, user] });
                setPendingCreds(creds);
                setShowAdd(false);
              }}
              onError={onUserError}
            />
          ) : (
            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => setShowAdd(true)}
                className="flex flex-1 items-center justify-center gap-2 rounded-lg border border-amber-200/5 border-dashed py-2.5 font-body text-stone-600 text-xs transition-colors hover:border-amber-500/60 hover:text-amber-500"
              >
                <span className="icon-[lucide--user-plus] text-[11px]" />
                Create user
              </button>
              {settings.attachableUsers.length > 0 && (
                <button
                  type="button"
                  onClick={() => setAttachUser(true)}
                  className="shrink-0 font-mono text-[11px] text-stone-600 transition-colors hover:text-stone-400"
                >
                  Attach existing user
                </button>
              )}
            </div>
          )}
          {attachUser && (
            <AttachList
              title="Attach existing user"
              items={settings.attachableUsers.map((u) => ({
                id: u.accessKey,
                label: u.accessKey,
                meta:
                  u.policies.length === 0
                    ? "No direct policy"
                    : `${u.policies.length} ${u.policies.length === 1 ? "policy" : "policies"}`,
              }))}
              onCancel={() => setAttachUser(false)}
              onConfirm={(ids) => {
                const picked = settings.attachableUsers.filter((u) => ids.includes(u.accessKey));
                onChange({
                  users: [...settings.users, ...picked],
                  attachableUsers: settings.attachableUsers.filter(
                    (u) => !ids.includes(u.accessKey),
                  ),
                  detachedUsers: settings.detachedUsers.filter((k) => !ids.includes(k)),
                });
                setAttachUser(false);
              }}
            />
          )}
        </div>
      </Collapse>
    </div>
  );
}

function genAccessKey(): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
  const bytes = crypto.getRandomValues(new Uint8Array(20));
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join("");
}

function genSecretKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(30));
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=/g, "").slice(0, 40);
}

function AddUserForm({
  isEdit,
  availablePolicies,
  onCancel,
  onCreated,
  onError,
}: {
  isEdit: boolean;
  availablePolicies: string[];
  onCancel: () => void;
  onCreated: (user: IAMUser, creds: { accessKey: string; secretKey: string }) => void;
  onError: (err: string) => void;
}) {
  const [autoGen, setAutoGen] = useState(true);
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [policies, setPolicies] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (policies.length === 0) {
      onError("Select at least one policy for the user.");
      return;
    }

    // Check user create input syntax
    if (!isEdit) {
      const ak = autoGen ? genAccessKey() : accessKey.trim();
      const sk = autoGen ? genSecretKey() : secretKey.trim();
      if (!ak || !sk) {
        onError("Access key and secret key are required.");
        return;
      }
      onCreated(
        { accessKey: ak, status: "enabled", policies, pendingSecretKey: sk },
        { accessKey: ak, secretKey: sk },
      );
      return;
    }

    // Edit user on backened
    setBusy(true);
    const res = await addUserAction(
      autoGen ? "" : accessKey,
      autoGen ? "" : secretKey,
      policies.join(","),
    );
    setBusy(false);
    if ("error" in res) {
      onError(res.error);
      return;
    }
    onCreated(
      { accessKey: res.accessKey, status: "enabled", policies },
      { accessKey: res.accessKey, secretKey: res.secretKey },
    );
  };

  return (
    <div className="space-y-3 rounded-lg border border-amber-500/40 bg-surface/60 p-3">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => setAutoGen(true)}
          className={`flex-1 rounded-md border px-3 py-1.5 font-body text-xs transition-colors ${
            autoGen
              ? "border-amber-500/40 bg-amber-500/10 text-amber-400"
              : "border-amber-200/10 bg-surface text-stone-600 hover:border-amber-200/20"
          }`}
        >
          Generate
        </button>
        <button
          type="button"
          onClick={() => setAutoGen(false)}
          className={`flex-1 rounded-md border px-3 py-1.5 font-body text-xs transition-colors ${
            !autoGen
              ? "border-amber-500/40 bg-amber-500/10 text-amber-400"
              : "border-amber-200/10 bg-surface text-stone-600 hover:border-amber-200/20"
          }`}
        >
          Custom
        </button>
      </div>

      {!autoGen && (
        <div className="space-y-2">
          <input
            type="text"
            value={accessKey}
            onChange={(e) => setAccessKey(e.target.value)}
            placeholder="Access key (≥ 3 chars)"
            className="w-full rounded-md border border-amber-200/5 bg-surface px-3 py-2 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
          />
          <input
            type="text"
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder="Secret key (≥ 8 chars)"
            className="w-full rounded-md border border-amber-200/5 bg-surface px-3 py-2 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
          />
        </div>
      )}

      <div>
        <p className="mb-1.5 font-mono text-[10px] text-stone-600 uppercase tracking-wider">
          Policies (at least one)
        </p>
        {availablePolicies.length === 0 ? (
          <p className="font-body text-[11px] text-stone-600">
            No policies defined yet. Add a policy in the Access Policies section first.
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {availablePolicies.map((pn) => {
              const checked = policies.includes(pn);
              return (
                <button
                  key={pn}
                  type="button"
                  onClick={() =>
                    setPolicies(checked ? policies.filter((x) => x !== pn) : [...policies, pn])
                  }
                  className={`rounded-md border px-2.5 py-1 font-mono text-[10px] transition-colors ${
                    checked
                      ? "border-amber-500/40 bg-amber-500/10 text-amber-400"
                      : "border-amber-200/10 bg-surface text-stone-600 hover:border-amber-200/20 hover:text-stone-400"
                  }`}
                >
                  {pn}
                </button>
              );
            })}
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 pt-1">
        <button
          type="button"
          onClick={submit}
          disabled={busy}
          className="flex-1 rounded-md bg-amber-500 px-3 py-2 font-body font-medium text-black text-xs transition-colors hover:bg-amber-400 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? "Creating..." : "Create User"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={busy}
          className="rounded-md border border-amber-200/5 px-3 py-2 font-body text-stone-600 text-xs transition-colors hover:bg-surface-overlay disabled:opacity-50"
        >
          Cancel
        </button>
      </div>
      {!isEdit && (
        <p className="font-body text-[11px] text-stone-600">
          User will be created when you save the bucket.
        </p>
      )}
    </div>
  );
}

function AccessTab({
  settings,
  onChange,
  isEdit,
  onUserError,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
  isEdit: boolean;
  onUserError: (err: string) => void;
}) {
  const [openSection, setOpenSection] = useState<"policies" | "users" | null>("policies");

  return (
    <div className="space-y-2">
      <PoliciesAccordion
        settings={settings}
        onChange={onChange}
        expanded={openSection === "policies"}
        onToggle={() => setOpenSection(openSection === "policies" ? null : "policies")}
        isEdit={isEdit}
      />
      <UsersAccordion
        settings={settings}
        onChange={onChange}
        expanded={openSection === "users"}
        onToggle={() => setOpenSection(openSection === "users" ? null : "users")}
        isEdit={isEdit}
        onUserError={onUserError}
      />
    </div>
  );
}

function FeaturesTab({
  settings,
  onChange,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
}) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <SectionLabel>Protocol Access</SectionLabel>
        <SettingRow label="S3 API" description="Access bucket via the S3-compatible API">
          <Toggle checked={settings.s3Enabled} onChange={(v) => onChange({ s3Enabled: v })} />
        </SettingRow>
        <SettingRow label="SFTP Access" description="Allow file access over SFTP protocol">
          <Toggle checked={settings.sftpEnabled} onChange={(v) => onChange({ sftpEnabled: v })} />
        </SettingRow>
      </div>
    </div>
  );
}

function QuotaTab({
  settings,
  onChange,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
}) {
  return (
    <div className="space-y-4">
      <SettingRow
        label="Enable Quota"
        description="Limit the amount of data that can be stored in this bucket"
      >
        <Toggle checked={settings.quotaEnabled} onChange={(v) => onChange({ quotaEnabled: v })} />
      </SettingRow>

      <Collapse open={settings.quotaEnabled} className="space-y-3">
        <div>
          <SectionLabel>Quota Type</SectionLabel>
          <div className="grid grid-cols-2 gap-2">
            {(["hard", "fifo"] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => onChange({ quotaType: t })}
                className={`rounded-lg border px-3 py-2.5 text-left transition-all ${
                  settings.quotaType === t
                    ? "border-amber-500/40 bg-amber-500/10"
                    : "border-amber-200/10 bg-surface/50 hover:border-amber-200/20"
                }`}
              >
                <p
                  className={`font-body font-medium text-xs ${settings.quotaType === t ? "text-amber-400" : "text-stone-100"}`}
                >
                  {t === "hard" ? "Hard Limit" : "FIFO"}
                </p>
                <p className="mt-0.5 font-body text-[10px] text-stone-600">
                  {t === "hard"
                    ? "Reject writes when limit is reached"
                    : "Delete oldest objects when limit is reached"}
                </p>
              </button>
            ))}
          </div>
        </div>

        <div>
          <SectionLabel>Quota Size</SectionLabel>
          <div className="flex gap-2">
            <input
              type="number"
              min="1"
              value={settings.quotaSize}
              onChange={(e) => onChange({ quotaSize: e.target.value })}
              placeholder="100"
              className="flex-1 rounded-lg border border-amber-200/5 bg-surface px-4 py-2.5 font-mono text-sm outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
            />
            <select
              value={settings.quotaUnit}
              onChange={(e) => onChange({ quotaUnit: e.target.value as "GB" | "TB" | "PB" })}
              className="rounded-lg border border-amber-200/5 bg-surface px-3 py-2.5 font-mono text-sm outline-none transition-colors focus:border-amber-500"
            >
              <option value="GB">GB</option>
              <option value="TB">TB</option>
              <option value="PB">PB</option>
            </select>
          </div>
        </div>
      </Collapse>
    </div>
  );
}

function EncryptionTab({
  settings,
  onChange,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
}) {
  return (
    <div className="space-y-4">
      <SettingRow
        label="Server-Side Encryption"
        description="Automatically encrypt objects when stored"
      >
        <Toggle
          checked={settings.encryptionEnabled}
          onChange={(v) => onChange({ encryptionEnabled: v })}
        />
      </SettingRow>

      <Collapse open={settings.encryptionEnabled} className="space-y-3">
        <div>
          <SectionLabel>Encryption Type</SectionLabel>
          <div className="grid grid-cols-2 gap-2">
            {(["SSE-S3", "SSE-KMS"] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => onChange({ encryptionType: t })}
                className={`rounded-lg border px-3 py-2.5 text-left transition-all ${
                  settings.encryptionType === t
                    ? "border-amber-500/40 bg-amber-500/10"
                    : "border-amber-200/10 bg-surface/50 hover:border-amber-200/20"
                }`}
              >
                <p
                  className={`font-body font-medium text-xs ${settings.encryptionType === t ? "text-amber-400" : "text-stone-100"}`}
                >
                  {t}
                </p>
                <p className="mt-0.5 font-body text-[10px] text-stone-600">
                  {t === "SSE-S3" ? "Server-managed encryption keys" : "External KMS-managed keys"}
                </p>
              </button>
            ))}
          </div>
        </div>

        <Collapse open={settings.encryptionType === "SSE-KMS"}>
          <SectionLabel>KMS Key ID</SectionLabel>
          <input
            type="text"
            value={settings.kmsKeyId}
            onChange={(e) => onChange({ kmsKeyId: e.target.value })}
            placeholder="arn:aws:kms:region:account:key/key-id"
            className="w-full rounded-lg border border-amber-200/5 bg-surface px-4 py-2.5 font-mono text-sm outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
          />
        </Collapse>
      </Collapse>
    </div>
  );
}

function TagsTab({
  settings,
  onChange,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
}) {
  const addTag = () => onChange({ tags: [...settings.tags, { key: "", value: "" }] });
  const removeTag = (i: number) => onChange({ tags: settings.tags.filter((_, idx) => idx !== i) });
  const updateTag = (i: number, field: "key" | "value", val: string) =>
    onChange({
      tags: settings.tags.map((t, idx) => (idx === i ? { ...t, [field]: val } : t)),
    });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SectionLabel>Bucket Tags</SectionLabel>
        <button
          type="button"
          onClick={addTag}
          className="flex items-center gap-1.5 rounded-md bg-surface-overlay px-2.5 py-1 font-body text-[11px] text-stone-400 transition-colors hover:text-stone-100"
        >
          <span className="icon-[lucide--plus] text-[10px]" />
          Add Tag
        </button>
      </div>

      {settings.tags.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-amber-200/5 border-dashed py-8">
          <span className="icon-[lucide--tag] mb-2 text-lg text-stone-600" />
          <p className="font-body text-stone-600 text-xs">No tags configured</p>
          <button
            type="button"
            onClick={addTag}
            className="mt-2 font-body text-amber-500 text-xs transition-colors hover:text-amber-400"
          >
            Add your first tag
          </button>
        </div>
      ) : (
        <div className="space-y-2">
          {settings.tags.map((tag, i) => (
            <div key={`tag-${tag.key || i}`} className="flex items-center gap-2">
              <input
                type="text"
                value={tag.key}
                onChange={(e) => updateTag(i, "key", e.target.value)}
                placeholder="Key"
                className="flex-1 rounded-lg border border-amber-200/5 bg-surface px-3 py-2 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
              />
              <input
                type="text"
                value={tag.value}
                onChange={(e) => updateTag(i, "value", e.target.value)}
                placeholder="Value"
                className="flex-1 rounded-lg border border-amber-200/5 bg-surface px-3 py-2 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
              />
              <button
                type="button"
                onClick={() => removeTag(i)}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-red-500/10 hover:text-red-400"
              >
                <span className="icon-[lucide--x] block text-xs" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function RegionTab({
  settings,
  onChange,
}: {
  settings: BucketSettings;
  onChange: (s: Partial<BucketSettings>) => void;
}) {
  const [regionInput, setRegionInput] = useState("");

  const strategies = [
    {
      id: "smart",
      label: "Smart Placement",
      desc: "Automatically distributes data across available regions for optimal durability and performance",
    },
    {
      id: "custom",
      label: "Custom Regions",
      desc: "Manually select which regions your data should be stored in",
    },
  ] as const;

  const addRegion = (value: string) => {
    const trimmed = value.trim().toLowerCase();
    if (trimmed && !settings.regions.includes(trimmed)) {
      onChange({ regions: [...settings.regions, trimmed] });
    }
    setRegionInput("");
  };

  const removeRegion = (index: number) => {
    onChange({ regions: settings.regions.filter((_, i) => i !== index) });
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      addRegion(regionInput);
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <SectionLabel>Placement Strategy</SectionLabel>
        <div className="grid grid-cols-2 gap-2">
          {strategies.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => onChange({ placementStrategy: s.id })}
              className={`rounded-lg border px-3 py-2.5 text-left transition-all ${
                settings.placementStrategy === s.id
                  ? "border-amber-500/40 bg-amber-500/10"
                  : "border-amber-200/10 bg-surface/50 hover:border-amber-200/20"
              }`}
            >
              <p
                className={`font-body font-medium text-xs ${settings.placementStrategy === s.id ? "text-amber-400" : "text-stone-100"}`}
              >
                {s.label}
              </p>
              <p className="mt-0.5 font-body text-[10px] text-stone-600">{s.desc}</p>
            </button>
          ))}
        </div>
      </div>

      <Collapse open={settings.placementStrategy === "custom"} className="space-y-3">
        <div>
          <SectionLabel>Regions</SectionLabel>
          <div className="flex flex-wrap items-center gap-1.5 rounded-lg border border-amber-200/5 bg-surface px-3 py-2">
            {settings.regions.map((region, i) => (
              <span
                key={region}
                className="flex items-center gap-1 rounded bg-surface-overlay px-2 py-0.5 font-mono text-[11px] text-stone-400"
              >
                {region}
                <button
                  type="button"
                  onClick={() => removeRegion(i)}
                  className="ml-0.5 text-stone-600 transition-colors hover:text-stone-100"
                >
                  <span className="icon-[lucide--x] text-[9px]" />
                </button>
              </span>
            ))}
            <input
              type="text"
              value={regionInput}
              onChange={(e) => setRegionInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Type a region and press Enter..."
              className="min-w-[180px] flex-1 bg-transparent py-0.5 font-mono text-sm outline-none placeholder:text-stone-600"
            />
          </div>
        </div>

        <p className="font-body text-[11px] text-stone-600 leading-relaxed">
          Data will only be replicated to the selected regions. At least 2 regions are recommended
          for durability.
        </p>
      </Collapse>
    </div>
  );
}

// Reducer for main modal state
interface ModalState {
  tab: TabId;
  settings: BucketSettings;
  loading: boolean;
  saving: boolean;
  error: string;
}

type ModalAction =
  | { type: "SET_TAB"; tab: TabId }
  | { type: "UPDATE_SETTINGS"; partial: Partial<BucketSettings> }
  | { type: "SET_SETTINGS"; settings: BucketSettings }
  | { type: "SET_LOADING"; loading: boolean }
  | { type: "SET_SAVING"; saving: boolean }
  | { type: "SET_ERROR"; error: string }
  | { type: "RESET"; editBucket: string | null };

function modalReducer(state: ModalState, action: ModalAction): ModalState {
  switch (action.type) {
    case "SET_TAB":
      return { ...state, tab: action.tab };
    case "UPDATE_SETTINGS":
      return { ...state, settings: { ...state.settings, ...action.partial }, error: "" };
    case "SET_SETTINGS":
      return { ...state, settings: action.settings };
    case "SET_LOADING":
      return { ...state, loading: action.loading };
    case "SET_SAVING":
      return { ...state, saving: action.saving };
    case "SET_ERROR":
      return { ...state, error: action.error };
    case "RESET":
      return {
        tab: "general",
        settings: EMPTY_SETTINGS,
        loading: !!action.editBucket,
        saving: false,
        error: "",
      };
    default:
      return state;
  }
}

const INITIAL_STATE: ModalState = {
  tab: "general",
  settings: EMPTY_SETTINGS,
  loading: false,
  saving: false,
  error: "",
};

// Bucket Modal
export function BucketModal({ open, onClose, onSuccess, editBucket }: Props) {
  const isEdit = !!editBucket;
  const [state, dispatch] = useReducer(modalReducer, INITIAL_STATE);
  const { tab, settings, loading, saving, error } = state;

  const update = useCallback((partial: Partial<BucketSettings>) => {
    dispatch({ type: "UPDATE_SETTINGS", partial });
  }, []);

  // Load settings when editing
  useEffect(() => {
    if (!open) return;
    dispatch({ type: "RESET", editBucket: editBucket ?? null });

    if (editBucket) {
      getBucketSettingsAction(editBucket)
        .then((res) => {
          if ("error" in res) {
            dispatch({ type: "SET_ERROR", error: res.error });
          } else {
            dispatch({ type: "SET_SETTINGS", settings: { ...EMPTY_SETTINGS, ...res } });
          }
        })
        .finally(() => dispatch({ type: "SET_LOADING", loading: false }));
    } else {
      // Create a default policy and replace BUCKET_NAME placeholder with real name
      dispatch({
        type: "SET_SETTINGS",
        settings: {
          ...EMPTY_SETTINGS,
          policies: [DEFAULT_POLICY_TEMPLATE("")],
        },
      });
    }
  }, [open, editBucket]);

  const handleSubmit = async () => {
    if (!isEdit && !settings.name) {
      dispatch({ type: "SET_ERROR", error: "Bucket name is required" });
      dispatch({ type: "SET_TAB", tab: "general" });
      return;
    }

    dispatch({ type: "SET_SAVING", saving: true });
    dispatch({ type: "SET_ERROR", error: "" });

    try {
      const result = isEdit
        ? await updateBucketSettingsAction(settings)
        : await createBucketWithSettingsAction(settings);

      if (result && "error" in result) {
        dispatch({ type: "SET_ERROR", error: result.error });
      } else {
        onSuccess();
        onClose();
      }
    } catch (err) {
      dispatch({
        type: "SET_ERROR",
        error: err instanceof Error ? err.message : "An error occurred",
      });
    } finally {
      dispatch({ type: "SET_SAVING", saving: false });
    }
  };

  return (
    <Dialog open={open} onClose={onClose} className="h-[80vh] max-w-2xl">
      <div className="flex h-full w-full flex-col overflow-hidden rounded-xl border border-amber-200/5 bg-abyss shadow-2xl shadow-black/50">
        {/* Header */}
        <div className="flex items-center justify-between border-amber-200/10 border-b px-6 py-4">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-amber-500/10">
              <span
                className={`text-amber-500 text-sm ${isEdit ? "icon-[lucide--settings]" : "icon-[lucide--plus-circle]"}`}
              />
            </div>
            <div>
              <h2 className="font-display font-semibold text-base">
                {isEdit ? "Bucket Settings" : "Create Bucket"}
              </h2>
              {isEdit && <p className="font-mono text-[10px] text-stone-600">{editBucket}</p>}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-md text-stone-600 transition-colors hover:bg-surface-overlay hover:text-stone-400"
          >
            <span className="icon-[lucide--x] block text-sm" />
          </button>
        </div>

        {/* Tabs */}
        <div className="flex gap-1 overflow-x-auto border-amber-200/10 border-b bg-surface/30 px-6 py-1.5">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => dispatch({ type: "SET_TAB", tab: t.id })}
              className={`flex items-center gap-1.5 whitespace-nowrap rounded-md px-3 py-1.5 font-body text-xs transition-all ${
                tab === t.id
                  ? "bg-amber-500/10 text-amber-500"
                  : "text-stone-600 hover:bg-surface-overlay hover:text-stone-400"
              }`}
            >
              <span className={`${t.icon} text-[11px]`} />
              {t.label}
            </button>
          ))}
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-6 py-5">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <div className="h-5 w-5 animate-spin rounded-full border-2 border-amber-500 border-t-transparent" />
            </div>
          ) : (
            <>
              {tab === "general" && (
                <GeneralTab settings={settings} onChange={update} isEdit={isEdit} />
              )}
              {tab === "access" && (
                <AccessTab
                  settings={settings}
                  onChange={update}
                  isEdit={isEdit}
                  onUserError={(err) => dispatch({ type: "SET_ERROR", error: err })}
                />
              )}
              {tab === "features" && <FeaturesTab settings={settings} onChange={update} />}
              {tab === "quota" && <QuotaTab settings={settings} onChange={update} />}
              {tab === "encryption" && <EncryptionTab settings={settings} onChange={update} />}
              {tab === "tags" && <TagsTab settings={settings} onChange={update} />}
              {tab === "region" && <RegionTab settings={settings} onChange={update} />}
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between border-amber-200/10 border-t bg-surface/20 px-6 py-4">
          <div className="min-w-0 flex-1">
            {error && (
              <div className="flex items-center gap-2">
                <span className="icon-[lucide--alert-circle] shrink-0 text-red-400 text-xs" />
                <p className="truncate font-body text-red-400 text-xs">{error}</p>
              </div>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-amber-200/5 px-4 py-2 font-body text-sm text-stone-600 transition-colors hover:bg-surface-overlay hover:text-stone-400"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleSubmit}
              disabled={saving || loading}
              className="rounded-lg bg-amber-500 px-5 py-2 font-body font-medium text-black text-sm transition-all hover:bg-amber-400 disabled:opacity-50"
            >
              {saving ? (
                <span className="flex items-center gap-2">
                  <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-black/30 border-t-black" />
                  {isEdit ? "Saving..." : "Creating..."}
                </span>
              ) : isEdit ? (
                "Save Changes"
              ) : (
                "Create Bucket"
              )}
            </button>
          </div>
        </div>
      </div>
    </Dialog>
  );
}

function AttachList({
  title,
  items,
  onCancel,
  onConfirm,
}: {
  title: string;
  items: { id: string; label: string; meta: string }[];
  onCancel: () => void;
  onConfirm: (ids: string[]) => void;
}) {
  const [picked, setPicked] = useState<string[]>([]);
  const toggle = (id: string) =>
    setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));

  return (
    <Dialog open onClose={onCancel} className="max-w-md" label={title}>
      <div className="w-full rounded-xl border border-amber-200/5 bg-abyss p-5">
        <p className="mb-3 font-display font-semibold text-sm">{title}</p>
        <div className="mb-4 max-h-72 space-y-1 overflow-y-auto">
          {items.map((it) => (
            <label
              key={it.id}
              className="flex items-center gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-surface/50"
            >
              <input
                type="checkbox"
                checked={picked.includes(it.id)}
                onChange={() => toggle(it.id)}
              />
              <span className="flex-1 truncate font-mono text-[11px] text-stone-300">
                {it.label}
              </span>
              <span className="font-mono text-[10px] text-stone-600">{it.meta}</span>
            </label>
          ))}
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="flex-1 rounded-md border border-amber-200/15 px-4 py-2 font-body text-sm text-stone-400 transition-colors hover:bg-white/5"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={picked.length === 0}
            onClick={() => onConfirm(picked)}
            className="flex-1 rounded-md bg-amber-500 px-4 py-2 font-body font-medium text-black text-sm transition-colors hover:bg-amber-400 disabled:opacity-40"
          >
            Attach
          </button>
        </div>
      </div>
    </Dialog>
  );
}
