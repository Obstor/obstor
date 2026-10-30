"use client";

import { useState } from "react";
import { type AccessDraft, type IAMUser, setUserStatusAction } from "@/lib/actions";
import { safeDisplayName } from "@/lib/safe-name";
import { CopyButton } from "../CopyButton";
import { Segmented } from "../Segmented";
import { AttachList, Toggle } from "./shared";

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

type Creds = { accessKey: string; secretKey: string };

export function UsersPanel({
  draft,
  onChange,
  onCommit,
  onError,
}: {
  draft: AccessDraft;
  onChange: (patch: Partial<AccessDraft>) => void;
  onCommit: (apply: (d: AccessDraft) => AccessDraft) => void;
  onError: (err: string) => void;
}) {
  const [adding, setAdding] = useState(false);
  const [attaching, setAttaching] = useState(false);
  const [creds, setCreds] = useState<Creds | null>(null);
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const policyNames = Array.from(new Set(draft.policies.map((p) => p.name).filter(Boolean)));
  // Hide when user leaves list
  const shownCreds =
    creds && draft.users.some((u) => u.accessKey === creds.accessKey) ? creds : null;

  const removeUser = (user: IAMUser) => {
    const users = draft.users.filter((u) => u.accessKey !== user.accessKey);
    // Staged user needs no detach on save
    if (user.pendingSecretKey) onChange({ users });
    else onChange({ users, detachedUsers: [...draft.detachedUsers, user.accessKey] });
  };

  const toggleStatus = async (user: IAMUser) => {
    const status: IAMUser["status"] = user.status === "enabled" ? "disabled" : "enabled";
    const withStatus = (list: IAMUser[]) =>
      list.map((u) => (u.accessKey === user.accessKey ? { ...u, status } : u));
    // Staged user's status saves with user
    if (user.pendingSecretKey) {
      onChange({ users: withStatus(draft.users) });
      return;
    }
    setBusyKey(user.accessKey);
    try {
      const res = await setUserStatusAction(user.accessKey, status === "enabled");
      if ("error" in res && res.error) {
        onError(res.error);
        return;
      }
      // Update loaded status too so Discard keeps server state
      onCommit((d) => ({
        ...d,
        users: withStatus(d.users),
        attachableUsers: withStatus(d.attachableUsers),
      }));
    } catch (err) {
      onError(err instanceof Error ? err.message : "Failed to set user status");
    } finally {
      setBusyKey(null);
    }
  };

  const setPolicies = (accessKey: string, policies: string[]) =>
    onChange({
      users: draft.users.map((u) => (u.accessKey === accessKey ? { ...u, policies } : u)),
    });

  return (
    <div className="space-y-3">
      {shownCreds && (
        <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3">
          <p className="mb-2 flex items-center gap-2 text-[13px] font-medium text-amber-400">
            <span className="icon-[tabler--key] shrink-0" />
            Save these credentials now. The secret is not shown again.
          </p>
          <dl className="grid gap-1.5">
            <CredRow label="Access key" value={shownCreds.accessKey} />
            <CredRow label="Secret key" value={shownCreds.secretKey} />
          </dl>
          <button
            type="button"
            onClick={() => setCreds(null)}
            className="font-body hover:bg-surface mt-2 inline-flex h-8 shrink-0 items-center justify-center gap-2 rounded-lg px-2.5 text-sm font-medium whitespace-nowrap text-stone-400 transition-colors hover:text-stone-100 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            Dismiss
          </button>
        </div>
      )}

      {draft.users.length === 0 && !adding && (
        <p className="text-[13px] text-stone-400">No users attached to this bucket yet.</p>
      )}

      {draft.users.map((u) => (
        <div key={u.accessKey} className="bg-abyss rounded-lg border border-stone-800/70 p-3">
          <div className="flex items-center gap-3">
            <span className="icon-[tabler--user-circle] shrink-0 text-lg text-stone-400" />
            <div className="min-w-0 flex-1">
              <bdi className="block truncate font-mono text-[13px] text-stone-200">
                {safeDisplayName(u.accessKey)}
              </bdi>
              <p className="text-[13px] text-stone-400">
                {u.status === "enabled" ? "Active" : "Disabled"}
                {u.pendingSecretKey ? " | Created on save" : ""}
              </p>
            </div>
            <Toggle
              label={`Enable user ${u.accessKey}`}
              checked={u.status === "enabled"}
              onChange={() => toggleStatus(u)}
              disabled={busyKey === u.accessKey}
            />
            <button
              type="button"
              onClick={() => removeUser(u)}
              disabled={busyKey === u.accessKey}
              aria-label={`Remove user ${u.accessKey}`}
              className="hover:bg-surface inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-red-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
            >
              <span className="icon-[tabler--trash]" />
            </button>
          </div>
          {policyNames.length > 0 && (
            <div className="mt-3 border-t border-stone-800/70 pt-3">
              <p className="font-body mb-2 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
                Member of
              </p>
              <PolicyChips
                label={`Policies for ${u.accessKey}`}
                names={policyNames}
                picked={u.policies}
                onChange={(next) => setPolicies(u.accessKey, next)}
              />
            </div>
          )}
        </div>
      ))}

      {adding ? (
        <AddUserForm
          policies={policyNames}
          existing={[...draft.users, ...draft.attachableUsers].map((u) => u.accessKey)}
          onCancel={() => setAdding(false)}
          onCreated={(user) => {
            onChange({ users: [...draft.users, user] });
            setCreds({ accessKey: user.accessKey, secretKey: user.pendingSecretKey ?? "" });
            setAdding(false);
          }}
          onError={onError}
        />
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            <span className="icon-[tabler--user-plus]" />
            Create user
          </button>
          {draft.attachableUsers.length > 0 && (
            <button
              type="button"
              onClick={() => setAttaching(true)}
              className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              <span className="icon-[tabler--link]" />
              Attach existing user
            </button>
          )}
        </div>
      )}

      {attaching && (
        <AttachList
          title="Attach existing user"
          items={draft.attachableUsers.map((u) => ({
            id: u.accessKey,
            label: u.accessKey,
            meta:
              u.policies.length === 0
                ? "No direct policy"
                : `${u.policies.length} ${u.policies.length === 1 ? "policy" : "policies"}`,
          }))}
          onCancel={() => setAttaching(false)}
          onConfirm={(ids) => {
            onChange({
              users: [
                ...draft.users,
                ...draft.attachableUsers.filter((u) => ids.includes(u.accessKey)),
              ],
              attachableUsers: draft.attachableUsers.filter((u) => !ids.includes(u.accessKey)),
              detachedUsers: draft.detachedUsers.filter((k) => !ids.includes(k)),
            });
            setAttaching(false);
          }}
        />
      )}
    </div>
  );
}

function CredRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-void flex items-center gap-2 rounded-md py-1 pr-1 pl-3">
      <dt className="w-20 shrink-0 text-[13px] text-stone-400">{label}</dt>
      <dd className="flex min-w-0 flex-1 items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-[13px] text-stone-200">
          {value}
        </span>
        <CopyButton value={value} label={`Copy ${label.toLowerCase()}`} />
      </dd>
    </div>
  );
}

function PolicyChips({
  label,
  names,
  picked,
  onChange,
}: {
  label: string;
  names: string[];
  picked: string[];
  onChange: (next: string[]) => void;
}) {
  return (
    <fieldset aria-label={label} className="flex min-w-0 flex-wrap gap-1.5">
      {names.map((pn) => {
        const on = picked.includes(pn);
        return (
          <button
            key={pn}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(on ? picked.filter((x) => x !== pn) : [...picked, pn])}
            className={`rounded-md border px-2.5 py-1 font-mono text-[13px] transition-colors ${
              on
                ? "border-amber-500/40 bg-amber-500/10 text-amber-400"
                : "border-stone-800/70 text-stone-400 hover:border-stone-700 hover:text-stone-200"
            }`}
          >
            <bdi>{safeDisplayName(pn)}</bdi>
          </button>
        );
      })}
    </fieldset>
  );
}

function AddUserForm({
  policies,
  existing,
  onCancel,
  onCreated,
  onError,
}: {
  policies: string[];
  existing: string[];
  onCancel: () => void;
  onCreated: (user: IAMUser) => void;
  onError: (err: string) => void;
}) {
  const [mode, setMode] = useState<"generate" | "custom">("generate");
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [picked, setPicked] = useState<string[]>([]);

  const submit = () => {
    if (picked.length === 0) {
      onError("Select at least one policy for the user.");
      return;
    }
    const ak = mode === "generate" ? genAccessKey() : accessKey.trim();
    const sk = mode === "generate" ? genSecretKey() : secretKey.trim();
    if (!ak || !sk) {
      onError("Access key and secret key are required.");
      return;
    }
    if (existing.includes(ak)) {
      onError("A user with that access key already exists.");
      return;
    }
    onCreated({ accessKey: ak, status: "enabled", policies: picked, pendingSecretKey: sk });
  };

  return (
    <div className="bg-abyss space-y-3 rounded-lg border border-stone-800/70 p-3">
      <Segmented
        label="Credentials"
        options={[
          { id: "generate", label: "Generate" },
          { id: "custom", label: "Custom" },
        ]}
        value={mode}
        onChange={setMode}
      />
      {mode === "custom" && (
        <div className="space-y-2">
          <input
            type="text"
            value={accessKey}
            onChange={(e) => setAccessKey(e.target.value)}
            placeholder="Access key (at least 3 characters)"
            aria-label="Access key"
            className="bg-surface h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
          />
          <input
            type="text"
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder="Secret key (at least 8 characters)"
            aria-label="Secret key"
            className="bg-surface h-9 w-full rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
          />
        </div>
      )}
      <div>
        <p className="font-body mb-2 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
          Policies (at least one)
        </p>
        {policies.length === 0 ? (
          <p className="text-[13px] text-stone-400">
            No policies yet. Create one in Policies first.
          </p>
        ) : (
          <PolicyChips
            label="Policies for the new user"
            names={policies}
            picked={picked}
            onChange={setPicked}
          />
        )}
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={submit}
          className="font-body inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-3.5 text-sm font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          Create user
        </button>
      </div>
      <p className="text-[13px] text-stone-400">The user is created when you save this tab.</p>
    </div>
  );
}
