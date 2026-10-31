import Link from "next/link";
import { BucketTable } from "@/components/BucketTable";
import { Card, cardSparkline, hoverLines, StatCard } from "@/components/Card";
import { CreateBucketButton } from "@/components/CreateBucketButton";
import { AreaChart, Gauge, Legend, RegionRow } from "@/components/charts";
import { PageHeader } from "@/components/PageHeader";
import { RangeMenu } from "@/components/RangeMenu";
import { RangeTabs } from "@/components/RangeTabs";
import {
  type Capacity,
  capacityBreakdown,
  getTimeRange,
  type Health,
  health,
  hourlyAverages,
  rangeDescription,
  usageNote,
} from "@/lib/chart";
import {
  capacityHistory,
  FLEET_DISABLED,
  fleetMetrics,
  fleetRegions,
  nodes,
  type RegionStat,
  settle,
  storage,
  TRAFFIC_OFF,
} from "@/lib/fleet";
import { formatClock, formatDay, humanCount, humanSize, isStale, plural } from "@/lib/format";
import {
  bucketAccess,
  listBuckets,
  publicHost,
  publicURL,
  rpcEndpoint,
  serverInfo,
} from "@/lib/rpc";
import { safeDisplayName } from "@/lib/safe-name";
import { AttentionBanner } from "./AttentionBanner";

const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0);

// Usage scan time shown on hover
const STALE_MS = 600_000;

export default async function Overview({
  searchParams,
}: {
  searchParams: Promise<{ range?: string | string[] }>;
}) {
  const range = getTimeRange((await searchParams).range);
  const info = await serverInfo();
  // Backend mode has no scanner or capacity history
  const historyCall = info?.usage ? capacityHistory(range.hours) : null;
  const fleet = info?.canSeeFleet ?? false;
  const metrics = info?.metrics ?? false;
  const bucketRes = await settle(listBuckets());
  const buckets = bucketRes.ok ? bucketRes.value : [];
  const newest = [...buckets].sort(
    (a, b) => Date.parse(b.creationDate) - Date.parse(a.creationDate),
  );
  const top = newest.slice(0, 5);

  const [storageRes, nodeRes, regionRes, metricRes, access] = await Promise.all([
    storage(),
    fleet ? nodes() : null,
    fleet && metrics ? fleetRegions() : null,
    fleet && metrics ? fleetMetrics(range.hours) : null,
    bucketAccess(top.map((b) => b.name)),
  ]);
  const errors = [bucketRes, storageRes, nodeRes, regionRes, metricRes].flatMap((r) =>
    r && !r.ok && r.code !== FLEET_DISABLED ? [r.error] : [],
  );
  // Store stopped since ServerInfo, treat as unavailable
  const traffic = metrics && !(metricRes && !metricRes.ok && metricRes.code === FLEET_DISABLED);
  const s = storageRes.ok ? storageRes.value : null;
  const historyRes = await historyCall;
  const hist = historyRes?.ok ? historyRes.value : null;
  const liveUsage = s?.usageAsOf ? s : null;
  const cap = s ? capacityBreakdown(s) : null;
  const h = nodeRes?.ok ? health(nodeRes.value) : null;
  const regions: RegionStat[] =
    regionRes?.ok && regionRes.value.length > 0
      ? [...regionRes.value].sort((a, b) => b.used - a.used)
      : s
        ? [{ region: s.region, used: s.used, total: s.total, free: s.free }]
        : [];
  const m = metricRes?.ok ? metricRes.value : null;
  const hours = m?.windowHours || range.hours;
  const bw = m?.bandwidth ?? [];
  const note = usageNote(info?.usage, s?.usageAsOf);
  const scanned = s && !note ? s : null;
  // Server renders hover text in UTC
  const asOf =
    scanned && isStale(scanned.usageAsOf, Date.now(), STALE_MS)
      ? ` | as of ${formatClock(scanned.usageAsOf, "UTC")} UTC`
      : "";
  const rows = top.map((b) => ({
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
        title="Storage overview"
        subtitle={
          <>
            Usage, traffic and cluster health for <span className="font-mono">{publicHost()}</span>.
          </>
        }
        actions={
          <>
            {((fleet && traffic) || info?.usage) && (
              <RangeMenu
                selected={range}
                path="/"
                className="bg-surface font-body hover:bg-surface-overlay inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
              />
            )}
            <CreateBucketButton className="font-body inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-amber-500 px-4 text-[15px] leading-[calc(20/14)] font-semibold whitespace-nowrap text-black transition-colors hover:bg-amber-400 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11" />
          </>
        }
      />

      <AttentionBanner />
      {errors.length > 0 && (
        <div className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3">
          <p className="flex items-center gap-2 text-sm text-red-400">
            <span className="icon-[tabler--alert-circle] text-base" />
            {errors[0]}
          </p>
          <p className="mt-1 pl-6 font-mono text-xs text-stone-400">
            Console is reading {rpcEndpoint()}.
          </p>
        </div>
      )}

      <div
        className={`grid grid-cols-1 gap-4 sm:grid-cols-2 ${fleet ? "xl:grid-cols-4" : "xl:grid-cols-3"}`}
      >
        <StatCard
          icon="icon-[tabler--database]"
          label="Storage used"
          value={scanned ? humanSize(scanned.used) : "N/A"}
          title={hoverLines(
            "Bytes stored by objects, before parity",
            note ?? (scanned && `across ${plural(regions.length, "region")}${asOf}`),
          )}
          aside={cardSparkline(
            hist,
            "Storage used history",
            hist?.used ?? null,
            liveUsage ? liveUsage.used : null,
            "bytes",
          )}
        />
        <StatCard
          icon="icon-[tabler--box]"
          label="Total objects"
          aside={cardSparkline(
            hist,
            "Total objects history",
            hist?.objects ?? null,
            liveUsage ? liveUsage.objectsCount : null,
            "count",
          )}
          value={scanned ? humanCount(scanned.objectsCount) : "N/A"}
          title={hoverLines(
            "Objects counted by the last usage scan",
            note ??
              (scanned &&
                scanned.objectsCount > 0 &&
                `avg ${humanSize(scanned.used / scanned.objectsCount)}${asOf}`),
          )}
        />
        <StatCard
          icon="icon-[tabler--bucket]"
          label="Buckets"
          aside={cardSparkline(
            hist,
            "Buckets history",
            hist?.buckets ?? null,
            bucketRes.ok ? buckets.length : null,
            "count",
          )}
          value={bucketRes.ok ? humanCount(buckets.length) : "N/A"}
          title={hoverLines(
            "Buckets this access key can list",
            newest[0] && `newest ${safeDisplayName(newest[0].name)}`,
          )}
        />
        {fleet && (
          <StatCard
            icon="icon-[tabler--chart-pie]"
            label="Available space"
            aside={cardSparkline(
              hist,
              "Available space history",
              hist?.free ?? null,
              s && s.total > 0 ? s.free : null,
              "bytes",
            )}
            value={cap ? humanSize(cap.free) : "N/A"}
            title={hoverLines(
              "Raw free space on every drive, before parity",
              cap && `of ${humanSize(cap.total)} raw`,
            )}
          />
        )}
      </div>

      {(fleet || cap) && (
        <div className="grid gap-4 xl:grid-cols-3">
          {fleet && (
            <Card
              title="Bandwidth"
              icon="icon-[tabler--activity]"
              className={cap ? "xl:col-span-2" : "xl:col-span-3"}
              bodyClassName="flex flex-col"
              action={traffic && <RangeTabs value={range.value} label="Bandwidth time range" />}
            >
              {traffic ? (
                <>
                  <Legend
                    items={[
                      {
                        color: "bg-cyan-500",
                        label: "Uploads",
                        value: m ? humanSize(sum(bw.map((b) => b.in))) : "N/A",
                      },
                      {
                        color: "bg-amber-500",
                        label: "Downloads",
                        value: m ? humanSize(sum(bw.map((b) => b.out))) : "N/A",
                      },
                    ]}
                  />
                  <AreaChart
                    kind="bytes"
                    hours={hours}
                    since={m?.since}
                    label={`Uploads and downloads over the last ${rangeDescription(hours)}`}
                    empty={
                      m
                        ? `No transfers in the last ${rangeDescription(hours)}.`
                        : "Bandwidth unavailable."
                    }
                    series={[
                      {
                        name: "Uploads",
                        color: "var(--color-cyan-500)",
                        data: hourlyAverages(
                          bw.map((b) => b.in),
                          hours,
                        ),
                      },
                      {
                        name: "Downloads",
                        color: "var(--color-amber-500)",
                        data: hourlyAverages(
                          bw.map((b) => b.out),
                          hours,
                        ),
                      },
                    ]}
                  />
                </>
              ) : (
                <p className="py-10 text-center text-sm text-stone-400">{TRAFFIC_OFF}</p>
              )}
            </Card>
          )}
          {cap && <CapacityCard cap={cap} />}
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-3">
        <Card
          title="Buckets"
          icon="icon-[tabler--bucket]"
          flush
          className={fleet ? "xl:col-span-2" : "xl:col-span-3"}
          action={
            buckets.length > 0 ? (
              <Link
                href="/buckets"
                className="bg-surface font-body hover:bg-surface-overlay inline-flex h-8 shrink-0 items-center justify-center gap-2 rounded-lg border border-stone-800 px-3.5 text-sm font-medium whitespace-nowrap text-stone-200 transition-colors hover:border-stone-700 disabled:pointer-events-none disabled:opacity-40 pointer-coarse:min-h-11"
              >
                View all
                <span className="icon-[tabler--arrow-right]" />
              </Link>
            ) : undefined
          }
        >
          {!bucketRes.ok ? (
            <p className="px-5 pb-5 text-sm text-stone-400">Bucket list unavailable.</p>
          ) : buckets.length > 0 ? (
            <BucketTable rows={rows} usage={info?.usage ?? false} />
          ) : (
            <FirstRun url={publicURL()} />
          )}
        </Card>
        {fleet && <ClusterCard h={h} regions={regions} />}
      </div>
    </div>
  );
}

function CapacityCard({ cap }: { cap: Capacity }) {
  return (
    <Card title="Storage capacity" icon="icon-[tabler--gauge]">
      <div className="flex justify-center">
        <Gauge capacity={cap} />
      </div>
      <dl className="mx-2 mt-3 grid grid-cols-[1fr_max-content_2fr_max-content_1fr] gap-y-3 border-t border-stone-800/70 pt-3">
        <div
          className="col-start-2 w-fit justify-self-start text-left"
          title="Raw space in use on every drive, including object data and other files"
        >
          <dt className="text-sm font-bold text-stone-400">Used</dt>
          <dd className="font-display mt-1 text-center text-2xl font-bold">
            {humanSize(cap.total - cap.free)}
          </dd>
        </div>
        <div
          className="col-start-4 w-fit justify-self-end text-left"
          title="Raw free space on every drive, before parity"
        >
          <dt className="text-sm font-bold text-stone-400">Free</dt>
          <dd className="font-display mt-1 text-center text-2xl font-bold">
            {humanSize(cap.free)}
          </dd>
        </div>
        <div
          className="col-span-5 grid grid-cols-subgrid items-center border-t border-stone-800/70 pt-3"
          title="Total raw space on every drive, before parity"
        >
          <dt className="col-start-2 text-xs font-bold text-stone-400">Raw Capacity</dt>
          <dd className="col-start-4 justify-self-end font-mono text-sm text-white">
            {humanSize(cap.total)}
          </dd>
        </div>
      </dl>
    </Card>
  );
}

function ClusterCard({ h, regions }: { h: Health | null; regions: RegionStat[] }) {
  return (
    <Card title="Cluster health" icon="icon-[tabler--server-2]">
      {h ? (
        <div className="divide-y divide-stone-800/70 overflow-hidden rounded-lg border border-stone-800/70">
          <HealthRow
            icon="icon-[tabler--server-2]"
            label="Nodes"
            online={h.nodesOnline}
            total={h.nodesTotal}
          />
          <HealthRow
            icon="icon-[tabler--database]"
            label="Drives"
            online={h.drivesOnline}
            total={h.drivesTotal}
          />
        </div>
      ) : (
        <p className="text-sm text-stone-400">Node status unavailable.</p>
      )}
      {regions.length > 0 && (
        <div className="mt-4 border-t border-stone-800/70 pt-4">
          <p className="font-body mb-3 text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
            {regions.length === 1 ? "Region" : "Regions"}
          </p>
          <div className="space-y-3">
            {regions.slice(0, 3).map((r) => (
              <RegionRow
                key={r.region}
                region={r.region}
                used={r.used}
                total={r.total}
                free={r.total > 0 ? r.free : undefined}
              />
            ))}
          </div>
        </div>
      )}
      <div className="mt-4 border-t border-stone-800/70 pt-4">
        <Link
          href="/cluster"
          className="inline-flex items-center gap-2 text-sm text-stone-400 hover:text-stone-200"
        >
          View cluster
          <span className="icon-[tabler--arrow-right]" />
        </Link>
      </div>
    </Card>
  );
}

function HealthRow({
  icon,
  label,
  online,
  total,
}: {
  icon: string;
  label: string;
  online: number;
  total: number;
}) {
  const offline = total - online;
  const ok = total > 0 && online === total;
  return (
    <Link
      href="/cluster"
      className="hover:bg-surface/60 grid grid-cols-[44px_1fr_auto_auto] items-center transition-colors"
    >
      <span className="flex h-11 items-center justify-center border-r border-stone-800/70">
        <span className={`${icon} text-lg text-stone-400`} />
      </span>
      <span className="px-4 text-sm text-stone-200">{label}</span>
      <span
        className={`inline-flex items-center gap-2 font-mono text-sm ${ok ? "text-green-400" : "text-amber-400"}`}
      >
        <span
          aria-hidden="true"
          className={`shrink-0 text-base ${ok ? "icon-[tabler--circle-check] text-green-400" : "icon-[tabler--circle-minus] text-amber-400"}`}
        />
        {offline > 0 ? `${offline} offline` : `${online} online`}
      </span>
      <span className="icon-[tabler--chevron-right] mx-3 text-stone-500" />
    </Link>
  );
}

function FirstRun({ url }: { url: string }) {
  return (
    <div className="space-y-5 px-5 pb-5">
      <div className="flex flex-wrap items-center gap-4">
        <p className="text-sm text-stone-300">
          No buckets yet. Objects live in buckets, so start with one.
        </p>
        <CreateBucketButton />
      </div>
      <div>
        <p className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
          S3 endpoint
        </p>
        <p className="mt-1.5 font-mono text-sm break-all select-all">{url}</p>
      </div>
      <div>
        <p className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
          Example request
        </p>
        <pre className="bg-abyss mt-1.5 overflow-x-auto rounded-lg border border-stone-800/70 px-3 py-2 font-mono text-[13px] text-stone-300 select-all">
          aws --endpoint-url {url} s3 ls
        </pre>
      </div>
      <p className="text-xs text-stone-400">
        Sign requests with an access key and secret. Create one in a bucket's settings, on the
        Access tab.
      </p>
    </div>
  );
}
