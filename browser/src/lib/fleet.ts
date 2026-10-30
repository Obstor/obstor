import { cache } from "react";
import { RPCError, rethrowSessionError, rpc } from "./rpc";

export interface StorageStats {
  used: number;
  total: number;
  free: number;
  objectsCount: number;
  bucketsCount: number;
  disksOnline: number;
  disksOffline: number;
  parityRatio: number;
  usageAsOf: string;
  region: string;
}

export interface NodeRow {
  endpoint: string;
  online: boolean;
  drivesOnline: number;
  drivesTotal: number;
  used: number;
  total: number;
  pool: number;
}

export interface RegionStat {
  region: string;
  used: number;
  total: number;
  free: number;
}

export interface Metrics {
  bandwidth: { in: number; out: number }[];
  ops: { read: number; write: number }[];
  windowHours: number;
  since: string;
}

export interface CapacityHistory {
  since: string;
  step: number;
  used: number[];
  objects: number[];
  buckets: number[];
  // Fleet tier only, null for scoped callers
  free: number[] | null;
  total: number[] | null;
  drivesOnline: number[] | null;
  drivesTotal: number[] | null;
  nodesOnline: number[] | null;
  nodesTotal: number[] | null;
  partial: boolean;
}

// Server error.data.code, absent if server was not reached
export type Result<T> = { ok: true; value: T } | { ok: false; error: string; code?: string };

export const FLEET_DISABLED = "XObstorFleetDisabled";
export const TRAFFIC_OFF =
  "Traffic metrics need the fleet store (start the server with --web-address).";

export async function settle<T>(p: Promise<T>): Promise<Result<T>> {
  try {
    return { ok: true, value: await p };
  } catch (err) {
    rethrowSessionError(err);
    return {
      ok: false,
      error: err instanceof Error ? err.message : "Request failed",
      code: err instanceof RPCError ? err.code : undefined,
    };
  }
}

// Fleet gets cluster totals, others get bucket usage and zero totals
export const storage = cache(() => settle(rpc<StorageStats>("StorageInfo")));

// Nodes in this deployment
export const nodes = cache(() =>
  settle(
    rpc<{ servers: NodeRow[] | null }>("ListServers", { offset: 0, limit: 256 }).then(
      (r) => r.servers ?? [],
    ),
  ),
);

export const fleetRegions = cache(() =>
  settle(rpc<{ regions: RegionStat[] | null }>("FleetRegions").then((r) => r.regions ?? [])),
);

export const fleetMetrics = cache((hours: number) =>
  settle(rpc<Metrics>("FleetMetrics", { windowHours: hours })),
);

// Fleet gets cluster series, others get listable bucket sums with null free
export const capacityHistory = cache((hours: number) =>
  settle(rpc<CapacityHistory>("CapacityHistory", { hours })),
);
