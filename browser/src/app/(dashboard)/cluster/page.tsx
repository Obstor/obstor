import { redirect } from "next/navigation";
import { Card, cardSparkline, hoverLines, StatCard } from "@/components/Card";
import {
  DivergingBars,
  Legend,
  RegionRow,
  shareOpacity,
  UsageBar,
  WorldHeatmap,
} from "@/components/charts";
import { PageHeader } from "@/components/PageHeader";
import { RangeTabs } from "@/components/RangeTabs";
import { SparkPlaceholder } from "@/components/Sparkline";
import {
  getTimeRange,
  health,
  hourlyAverages,
  rangeDescription,
  rawUsed,
  regionShares,
} from "@/lib/chart";
import {
  capacityHistory,
  FLEET_DISABLED,
  fleetMetrics,
  fleetRegions,
  nodes,
  type RegionStat,
  storage,
  TRAFFIC_OFF,
} from "@/lib/fleet";
import { humanCount, humanSize } from "@/lib/format";
import { REGION_COUNTRY } from "@/lib/regionmap";
import { rpcEndpoint, serverInfo } from "@/lib/rpc";

const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0);

export default async function ClusterPage({
  searchParams,
}: {
  searchParams: Promise<{ range?: string | string[] }>;
}) {
  const info = await serverInfo();
  if (!info?.canSeeFleet) redirect("/");
  const range = getTimeRange((await searchParams).range);
  const [nodeRes, storageRes, regionRes, metricRes, historyRes] = await Promise.all([
    nodes(),
    storage(),
    info.metrics ? fleetRegions() : null,
    info.metrics ? fleetMetrics(range.hours) : null,
    // Backend mode has no capacity history
    info.usage ? capacityHistory(range.hours) : null,
  ]);
  const errors = [nodeRes, storageRes, regionRes, metricRes].flatMap((r) =>
    r && !r.ok && r.code !== FLEET_DISABLED ? [r.error] : [],
  );
  const traffic =
    info.metrics && !(metricRes && !metricRes.ok && metricRes.code === FLEET_DISABLED);
  const rows = nodeRes.ok ? nodeRes.value : [];
  const h = health(rows);
  const s = storageRes.ok ? storageRes.value : null;
  const hist = historyRes?.ok ? historyRes.value : null;
  const regions: RegionStat[] =
    regionRes?.ok && regionRes.value.length > 0
      ? [...regionRes.value].sort((a, b) => b.used - a.used)
      : s
        ? [{ region: s.region, used: s.used, total: s.total, free: s.free }]
        : [];
  const regionsUsed = sum(regions.map((r) => r.used));
  const byCountry = new Map<string, number>();
  for (const r of regions) {
    const country = REGION_COUNTRY[r.region];
    if (country) byCountry.set(country, (byCountry.get(country) ?? 0) + r.used);
  }
  const m = metricRes?.ok ? metricRes.value : null;
  const hours = m?.windowHours || range.hours;
  const reads = (m?.ops ?? []).map((o) => o.read);
  const writes = (m?.ops ?? []).map((o) => o.write);
  // Multiple pool handling, FS is 0 and single pool is 1
  const showPool = rows.some((n) => n.pool > 1);
  return (
    <div className="space-y-4">
      <PageHeader title="Cluster" subtitle="Node health, drive capacity, and regional storage." />

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

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          icon="icon-[tabler--server-2]"
          label="Nodes online"
          value={nodeRes.ok ? `${h.nodesOnline} / ${h.nodesTotal}` : "N/A"}
          title={hoverLines(
            "Nodes answering, out of every node in the deployment",
            nodeRes.ok && offlineLine(h.nodesOffline),
          )}
          aside={cardSparkline(
            hist,
            "Nodes online history",
            hist?.nodesOnline,
            nodeRes.ok ? h.nodesOnline : null,
            "count",
          )}
        />
        <StatCard
          icon="icon-[tabler--database]"
          label="Drives online"
          value={nodeRes.ok ? `${h.drivesOnline} / ${h.drivesTotal}` : "N/A"}
          title={hoverLines(
            "Drives reporting OK, out of every drive in the deployment",
            nodeRes.ok && offlineLine(h.drivesOffline),
          )}
          aside={cardSparkline(
            hist,
            "Drives online history",
            hist?.drivesOnline,
            nodeRes.ok ? h.drivesOnline : null,
            "count",
          )}
        />
        <StatCard
          icon="icon-[tabler--world]"
          label="Regions"
          value={regions.length > 0 ? String(regions.length) : "N/A"}
          title={hoverLines(
            "Regions with at least one node reporting",
            regions.map((r) => r.region).join(", "),
          )}
          aside={<RegionBars regions={regions} />}
        />
        <StatCard
          icon="icon-[tabler--chart-pie]"
          label="Raw capacity"
          value={s && s.total > 0 ? humanSize(s.total) : "N/A"}
          title={hoverLines(
            "Total raw space on every drive, before parity",
            s && s.total > 0 && `${humanSize(s.free)} free`,
          )}
          aside={cardSparkline(
            hist,
            "Raw space used over time",
            rawUsed(hist?.total, hist?.free),
            s && s.total > 0 ? s.total - s.free : null,
            "bytes",
          )}
        />
      </div>

      <Card title="Nodes" icon="icon-[tabler--server-2]" flush>
        {!nodeRes.ok ? (
          <p className="px-5 pb-5 text-sm text-stone-400">Node list unavailable.</p>
        ) : rows.length === 0 ? (
          <p className="px-5 pb-5 text-sm text-stone-400">No nodes reported.</p>
        ) : (
          <div className="px-4 pb-4">
            <div className="overflow-hidden rounded-lg border border-stone-800/70">
              <table className="w-full table-fixed">
                <thead>
                  <tr>
                    <th className="bg-surface font-body h-8 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md">
                      Endpoint
                    </th>
                    {showPool && (
                      <th className="bg-surface font-body hidden h-8 w-20 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:table-cell">
                        Pool
                      </th>
                    )}
                    <th className="bg-surface font-body h-8 w-24 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:w-28">
                      Status
                    </th>
                    <th className="bg-surface font-body hidden h-8 w-24 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md md:table-cell">
                      Drives
                    </th>
                    <th
                      className="bg-surface font-body h-8 w-36 px-4 text-left text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase first:rounded-l-md last:rounded-r-md sm:w-64 lg:w-80"
                      title="Filesystem space used on each node's drives, including files outside Obstor"
                    >
                      Disk used
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((n) => (
                    <tr key={n.endpoint}>
                      <td
                        className="h-10 truncate border-t border-stone-800/70 px-4 font-mono text-[13px] pointer-coarse:h-11"
                        title={n.endpoint}
                      >
                        {n.endpoint}
                      </td>
                      {showPool && (
                        <td className="hidden h-10 border-t border-stone-800/70 px-4 font-mono text-[13px] text-stone-400 sm:table-cell pointer-coarse:h-11">
                          {n.pool}
                        </td>
                      )}
                      <td className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11">
                        <span className="inline-flex items-center gap-2">
                          <span
                            className={`h-2 w-2 rounded-full ${n.online ? "bg-green-500" : "bg-red-500"}`}
                          />
                          {n.online ? "Online" : "Offline"}
                        </span>
                      </td>
                      <td
                        className={`hidden h-10 border-t border-stone-800/70 px-4 font-mono text-[13px] md:table-cell pointer-coarse:h-11 ${n.drivesOnline < n.drivesTotal ? "text-amber-400" : "text-stone-300"}`}
                      >
                        {n.drivesOnline}
                        <span className="text-stone-500"> / </span>
                        {n.drivesTotal}
                      </td>
                      <td
                        className="h-10 border-t border-stone-800/70 px-4 text-[13px] pointer-coarse:h-11"
                        title={`${humanSize(n.used)} of ${humanSize(n.total)} in use on this node's drives`}
                      >
                        <div className="flex items-center gap-3">
                          <div className="hidden min-w-0 flex-1 sm:flex">
                            <UsageBar pct={n.total > 0 ? (n.used / n.total) * 100 : 0} />
                          </div>
                          <span className="shrink-0 font-mono text-xs text-stone-400">
                            {humanSize(n.used)}
                            <span className="text-stone-500"> / </span>
                            {humanSize(n.total)}
                          </span>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </Card>

      <div className="grid items-stretch gap-4 xl:grid-cols-5">
        <Card
          title="Operations"
          icon="icon-[tabler--arrows-exchange]"
          className="xl:col-span-3"
          action={traffic && <RangeTabs value={range.value} label="Operations time range" />}
        >
          {traffic ? (
            <>
              <Legend
                items={[
                  {
                    color: "bg-amber-400",
                    label: "Reads",
                    value: m ? humanCount(sum(reads)) : "N/A",
                  },
                  {
                    color: "bg-cyan-500",
                    label: "Writes",
                    value: m ? humanCount(sum(writes)) : "N/A",
                  },
                ]}
              />
              <DivergingBars
                up={hourlyAverages(reads, hours)}
                down={hourlyAverages(writes, hours)}
                upName="Reads"
                downName="Writes"
                upColor="var(--color-amber-400)"
                downColor="var(--color-cyan-500)"
                hours={hours}
                height={220}
                since={m?.since}
                label={`Read and write operations over the last ${rangeDescription(hours)}`}
                empty={
                  m
                    ? `No operations in the last ${rangeDescription(hours)}.`
                    : "Operation metrics unavailable."
                }
              />
            </>
          ) : (
            <p className="py-10 text-center text-sm text-stone-400">{TRAFFIC_OFF}</p>
          )}
        </Card>
        <Card title="Regions" icon="icon-[tabler--world]" className="xl:col-span-2">
          {regions.length === 0 ? (
            <p className="text-sm text-stone-400">Region list unavailable.</p>
          ) : (
            <div className="space-y-5">
              {byCountry.size >= 2 && (
                <div className="space-y-3">
                  <div className="bg-abyss overflow-hidden rounded-lg border border-stone-800/70 p-3">
                    <WorldHeatmap usedByCountry={byCountry} regionsUsed={regionsUsed} />
                  </div>
                  <div className="flex items-center gap-2 font-mono text-[11px] text-stone-400">
                    <span className="font-body text-[12px] font-medium tracking-[0.08em] text-stone-400 uppercase">
                      Share of stored bytes
                    </span>
                    <span>0%</span>
                    <div className="flex h-1.5 flex-1 overflow-hidden rounded-full">
                      {[10, 30, 60, 90].map((pct) => (
                        <div
                          key={pct}
                          className="h-full flex-1 bg-amber-500"
                          style={{ opacity: shareOpacity(pct) }}
                        />
                      ))}
                    </div>
                    <span>100%</span>
                  </div>
                </div>
              )}
              <div className="max-h-[280px] space-y-3 overflow-y-auto pr-1">
                {regions.map((r) => (
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
        </Card>
      </div>
    </div>
  );
}

function offlineLine(offline: number): string {
  return offline > 0 ? `${offline} offline` : "all online";
}

// Max four drive usage by region
function RegionBars({ regions }: { regions: RegionStat[] }) {
  const shares = regionShares(regions);
  if (shares.length === 0) return <SparkPlaceholder />;
  return (
    <div className="flex h-8 w-full flex-col justify-center gap-1.5 sm:h-10">
      {shares.map((r) => (
        <div
          key={r.region}
          title={`${r.region}: ${Math.round(r.pct)}% of drive space in use`}
          className="h-1.5 w-full overflow-hidden rounded-full bg-stone-800"
        >
          <div className="h-full rounded-full bg-amber-500" style={{ width: `${r.pct}%` }} />
        </div>
      ))}
    </div>
  );
}
