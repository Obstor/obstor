"use client";

import { useReducer } from "react";
import { type BucketConfigLoad, saveBucketConfigAction } from "@/lib/actions";
import {
  type BucketConfig,
  type BucketFeatures,
  type Control,
  cleanTags,
  controlReason,
  type Readable,
  sectionDirty,
} from "@/lib/bucket-config";
import { Card } from "../Card";
import { useDirty } from "../LeaveGuard";
import { EncryptionPanel } from "./EncryptionPanel";
import { FeaturesPanel } from "./FeaturesPanel";
import { LockingPanel } from "./LockingPanel";
import { QuotaPanel } from "./QuotaPanel";
import { initSection, type SectionAction, type SectionState, sectionReducer } from "./reducer";
import { SaveBar } from "./shared";
import { TagsPanel } from "./TagsPanel";
import { VersioningPanel } from "./VersioningPanel";

type Context = { features: BucketFeatures | null; readable: Readable };
type State = SectionState<BucketConfig, Context>;

const reducer: (state: State, action: SectionAction<BucketConfig, Context>) => State =
  sectionReducer;

const contextOf = (load: BucketConfigLoad): Context => ({
  features: load.features,
  readable: load.readable,
});

// Settings tab body
export function BucketSettings({
  bucket,
  load,
}: {
  bucket: string;
  load: BucketConfigLoad | { error: string };
}) {
  if ("error" in load) {
    return (
      <div
        role="alert"
        className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3 text-sm text-red-400"
      >
        <span className="icon-[tabler--alert-circle] mt-0.5 shrink-0 text-base" />
        <span className="break-words">{load.error}</span>
      </div>
    );
  }
  return <SettingsForm key={bucket} bucket={bucket} load={load} />;
}

function SettingsForm({ bucket, load }: { bucket: string; load: BucketConfigLoad }) {
  const [state, dispatch] = useReducer(reducer, load, (l) => initSection(l.config, contextOf(l)));
  const { loaded, draft, context, saving, error } = state;
  const dirty = sectionDirty(loaded, draft, "settings");
  useDirty(dirty);

  const reason = (c: Control) =>
    controlReason(c, {
      features: context.features,
      readable: context.readable,
      config: draft,
      isEdit: true,
    });
  const edit = (patch: Partial<BucketConfig>) => dispatch({ type: "edit", patch });

  const save = async () => {
    const sent = draft;
    dispatch({ type: "saving" });
    let res: Awaited<ReturnType<typeof saveBucketConfigAction>>;
    try {
      res = await saveBucketConfigAction(bucket, loaded, sent);
    } catch (err) {
      dispatch({
        type: "failed",
        error: err instanceof Error ? err.message : "Save failed. Check the connection and retry.",
      });
      return;
    }
    if (res.load && res.error) {
      // Some writes may have landed, rebase on the fresh read so only
      dispatch({
        type: "rebase",
        value: res.load.config,
        context: contextOf(res.load),
        error: res.error,
      });
    } else if (res.load) {
      dispatch({ type: "load", value: res.load.config, context: contextOf(res.load) });
    } else if (res.error) {
      dispatch({ type: "failed", error: res.error });
    } else {
      // Saved but re-read failed
      dispatch({ type: "load", value: { ...sent, tags: cleanTags(sent.tags) }, context });
    }
  };

  return (
    <div>
      <div inert={saving} aria-busy={saving} className="space-y-4">
        <Card title="Versioning" icon="icon-[tabler--versions]">
          <VersioningPanel config={draft} onChange={edit} reason={reason("versioning")} />
        </Card>
        <Card title="Object locking" icon="icon-[tabler--lock]">
          <LockingPanel config={draft} onChange={edit} reason={reason("objectLock")} />
        </Card>
        <Card title="Quota" icon="icon-[tabler--gauge]">
          <QuotaPanel config={draft} onChange={edit} reason={reason("quota")} />
        </Card>
        <Card title="Encryption" icon="icon-[tabler--shield-lock]">
          <EncryptionPanel
            config={draft}
            onChange={edit}
            reason={reason("encryption")}
            autoEncryption={context.features?.autoEncryption === true}
          />
        </Card>
        <Card title="Tags" icon="icon-[tabler--tag]">
          <TagsPanel config={draft} onChange={edit} reason={reason("tags")} />
        </Card>
        <Card title="Protocol access" icon="icon-[tabler--plug-connected]">
          <FeaturesPanel config={draft} onChange={edit} reason={reason("toggles")} />
        </Card>
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
