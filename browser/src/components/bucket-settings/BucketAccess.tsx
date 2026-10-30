"use client";

import { useReducer } from "react";
import { type AccessDraft, type BucketAccessLoad, saveBucketAccessAction } from "@/lib/actions";
import { controlReason, iamDirty, type Readable, sectionDirty } from "@/lib/bucket-config";
import { Card } from "../Card";
import { useDirty } from "../LeaveGuard";
import { PoliciesPanel } from "./PoliciesPanel";
import { PublicAccessPanel } from "./PublicAccessPanel";
import { initSection, sectionReducer } from "./reducer";
import { Note, SaveBar } from "./shared";
import { UsersPanel } from "./UsersPanel";

const IAM_UNREADABLE = "Needs the admin:ListUserPolicies and admin:ListUsers permissions to view.";
const RELOAD_FAILED = "Saved, but the settings could not be reloaded. Reload the page.";

type AccessContext = { readable: Readable; iam: boolean };

const counted = (n: number, one: string, many: string) => (
  <span className="text-[13px] text-stone-400">
    {n} {n === 1 ? one : many}
  </span>
);

export function BucketAccess({
  bucket,
  load,
}: {
  bucket: string;
  load: BucketAccessLoad | { error: string };
}) {
  if ("error" in load) {
    return (
      <p
        role="alert"
        className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400"
      >
        <span className="icon-[tabler--alert-circle] mt-0.5 shrink-0" />
        <span className="break-words">{load.error}</span>
      </p>
    );
  }
  // Bucket remounts form
  return <AccessForm key={bucket} load={load} />;
}

function AccessForm({ load }: { load: BucketAccessLoad }) {
  const [state, dispatch] = useReducer(
    sectionReducer<AccessDraft, AccessContext>,
    load,
    (l: BucketAccessLoad) => initSection(l.draft, { readable: l.readable, iam: l.iam }),
  );
  const { loaded, draft, context, saving, error } = state;
  const dirty = sectionDirty(loaded, draft, "access") || iamDirty(loaded, draft);
  useDirty(dirty);

  const policyReason = controlReason("policy", {
    features: null,
    readable: context.readable,
    config: draft,
    isEdit: true,
  });
  const edit = (patch: Partial<AccessDraft>) => dispatch({ type: "edit", patch });

  const save = async () => {
    dispatch({ type: "saving" });
    try {
      const res = await saveBucketAccessAction(draft, loaded, context.iam);
      if (res.load) {
        // Partial IAM save cannot edit, so it reloads
        dispatch({
          type: res.error && res.keepDraft ? "rebase" : "load",
          value: res.load.draft,
          context: { readable: res.load.readable, iam: res.load.iam },
          error: res.error,
        });
      } else {
        dispatch({ type: "failed", error: res.error || RELOAD_FAILED });
      }
    } catch (err) {
      dispatch({
        type: "failed",
        error: err instanceof Error ? err.message : "Failed to save access settings",
      });
    }
  };

  return (
    <div>
      <div inert={saving} aria-busy={saving} className="space-y-4">
        <Card title="Public access" icon="icon-[tabler--world]">
          <PublicAccessPanel config={draft} onChange={edit} reason={policyReason} />
        </Card>
        {context.iam ? (
          <>
            <Card
              title="Policies"
              icon="icon-[tabler--file-text-shield]"
              action={counted(draft.policies.length, "policy", "policies")}
            >
              <PoliciesPanel draft={draft} onChange={edit} />
            </Card>
            <Card
              title="Users"
              icon="icon-[tabler--users]"
              action={counted(draft.users.length, "user", "users")}
            >
              <UsersPanel
                draft={draft}
                onChange={edit}
                onCommit={(apply) => dispatch({ type: "commit", apply })}
                onError={(err) => dispatch({ type: "failed", error: err })}
              />
            </Card>
          </>
        ) : (
          <Note text={IAM_UNREADABLE} />
        )}
      </div>
      <SaveBar
        dirty={dirty}
        saving={saving}
        error={error}
        onDiscard={() => dispatch({ type: "discard" })}
        onSave={save}
      />
    </div>
  );
}
