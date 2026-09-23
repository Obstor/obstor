package cmd

import (
	"testing"
	"time"
)

func TestClassifyS3Requests(t *testing.T) {
	// Keys are the lowercase api names the router registers
	m := map[string]int{
		"getobject":          5,
		"headobject":         2,
		"listobjectversions": 1,
		"putobject":          3,
		"putobjectpart":      2,
		"deleteobject":       1,
		"postpolicybucket":   1,
		"unknownapi":         9,
	}
	reads, writes := classifyS3Requests(m)
	if reads != 8 {
		t.Fatalf("reads = %d, want 8", reads)
	}
	if writes != 7 {
		t.Fatalf("writes = %d, want 7", writes)
	}
}

func TestFleetReportPhaseDeterministicAndBounded(t *testing.T) {
	iv := 15 * time.Second
	a1 := fleetReportPhase("deployment-xyz", iv)
	a2 := fleetReportPhase("deployment-xyz", iv)
	b := fleetReportPhase("other", iv)
	if a1 != a2 {
		t.Fatalf("phase not deterministic: %v vs %v", a1, a2)
	}
	if a1 < 0 || a1 >= iv {
		t.Fatalf("phase %v out of [0,%v)", a1, iv)
	}
	if a1 == b {
		t.Log("phases collided (acceptable, but verify spread on real ids)")
	}
}

type fakeSource struct {
	in, out uint64
	reqs    map[string]int
}

func (f fakeSource) S3InBytes() uint64          { return f.in }
func (f fakeSource) S3OutBytes() uint64         { return f.out }
func (f fakeSource) S3Requests() map[string]int { return f.reqs }
func (f fakeSource) Region() string             { return "us-east-1" }
func (f fakeSource) ID() string                 { return "dep-1" }
func (f fakeSource) Label() string              { return "node-a" }
func (f fakeSource) Version() string            { return "test" }
func (f fakeSource) Storage() (used, total, free, objects, buckets uint64, dOn, dTot, nOn, nTot int) {
	return 10, 100, 90, 5, 2, 4, 4, 1, 1
}

func TestCollectOnceFirstTickNoSpike(t *testing.T) {
	src := fakeSource{in: 1000, out: 500, reqs: map[string]int{"GetObject": 7}}
	snap, d, r := collectOnce(src, readings{})
	if d != (Deltas{}) {
		t.Fatalf("first-tick deltas = %+v, want zero (no spike from cold counters)", d)
	}
	if snap.ID != "dep-1" || snap.Used != 10 || snap.Region != "us-east-1" {
		t.Fatalf("snapshot wrong: %+v", snap)
	}
	if r.in != 1000 || r.out != 500 {
		t.Fatalf("readings not captured: %+v", r)
	}
}

func TestCollectOnceDiffsAgainstPrev(t *testing.T) {
	prev := readings{in: 1000, out: 500, reqs: map[string]int{"GetObject": 7, "PutObject": 1}}
	src := fakeSource{in: 1300, out: 700, reqs: map[string]int{"GetObject": 10, "PutObject": 4}}
	_, d, _ := collectOnce(src, prev)
	if d.InBytes != 300 || d.OutBytes != 200 {
		t.Fatalf("byte deltas = %+v, want In300 Out200", d)
	}
	if d.Reads != 3 || d.Writes != 3 {
		t.Fatalf("op deltas = %+v, want R3 W3", d)
	}
}

func TestCollectOnceCounterResetClampsToZero(t *testing.T) {
	prev := readings{in: 5000, out: 5000, reqs: map[string]int{"GetObject": 100}}
	src := fakeSource{in: 10, out: 10, reqs: map[string]int{"GetObject": 1}} // process restarted
	_, d, _ := collectOnce(src, prev)
	if d.InBytes != 0 || d.Reads != 0 {
		t.Fatalf("reset deltas = %+v, want all zero (clamped)", d)
	}
}

func TestCurrentFleetRegionFallsBackToDefault(t *testing.T) {
	saved := globalServerRegion
	defer func() { globalServerRegion = saved }()

	// Nodes with unconfigured region group as default
	globalServerRegion = ""
	if got := currentFleetRegion(); got != "us-east-1" {
		t.Fatalf("empty region: got %q, want us-east-1", got)
	}

	globalServerRegion = "eu-west-1"
	if got := currentFleetRegion(); got != "eu-west-1" {
		t.Fatalf("configured region: got %q, want eu-west-1", got)
	}
}
