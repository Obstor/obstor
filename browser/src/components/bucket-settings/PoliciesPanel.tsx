"use client";

import { useEffect, useState } from "react";
import type { AccessDraft, NamedPolicy, PolicyRow } from "@/lib/actions";
import { fullBucketPolicy } from "@/lib/policy-templates";
import { safeDisplayName } from "@/lib/safe-name";
import { AttachList } from "./shared";

const resources = (bucket: string) => [`arn:aws:s3:::${bucket}`, `arn:aws:s3:::${bucket}/*`];

// Default policy from bucket template
const DEFAULT_POLICY_TEMPLATE = (bucket: string): PolicyRow => ({
  origin: "new",
  name: `${bucket}-full`,
  policy: fullBucketPolicy(bucket),
});

const READONLY_POLICY_TEMPLATE = (bucket: string): PolicyRow => ({
  origin: "new",
  name: `${bucket}-readonly`,
  policy: JSON.stringify(
    {
      Version: "2012-10-17",
      Statement: [
        { Effect: "Allow", Action: ["s3:GetObject", "s3:ListBucket"], Resource: resources(bucket) },
      ],
    },
    null,
    2,
  ),
});

// Stable row keys while policy names change
let keySeq = 0;
const newKey = () => `policy-${keySeq++}`;

export function PoliciesPanel({
  draft,
  onChange,
}: {
  draft: AccessDraft;
  onChange: (patch: Partial<AccessDraft>) => void;
}) {
  const [attaching, setAttaching] = useState(false);
  const [keys, setKeys] = useState(() => draft.policies.map(newKey));
  const count = draft.policies.length;
  useEffect(() => {
    setKeys((prev) => {
      if (prev.length === count) return prev;
      const next = prev.slice(0, count);
      while (next.length < count) next.push(newKey());
      return next;
    });
  }, [count]);

  const addPolicy = () => {
    const base =
      count === 0 ? DEFAULT_POLICY_TEMPLATE(draft.name) : READONLY_POLICY_TEMPLATE(draft.name);
    const taken = new Set([
      ...draft.policies.map((p) => p.name),
      ...draft.attachablePolicies.map((p) => p.name),
      ...draft.catchAllPolicies,
    ]);
    let name = base.name;
    for (let i = 2; taken.has(name); i++) name = `${base.name}-${i}`;
    onChange({ policies: [...draft.policies, { ...base, name }] });
  };

  const updatePolicy = (idx: number, patch: Partial<NamedPolicy>) => {
    const old = draft.policies[idx].name;
    const renamed = patch.name !== undefined && patch.name !== old;
    onChange({
      policies: draft.policies.map((p, i) => (i === idx ? { ...p, ...patch } : p)),
      // Users follow policy rename
      ...(renamed && {
        users: draft.users.map((u) => ({
          ...u,
          policies: u.policies.map((pn) => (pn === old ? (patch.name as string) : pn)),
        })),
      }),
    });
  };

  const removePolicy = (idx: number) => {
    const pol = draft.policies[idx];
    setKeys((k) => k.filter((_, i) => i !== idx));
    onChange({
      policies: draft.policies.filter((_, i) => i !== idx),
      // Staged policy needs no detach on save
      removedPolicies:
        pol.origin === "new" ? draft.removedPolicies : [...draft.removedPolicies, pol.name],
      users: draft.users.map((u) => ({
        ...u,
        policies: u.policies.filter((pn) => pn !== pol.name),
      })),
    });
  };

  return (
    <div className="space-y-3">
      {count === 0 && (
        <p className="text-[13px] text-stone-400">No policies scoped to this bucket yet.</p>
      )}
      {draft.policies.map((p, idx) => {
        const key = keys[idx] ?? `policy-row-${idx}`;
        return (
          <PolicyEditor
            key={key}
            policy={p}
            onChange={(patch) => updatePolicy(idx, patch)}
            onRemove={() => removePolicy(idx)}
          />
        );
      })}
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={addPolicy}
          className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
        >
          <span className="icon-[tabler--plus]" />
          Create policy
        </button>
        {draft.attachablePolicies.length > 0 && (
          <button
            type="button"
            onClick={() => setAttaching(true)}
            className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
          >
            <span className="icon-[tabler--link]" />
            Attach existing policy
          </button>
        )}
      </div>
      {draft.catchAllPolicies.length > 0 && (
        <p className="text-[13px] text-stone-400">
          Also applies here:{" "}
          <bdi className="font-mono">{safeDisplayName(draft.catchAllPolicies.join(", "))}</bdi>.
          Manage on the Access page.
        </p>
      )}
      {attaching && (
        <AttachList
          title="Attach existing policy"
          items={draft.attachablePolicies.map((p) => ({
            id: p.name,
            label: p.name,
            meta: "Grants other buckets",
          }))}
          onCancel={() => setAttaching(false)}
          onConfirm={(ids) => {
            onChange({
              policies: [
                ...draft.policies,
                ...draft.attachablePolicies.filter((p) => ids.includes(p.name)),
              ],
              attachablePolicies: draft.attachablePolicies.filter((p) => !ids.includes(p.name)),
              removedPolicies: draft.removedPolicies.filter((n) => !ids.includes(n)),
            });
            setAttaching(false);
          }}
        />
      )}
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
  const [badJSON, setBadJSON] = useState(false);
  const shared = policy.origin === "shared";
  const format = () => {
    try {
      onChange({ policy: JSON.stringify(JSON.parse(policy.policy), null, 2) });
      setBadJSON(false);
    } catch {
      setBadJSON(true);
    }
  };

  return (
    <div className="bg-abyss rounded-lg border border-stone-800/70 p-3">
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
          aria-label="Policy name"
          className="bg-surface h-9 w-full min-w-0 flex-1 rounded-lg border border-stone-800 px-3 font-mono text-base text-stone-100 transition-colors outline-none placeholder:text-stone-500 focus:border-amber-500 disabled:opacity-50 sm:text-sm"
        />
        {shared ? (
          <span className="bg-surface inline-flex h-6 items-center gap-1 rounded-md border border-stone-800 px-2 font-mono text-[11px] text-stone-400">
            Shared
          </span>
        ) : (
          <>
            <button
              type="button"
              onClick={format}
              className="bg-surface font-body hover:bg-surface-overlay inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
            >
              Format
            </button>
            <button
              type="button"
              onClick={onRemove}
              aria-label={policy.name ? `Remove policy ${policy.name}` : "Remove policy"}
              className="hover:bg-surface inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg text-stone-400 transition-colors hover:text-red-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
            >
              <span className="icon-[tabler--trash]" />
            </button>
          </>
        )}
      </div>
      {shared ? (
        <p className="text-[13px] text-stone-400">
          Grants other buckets too. Read only here, edit it on the Access page.
        </p>
      ) : (
        <>
          <textarea
            value={policy.policy}
            onChange={(e) => {
              setBadJSON(false);
              onChange({ policy: e.target.value });
            }}
            rows={10}
            spellCheck={false}
            aria-label={policy.name ? `Policy document for ${policy.name}` : "Policy document"}
            aria-invalid={badJSON}
            className="bg-void w-full resize-y rounded-md border border-stone-800/70 p-3 font-mono text-base leading-relaxed text-stone-300 transition-colors outline-none focus:border-amber-500 sm:text-[13px]"
          />
          {badJSON && (
            <p role="alert" className="mt-1.5 flex items-center gap-1.5 text-[13px] text-red-400">
              <span className="icon-[tabler--alert-circle] shrink-0" />
              Not valid JSON.
            </p>
          )}
        </>
      )}
    </div>
  );
}
