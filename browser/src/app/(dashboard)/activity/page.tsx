import { ActivityView } from "@/components/ActivityView";
import { PageHeader } from "@/components/PageHeader";
import { listActivityAction } from "@/lib/actions";
import { parseActivityParams } from "@/lib/activity";
import { listBuckets } from "@/lib/rpc";

export default async function ActivityPage({
  searchParams,
}: {
  searchParams: Promise<{
    bucket?: string | string[];
    accessKey?: string | string[];
    action?: string | string[];
  }>;
}) {
  const filters = parseActivityParams(await searchParams);
  const [initial, buckets] = await Promise.all([
    listActivityAction({
      bucket: filters.bucket,
      accessKey: filters.accessKey,
      group: filters.group,
    }),
    listBuckets().then(
      (list) => list.map((b) => b.name),
      (): string[] => [],
    ),
  ]);

  return (
    <div className="space-y-4">
      <PageHeader
        title="Activity"
        subtitle="Writes and deletes made through S3, SFTP, the console and the admin API."
      />
      <ActivityView
        key={JSON.stringify(filters)}
        initial={initial}
        filters={filters}
        buckets={buckets}
        scope="all"
      />
    </div>
  );
}
