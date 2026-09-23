import { AccessManager } from "@/components/AccessManager";
import { getAccessSnapshotAction } from "@/lib/actions";

export default async function AccessPage() {
  const snapshot = await getAccessSnapshotAction();

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="font-display font-semibold text-lg">Access</h1>
        <p className="mt-1 font-body text-stone-500 text-xs">
          Users and policies across every bucket.
        </p>
      </div>
      <AccessManager snapshot={snapshot} />
    </div>
  );
}
