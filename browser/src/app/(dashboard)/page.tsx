import type { ReactNode } from "react";
import { CreateBucketButton } from "@/components/CreateBucketButton";
import { EnrollNodeButton } from "@/components/EnrollNodeButton";
import { REGION_COUNTRY } from "@/lib/regionmap";
import { type BucketEntry, humanCount, humanSize, listBuckets, rpc } from "@/lib/rpc";
import { COUNTRIES, WORLD_VIEWBOX } from "@/lib/worldmap";

interface StorageResult {
  used: number;
  total: number;
  free: number;
  disksOnline: number;
  disksOffline: number;
  objectsCount: number;
}

interface FleetServersRep {
  servers: { online: boolean }[];
  total: number;
}

interface RegionStat {
  region: string;
  used: number;
  total: number;
}

interface FleetRegionsRep {
  regions: RegionStat[];
  totalUsed: number;
}

interface BWBucket {
  in: number;
  out: number;
}

interface OpsBucket {
  read: number;
  write: number;
}

interface FleetMetricsRep {
  bandwidth: BWBucket[];
  ops: OpsBucket[];
  windowHours: number;
}

// Scaling where empty is nothing, trace is too few samples, data is usable charts
type PlotMode = "empty" | "trace" | "data";

// Smallest domain a plot may use
const BYTES_FLOOR = 1 << 20;
const OPS_FLOOR = 10;

function plotScale(series: number[][], floor: number): { mode: PlotMode; domain: number } {
  const all = series.flat();
  const max = Math.max(...all, 0);
  if (max <= 0) return { mode: "empty", domain: floor };
  const samples = all.filter((v) => v > 0).length;
  if (samples < 3) return { mode: "trace", domain: floor };
  return { mode: "data", domain: Math.max(max, floor) };
}

// Sequential magnitude
function shareOpacity(pct: number): number {
  return pct >= 75 ? 0.9 : pct >= 50 ? 0.65 : pct >= 25 ? 0.4 : 0.2;
}

// Capacity meter
function Gauge({
  usedPct,
  size = 116,
  stroke = 12,
}: {
  usedPct: number; // 0..1
  size?: number;
  stroke?: number;
}) {
  const used = Math.max(0, Math.min(1, usedPct));
  const r = (size - stroke) / 2;
  const c = size / 2;
  const circ = 2 * Math.PI * r;
  const sweep = 270;
  const arcLen = circ * (sweep / 360);
  const usedLen = arcLen * used;
  // Rotate so the 90deg gap sits centered
  const rot = `rotate(135 ${c} ${c})`;
  return (
    <svg
      role="img"
      aria-label={`${Math.round(used * 100)}% disk used`}
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      className="shrink-0"
    >
      <circle
        cx={c}
        cy={c}
        r={r}
        fill="none"
        stroke="var(--color-zinc-800)"
        strokeWidth={stroke}
        strokeDasharray={`${arcLen} ${circ}`}
        strokeLinecap="round"
        transform={rot}
      />
      {/* Round cap on a zero-length arc */}
      {usedLen > 0 && (
        <circle
          cx={c}
          cy={c}
          r={r}
          fill="none"
          stroke="var(--color-amber-500)"
          strokeWidth={stroke}
          strokeDasharray={`${usedLen} ${circ}`}
          strokeLinecap="round"
          transform={rot}
        />
      )}
      <text
        x={c}
        y={c + 2}
        textAnchor="middle"
        className="fill-stone-100 font-bold font-display text-2xl leading-none"
      >
        {Math.round(used * 100)}%
      </text>
      <text x={c} y={c + 18} textAnchor="middle" className="fill-stone-500 text-[9px] capitalize">
        disk
      </text>
    </svg>
  );
}

function SegmentBar({ online, offline }: { online: number; offline: number }) {
  const total = online + offline;
  const onPct = total === 0 ? 0 : (online / total) * 100;
  const offPct = total === 0 ? 0 : (offline / total) * 100;
  const onColor = offline === 0 ? "bg-stone-700" : "bg-green-500";
  return (
    <div className="flex h-3 w-full overflow-hidden rounded-sm bg-surface-overlay">
      <div className={onColor} style={{ width: `${onPct}%` }} />
      <div className="bg-red-400" style={{ width: `${offPct}%` }} />
    </div>
  );
}

// Status row of server or drives
function ClusterRow({
  name,
  online,
  offline,
  total,
}: {
  name: string;
  online: number;
  offline: number;
  total: number;
}) {
  return (
    <div className="flex flex-1 flex-col justify-center gap-2">
      <div className="flex items-center justify-between">
        <span className="font-display font-semibold text-sm">{name}</span>
        <span className="whitespace-nowrap font-bold font-display text-lg leading-none">
          {online}
          <span className="text-stone-600">/{total}</span>
        </span>
      </div>
      <SegmentBar online={online} offline={offline} />
      <div className="flex items-center gap-3 font-mono text-[10px]">
        <span className="flex items-center gap-1.5 capitalize">
          <span className="icon-[tabler--power] text-[13px] text-stone-500" />
          <span className="text-stone-400">{online}</span>{" "}
          <span className="text-stone-600">online</span>
        </span>
        <span className="flex items-center gap-1.5 capitalize">
          <span
            className={`icon-[tabler--power] text-[13px] ${
              offline > 0 ? "text-red-400" : "text-stone-600"
            }`}
          />
          <span className={offline > 0 ? "text-red-400" : "text-stone-400"}>{offline}</span>{" "}
          <span className="text-stone-600">offline</span>
        </span>
      </div>
    </div>
  );
}

// Center baseline reads grow up, writes grow down
function DivergingBars({
  up,
  down,
  upColor,
  downColor,
  domain,
  mode,
  width = 600,
  height = 140,
}: {
  up: number[];
  down: number[];
  upColor: string;
  downColor: string;
  domain: number;
  mode: PlotMode;
  width?: number;
  height?: number;
}) {
  const n = up.length;
  const barGap = 2;
  const barWidth = (width - barGap * (n - 1)) / n;
  const mid = height / 2;
  const half = mid - 4;

  // One path per direction rather than a node per bucket
  const barPath = (values: number[], dir: "up" | "down") =>
    values
      .map((v, i) => {
        const h = (v / domain) * half;
        if (h <= 0) return "";
        const x = i * (barWidth + barGap);
        return `M${x} ${dir === "up" ? mid - h : mid}h${barWidth}v${h}h${-barWidth}Z`;
      })
      .join("");

  return (
    <svg
      aria-hidden="true"
      className="h-full w-full"
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
    >
      <line x1={0} y1={mid} x2={width} y2={mid} stroke="var(--color-zinc-800)" strokeWidth={1} />
      {mode !== "empty" &&
        [
          { d: barPath(up, "up"), color: upColor },
          { d: barPath(down, "down"), color: downColor },
        ].map((b) => (b.d ? <path key={b.color} d={b.d} fill={b.color} /> : null))}
    </svg>
  );
}

function LineChart({
  series,
  domain,
  mode,
  width = 600,
  height = 140,
}: {
  series: { data: number[]; color: string }[];
  domain: number;
  mode: PlotMode;
  width?: number;
  height?: number;
}) {
  const n = Math.max(...series.map((s) => s.data.length), 2);
  const stepX = (width - 2) / (n - 1);
  const base = height - 10;
  const x = (i: number) => 1 + i * stepX;
  const y = (v: number) => base - (v / domain) * (height - 20);

  return (
    <svg
      aria-hidden="true"
      className="h-full w-full"
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
    >
      <line
        x1={0}
        y1={base}
        x2={width}
        y2={base}
        stroke="var(--color-zinc-800)"
        strokeWidth={1}
        strokeDasharray="2 4"
      />
      {mode !== "empty" &&
        series.map((s, si) => {
          if (s.data.every((v) => v <= 0)) return null;
          if (mode === "trace") {
            const ticks = s.data
              .map((v, i) => (v > 0 ? `M${x(i) + si * 2} ${base}V${base - 6}` : ""))
              .join("");
            return <path key={s.color} d={ticks} fill="none" stroke={s.color} strokeWidth={1.5} />;
          }
          return (
            <polyline
              key={s.color}
              points={s.data.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ")}
              fill="none"
              stroke={s.color}
              strokeWidth={1.5}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          );
        })}
    </svg>
  );
}

function WorldHeatmap({
  usedByCountry,
  regionsUsed,
}: {
  usedByCountry: Map<string, number>;
  regionsUsed: number;
}) {
  return (
    <svg aria-hidden="true" width="100%" viewBox={WORLD_VIEWBOX} className="h-auto w-full">
      {COUNTRIES.map(([id, d]) => {
        const bytes = usedByCountry.get(id);
        const share = bytes !== undefined && regionsUsed > 0 ? (bytes / regionsUsed) * 100 : 0;
        return (
          <path
            key={id}
            d={d}
            fill={bytes === undefined ? "var(--color-surface-overlay)" : "var(--color-amber-500)"}
            fillOpacity={bytes === undefined ? 1 : shareOpacity(share)}
            stroke="var(--color-abyss)"
            strokeWidth={0.5}
          />
        );
      })}
    </svg>
  );
}

export default async function DashboardHome() {
  // Determine if it can see before any fleet calls
  let canSeeFleet = false;
  try {
    const sv = await rpc<{ canSeeFleet: boolean }>("ServerInfo");
    canSeeFleet = sv.canSeeFleet ?? false;
  } catch {}

  // Gate the first-run layout on the live bucket list
  let buckets: BucketEntry[] = [];
  try {
    buckets = await listBuckets();
  } catch {}
  const firstRun = buckets.length === 0;
  const endpoint = process.env.OBSTOR_ENDPOINT || "http://localhost:9000";

  let used = 0;
  let total = 0;
  let free = 0;
  let objectsCount = 0;
  let driveOnline = 0;
  let driveOffline = 0;

  let nodesOnline = 0;
  let nodesTotal = 0;
  let regions: RegionStat[] = [];
  let regionsUsed = 0;
  let windowHours = 24;
  let downloadSeries: number[] = Array(24).fill(0);
  let uploadSeries: number[] = Array(24).fill(0);
  let readSeries: number[] = Array(24).fill(0);
  let writeSeries: number[] = Array(24).fill(0);
  let fetchError = "";

  const errMsg = (e: unknown) => (e instanceof Error ? e.message : "Request failed");

  if (canSeeFleet) {
    const [siR, slR, frR, fmR] = await Promise.allSettled([
      rpc<StorageResult>("FleetStorageInfo"),
      rpc<FleetServersRep>("FleetServers", { offset: 0, limit: 256 }),
      rpc<FleetRegionsRep>("FleetRegions"),
      rpc<FleetMetricsRep>("FleetMetrics"),
    ]);

    if (siR.status === "fulfilled") {
      const s = siR.value;
      used = s.used;
      total = s.total;
      free = s.free;
      objectsCount = s.objectsCount;
      driveOnline = s.disksOnline;
      driveOffline = s.disksOffline;
    } else {
      fetchError ||= errMsg(siR.reason);
    }

    if (slR.status === "fulfilled") {
      nodesTotal = slR.value.total;
      nodesOnline = slR.value.servers.filter((n) => n.online).length;
    } else {
      fetchError ||= errMsg(slR.reason);
    }

    if (frR.status === "fulfilled") {
      regions = frR.value.regions;
      regionsUsed = frR.value.totalUsed;
    } else {
      fetchError ||= errMsg(frR.reason);
    }

    if (fmR.status === "fulfilled") {
      const fm = fmR.value;
      windowHours = fm.windowHours || 24;
      downloadSeries = fm.bandwidth.map((b) => b.out);
      uploadSeries = fm.bandwidth.map((b) => b.in);
      readSeries = fm.ops.map((o) => o.read);
      writeSeries = fm.ops.map((o) => o.write);
    } else {
      fetchError ||= errMsg(fmR.reason);
    }
  } else {
    try {
      const s = await rpc<StorageResult>("StorageInfo");
      used = s.used;
      total = s.total;
      free = s.free;
      objectsCount = s.objectsCount;
      driveOnline = s.disksOnline;
      driveOffline = s.disksOffline;
    } catch (err) {
      fetchError = errMsg(err);
    }
  }

  const nodesOffline = Math.max(0, nodesTotal - nodesOnline);
  const driveTotal = driveOnline + driveOffline;

  // One filled country is not a map
  const usedByCountry = new Map<string, number>();
  for (const r of regions) {
    const country = REGION_COUNTRY[r.region];
    if (country) usedByCountry.set(country, (usedByCountry.get(country) ?? 0) + r.used);
  }
  const showMap = usedByCountry.size >= 2;

  // The gauge meters disk fill
  const diskPct = total > 0 ? (total - free) / total : 0;
  const bw = plotScale([downloadSeries, uploadSeries], BYTES_FLOOR);
  const ops = plotScale([readSeries, writeSeries], OPS_FLOOR);
  const downloadTotal = downloadSeries.reduce((a, b) => a + b, 0);
  const uploadTotal = uploadSeries.reduce((a, b) => a + b, 0);
  const readTotal = readSeries.reduce((a, b) => a + b, 0);
  const writeTotal = writeSeries.reduce((a, b) => a + b, 0);

  const usageCard = (
    <Card title="Usage" icon="icon-[tabler--database]">
      <div className="flex h-full flex-col justify-between gap-4">
        {total > 0 ? (
          <div className="flex items-start gap-4">
            <Gauge usedPct={diskPct} />
            <div className="min-w-0 space-y-2.5">
              <div>
                <p className="whitespace-nowrap font-bold font-display text-2xl leading-none">
                  {humanSize(used)}
                </p>
                <p className="mt-1 font-mono text-[10px] text-stone-600 capitalize">stored</p>
              </div>
              <div>
                <p className="whitespace-nowrap font-bold font-display text-2xl leading-none">
                  {humanSize(free)}
                </p>
                <p className="mt-1 font-mono text-[10px] text-stone-600 capitalize">free</p>
              </div>
            </div>
          </div>
        ) : (
          <p className="py-6 font-body text-stone-600 text-xs">
            Capacity appears once at least one drive reports in.
          </p>
        )}
        <div className="grid grid-cols-3 gap-2 border-amber-200/10 border-t pt-3">
          <Stat value={humanCount(objectsCount)} label="objects" />
          <Stat value={humanCount(buckets.length)} label="buckets" />
          <Stat value={total > 0 ? humanSize(total) : "N/A"} label="raw capacity" />
        </div>
      </div>
    </Card>
  );

  const clusterCard = (
    <Card
      title="Cluster"
      icon="icon-[tabler--stack-2]"
      rightLabel={firstRun ? undefined : <EnrollNodeButton />}
    >
      <div className="flex h-full flex-col divide-y divide-amber-200/10">
        <ClusterRow name="Nodes" online={nodesOnline} offline={nodesOffline} total={nodesTotal} />
        <ClusterRow name="Drives" online={driveOnline} offline={driveOffline} total={driveTotal} />
      </div>
    </Card>
  );

  const bandwidthCard = (
    <Card title="Bandwidth" icon="icon-[tabler--chart-line]">
      <div className="flex h-full flex-col">
        <div className="flex items-center gap-3 font-mono text-[10px] text-stone-600">
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-1.5 w-5 rounded-[2px] bg-amber-500" /> Downloads{" "}
            <NumUnit text={humanSize(downloadTotal)} />
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-1.5 w-5 rounded-[2px] bg-cyan-500" /> Uploads{" "}
            <NumUnit text={humanSize(uploadTotal)} />
          </span>
        </div>
        <div className="relative mt-2 max-h-[200px] min-h-0 flex-1">
          {bw.mode !== "empty" && (
            <span className="absolute top-0 left-0 font-mono text-[9px] text-stone-600">
              {humanSize(bw.domain)}/h
            </span>
          )}
          {bw.mode === "empty" && (
            <span className="absolute inset-0 flex items-center justify-center font-mono text-[11px] text-stone-400">
              <span className="bg-abyss px-2">No transfers in the last {windowHours} hours.</span>
            </span>
          )}
          <LineChart
            series={[
              { data: downloadSeries, color: "var(--color-amber-500)" },
              { data: uploadSeries, color: "var(--color-cyan-500)" },
            ]}
            domain={bw.domain}
            mode={bw.mode}
          />
        </div>
        <AxisHours hours={windowHours} />
      </div>
    </Card>
  );

  const operationsCard = (
    <Card title="Operations" icon="icon-[tabler--activity]">
      <div className="flex h-full flex-col">
        <div className="flex items-center gap-3 font-mono text-[10px] text-stone-600">
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-1.5 w-5 rounded-[2px] bg-amber-400" /> Reads{" "}
            <span className="text-stone-400">{readTotal.toLocaleString()}</span>
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-1.5 w-5 rounded-[2px] bg-cyan-500" /> Writes{" "}
            <span className="text-stone-400">{writeTotal.toLocaleString()}</span>
          </span>
        </div>
        <div className="relative mt-2 max-h-[180px] min-h-0 flex-1">
          {ops.mode !== "empty" && (
            <span className="absolute top-0 left-0 font-mono text-[9px] text-stone-600">
              {ops.domain} ops/h
            </span>
          )}
          {ops.mode === "empty" && (
            <span className="absolute inset-0 flex items-center justify-center font-mono text-[11px] text-stone-400">
              <span className="bg-abyss px-2">No requests in the last {windowHours} hours.</span>
            </span>
          )}
          <DivergingBars
            up={readSeries}
            down={writeSeries}
            upColor="var(--color-amber-400)"
            downColor="var(--color-cyan-500)"
            domain={ops.domain}
            mode={ops.mode}
          />
        </div>
        <AxisHours hours={windowHours} />
      </div>
    </Card>
  );

  const regionsCard = (
    <Card
      title="Regions"
      icon="icon-[tabler--world]"
      rightLabel={
        <>
          {regionsUsed > 0 ? (
            <>
              <NumUnit text={humanSize(regionsUsed)} /> used <Sep />{" "}
            </>
          ) : null}
          <span className="text-stone-400">{regions.length}</span>{" "}
          {regions.length === 1 ? "region" : "regions"}
        </>
      }
      className={showMap ? "@container" : "@container self-start"}
    >
      {regions.length === 0 ? (
        <p className="py-6 font-body text-stone-600 text-xs">No regions reporting.</p>
      ) : (
        <div className={`grid gap-5 ${showMap ? "@xl:grid-cols-[1fr_200px]" : ""}`}>
          {showMap && (
            <div className="flex flex-col gap-3">
              <div className="overflow-hidden rounded-lg border border-amber-200/5 bg-void/40 p-3">
                <WorldHeatmap usedByCountry={usedByCountry} regionsUsed={regionsUsed} />
              </div>
              {regionsUsed > 0 && (
                <div className="flex items-center gap-2 font-mono text-[10px] text-stone-600">
                  <span className="uppercase tracking-wider">Share of stored bytes</span>
                  <span className="text-stone-400">0%</span>
                  <div className="flex h-1.5 flex-1 overflow-hidden rounded-full">
                    {[0.2, 0.4, 0.65, 0.9].map((o) => (
                      <div key={o} className="h-full flex-1 bg-amber-500" style={{ opacity: o }} />
                    ))}
                  </div>
                  <span className="text-stone-400">100%</span>
                </div>
              )}
            </div>
          )}
          <div className="max-h-[280px] space-y-2.5 overflow-y-auto pr-1">
            {[...regions]
              .sort((a, b) => b.used - a.used)
              .map((r) => {
                const share = regionsUsed > 0 ? (r.used / regionsUsed) * 100 : 0;
                return (
                  <div key={r.region}>
                    <div className="mb-1 flex items-center justify-between font-mono text-[11px]">
                      <span className="text-stone-400">{r.region}</span>
                      <span className="text-stone-600">{humanSize(r.used)}</span>
                    </div>
                    <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface-overlay">
                      <div
                        className="h-full rounded-full bg-amber-500"
                        style={{
                          width: share > 0 ? `${Math.max(share, 2)}%` : "0%",
                          opacity: shareOpacity(share),
                        }}
                      />
                    </div>
                  </div>
                );
              })}
          </div>
        </div>
      )}
    </Card>
  );

  const getStartedCard = (
    <Card title="Get started" icon="icon-[tabler--list-numbers]">
      <div className="divide-y divide-amber-200/10">
        <Step
          n={1}
          title="Create a bucket."
          body="Objects are stored in buckets. Nothing else on this page moves until one exists."
          action={<CreateBucketButton />}
        />
        {canSeeFleet && (
          <Step
            n={2}
            title="Add a node."
            body="Enroll another machine to spread data across nodes."
            action={<EnrollNodeButton />}
          />
        )}
      </div>
    </Card>
  );

  const connectCard = (
    <Card title="Connect" icon="icon-[tabler--plug-connected]">
      <div className="flex h-full flex-col gap-3">
        <div>
          <p className="font-mono text-[10px] text-stone-600 capitalize">s3 endpoint</p>
          <p className="mt-1 select-all break-all font-mono text-sm text-stone-300">{endpoint}</p>
        </div>
        <div>
          <p className="font-mono text-[10px] text-stone-600 capitalize">example request</p>
          <p className="mt-1 select-all break-all rounded-lg border border-amber-200/5 bg-void/40 px-3 py-2 font-mono text-[11px] text-stone-300">
            aws --endpoint-url {endpoint} s3 ls
          </p>
        </div>
        <p className="mt-auto font-body text-stone-600 text-xs">
          Sign requests with an access key and secret. Create one on a bucket's Access tab.
        </p>
      </div>
    </Card>
  );

  const errorBanner = fetchError ? (
    <div className="rounded-lg border border-red-500/20 bg-red-500/5 px-4 py-3">
      <p className="flex items-center gap-2 font-body text-red-400 text-sm">
        <span className="icon-[tabler--alert-circle] text-sm" />
        {fetchError}
      </p>
      <p className="mt-1 pl-6 font-mono text-[11px] text-stone-500">
        Console is reading {endpoint}.
      </p>
    </div>
  ) : null;

  if (!canSeeFleet) {
    return (
      <div className="flex flex-col gap-4">
        {errorBanner}
        {firstRun ? (
          <>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-[4fr_5fr]">
              {usageCard}
              {connectCard}
            </div>
            {getStartedCard}
          </>
        ) : (
          <div className="max-w-sm">{usageCard}</div>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {errorBanner}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-[4fr_5fr_9fr]">
        {usageCard}
        {clusterCard}
        {bandwidthCard}
      </div>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-[4fr_3fr]">
        {firstRun ? getStartedCard : regionsCard}
        {firstRun ? connectCard : operationsCard}
      </div>
    </div>
  );
}

function Card({
  title,
  icon,
  rightLabel,
  className,
  children,
}: {
  title: string;
  icon?: string;
  rightLabel?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={`flex flex-col rounded-xl border border-amber-200/5 bg-abyss p-5 ${className ?? ""}`}
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <p className="flex items-center gap-2 font-display font-semibold text-sm">
          {icon && <span className={`${icon} text-[13px] text-amber-500`} />}
          {title}
        </p>
        {rightLabel && <div className="font-mono text-[11px] text-stone-600">{rightLabel}</div>}
      </div>
      <div className="flex-1">{children}</div>
    </div>
  );
}

function Step({
  n,
  title,
  body,
  action,
}: {
  n: number;
  title: string;
  body: string;
  action: ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-4 py-4">
      <div className="min-w-0">
        <p className="font-display font-semibold text-sm">
          <span className="mr-2 font-mono text-[11px] text-amber-500">{n}</span>
          {title}
        </p>
        <p className="mt-1 font-body text-stone-400 text-xs">{body}</p>
      </div>
      {action}
    </div>
  );
}

function AxisHours({ hours }: { hours: number }) {
  const tickCount = 7;
  const now = new Date();
  const ticks = Array.from({ length: tickCount }, (_, i) => {
    const ms = now.getTime() - (1 - i / (tickCount - 1)) * hours * 3_600_000;
    const label =
      i === tickCount - 1 ? "now" : `${String(new Date(ms).getHours()).padStart(2, "0")}:00`;
    return { ms, label };
  });
  return (
    <div className="mt-1 flex justify-between font-mono text-[9px] text-stone-600">
      {ticks.map((t) => (
        <span key={t.ms}>{t.label}</span>
      ))}
    </div>
  );
}

function Sep() {
  return <span className="text-stone-600">|</span>;
}

function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div>
      <p className="whitespace-nowrap font-bold font-display text-lg leading-none">{value}</p>
      <p className="mt-1 font-mono text-[10px] text-stone-600 capitalize">{label}</p>
    </div>
  );
}

function NumUnit({ text, numClass = "text-stone-400" }: { text: string; numClass?: string }) {
  const i = text.lastIndexOf(" ");
  if (i === -1) return <span className={numClass}>{text}</span>;
  return (
    <>
      <span className={numClass}>{text.slice(0, i)}</span>
      <span className="text-stone-600">{text.slice(i)}</span>
    </>
  );
}
