package cmd

import (
	"context"
	"hash/fnv"
	"os"
	"strings"
	"time"

	"github.com/obstor/obstor/pkg/env"
	"github.com/obstor/obstor/pkg/madmin"
)

// Classify S3 handler names into read vs write traffic by verb prefix
var readPrefixes = []string{"get", "head", "list", "select"}
var writePrefixes = []string{"put", "post", "delete", "copy", "new", "complete", "abort", "make", "upload"}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Sum per-API counts into read and write totals by verb prefix
func classifyS3Requests(m map[string]int) (reads, writes uint64) {
	for api, n := range m {
		if n <= 0 {
			continue
		}
		api = strings.ToLower(api)
		switch {
		case hasAnyPrefix(api, readPrefixes):
			reads += uint64(n)
		case hasAnyPrefix(api, writePrefixes):
			writes += uint64(n)
		}
	}
	return reads, writes
}

// Per-member offset so large fleet never reports on the same second
func fleetReportPhase(id string, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return time.Duration(h.Sum64() % uint64(interval))
}

// Abstracts the live globals so collectOnce is testable
type counterSource interface {
	S3InBytes() uint64
	S3OutBytes() uint64
	S3Requests() map[string]int
	Region() string
	ID() string
	Label() string
	Version() string
	Storage() (used, total, free, objects, buckets uint64, dOn, dTot, nOn, nTot int)
}

// Previous tick's cumulative counters
type readings struct {
	in, out uint64
	reqs    map[string]int
}

func subClamp(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

// Read the source, diffs from previous, return snapshot and new readings
func collectOnce(src counterSource, prev readings) (MemberSnapshot, Deltas, readings) {
	in, out := src.S3InBytes(), src.S3OutBytes()
	reqs := src.S3Requests()
	cur := readings{in: in, out: out, reqs: reqs}

	var d Deltas
	if prev.reqs != nil {
		reads, writes := classifyS3Requests(reqs)
		pReads, pWrites := classifyS3Requests(prev.reqs)
		d = Deltas{
			InBytes:  subClamp(in, prev.in),
			OutBytes: subClamp(out, prev.out),
			Reads:    subClamp(reads, pReads),
			Writes:   subClamp(writes, pWrites),
		}
	}

	used, total, free, objects, buckets, dOn, dTot, nOn, nTot := src.Storage()
	snap := MemberSnapshot{
		ID:           src.ID(),
		Label:        src.Label(),
		Region:       src.Region(),
		Version:      src.Version(),
		Used:         used,
		Total:        total,
		Free:         free,
		ObjectsCount: objects,
		BucketsCount: buckets,
		DrivesOnline: dOn,
		DrivesTotal:  dTot,
		NodesOnline:  nOn,
		NodesTotal:   nTot,
	}
	return snap, d, cur
}

// Read the process globals
type globalCounterSource struct{}

func (globalCounterSource) S3InBytes() uint64  { return globalConnStats.getS3InputBytes() }
func (globalCounterSource) S3OutBytes() uint64 { return globalConnStats.getS3OutputBytes() }
func (globalCounterSource) S3Requests() map[string]int {
	return globalHTTPStats.totalS3Requests.Load()
}
func (globalCounterSource) Region() string  { return currentFleetRegion() }
func (globalCounterSource) ID() string      { return globalDeploymentID }
func (globalCounterSource) Label() string   { h, _ := os.Hostname(); return h }
func (globalCounterSource) Version() string { return Version }
func (globalCounterSource) Storage() (used, total, free, objects, buckets uint64, dOn, dTot, nOn, nTot int) {
	return readLocalStorageForFleet()
}

// Fold this node's own counters into the store
type fleetCollector struct {
	store    FleetStore
	src      counterSource
	interval time.Duration
}

// Return the collector tick interval, default 15s
func resolveFleetReportInterval() time.Duration {
	iv := 15 * time.Second
	if v := env.Get("OBSTOR_FLEET_REPORT_INTERVAL", ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			iv = d
		}
	}
	return iv
}

func newFleetCollector(store FleetStore) *fleetCollector {
	return &fleetCollector{store: store, src: globalCounterSource{}, interval: resolveFleetReportInterval()}
}

func (c *fleetCollector) run(ctx context.Context) {
	time.Sleep(fleetReportPhase(globalDeploymentID, c.interval))
	t := time.NewTicker(c.interval)
	defer t.Stop()
	var prev readings
	for {
		snap, d, cur := collectOnce(c.src, prev)
		now := time.Now()
		c.store.Upsert(snap, now)
		c.store.Fold(d, now)
		prev = cur
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Return deployment's configured region or default
func currentFleetRegion() string {
	if globalServerRegion != "" {
		return globalServerRegion
	}
	return "us-east-1"
}

// Reads local capacity, counts, and disk health
func readLocalStorageForFleet() (used, total, free, objects, buckets uint64, dOn, dTot, nOn, nTot int) {
	objectAPI := newObjectLayerFn()
	if objectAPI == nil {
		return
	}
	ctx := GlobalContext
	dui, _ := loadDataUsageFromBackend(ctx, objectAPI)
	for _, bu := range dui.BucketsUsage {
		used += bu.Size
		objects += bu.ObjectsCount
	}
	buckets = uint64(len(dui.BucketsUsage))
	si, _ := objectAPI.LocalStorageInfo(ctx)
	for _, d := range si.Disks {
		dTot++
		if d.State == madmin.DriveStateOk {
			dOn++
		}
		total += d.TotalSpace
		free += d.AvailableSpace
	}
	// Local is one node, multi-node peer counts are slice 2
	nOn, nTot = 1, 1
	return
}
