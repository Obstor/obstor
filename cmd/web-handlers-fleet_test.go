package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

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

func TestRegionStatsFree(t *testing.T) {
	rs := regionStats(views())
	if len(rs) != 1 || rs[0].Region != "us-east-1" {
		t.Fatalf("regions = %+v, want only us-east-1 (offline eu-west-1 skipped)", rs)
	}
	if rs[0].Free != 170 {
		t.Fatalf("us-east-1 free = %d, want 170 (90 + 80)", rs[0].Free)
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

// Serve one web RPC through fresh router
func serveFleetRPC(t *testing.T, method, token string, args interface{}) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	if err := registerWebRPCRouter(router); err != nil {
		t.Fatal(err)
	}
	req, err := newTestWebRPCRequest(method, token, args)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestWebHandlerFleetDisabled(t *testing.T) {
	savedStore := globalFleetStore
	defer func() { globalFleetStore = savedStore }()
	globalFleetStore = nil

	token, err := authenticateWeb(globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := authenticateJWTUsersWithCredentials(globalActiveCred, UTCNow().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	const disabled = "traffic metrics need the fleet store, start the server with --web-address"
	for _, test := range []struct {
		name, method, token string
		wantCode, wantMsg   string
	}{
		{name: "metrics", method: "web.FleetMetrics", token: token, wantCode: "XObstorFleetDisabled", wantMsg: disabled},
		{name: "regions", method: "web.FleetRegions", token: token, wantCode: "XObstorFleetDisabled", wantMsg: disabled},
		{name: "enroll token", method: "web.FleetEnrollToken", token: token, wantCode: "XObstorFleetDisabled", wantMsg: disabled},
		// Authenticate before revealing store state
		{name: "no token", method: "web.FleetMetrics", token: "", wantCode: "WebTokenMissing"},
		{name: "expired token", method: "web.FleetMetrics", token: expired, wantCode: "WebTokenInvalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := serveFleetRPC(t, test.method, test.token, map[string]int{})
			code, message := webRPCError(t, rec)
			if code != test.wantCode {
				t.Errorf("%s code = %q (%q), want %q", test.method, code, message, test.wantCode)
			}
			if test.wantMsg != "" && message != test.wantMsg {
				t.Errorf("%s message = %q, want %q", test.method, message, test.wantMsg)
			}
		})
	}
}

func TestWebHandlerFleetScopedDenied(t *testing.T) {
	ExecObjectLayerTest(t, testFleetScopedDenied)
}

// Callers outside Fleet tier get AccessDenied regardless of store state
func testFleetScopedDenied(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	savedStore := globalFleetStore
	defer func() { globalFleetStore = savedStore }()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	allowed := makeTestBucket(t, obj, BucketOptions{})
	// S3 access grants no instance statistics, even with every bucket and metrics off
	scoped := makeScopedIAMUser(t, "fleetscoped", allowed)
	token, err := getWebRPCToken(router, scoped.AccessKey, scoped.SecretKey)
	if err != nil {
		t.Fatalf("%s: login: %v", instanceType, err)
	}
	for _, store := range []FleetStore{nil, newInMemFleetStore(45*time.Second, "")} {
		globalFleetStore = store
		for _, method := range []string{"web.FleetMetrics", "web.FleetRegions"} {
			req, err := newTestWebRPCRequest(method, token, map[string]int{"windowHours": 24})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if code, message := webRPCError(t, rec); code != "AccessDenied" {
				t.Fatalf("%s: %s as scoped user (store set %v) = %q %q, want AccessDenied", instanceType, method, store != nil, code, message)
			}
		}
	}
}

func TestFleetMetricsSince(t *testing.T) {
	savedStore := globalFleetStore
	defer func() { globalFleetStore = savedStore }()

	token, err := authenticateWeb(globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	folded := time.Now().Add(-3 * time.Hour)
	foldedHour := time.Unix(absHour(folded)*3600, 0).UTC().Format(time.RFC3339)
	for _, test := range []struct {
		name string
		fold bool
		want string
	}{
		{name: "empty ring", fold: false, want: ""},
		{name: "folded", fold: true, want: foldedHour},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newInMemFleetStore(45*time.Second, "")
			if test.fold {
				s.Fold(Deltas{InBytes: 10}, folded)
			}
			globalFleetStore = s
			rec := serveFleetRPC(t, "web.FleetMetrics", token, map[string]int{"windowHours": 24})
			var reply FleetMetricsRep
			if err := getTestWebRPCResponse(rec, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Since != test.want {
				t.Fatalf("since = %q, want %q", reply.Since, test.want)
			}
		})
	}
}

func TestFleetMetricsWindows(t *testing.T) {
	savedStore := globalFleetStore
	defer func() { globalFleetStore = savedStore }()
	s := newInMemFleetStore(45*time.Second, "")
	now := time.Now()
	s.Fold(Deltas{InBytes: 10, Reads: 20}, now.Add(-48*time.Hour))
	globalFleetStore = s

	token, err := authenticateWeb(globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	if err := registerWebRPCRouter(router); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args map[string]int
		want int
	}{
		{name: "omitted", args: map[string]int{}, want: 24},
		{name: "zero", args: map[string]int{"windowHours": 0}, want: 24},
		{name: "day", args: map[string]int{"windowHours": 24}, want: 24},
		{name: "week", args: map[string]int{"windowHours": 168}, want: 168},
		{name: "month", args: map[string]int{"windowHours": 720}, want: 720},
		{name: "invalid", args: map[string]int{"windowHours": -1}, want: 24},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, err := newTestWebRPCRequest("web.FleetMetrics", token, test.args)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("response status = %d, want 200", rec.Code)
			}
			var reply FleetMetricsRep
			if err := getTestWebRPCResponse(rec, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.WindowHours != test.want || len(reply.Bandwidth) != test.want || len(reply.Ops) != test.want {
				t.Fatalf("window/series lengths = %d/%d/%d, want %d", reply.WindowHours, len(reply.Bandwidth), len(reply.Ops), test.want)
			}
			var bytes, reads uint64
			for hour := range reply.Bandwidth {
				bytes += reply.Bandwidth[hour].In
				reads += reply.Ops[hour].Read
			}
			if test.want == 24 {
				if bytes != 0 || reads != 0 {
					t.Fatalf("expired daily totals = %d/%d, want zero", bytes, reads)
				}
			} else if bytes != 10 || reads != 20 {
				t.Fatalf("extended totals = %d/%d, want 10/20", bytes, reads)
			}
		})
	}
}
