// Run via node --experimental-strip-types src/components/bucket-settings/reducer.test.ts
import assert from "node:assert/strict";
import { initSection, type SectionAction, type SectionState, sectionReducer } from "./reducer.ts";

type Draft = { versioning: boolean; quotaSize: string; status: "enabled" | "disabled" };
type Context = { readable: boolean };
type State = SectionState<Draft, Context>;

// Frozen inputs catch reducer mutations
const reduce = (s: State, a: SectionAction<Draft, Context>): State => {
  for (const part of [s.loaded, s.draft, s.context, s]) Object.freeze(part);
  return sectionReducer(s, a);
};

const loaded: Draft = { versioning: false, quotaSize: "", status: "enabled" };
const ctx: Context = { readable: true };
const start: State = initSection(loaded, ctx);
assert.deepEqual(start, { loaded, draft: loaded, context: ctx, saving: false, error: "" });

// Edit updates draft and clears error
const failed = reduce(start, { type: "failed", error: "Quota: Access Denied." });
assert.equal(failed.error, "Quota: Access Denied.");
const edited = reduce(failed, { type: "edit", patch: { versioning: true } });
assert.deepEqual(edited.draft, { ...loaded, versioning: true });
assert.deepEqual(edited.loaded, loaded);
assert.equal(edited.error, "");

// Discard restores loaded copy and clears error
const discarded = reduce({ ...edited, error: "Versioning: Access Denied." }, { type: "discard" });
assert.deepEqual(discarded.draft, loaded);
assert.equal(discarded.error, "");

// Saved status updates both copies, Discard keeps it
const disable = (d: Draft): Draft => ({ ...d, status: "disabled" });
const committed = reduce(edited, { type: "commit", apply: disable });
assert.deepEqual(committed.loaded, { ...loaded, status: "disabled" });
assert.deepEqual(committed.draft, { ...loaded, versioning: true, status: "disabled" });
assert.deepEqual(reduce(committed, { type: "discard" }).draft, {
  ...loaded,
  status: "disabled",
});

// Saving clears error, failure keeps draft
const saving = reduce({ ...edited, error: "old" }, { type: "saving" });
assert.equal(saving.saving, true);
assert.equal(saving.error, "");
const refused = reduce(saving, { type: "failed", error: "Tags: Access Denied." });
assert.equal(refused.saving, false);
assert.equal(refused.error, "Tags: Access Denied.");
assert.deepEqual(refused.draft, edited.draft);

// Partial save reloads server state, keeps unwritten edits dirty
const twoEdits = reduce(saving, { type: "edit", patch: { quotaSize: "5" } });
const fresh: Draft = { ...loaded, versioning: true };
const freshCtx: Context = { readable: false };
const rebased = reduce(
  { ...twoEdits, saving: true },
  { type: "rebase", value: fresh, context: freshCtx, error: "Quota: Access Denied." },
);
assert.deepEqual(rebased, {
  loaded: fresh,
  draft: { ...loaded, versioning: true, quotaSize: "5" },
  context: freshCtx,
  saving: false,
  error: "Quota: Access Denied.",
});

// Load replaces both copies and context, keeps supplied error
const reloaded = reduce(
  { ...twoEdits, saving: true },
  { type: "load", value: fresh, context: freshCtx, error: "Users: Access Denied." },
);
assert.deepEqual(reloaded, {
  loaded: fresh,
  draft: fresh,
  context: freshCtx,
  saving: false,
  error: "Users: Access Denied.",
});
assert.equal(reduce(failed, { type: "load", value: fresh, context: freshCtx }).error, "");

console.log("reducer tests passed");
