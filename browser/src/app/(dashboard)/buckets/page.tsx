import { BucketTable } from "@/components/BucketTable";
import { CreateBucketButton } from "@/components/CreateBucketButton";
import { PageHeader } from "@/components/PageHeader";
import type { Result } from "@/lib/fleet";
import { formatDay } from "@/lib/format";
import {
  type BucketList,
  bucketAccess,
  bucketList,
  rethrowSessionError,
  rpcEndpoint,
  serverInfo,
} from "@/lib/rpc";

export default async function BucketsPage() {
  const bucketRes: Result<BucketList> = await bucketList().then(
    (value) => ({ ok: true, value }),
    (err) => {
      rethrowSessionError(err);
      return { ok: false, error: err instanceof Error ? err.message : "Request failed" };
    },
  );
  const buckets = bucketRes.ok ? bucketRes.value.buckets : [];
  const usageAsOf = bucketRes.ok ? bucketRes.value.usageAsOf : "";
  const usage = (await serverInfo())?.usage ?? false;
  const access = await bucketAccess(buckets.map((b) => b.name));
  const rows = buckets.map((b) => ({
    name: b.name,
    created: formatDay(b.creationDate),
    createdAt: b.creationDate,
    access: access[b.name] ?? null,
    size: b.size,
    objects: b.objects,
  }));

  return (
    <div className="space-y-4">
      <PageHeader
        title="Buckets"
        subtitle="Every bucket this key can see."
        actions={
          <CreateBucketButton className="font-body inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11" />
        }
      />
      {!bucketRes.ok && (
        <div className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3">
          <p className="flex items-center gap-2 text-sm text-red-400">
            <span className="icon-[tabler--alert-circle] text-base" />
            {bucketRes.error}
          </p>
          <p className="mt-1 pl-6 font-mono text-xs text-stone-400">
            Console is reading {rpcEndpoint()}.
          </p>
        </div>
      )}
      <section className="bg-abyss overflow-hidden rounded-xl">
        {!bucketRes.ok ? (
          <p className="px-6 py-16 text-center text-sm text-stone-400">Bucket list unavailable.</p>
        ) : rows.length > 0 ? (
          <BucketTable rows={rows} paged usage={usage} usageAsOf={usageAsOf} />
        ) : (
          <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
            <span className="icon-[tabler--bucket] text-4xl text-stone-500" />
            <p className="text-sm text-stone-300">No buckets yet.</p>
            <CreateBucketButton />
          </div>
        )}
      </section>
    </div>
  );
}
