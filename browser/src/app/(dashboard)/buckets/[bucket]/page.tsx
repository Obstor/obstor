import type { ReactNode } from "react";
import { ActivityView } from "@/components/ActivityView";
import { BucketView } from "@/components/BucketView";
import { BucketAccess } from "@/components/bucket-settings/BucketAccess";
import { BucketSettings } from "@/components/bucket-settings/BucketSettings";
import { getBucketAccessAction, getBucketConfigAction, listActivityAction } from "@/lib/actions";
import { parseTab } from "@/lib/bucket-tabs";
import { formatDay } from "@/lib/format";
import type { Entry } from "@/lib/objects";
import { bucketAccess, listBuckets, publicURL, rpc, rpcSettled, serverInfo } from "@/lib/rpc";

interface RawObject {
  name: string;
  size: number;
  lastModified: string;
  etag?: string;
}

export default async function BucketPage({
  params,
  searchParams,
}: {
  params: Promise<{ bucket: string }>;
  searchParams: Promise<{ prefix?: string | string[]; tab?: string | string[] }>;
}) {
  const bucket = decodeURIComponent((await params).bucket);
  const sp = await searchParams;
  const prefix = typeof sp.prefix === "string" ? sp.prefix : "";
  const tab = parseTab(sp.tab);
  const info = await serverInfo();
  // Locations expose topology and disk paths
  const fleet = info?.canSeeFleet ?? false;

  // Start selected tab load alongside shared reads
  const settingsLoad = tab === "settings" ? getBucketConfigAction(bucket) : null;
  const accessLoad = tab === "access" ? getBucketAccessAction(bucket) : null;
  const activityLoad = tab === "activity" ? listActivityAction({ bucket }) : null;

  const [list, all, access] = await rpcSettled([
    tab === "objects"
      ? rpc<{ objects: RawObject[] | null }>("ListObjects", {
          bucketName: bucket,
          prefix,
          marker: "",
        })
      : null,
    listBuckets(),
    bucketAccess([bucket]),
  ]);

  const entries: Entry[] =
    list.status === "fulfilled" && list.value
      ? (list.value.objects ?? []).map((o) => ({
          name: o.name,
          size: o.size,
          modified: o.lastModified,
          etag: o.etag ?? "",
        }))
      : [];
  const buckets = all.status === "fulfilled" ? all.value : [];
  const created = buckets.find((b) => b.name === bucket)?.creationDate;

  // Bucket key remounts with that bucket data
  let body: ReactNode = null;
  if (settingsLoad)
    body = <BucketSettings key={bucket} bucket={bucket} load={await settingsLoad} />;
  if (accessLoad) body = <BucketAccess key={bucket} bucket={bucket} load={await accessLoad} />;
  if (activityLoad)
    body = (
      <ActivityView
        key={bucket}
        initial={await activityLoad}
        filters={{ bucket, accessKey: "", group: "" }}
        buckets={[]}
        scope="bucket"
      />
    );

  return (
    <BucketView
      bucket={bucket}
      prefix={prefix}
      tab={tab}
      entries={entries}
      error={
        list.status === "rejected"
          ? list.reason instanceof Error
            ? list.reason.message
            : "Failed to load objects"
          : ""
      }
      created={created ? formatDay(created) : ""}
      access={access.status === "fulfilled" ? (access.value[bucket] ?? null) : null}
      url={`${publicURL()}/${bucket}`}
      buckets={buckets.map((b) => b.name)}
      showLocations={fleet}
    >
      {body}
    </BucketView>
  );
}
