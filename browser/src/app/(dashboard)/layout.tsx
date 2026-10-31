import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { AttentionProvider } from "@/components/AttentionProvider";
import { BucketProvider } from "@/components/BucketProvider";
import { Shell } from "@/components/Shell";
import { getAccessBadgeAction } from "@/lib/actions";
import { health } from "@/lib/chart";
import { nodes, storage } from "@/lib/fleet";
import { versionLabel } from "@/lib/format";
import {
  attention,
  type BucketEntry,
  listBuckets,
  publicHost,
  rethrowSessionError,
  serverInfo,
  sessionAccessKey,
} from "@/lib/rpc";

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  if (!(await cookies()).get("obstor_token")) redirect("/login");

  let buckets: BucketEntry[] = [];
  try {
    buckets = await listBuckets();
  } catch (err) {
    rethrowSessionError(err);
  }

  const [info, access, accessKey, items] = await Promise.all([
    serverInfo(),
    getAccessBadgeAction(),
    sessionAccessKey(),
    attention(),
  ]);
  const canSeeFleet = info?.canSeeFleet ?? false;
  const [nodeRes, storageRes] = await Promise.all([
    canSeeFleet ? nodes() : null,
    info?.usage ? storage() : null,
  ]);
  const h = nodeRes?.ok ? health(nodeRes.value) : null;
  const used = storageRes?.ok && storageRes.value.usageAsOf ? storageRes.value.used : null;

  return (
    <BucketProvider buckets={buckets.map((b) => b.name)}>
      <AttentionProvider accessKey={accessKey} initial={items}>
        <Shell
          buckets={buckets}
          used={used}
          nav={{
            canManage: access.canManage,
            noPolicy: access.noPolicy,
            canSeeFleet,
            attention: !!h && (h.nodesOffline > 0 || h.drivesOffline > 0),
          }}
          host={publicHost()}
          server={{
            version: versionLabel(info?.releaseTag ?? "", process.env.NODE_ENV === "production"),
            region: info?.region ?? "",
            mode: info?.mode ?? "",
          }}
          user={{ accessKey, account: info?.account ?? "" }}
        >
          {children}
        </Shell>
      </AttentionProvider>
    </BucketProvider>
  );
}
