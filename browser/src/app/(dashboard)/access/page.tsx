import { AccessManager } from "@/components/AccessManager";
import { getAccessSnapshotAction } from "@/lib/actions";

export default async function AccessPage() {
  const snapshot = await getAccessSnapshotAction();

  return (
    <div className="space-y-4">
      <AccessManager snapshot={snapshot} />
    </div>
  );
}
