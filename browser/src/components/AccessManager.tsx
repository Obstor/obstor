"use client";

import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { buildAccessIndex, type IndexedPolicy, type IndexedUser } from "@/lib/access-index";
import {
  type AccessSnapshot,
  deletePolicyAction,
  deleteUserAction,
  renamePolicyAction,
  rotateSecretAction,
  savePolicyAction,
  setUserPoliciesAction,
  setUserStatusAction,
} from "@/lib/actions";
import { Dialog } from "./Dialog";

type UserFilter = "all" | "no-policy" | "not-scoped";
type PolicyFilter = "all" | "not-scoped" | "missing-bucket";

const NEW_POLICY_BODY = JSON.stringify(
  {
    Version: "2012-10-17",
    Statement: [
      {
        Effect: "Allow",
        Action: ["s3:*"],
        Resource: ["arn:aws:s3:::bucket-name", "arn:aws:s3:::bucket-name/*"],
      },
    ],
  },
  null,
  2,
);

export function AccessManager({ snapshot }: { snapshot: AccessSnapshot }) {
  const router = useRouter();
  const idx = useMemo(
    () => buildAccessIndex(snapshot.buckets, snapshot.policies, snapshot.users),
    [snapshot],
  );

  const [userFilter, setUserFilter] = useState<UserFilter>("all");
  const [policyFilter, setPolicyFilter] = useState<PolicyFilter>("all");
  const [query, setQuery] = useState("");
  const [openUser, setOpenUser] = useState<IndexedUser | null>(null);
  const [openPolicy, setOpenPolicy] = useState<IndexedPolicy | null>(null);
  const [newPolicy, setNewPolicy] = useState(false);
  const [banner, setBanner] = useState("");

  const noPolicy = idx.users.filter((u) => u.policies.length === 0);
  const notScopedUsers = idx.users.filter((u) => u.buckets.length === 0 && u.policies.length > 0);
  const notScopedPolicies = idx.policies.filter((p) => p.buckets.length === 0);
  const missingBucket = idx.policies.filter((p) => p.missingBuckets.length > 0);

  const matches = (s: string) => s.toLowerCase().includes(query.toLowerCase());

  const users = idx.users.filter((u) => {
    if (query && !matches(u.accessKey) && !u.policies.some(matches)) return false;
    if (userFilter === "no-policy") return u.policies.length === 0;
    if (userFilter === "not-scoped") return u.buckets.length === 0 && u.policies.length > 0;
    return true;
  });

  const policies = idx.policies.filter((p) => {
    if (query && !matches(p.name) && !p.buckets.some(matches)) return false;
    if (policyFilter === "not-scoped") return p.buckets.length === 0;
    if (policyFilter === "missing-bucket") return p.missingBuckets.length > 0;
    return true;
  });

  const done = (res: { error?: string } | { success: true } | { secretKey: string }) => {
    if ("error" in res && res.error) {
      setBanner(res.error);
      return false;
    }
    setBanner("");
    router.refresh();
    return true;
  };

  return (
    <div className="flex flex-col gap-4">
      {banner && (
        <p className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 font-body text-red-400 text-sm">
          {banner}
        </p>
      )}

      <div className="flex items-center gap-2">
        <div className="relative max-w-xs flex-1">
          <span className="icon-[tabler--search] pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-stone-600 text-xs" />
          <input
            type="text"
            placeholder="Filter by name..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="w-full rounded-md border border-amber-200/5 bg-surface py-1.5 pr-3 pl-8 font-mono text-xs outline-none transition-colors placeholder:text-stone-600 focus:border-amber-500"
          />
        </div>
        <p className="font-mono text-[10px] text-stone-600">
          Status and policies are written when you click Save.
        </p>
      </div>

      <Panel
        title="Users"
        icon="icon-[tabler--user]"
        error={snapshot.usersError && `Could not load users: ${snapshot.usersError}`}
        action={
          <Chips
            options={[
              { id: "all", label: "All" },
              { id: "no-policy", label: `No direct policy (${noPolicy.length})` },
              { id: "not-scoped", label: `Not bucket scoped (${notScopedUsers.length})` },
            ]}
            value={userFilter}
            onChange={(v) => setUserFilter(v as UserFilter)}
          />
        }
      >
        {users.length === 0 ? (
          <Empty text={idx.users.length === 0 ? "No users yet." : "No users match that filter."} />
        ) : (
          <Rows>
            {users.map((u) => (
              <button
                key={u.accessKey}
                type="button"
                onClick={() => setOpenUser(u)}
                className="grid w-full grid-cols-[1fr_auto] items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-surface/50"
              >
                <span className="min-w-0">
                  <span className="block truncate font-mono text-stone-300 text-xs">
                    {u.accessKey}
                  </span>
                  <span className="mt-0.5 block truncate font-mono text-[10px] text-stone-600">
                    {u.catchAll
                      ? "All buckets"
                      : u.buckets.length > 0
                        ? u.buckets.join(", ")
                        : "No bucket access"}
                  </span>
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  {u.policies.length === 0 && <Badge tone="warn">No direct policy</Badge>}
                  {u.missingPolicies.length > 0 && <Badge tone="warn">Missing</Badge>}
                  {u.status === "disabled" && <Badge>Disabled</Badge>}
                  <span className="font-mono text-[10px] text-stone-600">
                    {u.policies.length} {u.policies.length === 1 ? "policy" : "policies"}
                  </span>
                </span>
              </button>
            ))}
          </Rows>
        )}
        <p className="mt-3 font-mono text-[10px] text-stone-600">
          Service accounts, temporary credentials and groups are not listed. The server does not
          list them on the dashboard.
        </p>
      </Panel>

      <Panel
        title="Policies"
        icon="icon-[tabler--file-text]"
        error={snapshot.policiesError && `Could not load policies: ${snapshot.policiesError}`}
        action={
          <div className="flex items-center gap-3">
            <Chips
              options={[
                { id: "all", label: "All" },
                { id: "not-scoped", label: `Not bucket scoped (${notScopedPolicies.length})` },
                { id: "missing-bucket", label: `Missing bucket (${missingBucket.length})` },
              ]}
              value={policyFilter}
              onChange={(v) => setPolicyFilter(v as PolicyFilter)}
            />
            <button
              type="button"
              onClick={() => setNewPolicy(true)}
              className="flex items-center gap-1.5 rounded-md border border-amber-200/15 px-2 py-1 font-mono text-[11px] text-stone-400 transition-colors hover:bg-white/5"
            >
              <span className="icon-[tabler--plus] block text-[12px] text-amber-500" />
              New policy
            </button>
          </div>
        }
      >
        {policies.length === 0 ? (
          <Empty
            text={idx.policies.length === 0 ? "No policies yet." : "No policies match that filter."}
          />
        ) : (
          <Rows>
            {policies.map((p) => (
              <button
                key={p.name}
                type="button"
                onClick={() => setOpenPolicy(p)}
                className="grid w-full grid-cols-[1fr_auto] items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-surface/50"
              >
                <span className="min-w-0">
                  <span className="block truncate font-mono text-stone-300 text-xs">{p.name}</span>
                  <span className="mt-0.5 block truncate font-mono text-[10px] text-stone-600">
                    {scopeLabel(p)}
                  </span>
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  {p.builtIn && <Badge>Built in</Badge>}
                  {p.denyOnly && <Badge>Deny only</Badge>}
                  {p.missingBuckets.length > 0 && <Badge tone="warn">Missing bucket</Badge>}
                  {p.buckets.length > 1 && <Badge>Shared</Badge>}
                  <span className="font-mono text-[10px] text-stone-600">
                    {p.holders.length > 0 ? `Held by ${p.holders.length}` : "Held by nobody"}
                  </span>
                </span>
              </button>
            ))}
          </Rows>
        )}
      </Panel>

      {openUser && (
        <UserDialog
          user={openUser}
          policies={idx.policies}
          onClose={() => setOpenUser(null)}
          onSave={async (next, status) => {
            const a = await setUserPoliciesAction(openUser.accessKey, next);
            if (!done(a)) return;
            if (status !== (openUser.status === "enabled")) {
              const b = await setUserStatusAction(openUser.accessKey, status);
              if (!done(b)) return;
            }
            setOpenUser(null);
          }}
          onRotate={async () => {
            const res = await rotateSecretAction(
              openUser.accessKey,
              openUser.status === "disabled",
            );
            if ("error" in res) {
              setBanner(res.error);
              return null;
            }
            router.refresh();
            return res.secretKey;
          }}
          onDelete={async () => {
            if (done(await deleteUserAction(openUser.accessKey))) setOpenUser(null);
          }}
        />
      )}

      {openPolicy && (
        <PolicyDialog
          policy={openPolicy}
          buckets={idx.buckets}
          onClose={() => setOpenPolicy(null)}
          onSave={async (name, body) => {
            const res =
              name === openPolicy.name
                ? await savePolicyAction(name, body)
                : await renamePolicyAction(openPolicy.name, name, body);
            if (done(res)) setOpenPolicy(null);
          }}
          onDelete={async () => {
            if (done(await deletePolicyAction(openPolicy.name))) setOpenPolicy(null);
          }}
        />
      )}

      {newPolicy && (
        <PolicyDialog
          policy={null}
          buckets={idx.buckets}
          onClose={() => setNewPolicy(false)}
          onSave={async (name, body) => {
            if (done(await savePolicyAction(name, body))) setNewPolicy(false);
          }}
          onDelete={null}
        />
      )}
    </div>
  );
}

function scopeLabel(p: IndexedPolicy): string {
  if (p.parseError) return "Could not parse";
  if (p.catchAll) return "All buckets";
  if (p.missingBuckets.length > 0) return `Missing bucket: ${p.missingBuckets.join(", ")}`;
  if (p.buckets.length > 0) return p.buckets.join(", ");
  if (p.adminOnly) return "No bucket scope";
  return "No bucket scope";
}

// Modals
function UserDialog({
  user,
  policies,
  onClose,
  onSave,
  onRotate,
  onDelete,
}: {
  user: IndexedUser;
  policies: IndexedPolicy[];
  onClose: () => void;
  onSave: (policies: string[], enabled: boolean) => Promise<void>;
  onRotate: () => Promise<string | null>;
  onDelete: () => Promise<void>;
}) {
  const [held, setHeld] = useState<string[]>(user.policies);
  const [enabled, setEnabled] = useState(user.status === "enabled");
  const [confirm, setConfirm] = useState("");
  const [secret, setSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const missing = held.filter((pn) => !policies.some((p) => p.name === pn));
  const toggle = (name: string) =>
    setHeld((h) => (h.includes(name) ? h.filter((x) => x !== name) : [...h, name]));

  return (
    <Dialog open onClose={onClose} className="max-w-2xl" label={user.accessKey}>
      <div className="w-full rounded-xl border border-amber-200/5 bg-abyss p-5">
        <DialogHead title={user.accessKey} onClose={onClose} />

        {user.policies.length === 0 && (
          <Note>
            No policy is attached directly to this account. If it belongs to a group, that grant is
            not visible here.
          </Note>
        )}

        {missing.map((pn) => (
          <Note key={pn} tone="warn">
            Policy {pn} no longer exists. Remove it before saving.
          </Note>
        ))}

        <SectionLabel>Policies</SectionLabel>
        <div className="mb-4 max-h-64 space-y-1 overflow-y-auto">
          {held
            .filter((pn) => !policies.some((p) => p.name === pn))
            .map((pn) => (
              <label
                key={pn}
                className="flex items-center gap-2 rounded-md border border-red-500/20 px-3 py-1.5"
              >
                <input type="checkbox" checked onChange={() => toggle(pn)} />
                <span className="font-mono text-[11px] text-red-400">{pn}</span>
                <Badge tone="warn">Missing</Badge>
              </label>
            ))}
          {policies.map((p) => (
            <label
              key={p.name}
              className="flex items-center gap-2 rounded-md px-3 py-1.5 transition-colors hover:bg-surface/50"
            >
              <input
                type="checkbox"
                checked={held.includes(p.name)}
                onChange={() => toggle(p.name)}
              />
              <span className="font-mono text-[11px] text-stone-300">{p.name}</span>
              <span className="font-mono text-[10px] text-stone-600">{scopeLabel(p)}</span>
              {p.builtIn && <Badge>Built in</Badge>}
            </label>
          ))}
        </div>

        <SectionLabel>Account</SectionLabel>
        <label className="mb-4 flex items-center gap-2 font-mono text-[11px] text-stone-400">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          {enabled ? "Active" : "Disabled"}
        </label>

        {secret && (
          <Note>
            Copy the new secret key. It is not shown again.
            <span className="mt-2 block select-all break-all rounded-lg border border-amber-200/5 bg-void/40 px-3 py-2 font-mono text-[11px] text-stone-300">
              {secret}
            </span>
          </Note>
        )}

        <SectionLabel>Danger zone</SectionLabel>
        <p className="mb-2 font-body text-stone-500 text-xs">
          Deleting {user.accessKey} revokes its access key now. Service accounts and temporary
          credentials created by it are deleted too, and the console cannot list them. Type the
          access key to confirm.
        </p>
        <input
          type="text"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          placeholder={user.accessKey}
          className="mb-4 w-full rounded-md border border-amber-200/5 bg-surface px-3 py-1.5 font-mono text-xs outline-none focus:border-red-500"
        />

        <div className="flex gap-2">
          <button
            type="button"
            disabled={busy || missing.length > 0}
            onClick={async () => {
              setBusy(true);
              await onSave(held, enabled);
              setBusy(false);
            }}
            className="flex-1 rounded-md bg-amber-500 px-4 py-2 font-body font-medium text-black text-sm transition-colors hover:bg-amber-400 disabled:opacity-40"
          >
            Save
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              setSecret(await onRotate());
              setBusy(false);
            }}
            className="rounded-md border border-amber-200/15 px-3 py-2 font-mono text-[11px] text-stone-400 transition-colors hover:bg-white/5 disabled:opacity-40"
          >
            Rotate secret key
          </button>
          <button
            type="button"
            disabled={busy || confirm !== user.accessKey}
            onClick={async () => {
              setBusy(true);
              await onDelete();
              setBusy(false);
            }}
            className="rounded-md bg-red-500 px-3 py-2 font-body font-medium text-sm text-white transition-colors hover:bg-red-500/90 disabled:opacity-30"
          >
            Delete user
          </button>
        </div>
      </div>
    </Dialog>
  );
}

function PolicyDialog({
  policy,
  buckets,
  onClose,
  onSave,
  onDelete,
}: {
  policy: IndexedPolicy | null;
  buckets: string[];
  onClose: () => void;
  onSave: (name: string, body: string) => Promise<void>;
  onDelete: (() => Promise<void>) | null;
}) {
  const [name, setName] = useState(policy?.name ?? "");
  const [body, setBody] = useState(policy?.policy ?? NEW_POLICY_BODY);
  const [addBucket, setAddBucket] = useState("");
  const [busy, setBusy] = useState(false);

  const builtIn = policy?.builtIn ?? false;
  const canWiden = !policy || (!policy.adminOnly && !policy.denyOnly && !policy.catchAll);

  // Appends both ARNs into the textarea so the operator reads the change before saving.
  const widen = (bucket: string) => {
    if (!bucket) return;
    try {
      const parsed = JSON.parse(body);
      const list = Array.isArray(parsed.Statement) ? parsed.Statement : [parsed.Statement];
      for (const st of list) {
        if (st?.Effect !== "Allow") continue;
        const res = Array.isArray(st.Resource) ? st.Resource : st.Resource ? [st.Resource] : [];
        st.Resource = [...new Set([...res, `arn:aws:s3:::${bucket}`, `arn:aws:s3:::${bucket}/*`])];
      }
      parsed.Statement = list;
      setBody(JSON.stringify(parsed, null, 2));
      setAddBucket("");
    } catch {
      // Unparseable body, user edits the json directly
    }
  };

  return (
    <Dialog open onClose={onClose} className="max-w-2xl" label={policy?.name ?? "New policy"}>
      <div className="w-full rounded-xl border border-amber-200/5 bg-abyss p-5">
        <DialogHead title={policy?.name ?? "New policy"} onClose={onClose} />

        {builtIn && (
          <Note>Built in policy. The server recreates it on restart if you delete it.</Note>
        )}
        {policy?.parseError && <Note tone="warn">This policy is not valid JSON.</Note>}
        {policy && policy.holders.length > 0 && name !== policy.name && (
          <Note>
            Renaming writes the new policy, moves every holder, then deletes the old name.
          </Note>
        )}

        <SectionLabel>Name</SectionLabel>
        <input
          type="text"
          value={name}
          disabled={builtIn}
          onChange={(e) => setName(e.target.value.replace(/[^a-zA-Z0-9_-]/g, ""))}
          placeholder="policy-name"
          className="mb-4 w-full rounded-md border border-amber-200/5 bg-surface px-3 py-1.5 font-mono text-xs outline-none focus:border-amber-500 disabled:opacity-50"
        />

        <SectionLabel>Document</SectionLabel>
        {!policy && (
          <p className="mb-2 font-body text-stone-500 text-xs">
            Replace bucket-name with a real bucket.
          </p>
        )}
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          spellCheck={false}
          rows={12}
          className="mb-3 w-full rounded-lg border border-amber-200/5 bg-void/40 px-3 py-2 font-mono text-[11px] text-stone-300 outline-none focus:border-amber-500"
        />

        {canWiden ? (
          <div className="mb-4 flex items-center gap-2">
            <select
              value={addBucket}
              onChange={(e) => setAddBucket(e.target.value)}
              className="rounded-md border border-amber-200/5 bg-surface px-2 py-1.5 font-mono text-[11px] text-stone-300 outline-none focus:border-amber-500"
            >
              <option value="">Add bucket to this policy</option>
              {buckets.map((b) => (
                <option key={b} value={b}>
                  {b}
                </option>
              ))}
            </select>
            <button
              type="button"
              disabled={!addBucket}
              onClick={() => widen(addBucket)}
              className="rounded-md border border-amber-200/15 px-2 py-1.5 font-mono text-[11px] text-stone-400 transition-colors hover:bg-white/5 disabled:opacity-40"
            >
              Add
            </button>
            <span className="font-mono text-[10px] text-stone-600">
              Review the JSON, then Save policy.
            </span>
          </div>
        ) : (
          <p className="mb-4 font-mono text-[10px] text-stone-600">Edit the JSON directly.</p>
        )}

        {policy && onDelete && (
          <>
            <SectionLabel>Danger zone</SectionLabel>
            <p className="mb-3 font-body text-stone-500 text-xs">
              {policy.holders.length > 0
                ? `Deleting ${policy.name} removes it from ${policy.holders.length} ${
                    policy.holders.length === 1 ? "user" : "users"
                  }: ${policy.holders.join(", ")}.`
                : `Deleting ${policy.name} affects no users.`}{" "}
              {policy.buckets.length > 0 && `It stops granting ${policy.buckets.join(", ")}.`} This
              cannot be undone.
            </p>
          </>
        )}

        <div className="flex gap-2">
          <button
            type="button"
            disabled={busy || !name.trim()}
            onClick={async () => {
              setBusy(true);
              await onSave(name.trim(), body);
              setBusy(false);
            }}
            className="flex-1 rounded-md bg-amber-500 px-4 py-2 font-body font-medium text-black text-sm transition-colors hover:bg-amber-400 disabled:opacity-40"
          >
            Save policy
          </button>
          {policy && onDelete && (
            <button
              type="button"
              disabled={busy || builtIn}
              title={builtIn ? "The server recreates built in policies." : undefined}
              onClick={async () => {
                setBusy(true);
                await onDelete();
                setBusy(false);
              }}
              className="rounded-md bg-red-500 px-3 py-2 font-body font-medium text-sm text-white transition-colors hover:bg-red-500/90 disabled:opacity-30"
            >
              Delete policy
            </button>
          )}
        </div>
      </div>
    </Dialog>
  );
}

function Panel({
  title,
  icon,
  action,
  error,
  children,
}: {
  title: string;
  icon: string;
  action?: React.ReactNode;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col rounded-xl border border-amber-200/5 bg-abyss p-5">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <p className="flex items-center gap-2 font-display font-semibold text-sm">
          <span className={`${icon} text-[13px] text-amber-500`} />
          {title}
        </p>
        {action}
      </div>
      {error ? (
        <p className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 font-body text-red-400 text-sm">
          {error}
        </p>
      ) : (
        children
      )}
    </div>
  );
}

function Rows({ children }: { children: React.ReactNode }) {
  return (
    <div className="divide-y divide-amber-200/10 overflow-hidden rounded-lg border border-amber-200/5">
      {children}
    </div>
  );
}

function Empty({ text }: { text: string }) {
  return <p className="py-6 font-body text-stone-600 text-xs">{text}</p>;
}

function Badge({ children, tone }: { children: React.ReactNode; tone?: "warn" }) {
  const cls =
    tone === "warn" ? "border-red-500/30 text-red-400" : "border-amber-200/15 text-stone-500";
  return (
    <span className={`rounded border px-1.5 py-0.5 font-mono text-[9px] uppercase ${cls}`}>
      {children}
    </span>
  );
}

function Chips<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { id: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          onClick={() => onChange(o.id)}
          className={`rounded-md px-2 py-1 font-mono text-[10px] transition-colors ${
            value === o.id
              ? "bg-surface-overlay text-stone-200"
              : "text-stone-600 hover:text-stone-400"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-2 font-mono text-[10px] text-stone-600 uppercase tracking-wider">{children}</p>
  );
}

function Note({ children, tone }: { children: React.ReactNode; tone?: "warn" }) {
  const cls =
    tone === "warn"
      ? "border-red-500/20 bg-red-500/5 text-red-400"
      : "border-amber-200/10 bg-void/40 text-stone-400";
  return <p className={`mb-3 rounded-lg border px-3 py-2 font-body text-xs ${cls}`}>{children}</p>;
}

function DialogHead({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div className="mb-4 flex items-center justify-between gap-2">
      <p className="truncate font-display font-semibold text-sm">{title}</p>
      <button
        type="button"
        aria-label="Close"
        onClick={onClose}
        className="text-stone-500 transition-colors hover:text-stone-300"
      >
        <span className="icon-[tabler--x] text-sm" />
      </button>
    </div>
  );
}
