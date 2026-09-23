package cmd

import "testing"

func views() []MemberView {
	return []MemberView{
		{MemberSnapshot: MemberSnapshot{ID: "a", Region: "us-east-1", Used: 10, Total: 100, Free: 90, DrivesOnline: 4, DrivesTotal: 4, ObjectsCount: 5, BucketsCount: 2}, Online: true},
		{MemberSnapshot: MemberSnapshot{ID: "b", Region: "us-east-1", Used: 20, Total: 100, Free: 80, DrivesOnline: 3, DrivesTotal: 4, ObjectsCount: 7, BucketsCount: 1}, Online: true},
		{MemberSnapshot: MemberSnapshot{ID: "c", Region: "eu-west-1", Used: 50, Total: 200, Free: 150, DrivesOnline: 8, DrivesTotal: 8, ObjectsCount: 1, BucketsCount: 1}, Online: false},
	}
}

func TestAggStorageOnlineOnly(t *testing.T) {
	r := aggStorage(views())
	if r.Used != 30 || r.Total != 200 || r.Free != 170 {
		t.Fatalf("agg = %+v, want Used30 Total200 Free170 (online only)", r)
	}
	if r.DisksOnline != 7 || r.DisksOffline != 1 {
		t.Fatalf("disks = on%d off%d, want on7 off1", r.DisksOnline, r.DisksOffline)
	}
	if r.ObjectsCount != 12 || r.BucketsCount != 3 {
		t.Fatalf("counts = obj%d bkt%d, want obj12 bkt3", r.ObjectsCount, r.BucketsCount)
	}
}

func TestRegionStatsGrouping(t *testing.T) {
	rs := regionStats(views())
	got := map[string]RegionStat{}
	for _, s := range rs {
		got[s.Region] = s
	}
	if len(got) != 1 {
		t.Fatalf("regions = %v, want only us-east-1 (online)", got)
	}
	ue := got["us-east-1"]
	if ue.Used != 30 || ue.Total != 200 {
		t.Fatalf("us-east-1 = %+v, want Used30 Total200", ue)
	}
}

func TestFleetServersPaging(t *testing.T) {
	rows, total := fleetServersPage(views(), 1, 1)
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (limit)", len(rows))
	}
	if rows[0].ID != "b" {
		t.Fatalf("row[0].ID = %q, want b", rows[0].ID)
	}
}
