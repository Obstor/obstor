package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/obstor/obstor/pkg/madmin"
)

// Usage bytes and one object per 10 bytes, including bucket sizes
func capacityUsage(total uint64, buckets map[string]uint64) madmin.DataUsageInfo {
	dui := madmin.DataUsageInfo{
		ObjectsTotalSize:  total,
		ObjectsTotalCount: total / 10,
		BucketsCount:      uint64(len(buckets)),
		BucketsUsage:      map[string]madmin.BucketUsageInfo{},
	}
	for name, size := range buckets {
		dui.BucketsUsage[name] = madmin.BucketUsageInfo{Size: size, ObjectsCount: size / 10}
	}
	return dui
}

func capacityKept(string) bool { return true }

func capacityGone(string) bool { return false }

func capacityHourStart(h int64) time.Time { return time.Unix(h*3600, 0).UTC() }

// Sample drive space and health
func capacityDrives(total, free, dOn, dTot, nOn, nTot uint64) capacityBucket {
	return capacityBucket{Total: total, Free: free, DrivesOnline: dOn, DrivesTotal: dTot, NodesOnline: nOn, NodesTotal: nTot}
}

func capacityZeros(n int) []uint64 { return make([]uint64, n) }

func checkCapacitySeries(t *testing.T, name string, got, want capacitySeries) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got  %+v\n want %+v", name, got, want)
	}
}

func TestCapacityRingAdvance(t *testing.T) {
	if got := ringSlot(-1, capacityHourlySlots); got != capacityHourlySlots-1 {
		t.Fatalf("ringSlot(-1) = %d, want %d", got, capacityHourlySlots-1)
	}
	if got := ringSlot(capacityHourlySlots, capacityHourlySlots); got != 0 {
		t.Fatalf("ringSlot(size) = %d, want 0", got)
	}
	now := absHour(hourBase())
	var s capacityState
	if !s.advance(now) || !s.Initialized || s.EpochHour != now {
		t.Fatalf("first advance: initialized %v, epoch %d, want %d", s.Initialized, s.EpochHour, now)
	}
	s.CapRing[ringSlot(now, fleetRingSize)] = capacityBucket{Used: 1, Set: true}
	for h := now + 1; h <= now+3; h++ {
		s.CapRing[ringSlot(h, fleetRingSize)] = capacityBucket{Used: 9, Set: true}
	}
	if !s.advance(now+3) || s.EpochHour != now+3 {
		t.Fatalf("3h advance: epoch %d, want %d", s.EpochHour, now+3)
	}
	for h := now + 1; h <= now+3; h++ {
		if s.CapRing[ringSlot(h, fleetRingSize)].Set {
			t.Errorf("hour +%d still set after a 3h advance", h-now)
		}
	}
	if !s.CapRing[ringSlot(now, fleetRingSize)].Set {
		t.Error("3h advance cleared the previous epoch hour")
	}
	if s.advance(now + 3) {
		t.Error("advance to the epoch hour reported a move")
	}
	if s.advance(now) || s.EpochHour != now+3 {
		t.Errorf("backward advance: epoch %d, want %d", s.EpochHour, now+3)
	}
	far := now + 3 + fleetRingSize
	if !s.advance(far) || s.EpochHour != far {
		t.Fatalf("full-window advance: epoch %d, want %d", s.EpochHour, far)
	}
	for i, b := range s.CapRing {
		if b != (capacityBucket{}) {
			t.Fatalf("slot %d survived a full-window gap: %+v", i, b)
		}
	}
}

func TestCapacityCarryForward(t *testing.T) {
	now := absHour(hourBase())
	none := capacitySeries{Step: time.Hour, Used: []uint64{}, Objects: []uint64{}, Buckets: []uint64{}}
	var empty capacityState
	checkCapacitySeries(t, "empty", empty.series(now, 24, nil), none)
	checkCapacitySeries(t, "empty scoped", empty.series(now, 24, capacityScope{"alpha": now}), none)

	var s capacityState
	s.record(now-5, capacityUsage(100, nil), capacityDrives(1000, 900, 3, 4, 1, 2), capacityGone)
	s.record(now-2, capacityUsage(200, nil), capacityDrives(1000, 800, 4, 4, 2, 2), capacityGone)
	checkCapacitySeries(t, "24h", s.series(now, 24, nil), capacitySeries{
		Since:        capacityHourStart(now - 5),
		Step:         time.Hour,
		Used:         []uint64{100, 100, 100, 200, 200, 200},
		Objects:      []uint64{10, 10, 10, 20, 20, 20},
		Buckets:      []uint64{0, 0, 0, 0, 0, 0},
		Free:         []uint64{900, 900, 900, 800, 800, 800},
		Total:        []uint64{1000, 1000, 1000, 1000, 1000, 1000},
		DrivesOnline: []uint64{3, 3, 3, 4, 4, 4},
		DrivesTotal:  []uint64{4, 4, 4, 4, 4, 4},
		NodesOnline:  []uint64{1, 1, 1, 2, 2, 2},
		NodesTotal:   []uint64{2, 2, 2, 2, 2, 2},
	})

	later := s.series(now+30, 24, nil)
	if !later.Since.Equal(capacityHourStart(now+7)) || len(later.Used) != 24 || len(later.Free) != 24 || len(later.NodesTotal) != 24 {
		t.Fatalf("window after the last sample: since %v, %d points, want %v and 24", later.Since, len(later.Used), capacityHourStart(now+7))
	}
	for i := range later.Used {
		if later.Used[i] != 200 || later.Free[i] != 800 || later.Total[i] != 1000 || later.DrivesOnline[i] != 4 || later.NodesOnline[i] != 2 {
			t.Fatalf("point %d = used %d free %d total %d drives %d nodes %d, want the carried 200 800 1000 4 2",
				i, later.Used[i], later.Free[i], later.Total[i], later.DrivesOnline[i], later.NodesOnline[i])
		}
	}
}

func TestCapacityDailySeries(t *testing.T) {
	for hours, want := range map[int]bool{24: true, 168: true, 720: true, 0: false, 48: false, -24: false, 721: false} {
		if got := capacityHistoryHours(hours); got != want {
			t.Errorf("capacityHistoryHours(%d) = %v, want %v", hours, got, want)
		}
	}
	now := absHour(hourBase())
	today := hourDay(now)
	yesterday := (today - 1) * 24
	var s capacityState
	s.record(yesterday+3, capacityUsage(100, map[string]uint64{"alpha": 50}), capacityDrives(2000, 1000, 4, 4, 2, 2), capacityKept)
	s.record(yesterday+20, capacityUsage(150, map[string]uint64{"alpha": 75}), capacityDrives(2000, 900, 3, 4, 1, 2), capacityKept)
	s.record(now-2, capacityUsage(300, map[string]uint64{"alpha": 150}), capacityDrives(2000, 700, 4, 4, 2, 2), capacityKept)

	checkCapacitySeries(t, "today", s.series(now, 720, nil), capacitySeries{
		Since:        capacityHourStart(yesterday),
		Step:         24 * time.Hour,
		Used:         []uint64{150, 300},
		Objects:      []uint64{15, 30},
		Buckets:      []uint64{1, 1},
		Free:         []uint64{900, 700},
		Total:        []uint64{2000, 2000},
		DrivesOnline: []uint64{3, 4},
		DrivesTotal:  []uint64{4, 4},
		NodesOnline:  []uint64{1, 2},
		NodesTotal:   []uint64{2, 2},
	})
	checkCapacitySeries(t, "two days later", s.series(now+48, 720, nil), capacitySeries{
		Since:        capacityHourStart(yesterday),
		Step:         24 * time.Hour,
		Used:         []uint64{150, 300, 300, 300},
		Objects:      []uint64{15, 30, 30, 30},
		Buckets:      []uint64{1, 1, 1, 1},
		Free:         []uint64{900, 700, 700, 700},
		Total:        []uint64{2000, 2000, 2000, 2000},
		DrivesOnline: []uint64{3, 4, 4, 4},
		DrivesTotal:  []uint64{4, 4, 4, 4},
		NodesOnline:  []uint64{1, 2, 2, 2},
		NodesTotal:   []uint64{2, 2, 2, 2},
	})
	checkCapacitySeries(t, "alpha two days later", s.series(now+48, 720, capacityScope{"alpha": 0}), capacitySeries{
		Since:   capacityHourStart(yesterday),
		Step:    24 * time.Hour,
		Used:    []uint64{75, 150, 150, 150},
		Objects: []uint64{7, 15, 15, 15},
		Buckets: []uint64{1, 1, 1, 1},
	})
}

func TestCapacityBucketTracking(t *testing.T) {
	now := absHour(hourBase())
	usage := map[string]uint64{}
	for i := 0; i < maxTrackedBuckets+1; i++ {
		usage[fmt.Sprintf("b%04d", i)] = 1
	}
	var s capacityState
	s.record(now, capacityUsage(1, usage), capacityBucket{}, capacityKept)
	if len(s.Buckets) != maxTrackedBuckets || !s.tracked("b0000") || !s.tracked("b0999") || s.tracked("b1000") {
		t.Fatalf("first sample tracks %d buckets (b0000 %v, b0999 %v, b1000 %v), want the first 1000 by name",
			len(s.Buckets), s.tracked("b0000"), s.tracked("b0999"), s.tracked("b1000"))
	}

	usage["a-new"] = 1
	s.record(now+1, capacityUsage(1, usage), capacityBucket{}, capacityKept)
	if s.tracked("a-new") || len(s.Buckets) != maxTrackedBuckets {
		t.Fatalf("full set took a new bucket: a-new %v, %d tracked", s.tracked("a-new"), len(s.Buckets))
	}

	delete(usage, "b0000")
	s.record(now+2, capacityUsage(1, usage), capacityBucket{}, capacityKept)
	if !s.tracked("b0000") {
		t.Fatal("bucket missing from the snapshot but still existing was dropped")
	}

	delete(usage, "b0001")
	s.record(now+3, capacityUsage(1, usage), capacityBucket{}, func(b string) bool { return b != "b0001" })
	if s.tracked("b0001") {
		t.Fatal("deleted bucket kept its series")
	}
	if !s.tracked("a-new") || s.tracked("b1000") || len(s.Buckets) != maxTrackedBuckets {
		t.Fatalf("after the drop: a-new %v, b1000 %v, %d tracked; want a-new tracked first by name", s.tracked("a-new"), s.tracked("b1000"), len(s.Buckets))
	}
	if got := s.Buckets["a-new"].FirstHour; got != now+3 {
		t.Fatalf("a-new FirstHour = %d, want %d", got, now+3)
	}
	if !s.tracked("b0000") {
		t.Fatal("existing bucket missing from the snapshot was evicted")
	}
}

func TestCapacityScopedSeries(t *testing.T) {
	now := absHour(hourBase())
	var s capacityState
	s.record(now-2, capacityUsage(300, map[string]uint64{"alpha": 100, "beta": 200}), capacityBucket{Free: 5000}, capacityKept)
	s.record(now-1, capacityUsage(360, map[string]uint64{"alpha": 110, "beta": 200, "gamma": 50}), capacityBucket{Free: 4900}, capacityKept)
	s.record(now, capacityUsage(400, map[string]uint64{"alpha": 120, "beta": 210, "gamma": 70}), capacityBucket{Free: 4800}, capacityKept)
	since := capacityHourStart(now - 2)

	checkCapacitySeries(t, "alpha", s.series(now, 24, capacityScope{"alpha": 0}), capacitySeries{
		Since:   since,
		Step:    time.Hour,
		Used:    []uint64{100, 110, 120},
		Objects: []uint64{10, 11, 12},
		Buckets: []uint64{1, 1, 1},
	})
	checkCapacitySeries(t, "alpha and gamma", s.series(now, 24, capacityScope{"alpha": 0, "gamma": 0}), capacitySeries{
		Since:   since,
		Step:    time.Hour,
		Used:    []uint64{100, 160, 190},
		Objects: []uint64{10, 16, 19},
		Buckets: []uint64{1, 2, 2},
	})
	checkCapacitySeries(t, "nothing listable", s.series(now, 24, capacityScope{}), capacitySeries{
		Since:   since,
		Step:    time.Hour,
		Used:    []uint64{0, 0, 0},
		Objects: []uint64{0, 0, 0},
		Buckets: []uint64{0, 0, 0},
	})
	checkCapacitySeries(t, "cluster", s.series(now, 24, nil), capacitySeries{
		Since:        since,
		Step:         time.Hour,
		Used:         []uint64{300, 360, 400},
		Objects:      []uint64{30, 36, 40},
		Buckets:      []uint64{2, 3, 3},
		Free:         []uint64{5000, 4900, 4800},
		Total:        capacityZeros(3),
		DrivesOnline: capacityZeros(3),
		DrivesTotal:  capacityZeros(3),
		NodesOnline:  capacityZeros(3),
		NodesTotal:   capacityZeros(3),
	})
}

// Recreated bucket hides hours and days before creation
func TestCapacityScopedSeriesCreated(t *testing.T) {
	now := absHour(hourBase())
	today := hourDay(now)
	var s capacityState
	s.record(now-3, capacityUsage(100, map[string]uint64{"alpha": 100}), capacityBucket{}, capacityKept)
	s.record(now-2, capacityUsage(110, map[string]uint64{"alpha": 110}), capacityBucket{}, capacityKept)
	s.record(now-1, capacityUsage(120, map[string]uint64{"alpha": 120}), capacityBucket{}, capacityKept)
	s.record(now, capacityUsage(130, map[string]uint64{"alpha": 130}), capacityBucket{}, capacityKept)

	checkCapacitySeries(t, "hourly", s.series(now, 24, capacityScope{"alpha": now - 1}), capacitySeries{
		Since:   capacityHourStart(now - 3),
		Step:    time.Hour,
		Used:    []uint64{0, 0, 120, 130},
		Objects: []uint64{0, 0, 12, 13},
		Buckets: []uint64{0, 0, 1, 1},
	})

	var d capacityState
	d.record((today-2)*24+5, capacityUsage(70, map[string]uint64{"alpha": 70}), capacityBucket{}, capacityKept)
	d.record((today-1)*24+5, capacityUsage(80, map[string]uint64{"alpha": 80}), capacityBucket{}, capacityKept)
	d.record(now, capacityUsage(90, map[string]uint64{"alpha": 90}), capacityBucket{}, capacityKept)
	checkCapacitySeries(t, "daily", d.series(now, 720, capacityScope{"alpha": (today-1)*24 + 2}), capacitySeries{
		Since:   capacityHourStart((today - 2) * 24),
		Step:    24 * time.Hour,
		Used:    []uint64{0, 80, 90},
		Objects: []uint64{0, 8, 9},
		Buckets: []uint64{0, 1, 1},
	})
}

func TestCapacityClockBack(t *testing.T) {
	now := absHour(hourBase())
	today := hourDay(now)
	var s capacityState
	s.record(now, capacityUsage(500, map[string]uint64{"alpha": 500}), capacityBucket{Free: 100}, capacityKept)
	s.record(now-200, capacityUsage(50, map[string]uint64{"alpha": 50}), capacityBucket{Free: 900}, capacityKept)
	if s.EpochHour != now {
		t.Fatalf("clock-back sample moved epoch to %d, want %d", s.EpochHour, now)
	}
	if got := s.CapRing[ringSlot(now-200, fleetRingSize)]; !got.Set || got.Used != 50 || got.Free != 900 {
		t.Fatalf("cluster slot of now-200 = %+v, want the 50 sample", got)
	}
	if got := s.CapRing[ringSlot(now, fleetRingSize)]; got.Used != 500 || got.Free != 100 {
		t.Fatalf("cluster slot of now = %+v, want the 500 sample", got)
	}
	a := s.Buckets["alpha"]
	if got := a.Hourly[ringSlot(now-200, capacityHourlySlots)].Size; got != 0 {
		t.Fatalf("hourly slot of now-200 holds %d; it is outside the 168h window", got)
	}

	s.record(now-800, capacityUsage(5, map[string]uint64{"alpha": 5}), capacityBucket{Free: 999}, capacityKept)
	if s.CapRing[ringSlot(now-800, fleetRingSize)].Set {
		t.Fatal("sample older than the 720h window was written")
	}
	if old := hourDay(now - 800); a.DailyDay[ringSlot(old, capacityDailySlots)] == old {
		t.Fatal("sample older than 30 days was written to the daily ring")
	}
	if got := a.Hourly[ringSlot(now, capacityHourlySlots)].Size; got != 500 {
		t.Fatalf("hourly slot of now = %d, want 500", got)
	}
	if slot := ringSlot(today, capacityDailySlots); a.DailyDay[slot] != today || a.Daily[slot].Size != 500 {
		t.Fatalf("daily slot of today = day %d size %d, want day %d size 500", a.DailyDay[slot], a.Daily[slot].Size, today)
	}

	used := make([]uint64, 168)
	for i := range used {
		used[i] = 50
	}
	used[167] = 500
	hourly := s.series(now, 168, nil)
	if !hourly.Since.Equal(capacityHourStart(now-167)) || !reflect.DeepEqual(hourly.Used, used) {
		t.Fatalf("168h series: since %v used %v, want since %v, 167 x 50 then 500", hourly.Since, hourly.Used, capacityHourStart(now-167))
	}
	checkCapacitySeries(t, "720h", s.series(now, 720, nil), capacitySeries{
		Since:        capacityHourStart((today - 8) * 24),
		Step:         24 * time.Hour,
		Used:         []uint64{50, 50, 50, 50, 50, 50, 50, 50, 500},
		Objects:      []uint64{5, 5, 5, 5, 5, 5, 5, 5, 50},
		Buckets:      []uint64{1, 1, 1, 1, 1, 1, 1, 1, 1},
		Free:         []uint64{900, 900, 900, 900, 900, 900, 900, 900, 100},
		Total:        capacityZeros(9),
		DrivesOnline: capacityZeros(9),
		DrivesTotal:  capacityZeros(9),
		NodesOnline:  capacityZeros(9),
		NodesTotal:   capacityZeros(9),
	})
}

func TestCapacityMemoryBudget(t *testing.T) {
	if size := unsafe.Sizeof(bucketSeries{}) * maxTrackedBuckets; size > 4_000_000 {
		t.Fatalf("%d tracked bucket series take %d bytes, budget 4000000", maxTrackedBuckets, size)
	}
}

// Older capacity file loads with zero drive fields
func TestCapacityHistoryOldShape(t *testing.T) {
	type oldBucket struct {
		Used, Objects, Buckets, Free uint64
		Set                          bool
	}
	type oldState struct {
		CapRing     [fleetRingSize]oldBucket
		EpochHour   int64
		Initialized bool
		Buckets     map[string]*bucketSeries
	}
	hour := absHour(hourBase())
	old := oldState{EpochHour: hour, Initialized: true, Buckets: map[string]*bucketSeries{"alpha": {FirstHour: hour}}}
	old.CapRing[ringSlot(hour, fleetRingSize)] = oldBucket{Used: 100, Objects: 10, Buckets: 1, Free: 900, Set: true}
	data, err := encodeHistory(&old)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := newCapacityState()
	if err := decodeHistory(data, &s); err != nil {
		t.Fatalf("decode an old-shape file: %v", err)
	}
	if got, want := s.CapRing[ringSlot(hour, fleetRingSize)], (capacityBucket{Used: 100, Objects: 10, Buckets: 1, Free: 900, Set: true}); got != want {
		t.Fatalf("old sample = %+v, want %+v", got, want)
	}
	if !s.Initialized || s.EpochHour != hour || !s.tracked("alpha") {
		t.Fatalf("old state: initialized %v epoch %d alpha %v, want true %d true", s.Initialized, s.EpochHour, s.tracked("alpha"), hour)
	}
	checkCapacitySeries(t, "old-shape series", s.series(hour, 24, nil), capacitySeries{
		Since:        capacityHourStart(hour),
		Step:         time.Hour,
		Used:         []uint64{100},
		Objects:      []uint64{10},
		Buckets:      []uint64{1},
		Free:         []uint64{900},
		Total:        capacityZeros(1),
		DrivesOnline: capacityZeros(1),
		DrivesTotal:  capacityZeros(1),
		NodesOnline:  capacityZeros(1),
		NodesTotal:   capacityZeros(1),
	})
}

func TestCapacityHistoryRoundTrip(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryRoundTrip)
}

// Second scan omits existing beta, ghost was deleted
func testCapacityHistoryRoundTrip(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	alpha := makeTestBucket(t, obj, BucketOptions{})
	beta := makeTestBucket(t, obj, BucketOptions{})
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	hour := absHour(now)

	a := newCapacityHistory()
	a.BecomeLeader(ctx, obj)
	a.Sample(ctx, obj, historyTestUsage(map[string]uint64{alpha: 60, beta: 30, "ghost": 40}), now.Add(-time.Hour))
	a.Sample(ctx, obj, historyTestUsage(map[string]uint64{alpha: 200}), now)
	if !a.state.tracked(alpha) || !a.state.tracked(beta) || a.state.tracked("ghost") {
		t.Fatalf("%s: tracked alpha %v beta %v ghost %v, want alpha and beta kept, ghost dropped",
			instanceType, a.state.tracked(alpha), a.state.tracked(beta), a.state.tracked("ghost"))
	}
	if b := a.state.CapRing[ringSlot(hour, fleetRingSize)]; b.Free == 0 || b.Total < b.Free || b.DrivesTotal == 0 || b.DrivesOnline != b.DrivesTotal || b.NodesTotal == 0 || b.NodesOnline != b.NodesTotal {
		t.Fatalf("%s: sampled drives = %+v, want free, total and every drive and node online", instanceType, b)
	}
	if err := a.SaveHistory(ctx, obj, now, false); err != nil {
		t.Fatalf("%s: save: %v", instanceType, err)
	}

	b := newCapacityHistory()
	b.BecomeLeader(ctx, obj)
	if st := b.HistoryStatus(); st != (historyStatus{}) {
		t.Fatalf("%s: status after load = %+v, want clean", instanceType, st)
	}
	if !reflect.DeepEqual(b.state, a.state) {
		t.Fatalf("%s: loaded state differs from the saved one", instanceType)
	}
	for _, hours := range []int{24, 168, 720} {
		want, got := a.state.series(hour, hours, nil), b.state.series(hour, hours, nil)
		if len(got.Used) == 0 || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %dh series = %+v, want %+v", instanceType, hours, got, want)
		}
	}
	scoped := capacityScope{alpha: 0}
	if want, got := a.state.series(hour, 24, scoped), b.state.series(hour, 24, scoped); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: scoped series = %+v, want %+v", instanceType, got, want)
	}
}

func TestHistoryLoadCorrupt(t *testing.T) {
	ExecObjectLayerTest(t, testHistoryLoadCorrupt)
}

// Torn save starts empty, flags error and is overwritten on next save
func testHistoryLoadCorrupt(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	good := newCapacityState()
	good.record(absHour(now)-5, historyTestUsage(map[string]uint64{"alpha": 50}), capacityBucket{Free: 1 << 30}, func(string) bool { return true })
	valid, err := encodeHistory(&good)
	if err != nil {
		t.Fatalf("%s: encode: %v", instanceType, err)
	}

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"zero-length", []byte{}},
		{"truncated", valid[:len(valid)/2]},
		{"version 2", append([]byte{2}, valid[1:]...)},
	} {
		if err := saveConfig(ctx, obj, capacityHistoryKey, tc.data); err != nil {
			t.Fatalf("%s: %s: write fixture: %v", instanceType, tc.name, err)
		}
		h := newCapacityHistory()
		h.BecomeLeader(ctx, obj)
		if h.state.Initialized || len(h.state.Buckets) != 0 {
			t.Fatalf("%s: %s: loaded state = %+v, want empty", instanceType, tc.name, h.state.Buckets)
		}
		if !h.HistoryStatus().LoadFailed {
			t.Fatalf("%s: %s: LoadFailed = false, want true", instanceType, tc.name)
		}

		h.Sample(ctx, obj, historyTestUsage(map[string]uint64{"beta": 70}), now)
		if err := h.SaveHistory(ctx, obj, now, false); err != nil {
			t.Fatalf("%s: %s: save after a corrupt load: %v", instanceType, tc.name, err)
		}
		if st := h.HistoryStatus(); st.LastSaveErr != nil {
			t.Fatalf("%s: %s: LastSaveErr = %v, want nil", instanceType, tc.name, st.LastSaveErr)
		}
		data, err := readConfig(ctx, obj, capacityHistoryKey)
		saved := newCapacityState()
		if err == nil {
			err = decodeHistory(data, &saved)
		}
		if err != nil || saved.EpochHour != absHour(now) || !saved.tracked("beta") || saved.tracked("alpha") {
			t.Fatalf("%s: %s: rewritten file = %v, epoch %d; want the new sample only", instanceType, tc.name, err, saved.EpochHour)
		}
	}
}

func TestCapacityHistoryLeaderHandover(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryLeaderHandover)
}

// New leader loads history before first sample
func testCapacityHistoryLeaderHandover(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	hour := absHour(now)

	a := newCapacityHistory()
	a.BecomeLeader(ctx, obj)
	a.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100}), now.Add(-3*time.Hour))
	if err := a.SaveHistory(ctx, obj, now.Add(-3*time.Hour), true); err != nil {
		t.Fatalf("%s: save: %v", instanceType, err)
	}

	b := newCapacityHistory()
	b.BecomeLeader(ctx, obj)
	b.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100, "beta": 300}), now)
	s := b.state.series(hour, 24, nil)
	if want := []uint64{100, 100, 100, 400}; !reflect.DeepEqual(s.Used, want) {
		t.Fatalf("%s: used = %v, want %v", instanceType, s.Used, want)
	}
	if want := []uint64{10, 10, 10, 40}; !reflect.DeepEqual(s.Objects, want) {
		t.Fatalf("%s: objects = %v, want %v", instanceType, s.Objects, want)
	}
	if want := []uint64{1, 1, 1, 2}; !reflect.DeepEqual(s.Buckets, want) {
		t.Fatalf("%s: buckets = %v, want %v", instanceType, s.Buckets, want)
	}
	if len(s.Free) != 4 || s.Free[1] != s.Free[0] || s.Free[2] != s.Free[0] {
		t.Fatalf("%s: free = %v, want A's value carried to H-1", instanceType, s.Free)
	}
	if !s.Since.Equal(time.Unix((hour-3)*3600, 0)) || s.Step != time.Hour {
		t.Fatalf("%s: since %v step %v, want %v and 1h", instanceType, s.Since, s.Step, time.Unix((hour-3)*3600, 0).UTC())
	}
}

func TestCapacityHistoryResignLeader(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryResignLeader)
}

func TestCapacityHistoryHandoverDuringBlockedSave(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryHandoverDuringBlockedSave)
}

func testCapacityHistoryHandoverDuringBlockedSave(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	a := newCapacityHistory()
	if err := a.BecomeLeader(ctx, obj); err != nil {
		t.Fatal(err)
	}
	a.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100}), now)
	p := &pausedStoreLayer{ObjectLayer: obj, key: capacityHistoryKey, entered: make(chan context.Context, 1), release: make(chan struct{})}
	defer p.unblock()
	oldSave := make(chan error, 1)
	go func() { oldSave <- a.SaveHistory(ctx, p, now, true) }()
	writeCtx := awaitStoreWrite(t, p.entered)
	a.ResignLeader()
	select {
	case <-writeCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: resignation did not cancel the in-flight save", instanceType)
	}

	// Separate store waits for old write, even if backing layer ignores cancellation
	b := newCapacityHistory()
	observed := observedHistoryLayer{ObjectLayer: obj, locking: make(chan context.Context, 2)}
	newSave := make(chan error, 1)
	go func() {
		if err := b.BecomeLeader(ctx, observed); err != nil {
			newSave <- err
			return
		}
		b.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 500}), now.Add(time.Hour))
		newSave <- b.SaveHistory(ctx, observed, now.Add(time.Hour), true)
	}()
	awaitStoreWrite(t, observed.locking)
	select {
	case err := <-newSave:
		t.Fatalf("%s: new leader overtook a blocked former-leader write: %v", instanceType, err)
	case <-time.After(50 * time.Millisecond):
	}
	p.unblock()
	if err := awaitStoreSave(t, oldSave); err != nil {
		t.Fatal(err)
	}
	if err := awaitStoreSave(t, newSave); err != nil {
		t.Fatal(err)
	}
	saved := readCapacityState(ctx, obj)
	if saved.EpochHour != absHour(now.Add(time.Hour)) || !reflect.DeepEqual(saved.series(absHour(now.Add(time.Hour)), 24, nil).Used, []uint64{100, 500}) {
		t.Fatalf("%s: handover history = hour %d, series %v", instanceType, saved.EpochHour, saved.series(absHour(now.Add(time.Hour)), 24, nil).Used)
	}
	if a.HistoryStatus() != (historyStatus{}) {
		t.Fatalf("%s: former save restored resigned status: %+v", instanceType, a.HistoryStatus())
	}
}

func TestCapacityHistorySaveUsesScannerLease(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistorySaveUsesScannerLease)
}

func TestCapacityHistoryOverlappingSaves(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryOverlappingSaves)
}

func testCapacityHistoryOverlappingSaves(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	h := newCapacityHistory()
	if err := h.BecomeLeader(ctx, obj); err != nil {
		t.Fatal(err)
	}
	h.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100}), now)
	p := &pausedStoreLayer{ObjectLayer: obj, key: capacityHistoryKey, entered: make(chan context.Context, 1), release: make(chan struct{})}
	defer p.unblock()
	oldSave := make(chan error, 1)
	go func() { oldSave <- h.SaveHistory(ctx, p, now, false) }()
	awaitStoreWrite(t, p.entered)
	h.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 500}), now.Add(time.Hour))
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := h.SaveHistory(deadline, obj, now.Add(time.Hour), true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s: blocked forced save = %v, want deadline exceeded", instanceType, err)
	}
	observed := observedHistoryLayer{ObjectLayer: obj, locking: make(chan context.Context, 1)}
	newSave := make(chan error, 1)
	go func() { newSave <- h.SaveHistory(ctx, observed, now.Add(time.Hour), true) }()
	awaitStoreWrite(t, observed.locking)
	select {
	case err := <-newSave:
		t.Fatalf("%s: forced save overtook the periodic save: %v", instanceType, err)
	case <-time.After(50 * time.Millisecond):
	}
	p.unblock()
	if err := awaitStoreSave(t, oldSave); err != nil {
		t.Fatal(err)
	}
	if err := awaitStoreSave(t, newSave); err != nil {
		t.Fatal(err)
	}
	saved := readCapacityState(ctx, obj)
	if saved.EpochHour != absHour(now.Add(time.Hour)) || saved.CapRing[ringSlot(saved.EpochHour, fleetRingSize)].Used != 500 || h.file.dirty || !h.file.lastSave.Equal(now.Add(time.Hour)) {
		t.Fatalf("%s: saved hour %d, dirty %t, lastSave %v", instanceType, saved.EpochHour, h.file.dirty, h.file.lastSave)
	}
}

func testCapacityHistorySaveUsesScannerLease(obj ObjectLayer, instanceType string, t TestErrHandler) {
	lease, loseLease := context.WithCancel(context.Background())
	defer loseLease()
	h := newCapacityHistory()
	if err := h.BecomeLeader(lease, obj); err != nil {
		t.Fatal(err)
	}
	now := hourBase()
	h.Sample(context.Background(), obj, historyTestUsage(map[string]uint64{"alpha": 100}), now)
	p := &pausedStoreLayer{ObjectLayer: obj, key: capacityHistoryKey, entered: make(chan context.Context, 1), release: make(chan struct{}), cancelAware: true}
	defer p.unblock()
	done := make(chan error, 1)
	go func() { done <- h.SaveHistory(context.Background(), p, now, true) }()
	writeCtx := awaitStoreWrite(t, p.entered)
	// Lease loss cancels I/O before scanner can clear leadership
	loseLease()
	if err := awaitStoreSave(t, done); err != nil {
		t.Fatalf("%s: former-leader save returned a persistence fault: %v", instanceType, err)
	}
	if !errors.Is(writeCtx.Err(), context.Canceled) {
		t.Fatalf("%s: lease loss did not cancel the blocked disk write", instanceType)
	}
	if _, err := readConfig(context.Background(), obj, capacityHistoryKey); !errors.Is(err, errConfigNotFound) {
		t.Fatalf("%s: canceled leader save reached disk: %v", instanceType, err)
	}
	h.Sample(context.Background(), obj, historyTestUsage(map[string]uint64{"alpha": 999}), now.Add(time.Hour))
	if err := h.SaveHistory(context.Background(), obj, now.Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if h.state.EpochHour != absHour(now) {
		t.Fatalf("%s: sampling after lease loss changed the state", instanceType)
	}
	if _, err := readConfig(context.Background(), obj, capacityHistoryKey); !errors.Is(err, errConfigNotFound) {
		t.Fatalf("%s: forced shutdown save after lease loss reached disk: %v", instanceType, err)
	}
}

// Lost scanner lock stops writes, former leader reads as follower
func testCapacityHistoryResignLeader(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	hour := absHour(now)

	a := newCapacityHistory()
	a.BecomeLeader(ctx, obj)
	a.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100}), now)
	if err := a.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: save: %v", instanceType, err)
	}
	a.ResignLeader()

	b := newCapacityHistory()
	b.BecomeLeader(ctx, obj)
	b.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 500}), now.Add(time.Hour))
	if err := b.SaveHistory(ctx, obj, now.Add(time.Hour), true); err != nil {
		t.Fatalf("%s: new leader save: %v", instanceType, err)
	}

	later := now.Add(2 * time.Hour)
	a.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 1}), later)
	if err := a.SaveHistory(ctx, obj, later, true); err != nil {
		t.Fatalf("%s: ex-leader save: %v", instanceType, err)
	}
	saved := newCapacityState()
	data, err := readConfig(ctx, obj, capacityHistoryKey)
	if err == nil {
		err = decodeHistory(data, &saved)
	}
	if err != nil || saved.EpochHour != hour+1 {
		t.Fatalf("%s: capacity.bin epoch = %d (%v), want the new leader's %d", instanceType, saved.EpochHour, err, hour+1)
	}
	var got []uint64
	a.view(ctx, obj, later, func(s *capacityState) { got = s.series(hour+1, 24, nil).Used })
	if want := []uint64{100, 500}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: ex-leader view = %v, want the saved file's %v", instanceType, got, want)
	}
}

func TestCapacityHistoryResignClearsStatus(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryResignClearsStatus)
}

// Former leader's file faults leave bell
func testCapacityHistoryResignClearsStatus(obj ObjectLayer, instanceType string, t TestErrHandler) {
	h := newCapacityHistory()
	h.BecomeLeader(context.Background(), obj)
	h.file.loadFailed, h.file.unread, h.file.lastSaveErr = true, true, errors.New("disk full")
	if got := h.HistoryStatus(); !got.LoadFailed || got.LastSaveErr == nil {
		t.Fatalf("%s: leader status = %+v, want both faults", instanceType, got)
	}
	h.ResignLeader()
	if got := h.HistoryStatus(); got != (historyStatus{}) {
		t.Fatalf("%s: ex-leader status = %+v, want historyStatus{}", instanceType, got)
	}
	if h.file.unread || h.file.key != capacityHistoryKey {
		t.Fatalf("%s: ex-leader file = %+v, want unread cleared and key %q kept", instanceType, h.file, capacityHistoryKey)
	}
}

func TestCapacityHistoryCleanStoreNoSave(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryCleanStoreNoSave)
}

// Save dirty sample or hour advance, hourly unless forced
func testCapacityHistoryCleanStoreNoSave(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	usage := historyTestUsage(map[string]uint64{"alpha": 100})
	missing := func(step string) {
		t.Helper()
		if _, err := readConfig(ctx, obj, capacityHistoryKey); !errors.Is(err, errConfigNotFound) {
			t.Fatalf("%s: %s: capacity.bin read = %v, want %v", instanceType, step, err, errConfigNotFound)
		}
	}
	saveAt := func(h *capacityHistory, at time.Time, force bool, wantLastSave time.Time, step string) {
		t.Helper()
		if err := h.SaveHistory(ctx, obj, at, force); err != nil {
			t.Fatalf("%s: %s: save: %v", instanceType, step, err)
		}
		if !h.file.lastSave.Equal(wantLastSave) {
			t.Fatalf("%s: %s: lastSave = %v, want %v", instanceType, step, h.file.lastSave, wantLastSave)
		}
	}

	follower := newCapacityHistory()
	follower.Sample(ctx, obj, usage, now)
	if follower.state.Initialized || follower.file.dirty {
		t.Fatalf("%s: a non-leader Sample changed the store", instanceType)
	}
	saveAt(follower, now, true, time.Time{}, "non-leader forced save")
	missing("non-leader forced save")

	h := newCapacityHistory()
	h.BecomeLeader(ctx, obj)
	if st := h.HistoryStatus(); st != (historyStatus{}) {
		t.Fatalf("%s: status with no file = %+v, want clean", instanceType, st)
	}
	saveAt(h, now, true, time.Time{}, "leader with no samples")
	missing("leader with no samples")

	h.Sample(ctx, obj, usage, now)
	saveAt(h, now, false, now, "first sample")
	saveAt(h, now.Add(10*time.Minute), false, now, "clean store")
	h.Sample(ctx, obj, usage, now.Add(20*time.Minute))
	saveAt(h, now.Add(20*time.Minute), false, now, "dirty 20 min after the last save")
	if !h.file.dirty {
		t.Fatalf("%s: store clean after an unsaved sample", instanceType)
	}
	saveAt(h, now.Add(20*time.Minute), true, now.Add(20*time.Minute), "forced")
	saveAt(h, now.Add(3*time.Hour), false, now.Add(3*time.Hour), "hour advanced")

	data, err := readConfig(ctx, obj, capacityHistoryKey)
	saved := newCapacityState()
	if err == nil {
		err = decodeHistory(data, &saved)
	}
	if err != nil || saved.EpochHour != absHour(now.Add(3*time.Hour)) {
		t.Fatalf("%s: saved epoch = %d (%v), want %d", instanceType, saved.EpochHour, err, absHour(now.Add(3*time.Hour)))
	}
}

func TestCapacityHistoryFollowerView(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryFollowerView)
}

// Leader reads live ring, followers cache file for a minute
func testCapacityHistoryFollowerView(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	hour := absHour(now)
	used := func(h *capacityHistory, at time.Time) []uint64 {
		var out []uint64
		h.view(ctx, obj, at, func(s *capacityState) { out = s.series(hour, 24, nil).Used })
		return out
	}

	early := newCapacityHistory()
	if got := used(early, now); len(got) != 0 {
		t.Fatalf("%s: follower with no file = %v, want no points", instanceType, got)
	}

	leader := newCapacityHistory()
	leader.BecomeLeader(ctx, obj)
	leader.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100}), now)
	if err := leader.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: save: %v", instanceType, err)
	}
	follower := newCapacityHistory()
	if got := used(follower, now); !reflect.DeepEqual(got, []uint64{100}) {
		t.Fatalf("%s: follower = %v, want [100]", instanceType, got)
	}

	leader.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 200}), now)
	if got := used(leader, now); !reflect.DeepEqual(got, []uint64{200}) {
		t.Fatalf("%s: leader before save = %v, want [200]", instanceType, got)
	}
	if err := leader.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: second save: %v", instanceType, err)
	}
	if got := used(follower, now.Add(30*time.Second)); !reflect.DeepEqual(got, []uint64{100}) {
		t.Fatalf("%s: follower at +30s = %v, want the cached [100]", instanceType, got)
	}
	if got := used(follower, now.Add(61*time.Second)); !reflect.DeepEqual(got, []uint64{200}) {
		t.Fatalf("%s: follower at +61s = %v, want [200]", instanceType, got)
	}

	if err := saveConfig(ctx, obj, capacityHistoryKey, []byte{historyFormatVersion, 0xff}); err != nil {
		t.Fatalf("%s: write corrupt file: %v", instanceType, err)
	}
	late := newCapacityHistory()
	if got := used(late, now); len(got) != 0 {
		t.Fatalf("%s: follower on a corrupt file = %v, want no points", instanceType, got)
	}
	if st := late.HistoryStatus(); st != (historyStatus{}) {
		t.Fatalf("%s: follower status = %+v, want clean (only the leader reports load failures)", instanceType, st)
	}
}

// Seed 48 hours of history in stopped FS server
func TestSeedCapacityHistoryFile(t *testing.T) {
	dir := os.Getenv("OBSTOR_SEED_FS")
	if dir == "" {
		t.Skip("set OBSTOR_SEED_FS to a stopped FS server's data dir to seed capacity history")
	}
	obj, err := NewFSObjectLayer(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	defer obj.Shutdown(context.Background())

	const mib, gib = 1 << 20, 1 << 30
	now := absHour(time.Now())
	s := newCapacityState()
	for i := uint64(0); i < 48; i++ {
		alpha := madmin.BucketUsageInfo{Size: (64 + 8*i) * mib, ObjectsCount: 40 + 5*i}
		beta := madmin.BucketUsageInfo{Size: (16 + 2*i) * mib, ObjectsCount: 10 + i}
		dui := madmin.DataUsageInfo{
			ObjectsTotalSize:  alpha.Size + beta.Size,
			ObjectsTotalCount: alpha.ObjectsCount + beta.ObjectsCount,
			BucketsCount:      2,
			BucketsUsage:      map[string]madmin.BucketUsageInfo{"alpha": alpha, "beta": beta},
		}
		s.record(now-47+int64(i), dui, capacityBucket{Total: 1024 * gib, Free: (400 - i) * gib, DrivesOnline: 1, DrivesTotal: 1, NodesOnline: 1, NodesTotal: 1}, func(string) bool { return true })
	}
	data, err := encodeHistory(&s)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := saveConfig(context.Background(), obj, capacityHistoryKey, data); err != nil {
		t.Fatalf("save %s: %v", capacityHistoryKey, err)
	}
	t.Logf("seeded 48 hours of capacity history into %s", dir)
}

func TestStoreDataUsageSamplesCapacity(t *testing.T) {
	ExecObjectLayerTest(t, testStoreDataUsageSamplesCapacity)
}

// Scanner leader samples each stored usage snapshot
func testStoreDataUsageSamplesCapacity(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	saved := globalCapacityHistory
	defer func() { globalCapacityHistory = saved }()
	globalCapacityHistory = newCapacityHistory()
	ctx := context.Background()
	globalCapacityHistory.BecomeLeader(ctx, obj)

	bucket := makeTestBucket(t, obj, BucketOptions{})
	snap := madmin.DataUsageInfo{
		LastUpdate:        time.Now().UTC(),
		ObjectsTotalCount: 3,
		ObjectsTotalSize:  300,
		BucketsCount:      1,
		BucketsUsage:      map[string]madmin.BucketUsageInfo{bucket: {Size: 300, ObjectsCount: 3}},
	}
	ch := make(chan madmin.DataUsageInfo, 1)
	ch <- snap
	close(ch)
	storeDataUsageInBackend(ctx, obj, ch)

	var got capacitySeries
	now := time.Now()
	globalCapacityHistory.view(ctx, obj, now, func(s *capacityState) {
		got = s.series(absHour(now), 24, nil)
	})
	n := len(got.Used)
	if n == 0 || len(got.Objects) != n || len(got.Buckets) != n || len(got.Free) != n {
		t.Fatalf("%s: series lengths used %d objects %d buckets %d free %d, want equal and > 0",
			instanceType, n, len(got.Objects), len(got.Buckets), len(got.Free))
	}
	if got.Used[n-1] != 300 || got.Objects[n-1] != 3 || got.Buckets[n-1] != 1 {
		t.Fatalf("%s: last point used %d objects %d buckets %d, want 300 3 1",
			instanceType, got.Used[n-1], got.Objects[n-1], got.Buckets[n-1])
	}
	if got.Free[n-1] == 0 {
		t.Fatalf("%s: last point free = 0, want the drives' free space", instanceType)
	}
	for name, v := range map[string][]uint64{"total": got.Total, "drivesOnline": got.DrivesOnline, "drivesTotal": got.DrivesTotal, "nodesOnline": got.NodesOnline, "nodesTotal": got.NodesTotal} {
		if len(v) != n || v[n-1] == 0 {
			t.Fatalf("%s: %s = %v, want %d points ending above 0", instanceType, name, v, n)
		}
	}
	loaded, err := loadDataUsageFromBackend(ctx, obj)
	if err != nil || loaded.ObjectsTotalSize != 300 || loaded.BucketsCount != 1 {
		t.Fatalf("%s: .usage.json = %+v, %v; want the stored snapshot", instanceType, loaded, err)
	}
}

// Cancel scanner lock ctx to simulate lease loss
type scannerLockLayer struct {
	ObjectLayer
	lockCtx chan context.CancelFunc
}

func (l scannerLockLayer) NewNSLock(bucket string, objects ...string) RWLocker {
	if bucket == obstorMetaBucket && len(objects) == 1 && objects[0] == "runDataScanner.lock" {
		return scannerLock{l.lockCtx}
	}
	return l.ObjectLayer.NewNSLock(bucket, objects...)
}

type scannerLock struct{ lockCtx chan context.CancelFunc }

func (l scannerLock) GetLock(ctx context.Context, _ *dynamicTimeout) (context.Context, error) {
	ctx, cancel := context.WithCancel(ctx)
	l.lockCtx <- cancel
	return ctx, nil
}
func (scannerLock) Unlock() {}
func (l scannerLock) GetRLock(ctx context.Context, t *dynamicTimeout) (context.Context, error) {
	return l.GetLock(ctx, t)
}
func (scannerLock) RUnlock() {}

func TestDataScannerCapacityLeadership(t *testing.T) {
	ExecObjectLayerTest(t, testDataScannerCapacityLeadership)
}

// Lease loss resigns leadership, shutdown keeps lease for final save
func testDataScannerCapacityLeadership(obj ObjectLayer, instanceType string, t TestErrHandler) {
	saved, savedCycle := globalCapacityHistory, scannerCycle.Get()
	defer func() { globalCapacityHistory = saved; scannerCycle.Update(savedCycle) }()
	scannerCycle.Update(time.Hour)

	isLeader := func() bool {
		globalCapacityHistory.mu.Lock()
		defer globalCapacityHistory.mu.Unlock()
		return globalCapacityHistory.leader
	}
	// Run scanner until stop, return final leadership state
	var finalLockCancel context.CancelFunc
	run := func(stop func(parentCancel, lockCancel context.CancelFunc)) bool {
		globalCapacityHistory = newCapacityHistory()
		layer := scannerLockLayer{obj, make(chan context.CancelFunc, 1)}
		parent, parentCancel := context.WithCancel(context.Background())
		defer parentCancel()
		done := make(chan struct{})
		go func() { runDataScanner(parent, layer); close(done) }()
		lockCancel := <-layer.lockCtx
		finalLockCancel = lockCancel
		deadline := time.Now().Add(10 * time.Second)
		for !isLeader() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: scanner never took capacity leadership", instanceType)
			}
			time.Sleep(time.Millisecond)
		}
		stop(parentCancel, lockCancel)
		<-done
		return isLeader()
	}

	if run(func(_, lockCancel context.CancelFunc) { lockCancel() }) {
		t.Fatalf("%s: still leader after losing the scanner lock", instanceType)
	}
	if run(func(parentCancel, lockCancel context.CancelFunc) { parentCancel(); lockCancel() }) {
		t.Fatalf("%s: lease loss racing shutdown kept capacity leadership", instanceType)
	}

	if !run(func(parentCancel, _ context.CancelFunc) { parentCancel() }) {
		t.Fatalf("%s: shutdown dropped capacity leadership before the final save", instanceType)
	}
	defer globalCapacityHistory.ResignLeader()
	if globalCapacityHistory.leaderCtx.Err() != nil {
		t.Fatalf("%s: graceful shutdown canceled the lease before its final save", instanceType)
	}
	ctx := context.Background()
	now := time.Now()
	globalCapacityHistory.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 100}), now)
	saveHistories(ctx, obj, now, true)
	got := readCapacityState(ctx, obj)
	if !got.Initialized {
		t.Fatalf("%s: final forced save after shutdown wrote nothing", instanceType)
	}
	finalLockCancel()
	globalCapacityHistory.Sample(ctx, obj, historyTestUsage(map[string]uint64{"alpha": 999}), now.Add(time.Hour))
	saveHistories(ctx, obj, now.Add(time.Hour), true)
	if after := readCapacityState(ctx, obj); after.EpochHour != got.EpochHour {
		t.Fatalf("%s: shutdown saved capacity after its retained lease was lost", instanceType)
	}
}
