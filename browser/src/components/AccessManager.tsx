"use client";

import { useRouter } from "next/navigation";
import { type ReactNode, useMemo, useState } from "react";
import { Checkbox } from "@/components/Checkbox";
import {
  buildAccessIndex,
  type IndexedPolicy,
  type IndexedUser,
  newUserError,
} from "@/lib/access-index";
import {
  type AccessSnapshot,
  createUserAction,
  deletePolicyAction,
  deleteUserAction,
  renamePolicyAction,
  rotateSecretAction,
  savePolicyAction,
  setUserPoliciesAction,
  setUserStatusAction,
} from "@/lib/actions";
import { addBucketToPolicy, fullBucketPolicy } from "@/lib/policy-templates";
import { safeDisplayName } from "@/lib/safe-name";
import { CopyButton } from "./CopyButton";
import { Dialog } from "./Dialog";
import { PageHeader } from "./PageHeader";
import { Segmented } from "./Segmented";
import { SelectMenu } from "./SelectMenu";
import { SearchField, Toolbar } from "./Toolbar";

type Tab = "users" | "policies";
type UserFilter = "all" | "no-policy" | "not-scoped";
type PolicyFilter = "all" | "not-scoped" | "missing-bucket";

interface Credentials {
  accessKey: string;
  secretKey: string;
}

// Anywhere on row opens dialog
const ROW = "relative transition-colors hover:bg-white/[0.03]";
const ROW_BUTTON = "block max-w-full text-left after:absolute after:inset-0";

export function AccessManager({ snapshot }: { snapshot: AccessSnapshot }) {
  const router = useRouter();
  const idx = useMemo(
    () => buildAccessIndex(snapshot.buckets, snapshot.policies, snapshot.users),
    [snapshot],
  );

  const [tab, setTab] = useState<Tab>("users");
  const [userFilter, setUserFilter] = useState<UserFilter>("all");
  const [policyFilter, setPolicyFilter] = useState<PolicyFilter>("all");
  const [query, setQuery] = useState("");
  const [openUser, setOpenUser] = useState<IndexedUser | null>(null);
  const [newUser, setNewUser] = useState(false);
  const [openPolicy, setOpenPolicy] = useState<IndexedPolicy | null>(null);
  const [newPolicy, setNewPolicy] = useState(false);
  const [banner, setBanner] = useState("");

  const noPolicy = idx.users.filter((u) => u.policies.length === 0).length;
  const notScopedUsers = idx.users.filter(
    (u) => u.buckets.length === 0 && u.policies.length > 0,
  ).length;
  const notScopedPolicies = idx.policies.filter((p) => p.buckets.length === 0).length;
  const missingBucket = idx.policies.filter((p) => p.missingBuckets.length > 0).length;

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
    <>
      <PageHeader
        title="Access"
        subtitle="Users and policies across every bucket."
        actions={
          tab === "users" ? (
            <button
              type="button"
              onClick={() => setNewUser(true)}
              disabled={Boolean(snapshot.usersError)}
              title={snapshot.usersError ? "Users could not be listed." : undefined}
              className="font-body inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              <span className="icon-[tabler--user-plus] text-lg" />
              New user
            </button>
          ) : (
            <button
              type="button"
              onClick={() => setNewPolicy(true)}
              className="font-body inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              <span className="icon-[tabler--plus] text-lg" />
              New policy
            </button>
          )
        }
      />

      {banner && (
        <p className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400">
          {banner}
        </p>
      )}

      <div role="tablist" aria-label="Access" className="flex gap-6 border-b border-stone-800/70">
        {(["users", "policies"] as const).map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`-mb-px inline-flex items-center gap-2 border-b-2 px-1 pb-3 text-[15px] font-medium transition-colors ${
              tab === t
                ? "border-amber-500 text-stone-100"
                : "border-transparent text-stone-400 hover:text-stone-200"
            }`}
          >
            <span>{t === "users" ? "Users" : "Policies"}</span>
            <span className="bg-surface-overlay inline-flex h-5 min-w-5 items-center justify-center rounded-md px-1.5 font-mono text-xs text-stone-300">
              {t === "users" ? idx.users.length : idx.policies.length}
            </span>
          </button>
        ))}
      </div>

      {tab === "users" ? (
        <>
          <section className="bg-abyss rounded-xl">
            {snapshot.usersError ? (
              <LoadError text={`Could not load users: ${snapshot.usersError}`} />
            ) : (
              <>
                <Toolbar>
                  <SearchField value={query} onChange={setQuery} placeholder="Filter users" />
                  <Segmented
                    label="Show users"
                    options={[
                      { id: "all", label: "All", count: idx.users.length },
                      { id: "no-policy", label: "No policy", count: noPolicy },
                      { id: "not-scoped", label: "Not scoped", count: notScopedUsers },
                    ]}
                    value={userFilter}
                    onChange={setUserFilter}
                  />
                </Toolbar>
                {users.length === 0 ? (
                  <Empty
                    text={idx.users.length === 0 ? "No users yet." : "No users match that filter."}
                  />
                ) : (
                  <div className="px-4 pb-4">
                    <div className="overflow-x-auto rounded-lg border border-stone-800/70">
                      <table className="w-full">
                        <thead>
                          <tr>
                            <th
                              scope="col"
                              className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md"
                            >
                              Access key
                            </th>
                            <th
                              scope="col"
                              className="bg-surface font-body hidden h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md md:table-cell"
                            >
                              Buckets
                            </th>
                            <th
                              scope="col"
                              className="bg-surface font-body hidden h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:table-cell"
                            >
                              Policies
                            </th>
                            <th
                              scope="col"
                              className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md"
                            >
                              Status
                            </th>
                            <th
                              scope="col"
                              className="bg-surface font-body h-8 w-10 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md"
                            >
                              <span className="sr-only">Open</span>
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          {users.map((u) => (
                            <tr key={u.accessKey} className={ROW}>
                              <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                                <button
                                  type="button"
                                  onClick={() => setOpenUser(u)}
                                  title={safeDisplayName(u.accessKey)}
                                  className={ROW_BUTTON}
                                >
                                  <span className="block max-w-[16rem] truncate font-mono text-stone-100">
                                    <bdi>{safeDisplayName(u.accessKey)}</bdi>
                                  </span>
                                </button>
                              </td>
                              <td className="hidden h-10 border-t border-stone-800/70 px-4 text-[13px] md:table-cell pointer-coarse:h-11">
                                <span className="block max-w-[18rem] truncate text-stone-300">
                                  {u.catchAll ? (
                                    "All buckets"
                                  ) : u.buckets.length > 0 ? (
                                    <bdi>{safeDisplayName(u.buckets.join(", "))}</bdi>
                                  ) : (
                                    <span className="text-stone-400">No bucket access</span>
                                  )}
                                </span>
                              </td>
                              <td className="hidden h-10 border-t border-stone-800/70 px-4 text-[13px] sm:table-cell pointer-coarse:h-11">
                                {u.policies.length === 0 ? (
                                  <Badge tone="warn">No direct policy</Badge>
                                ) : (
                                  <span className="flex min-w-0 items-center gap-2">
                                    <span className="block max-w-[14rem] truncate text-stone-300">
                                      <bdi>{safeDisplayName(u.policies.join(", "))}</bdi>
                                    </span>
                                    {u.missingPolicies.length > 0 && (
                                      <Badge tone="warn">Missing</Badge>
                                    )}
                                  </span>
                                )}
                              </td>
                              <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                                <span className="inline-flex items-center gap-2 whitespace-nowrap text-stone-300">
                                  <span
                                    className={`h-2 w-2 shrink-0 rounded-full ${
                                      u.status === "enabled" ? "bg-green-500" : "bg-stone-500"
                                    }`}
                                  />
                                  {u.status === "enabled" ? "Active" : "Disabled"}
                                </span>
                              </td>
                              <td className="h-10 w-10 border-t border-stone-800/70 px-4 text-right text-[13px] pointer-coarse:h-11">
                                <span className="icon-[tabler--chevron-right] align-middle text-lg text-stone-500" />
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </>
            )}
          </section>
          <p className="flex items-center gap-2 text-[13px] text-stone-400">
            <span className="icon-[tabler--info-circle] shrink-0 text-base" />
            Service accounts, temporary credentials and groups are not listed.
          </p>
        </>
      ) : (
        <section className="bg-abyss rounded-xl">
          {snapshot.policiesError ? (
            <LoadError text={`Could not load policies: ${snapshot.policiesError}`} />
          ) : (
            <>
              <Toolbar>
                <SearchField value={query} onChange={setQuery} placeholder="Filter policies" />
                <Segmented
                  label="Show policies"
                  options={[
                    { id: "all", label: "All", count: idx.policies.length },
                    { id: "not-scoped", label: "Not scoped", count: notScopedPolicies },
                    { id: "missing-bucket", label: "Missing bucket", count: missingBucket },
                  ]}
                  value={policyFilter}
                  onChange={setPolicyFilter}
                />
              </Toolbar>
              {policies.length === 0 ? (
                <Empty
                  text={
                    idx.policies.length === 0
                      ? "No policies yet."
                      : "No policies match that filter."
                  }
                />
              ) : (
                <div className="px-4 pb-4">
                  <div className="overflow-x-auto rounded-lg border border-stone-800/70">
                    <table className="w-full">
                      <thead>
                        <tr>
                          <th
                            scope="col"
                            className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md"
                          >
                            Name
                          </th>
                          <th
                            scope="col"
                            className="bg-surface font-body hidden h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:table-cell"
                          >
                            Scope
                          </th>
                          <th
                            scope="col"
                            className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md"
                          >
                            Held by
                          </th>
                          <th
                            scope="col"
                            className="bg-surface font-body hidden h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md md:table-cell"
                          >
                            Flags
                          </th>
                          <th
                            scope="col"
                            className="bg-surface font-body h-8 w-10 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md"
                          >
                            <span className="sr-only">Open</span>
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {policies.map((p) => (
                          <tr key={p.name} className={ROW}>
                            <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                              <button
                                type="button"
                                onClick={() => setOpenPolicy(p)}
                                title={safeDisplayName(p.name)}
                                className={ROW_BUTTON}
                              >
                                <span className="block max-w-[16rem] truncate text-[14px] text-stone-100">
                                  <bdi>{safeDisplayName(p.name)}</bdi>
                                </span>
                              </button>
                            </td>
                            <td className="hidden h-10 border-t border-stone-800/70 px-4 text-[13px] sm:table-cell pointer-coarse:h-11">
                              <span
                                className={`block max-w-[18rem] truncate ${
                                  p.parseError || p.missingBuckets.length > 0
                                    ? "text-red-400"
                                    : "text-stone-300"
                                }`}
                              >
                                <bdi>{safeDisplayName(scopeLabel(p))}</bdi>
                              </span>
                            </td>
                            <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] whitespace-nowrap pointer-coarse:h-11">
                              {p.holders.length === 0 ? (
                                <span className="text-stone-400">Nobody</span>
                              ) : (
                                <span className="text-stone-300">
                                  <span className="font-mono">{p.holders.length}</span>{" "}
                                  {p.holders.length === 1 ? "user" : "users"}
                                </span>
                              )}
                            </td>
                            <td className="hidden h-10 border-t border-stone-800/70 px-4 text-[13px] md:table-cell pointer-coarse:h-11">
                              <span className="flex items-center gap-1.5 whitespace-nowrap">
                                {p.builtIn && <Badge>Built in</Badge>}
                                {p.denyOnly && <Badge>Deny only</Badge>}
                                {p.missingBuckets.length > 0 && (
                                  <Badge tone="warn">Missing bucket</Badge>
                                )}
                                {p.buckets.length > 1 && <Badge>Shared</Badge>}
                              </span>
                            </td>
                            <td className="h-10 w-10 border-t border-stone-800/70 px-4 text-right text-[13px] pointer-coarse:h-11">
                              <span className="icon-[tabler--chevron-right] align-middle text-lg text-stone-500" />
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </>
          )}
        </section>
      )}

      {newUser && (
        <UserDialog
          mode="create"
          policies={idx.policies}
          existing={idx.users.map((u) => u.accessKey)}
          onClose={() => setNewUser(false)}
          onCreate={async (accessKey, held) => {
            const res = await createUserAction(accessKey, held);
            if (!("error" in res)) router.refresh();
            return res;
          }}
        />
      )}

      {openUser && (
        <UserDialog
          mode="edit"
          user={openUser}
          policies={idx.policies}
          existing={idx.users.map((u) => u.accessKey)}
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
    </>
  );
}

function scopeLabel(p: IndexedPolicy): string {
  if (p.parseError) return "Could not parse";
  if (p.catchAll) return "All buckets";
  if (p.missingBuckets.length > 0) return `Missing bucket: ${p.missingBuckets.join(", ")}`;
  if (p.buckets.length > 0) return p.buckets.join(", ");
  return "No bucket scope";
}

// Modals
interface UserDialogBase {
  policies: IndexedPolicy[];
  existing: string[];
  onClose: () => void;
}

interface CreateUserProps extends UserDialogBase {
  mode: "create";
  onCreate: (accessKey: string, policies: string[]) => Promise<Credentials | { error: string }>;
}

interface EditUserProps extends UserDialogBase {
  mode: "edit";
  user: IndexedUser;
  onSave: (policies: string[], enabled: boolean) => Promise<void>;
  onRotate: () => Promise<string | null>;
  onDelete: () => Promise<void>;
}

type UserDialogProps = CreateUserProps | EditUserProps;

function UserDialog(props: UserDialogProps) {
  const title = props.mode === "edit" ? props.user.accessKey : "New user";
  return (
    <Dialog open onClose={props.onClose} className="max-w-2xl" label={title}>
      <div className="bg-abyss max-h-[calc(100dvh-2rem)] w-full overflow-y-auto rounded-xl p-6">
        <DialogHead title={title} onClose={props.onClose} />
        {props.mode === "edit" ? <EditUser {...props} /> : <CreateUser {...props} />}
      </div>
    </Dialog>
  );
}

function CreateUser({ policies, existing, onClose, onCreate }: CreateUserProps) {
  const [accessKey, setAccessKey] = useState("");
  const [held, setHeld] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [created, setCreated] = useState<Credentials | null>(null);

  const keyError = newUserError(accessKey, existing);
  const toggle = (name: string) =>
    setHeld((h) => (h.includes(name) ? h.filter((x) => x !== name) : [...h, name]));

  if (created) {
    return (
      <>
        <Note>Copy the secret key now. It is not shown again.</Note>
        <CredentialRow label="Access key" value={created.accessKey} />
        <CredentialRow label="Secret key" value={created.secretKey} />
        <button
          type="button"
          onClick={onClose}
          className="font-body mt-2 inline-flex h-9 w-full shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          Done
        </button>
      </>
    );
  }

  return (
    <>
      {error && (
        <p
          role="alert"
          className="mb-3 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-400"
        >
          {error}
        </p>
      )}

      <label
        htmlFor="new-user-access-key"
        className="font-body mb-2 block text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase"
      >
        Access key
      </label>
      <input
        id="new-user-access-key"
        type="text"
        value={accessKey}
        onChange={(e) => setAccessKey(e.target.value)}
        placeholder="Leave blank to generate one"
        autoComplete="off"
        spellCheck={false}
        aria-invalid={keyError !== null}
        aria-describedby="new-user-access-key-help"
        className="bg-surface h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
      />
      <p
        id="new-user-access-key-help"
        className={`mt-1.5 mb-4 text-[13px] ${keyError ? "text-red-400" : "text-stone-400"}`}
      >
        {keyError ?? "3 to 20 characters, no spaces. The secret key is always generated."}
      </p>

      <SectionLabel>Policies</SectionLabel>
      <PolicyChecklist policies={policies} held={held} onToggle={toggle} />

      <div className="flex gap-2">
        <button
          type="button"
          disabled={busy || keyError !== null}
          onClick={async () => {
            setBusy(true);
            const res = await onCreate(accessKey.trim(), held);
            setBusy(false);
            if ("error" in res) setError(res.error);
            else setCreated(res);
          }}
          className="font-body inline-flex h-9 flex-1 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          {busy ? "Creating..." : "Create user"}
        </button>
        <button
          type="button"
          onClick={onClose}
          className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          Cancel
        </button>
      </div>
    </>
  );
}

function EditUser({ user, policies, onSave, onRotate, onDelete }: EditUserProps) {
  const [held, setHeld] = useState<string[]>(user.policies);
  const [enabled, setEnabled] = useState(user.status === "enabled");
  const [confirm, setConfirm] = useState("");
  const [secret, setSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const missing = held.filter((pn) => !policies.some((p) => p.name === pn));
  const toggle = (name: string) =>
    setHeld((h) => (h.includes(name) ? h.filter((x) => x !== name) : [...h, name]));

  return (
    <>
      {user.policies.length === 0 && (
        <Note>
          No policy is attached directly to this account. If it belongs to a group, that grant is
          not visible here.
        </Note>
      )}

      {missing.map((pn) => (
        <Note key={pn} tone="warn">
          Policy <bdi>{safeDisplayName(pn)}</bdi> no longer exists. Remove it before saving.
        </Note>
      ))}

      <SectionLabel>Policies</SectionLabel>
      <PolicyChecklist policies={policies} held={held} onToggle={toggle} />

      <SectionLabel>Account</SectionLabel>
      <label className="mb-4 flex items-center gap-2 text-sm text-stone-300">
        <Checkbox checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        {enabled ? "Active" : "Disabled"}
      </label>

      {secret && (
        <>
          <Note>Copy the new secret key. It is not shown again.</Note>
          <CredentialRow label="Secret key" value={secret} />
        </>
      )}

      <SectionLabel>Danger zone</SectionLabel>
      <p className="mb-2 text-sm text-stone-400">
        Deleting <bdi>{safeDisplayName(user.accessKey)}</bdi> revokes its access key now. Service
        accounts and temporary credentials created by it are deleted too, and the console cannot
        list them. Type the access key to confirm.
      </p>
      <input
        type="text"
        value={confirm}
        onChange={(e) => setConfirm(e.target.value)}
        placeholder={user.accessKey}
        aria-label="Type the access key to confirm"
        autoComplete="off"
        spellCheck={false}
        className="bg-surface mb-4 h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 focus:border-red-500 disabled:opacity-50 sm:text-sm"
      />

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          disabled={busy || missing.length > 0}
          onClick={async () => {
            setBusy(true);
            await onSave(held, enabled);
            setBusy(false);
          }}
          className="font-body inline-flex h-9 flex-1 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
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
          className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
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
          className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-red-500 px-3.5 text-sm font-medium whitespace-nowrap text-white transition-colors hover:bg-red-500/90 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          Delete user
        </button>
      </div>
    </>
  );
}

function PolicyChecklist({
  policies,
  held,
  onToggle,
}: {
  policies: IndexedPolicy[];
  held: string[];
  onToggle: (name: string) => void;
}) {
  const missing = held.filter((pn) => !policies.some((p) => p.name === pn));
  return (
    <div className="mb-4 max-h-64 space-y-1 overflow-y-auto">
      {missing.map((pn) => (
        <label
          key={pn}
          className="flex items-center gap-2 rounded-md border border-red-500/20 px-3 py-1.5"
        >
          <Checkbox checked onChange={() => onToggle(pn)} />
          <span className="text-sm text-red-400">
            <bdi>{safeDisplayName(pn)}</bdi>
          </span>
          <Badge tone="warn">Missing</Badge>
        </label>
      ))}
      {policies.length === 0 && missing.length === 0 && (
        <p className="px-3 py-1.5 text-[13px] text-stone-400">
          No policies yet. Create one on the Policies tab.
        </p>
      )}
      {policies.map((p) => (
        <label
          key={p.name}
          className="hover:bg-surface/50 flex items-center gap-2 rounded-md px-3 py-1.5 transition-colors"
        >
          <Checkbox checked={held.includes(p.name)} onChange={() => onToggle(p.name)} />
          <span className="shrink-0 text-sm text-stone-200">
            <bdi>{safeDisplayName(p.name)}</bdi>
          </span>
          <span className="min-w-0 truncate text-[13px] text-stone-400">
            <bdi>{safeDisplayName(scopeLabel(p))}</bdi>
          </span>
          {p.builtIn && <Badge>Built in</Badge>}
        </label>
      ))}
    </div>
  );
}

function CredentialRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-void mb-2 flex items-center gap-3 rounded-lg border border-stone-800/70 px-3 py-2">
      <span className="w-20 shrink-0 text-[13px] text-stone-400">{label}</span>
      <span className="min-w-0 flex-1 font-mono text-[13px] break-all text-stone-100 select-all">
        <bdi>{safeDisplayName(value)}</bdi>
      </span>
      <CopyButton value={value} label={`Copy ${label.toLowerCase()}`} />
    </div>
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
  const defaultBucket = buckets[0] ?? "bucket-name";
  const [body, setBody] = useState(() => policy?.policy ?? fullBucketPolicy(defaultBucket));
  const [addBucket, setAddBucket] = useState("");
  const [widenError, setWidenError] = useState("");
  const [busy, setBusy] = useState(false);

  const builtIn = policy?.builtIn ?? false;
  const canWiden = !policy || (!policy.adminOnly && !policy.denyOnly && !policy.catchAll);

  // Appends both ARNs into the textarea so the operator reads the change before saving.
  const widen = (bucket: string) => {
    if (!bucket) return;
    try {
      setBody(addBucketToPolicy(body, bucket));
      setAddBucket("");
      setWidenError("");
    } catch (error) {
      setWidenError(
        error instanceof SyntaxError
          ? "Enter a valid JSON policy before adding a bucket."
          : error instanceof Error
            ? error.message
            : "Edit the JSON directly to add this bucket.",
      );
    }
  };

  return (
    <Dialog open onClose={onClose} className="max-w-2xl" label={policy?.name ?? "New policy"}>
      <div className="bg-abyss max-h-[calc(100dvh-2rem)] w-full overflow-y-auto rounded-xl p-6">
        <DialogHead title={policy?.name ?? "New policy"} onClose={onClose} />

        {builtIn && (
          <Note>Built in policy. The server recreates it on restart if you delete it.</Note>
        )}
        {policy?.parseError && <Note tone="warn">This policy is not valid JSON.</Note>}
        {widenError && <Note tone="warn">{widenError}</Note>}
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
          aria-label="Policy name"
          className="bg-surface mb-4 h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />

        <SectionLabel>Document</SectionLabel>
        {!policy && buckets.length === 0 && (
          <p className="mb-2 text-sm text-stone-400">Replace bucket-name with a real bucket.</p>
        )}
        <textarea
          value={body}
          onChange={(e) => {
            setBody(e.target.value);
            setWidenError("");
          }}
          spellCheck={false}
          rows={12}
          aria-label="Policy document"
          className="bg-void mb-3 w-full rounded-lg border border-stone-800/70 px-3 py-2 font-mono text-[13px] text-stone-200 outline-none focus:border-amber-500"
        />

        {canWiden ? (
          <div className="mb-4 flex flex-wrap items-center gap-2">
            <SelectMenu
              value={addBucket}
              onChange={setAddBucket}
              label="Add bucket to this policy"
              options={[
                { value: "", label: "Add bucket to this policy" },
                ...buckets.map((b) => ({ value: b, label: safeDisplayName(b) })),
              ]}
              className="h-8"
            />
            <button
              type="button"
              disabled={!addBucket}
              onClick={() => widen(addBucket)}
              className="bg-surface font-body hover:bg-surface-overlay inline-flex h-8 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              Add
            </button>
            <span className="text-[13px] text-stone-400">Review the JSON, then Save policy.</span>
          </div>
        ) : (
          <p className="mb-4 text-[13px] text-stone-400">Edit the JSON directly.</p>
        )}

        {policy && onDelete && (
          <>
            <SectionLabel>Danger zone</SectionLabel>
            <p className="mb-3 text-sm text-stone-400">
              {policy.holders.length > 0 ? (
                <>
                  Deleting <bdi>{safeDisplayName(policy.name)}</bdi> removes it from{" "}
                  {policy.holders.length} {policy.holders.length === 1 ? "user" : "users"}:{" "}
                  <bdi>{safeDisplayName(policy.holders.join(", "))}</bdi>.
                </>
              ) : (
                <>
                  Deleting <bdi>{safeDisplayName(policy.name)}</bdi> affects no users.
                </>
              )}{" "}
              {policy.buckets.length > 0 && (
                <>
                  It stops granting <bdi>{safeDisplayName(policy.buckets.join(", "))}</bdi>.
                </>
              )}{" "}
              This cannot be undone.
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
            className="font-body inline-flex h-9 flex-1 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
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
              className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-red-500 px-3.5 text-sm font-medium whitespace-nowrap text-white transition-colors hover:bg-red-500/90 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              Delete policy
            </button>
          )}
        </div>
      </div>
    </Dialog>
  );
}

function LoadError({ text }: { text: string }) {
  return (
    <p className="m-4 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400">
      {text}
    </p>
  );
}

function Empty({ text }: { text: string }) {
  return <p className="px-4 pt-4 pb-8 text-center text-sm text-stone-400">{text}</p>;
}

function Badge({ children, tone }: { children: ReactNode; tone?: "warn" }) {
  const cls =
    tone === "warn" ? "border-red-500/30 text-red-400" : "border-stone-700 text-stone-400";
  return (
    <span
      className={`rounded-md border px-1.5 py-0.5 text-[11px] font-medium tracking-[0.08em] whitespace-nowrap uppercase ${cls}`}
    >
      {children}
    </span>
  );
}

function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <p className="font-body mb-2 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
      {children}
    </p>
  );
}

function Note({ children, tone }: { children: ReactNode; tone?: "warn" }) {
  const cls =
    tone === "warn"
      ? "border-red-500/20 bg-red-500/5 text-red-400"
      : "border-stone-800/70 bg-abyss text-stone-300";
  return <p className={`mb-3 rounded-lg border px-3 py-2 text-[13px] ${cls}`}>{children}</p>;
}

function DialogHead({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div className="mb-4 flex items-center justify-between gap-2">
      <h3 className="font-display truncate text-lg font-semibold">
        <bdi>{safeDisplayName(title)}</bdi>
      </h3>
      <button
        type="button"
        aria-label="Close"
        onClick={onClose}
        className="hover:bg-surface inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
      >
        <span className="icon-[tabler--x]" />
      </button>
    </div>
  );
}
