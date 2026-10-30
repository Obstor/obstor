package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func hourBase() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) }

func TestFoldAccumulatesCurrentHour(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	s.Fold(Deltas{InBytes: 100, OutBytes: 10, Reads: 3, Writes: 1}, now)
	s.Fold(Deltas{InBytes: 50, OutBytes: 5, Reads: 2, Writes: 1}, now.Add(time.Minute))
	bw, ops := s.Series()
	last := bw[len(bw)-1]
	if last.In != 150 || last.Out != 15 {
		t.Fatalf("bw current hour = %+v, want In150 Out15", last)
	}
	if ops[len(ops)-1].Read != 5 || ops[len(ops)-1].Write != 2 {
		t.Fatalf("ops current hour = %+v, want R5 W2", ops[len(ops)-1])
	}
}

func TestFoldRolloverZeroesElapsedHours(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	s.Fold(Deltas{InBytes: 100}, now)
	// Two hours later, hour is zeroed
	s.Fold(Deltas{InBytes: 7}, now.Add(2*time.Hour))
	bw, _ := s.Series()
	if bw[len(bw)-1].In != 7 {
		t.Fatalf("current hour In = %d, want 7", bw[len(bw)-1].In)
	}
	// The intermediate hour must be cleared
	if bw[len(bw)-2].In != 0 {
		t.Fatalf("hour -1 (skipped) In = %d, want 0", bw[len(bw)-2].In)
	}
}

func TestSeriesAlwaysTwentyFour(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	bw, ops := s.Series()
	if len(bw) != 24 || len(ops) != 24 {
		t.Fatalf("series lengths = %d/%d, want 24/24", len(bw), len(ops))
	}
}

func TestSeriesWindowRanges(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	for hour := 0; hour < 720; hour++ {
		value := uint64(hour + 1)
		s.Fold(Deltas{InBytes: value, OutBytes: 2 * value, Reads: 3 * value, Writes: 4 * value}, now.Add(time.Duration(hour-719)*time.Hour))
	}
	for _, windowHours := range []int{24, 168, 720} {
		t.Run(fmt.Sprint(windowHours), func(t *testing.T) {
			bw, ops := s.SeriesWindow(now, windowHours)
			if len(bw) != windowHours || len(ops) != windowHours {
				t.Fatalf("series lengths = %d/%d, want %d/%d", len(bw), len(ops), windowHours, windowHours)
			}
			for hour := range bw {
				value := uint64(720 - windowHours + hour + 1)
				if bw[hour] != (BWBucket{In: value, Out: 2 * value}) || ops[hour] != (OpsBucket{Read: 3 * value, Write: 4 * value}) {
					t.Fatalf("hour %d = %+v/%+v, want value %d in chronological order", hour, bw[hour], ops[hour], value)
				}
			}
		})
	}
}

func TestSeriesWindowDefaultsToTwentyFour(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	for _, windowHours := range []int{0, -1, 1, 25, 721} {
		bw, ops := s.SeriesWindow(hourBase(), windowHours)
		if len(bw) != 24 || len(ops) != 24 {
			t.Fatalf("window %d: series lengths = %d/%d, want 24/24", windowHours, len(bw), len(ops))
		}
	}
}

func TestSeriesWindowExpiresWithoutReports(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	s.Fold(Deltas{InBytes: 10, OutBytes: 20, Reads: 30, Writes: 40}, now)

	// Reading expires hours even without reports
	bw, ops := s.SeriesWindow(now.Add(24*time.Hour), 24)
	for hour := range bw {
		if bw[hour] != (BWBucket{}) || ops[hour] != (OpsBucket{}) {
			t.Fatalf("expired daily hour %d = %+v/%+v, want zero", hour, bw[hour], ops[hour])
		}
	}
	bw, ops = s.SeriesWindow(now.Add(24*time.Hour), 720)
	if bw[695].In != 10 || ops[695].Read != 30 {
		t.Fatalf("monthly hour at -24h = %+v/%+v, want retained sample", bw[695], ops[695])
	}
	bw, ops = s.SeriesWindow(now.Add(720*time.Hour), 720)
	for hour := range bw {
		if bw[hour] != (BWBucket{}) || ops[hour] != (OpsBucket{}) {
			t.Fatalf("expired monthly hour %d = %+v/%+v, want zero", hour, bw[hour], ops[hour])
		}
	}
}

func TestFoldLateReportsRespectRetention(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	s.SeriesWindow(now, 720)
	s.Fold(Deltas{InBytes: 10, Reads: 20}, now)
	// Expired report shares current hour's ring slot
	s.Fold(Deltas{InBytes: 1000, Reads: 2000}, now.Add(-720*time.Hour))
	// Retained reports still reach their original hour
	s.Fold(Deltas{InBytes: 30, Reads: 40}, now.Add(-719*time.Hour))
	bw, ops := s.SeriesWindow(now, 720)
	if bw[719].In != 10 || ops[719].Read != 20 {
		t.Fatalf("current hour = %+v/%+v, want original sample without expired report", bw[719], ops[719])
	}
	if bw[0].In != 30 || ops[0].Read != 40 {
		t.Fatalf("oldest retained hour = %+v/%+v, want accepted late sample", bw[0], ops[0])
	}
	// Advancing one hour evicts oldest sample only
	bw, ops = s.SeriesWindow(now.Add(time.Hour), 720)
	if bw[0] != (BWBucket{}) || ops[0] != (OpsBucket{}) || bw[718].In != 10 || bw[719] != (BWBucket{}) {
		t.Fatalf("after rollover: oldest=%+v/%+v previous=%+v current=%+v", bw[0], ops[0], bw[718], bw[719])
	}
}

func TestRingSince(t *testing.T) {
	base := hourBase()
	h := absHour(base)
	stamp := func(hour int64) string { return time.Unix(hour*3600, 0).UTC().Format(time.RFC3339) }

	s := newInMemFleetStore(45*time.Second, "")
	if got := s.Since(base); !got.IsZero() {
		t.Fatalf("empty store Since = %v, want zero", got)
	}
	// Reading empty ring does not start it
	s.SeriesWindow(base, 24)
	if got := s.Since(base); !got.IsZero() {
		t.Fatalf("Since after a read on an empty store = %v, want zero", got)
	}

	s.Fold(Deltas{InBytes: 1}, base.Add(20*time.Minute))
	if got := s.ring.sinceHour(); got != h {
		t.Fatalf("sinceHour after the first fold = %d, want %d", got, h)
	}
	s.Fold(Deltas{InBytes: 1}, base.Add(10*time.Hour))
	if got := s.ring.sinceHour(); got != h {
		t.Fatalf("sinceHour after a later fold = %d, want %d", got, h)
	}
	if got := s.Since(base.Add(10 * time.Hour)).Format(time.RFC3339); got != stamp(h) {
		t.Fatalf("Since = %s, want %s in UTC", got, stamp(h))
	}

	// Since follows oldest retained hour
	s.SeriesWindow(base.Add(729*time.Hour), 720)
	if got, want := s.ring.sinceHour(), s.ring.epochHour-fleetRingSize+1; got != want || want != h+10 {
		t.Fatalf("sinceHour after the window slid = %d, want %d (h+10)", got, want)
	}

	// Gap longer than ring resets Since to current hour
	s.SeriesWindow(base.Add(1529*time.Hour), 720)
	if got := s.ring.sinceHour(); got != s.ring.epochHour || got != h+1529 {
		t.Fatalf("sinceHour after a full reset = %d, want epochHour %d (h+1529)", got, s.ring.epochHour)
	}

	// Late report moves Since back
	s.Fold(Deltas{InBytes: 1}, base.Add(1524*time.Hour))
	if got := s.ring.sinceHour(); got != h+1524 {
		t.Fatalf("sinceHour after a late report = %d, want %d", got, h+1524)
	}

	// Expired report leaves Since unchanged
	s.Fold(Deltas{InBytes: 1}, base.Add(809*time.Hour))
	if got := s.ring.sinceHour(); got != h+1524 {
		t.Fatalf("sinceHour after an expired report = %d, want %d", got, h+1524)
	}
}

func TestMembersLiveness(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	s.Upsert(MemberSnapshot{ID: "a", Used: 10}, now)
	s.Upsert(MemberSnapshot{ID: "b", Used: 20}, now.Add(-2*time.Minute))
	views := s.Members(now)
	got := map[string]bool{}
	for _, v := range views {
		got[v.ID] = v.Online
	}
	if !got["a"] {
		t.Fatal("member a should be online")
	}
	if got["b"] {
		t.Fatal("member b should be stale (offline)")
	}
}

func TestUpsertReplacesSnapshot(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	now := hourBase()
	s.Upsert(MemberSnapshot{ID: "a", Used: 10}, now)
	s.Upsert(MemberSnapshot{ID: "a", Used: 99}, now.Add(time.Second))
	views := s.Members(now.Add(time.Second))
	if len(views) != 1 || views[0].Used != 99 {
		t.Fatalf("views = %+v, want single Used99", views)
	}
}

func TestMintEnrollTokenUniqueAndNonEmpty(t *testing.T) {
	s := newInMemFleetStore(45*time.Second, "")
	t1 := s.MintEnrollToken(hourBase())
	t2 := s.MintEnrollToken(hourBase())
	if t1 == "" || t2 == "" || t1 == t2 {
		t.Fatalf("tokens not unique/non-empty: %q %q", t1, t2)
	}
}

func trafficInSum(bw []BWBucket) uint64 {
	var n uint64
	for _, b := range bw {
		n += b.In
	}
	return n
}

func TestTrafficHistoryNodes(t *testing.T) {
	ExecObjectLayerTest(t, testTrafficHistoryNodes)
}

// Each controller saves and loads its own traffic file
func testTrafficHistoryNodes(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	nodes := []string{"node1:9000", "node2:9000"}
	want := []uint64{300, 3000}
	since := make([]time.Time, len(nodes))
	if trafficHistoryKey(nodes[0]) == trafficHistoryKey(nodes[1]) {
		t.Fatalf("%s: both nodes map to %s", instanceType, trafficHistoryKey(nodes[0]))
	}
	for i, node := range nodes {
		s := newInMemFleetStore(45*time.Second, trafficHistoryKey(node))
		s.Fold(Deltas{InBytes: want[i] / 3}, now.Add(-2*time.Hour))
		s.Fold(Deltas{InBytes: want[i] - want[i]/3}, now)
		since[i] = s.Since(now)
		if err := s.SaveHistory(ctx, obj, now, true); err != nil {
			t.Fatalf("%s: save %s: %v", instanceType, node, err)
		}
		if st := s.HistoryStatus(); st.LastSaveErr != nil || s.file.dirty {
			t.Fatalf("%s: %s after save: status %+v, dirty %v", instanceType, node, st, s.file.dirty)
		}
	}
	for i, node := range nodes {
		s := newInMemFleetStore(45*time.Second, trafficHistoryKey(node))
		s.LoadHistory(ctx, obj)
		if s.HistoryStatus().LoadFailed {
			t.Fatalf("%s: %s load failed", instanceType, node)
		}
		bw, _ := s.SeriesWindow(now, 24)
		if got := trafficInSum(bw); got != want[i] {
			t.Fatalf("%s: %s loaded In sum = %d, want %d", instanceType, node, got, want[i])
		}
		if got := s.Since(now); !got.Equal(since[i]) {
			t.Fatalf("%s: %s loaded Since = %v, want %v", instanceType, node, got, since[i])
		}
	}
}

func TestTrafficHistoryCorrupt(t *testing.T) {
	ExecObjectLayerTest(t, testTrafficHistoryCorrupt)
}

// Bad traffic file starts empty, next save overwrites
func testTrafficHistoryCorrupt(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	good := newInMemFleetStore(45*time.Second, "")
	good.Fold(Deltas{InBytes: 77}, now)
	valid, err := encodeHistory(good.ring.state())
	if err != nil {
		t.Fatalf("%s: encode: %v", instanceType, err)
	}
	wrongVersion := append([]byte{historyFormatVersion + 1}, valid[1:]...)
	for _, tc := range []struct {
		name       string
		data       []byte
		wantFailed bool
	}{
		// Empty object means torn save
		{"empty", []byte{}, true},
		{"truncated", valid[:len(valid)/2], true},
		{"version-2", wrongVersion, true},
	} {
		key := trafficHistoryKey("corrupt-" + tc.name + ":9000")
		if err := saveConfig(ctx, obj, key, tc.data); err != nil {
			t.Fatalf("%s: %s: seed file: %v", instanceType, tc.name, err)
		}
		s := newInMemFleetStore(45*time.Second, key)
		s.LoadHistory(ctx, obj)
		if got := s.HistoryStatus().LoadFailed; got != tc.wantFailed {
			t.Fatalf("%s: %s: LoadFailed = %v, want %v", instanceType, tc.name, got, tc.wantFailed)
		}
		if s.ring.initialized {
			t.Fatalf("%s: %s: ring restored from a bad file", instanceType, tc.name)
		}
		s.Fold(Deltas{InBytes: 5}, now)
		if err := s.SaveHistory(ctx, obj, now, true); err != nil {
			t.Fatalf("%s: %s: save after bad load: %v", instanceType, tc.name, err)
		}
		if st := s.HistoryStatus(); st.LastSaveErr != nil {
			t.Fatalf("%s: %s: LastSaveErr = %v", instanceType, tc.name, st.LastSaveErr)
		}
		fresh := newInMemFleetStore(45*time.Second, key)
		fresh.LoadHistory(ctx, obj)
		if fresh.HistoryStatus().LoadFailed {
			t.Fatalf("%s: %s: rewritten file still fails to load", instanceType, tc.name)
		}
		bw, _ := fresh.SeriesWindow(now, 24)
		if got := trafficInSum(bw); got != 5 {
			t.Fatalf("%s: %s: rewritten In sum = %d, want 5", instanceType, tc.name, got)
		}
	}
}

func TestTrafficHistoryCleanNoSave(t *testing.T) {
	ExecObjectLayerTest(t, testTrafficHistoryCleanNoSave)
}

func TestTrafficHistoryOverlappingSaves(t *testing.T) {
	ExecObjectLayerTest(t, testTrafficHistoryOverlappingSaves)
}

func testTrafficHistoryOverlappingSaves(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	key := trafficHistoryKey("overlapping:9000")
	s := newInMemFleetStore(45*time.Second, key)
	s.Fold(Deltas{InBytes: 100}, now)
	p := &pausedStoreLayer{ObjectLayer: obj, key: key, entered: make(chan context.Context, 1), release: make(chan struct{})}
	defer p.unblock()
	oldSave := make(chan error, 1)
	go func() { oldSave <- s.SaveHistory(ctx, p, now, false) }()
	awaitStoreWrite(t, p.entered)
	s.Fold(Deltas{InBytes: 50}, now.Add(time.Hour))

	// Shutdown deadline bounds waiting for periodic saver
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := s.SaveHistory(deadline, obj, now.Add(time.Hour), true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s: blocked forced save = %v, want deadline exceeded", instanceType, err)
	}
	observed := observedHistoryLayer{ObjectLayer: obj, locking: make(chan context.Context, 1)}
	newSave := make(chan error, 1)
	go func() { newSave <- s.SaveHistory(ctx, observed, now.Add(time.Hour), true) }()
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
	fresh := newInMemFleetStore(45*time.Second, key)
	fresh.LoadHistory(ctx, obj)
	bw, _ := fresh.SeriesWindow(now.Add(time.Hour), 24)
	if trafficInSum(bw) != 150 || fresh.ring.epochHour != absHour(now.Add(time.Hour)) || s.file.dirty || !s.file.lastSave.Equal(now.Add(time.Hour)) {
		t.Fatalf("%s: saved sum %d hour %d, dirty %t, lastSave %v", instanceType, trafficInSum(bw), fresh.ring.epochHour, s.file.dirty, s.file.lastSave)
	}
}

// Save only after fold or hour advance, no key means no backend writes
func testTrafficHistoryCleanNoSave(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	key := trafficHistoryKey("clean:9000")
	s := newInMemFleetStore(45*time.Second, key)
	if err := s.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: clean save: %v", instanceType, err)
	}
	if _, err := readConfig(ctx, obj, key); !errors.Is(err, errConfigNotFound) {
		t.Fatalf("%s: clean store wrote %s: %v", instanceType, key, err)
	}
	if !s.file.lastSave.IsZero() {
		t.Fatalf("%s: clean store recorded a save", instanceType)
	}

	if !s.Since(now).IsZero() {
		t.Fatalf("%s: a save initialized the empty ring", instanceType)
	}
	// Hour advance changes initialized ring
	s.Fold(Deltas{InBytes: 1}, now)
	if err := s.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: save after fold: %v", instanceType, err)
	}
	if err := s.SaveHistory(ctx, obj, now.Add(time.Hour), false); err != nil {
		t.Fatalf("%s: save after hour advance: %v", instanceType, err)
	}
	if !s.file.lastSave.Equal(now.Add(time.Hour)) {
		t.Fatalf("%s: hour advance did not save %s", instanceType, key)
	}

	unkeyed := newInMemFleetStore(45*time.Second, "")
	unkeyed.Fold(Deltas{InBytes: 9}, now)
	if err := unkeyed.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: unkeyed save: %v", instanceType, err)
	}
	if !unkeyed.file.lastSave.IsZero() {
		t.Fatalf("%s: store without a key saved", instanceType)
	}
	unkeyed.LoadHistory(ctx, obj)
	if unkeyed.HistoryStatus().LoadFailed {
		t.Fatalf("%s: store without a key tried to load", instanceType)
	}
	bw, _ := unkeyed.SeriesWindow(now, 24)
	if got := trafficInSum(bw); got != 9 {
		t.Fatalf("%s: unkeyed In sum = %d, want 9 (LoadHistory must not touch the ring)", instanceType, got)
	}
}
