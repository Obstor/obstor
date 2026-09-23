package cmd

import (
	"testing"
	"time"
)

func hourBase() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) }

func TestFoldAccumulatesCurrentHour(t *testing.T) {
	s := newInMemFleetStore(45 * time.Second)
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
	s := newInMemFleetStore(45 * time.Second)
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
	s := newInMemFleetStore(45 * time.Second)
	bw, ops := s.Series()
	if len(bw) != 24 || len(ops) != 24 {
		t.Fatalf("series lengths = %d/%d, want 24/24", len(bw), len(ops))
	}
}

func TestMembersLiveness(t *testing.T) {
	s := newInMemFleetStore(45 * time.Second)
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
	s := newInMemFleetStore(45 * time.Second)
	now := hourBase()
	s.Upsert(MemberSnapshot{ID: "a", Used: 10}, now)
	s.Upsert(MemberSnapshot{ID: "a", Used: 99}, now.Add(time.Second))
	views := s.Members(now.Add(time.Second))
	if len(views) != 1 || views[0].Used != 99 {
		t.Fatalf("views = %+v, want single Used99", views)
	}
}

func TestMintEnrollTokenUniqueAndNonEmpty(t *testing.T) {
	s := newInMemFleetStore(45 * time.Second)
	t1 := s.MintEnrollToken(hourBase())
	t2 := s.MintEnrollToken(hourBase())
	if t1 == "" || t2 == "" || t1 == t2 {
		t.Fatalf("tokens not unique/non-empty: %q %q", t1, t2)
	}
}
