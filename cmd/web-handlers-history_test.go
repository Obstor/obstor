package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
)

func TestRFC3339OrEmpty(t *testing.T) {
	plus2 := time.FixedZone("plus2", 2*3600)
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Time{}, ""},
		{time.Time{}.In(plus2), ""},
		{time.Date(2026, 10, 9, 6, 30, 15, 500, plus2), "2026-10-09T04:30:15Z"},
		{time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), "2026-10-09T00:00:00Z"},
	}
	for _, c := range cases {
		if got := rfc3339OrEmpty(c.in); got != c.want {
			t.Errorf("rfc3339OrEmpty(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Capacity history as user, return recorder
func capacityHistoryCall(t TestErrHandler, router http.Handler, accessKey, secretKey string, hours int) *httptest.ResponseRecorder {
	t.Helper()
	token, err := getWebRPCToken(router, accessKey, secretKey)
	if err != nil {
		t.Fatalf("authenticate %s: %v", accessKey, err)
	}
	req, err := newTestWebRPCRequest("web.CapacityHistory", token, &CapacityHistoryArgs{Hours: hours})
	if err != nil {
		t.Fatalf("CapacityHistory request: %v", err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("CapacityHistory: HTTP %d", rec.Code)
	}
	return rec
}

// Check exact JSON field encodings
func checkRawHistory(t TestErrHandler, label string, raw map[string]json.RawMessage, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got := string(raw[k]); got != v {
			t.Errorf("%s: %s = %s, want %s", label, k, got, v)
		}
	}
}

// Fleet drive series by JSON name
func driveSeries(rep CapacityHistoryRep) map[string][]uint64 {
	return map[string][]uint64{
		"total": rep.Total, "drivesOnline": rep.DrivesOnline, "drivesTotal": rep.DrivesTotal,
		"nodesOnline": rep.NodesOnline, "nodesTotal": rep.NodesTotal,
	}
}

func TestWebHandlerCapacityHistoryHours(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryHoursWebHandler)
}

// Only 24, 168 and 720 hours, no samples means null free
func testCapacityHistoryHoursWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	defer func(h *capacityHistory) { globalCapacityHistory = h }(globalCapacityHistory)
	globalCapacityHistory = newCapacityHistory()
	router := initTestWebRPCEndPoint(obj)
	root := globalActiveCred

	for _, hours := range []int{0, 48, 721, -24} {
		rec := capacityHistoryCall(t, router, root.AccessKey, root.SecretKey, hours)
		if _, msg := webRPCError(t, rec); msg != "Hours must be 24, 168 or 720." {
			t.Errorf("%s: hours %d message = %q, want the hours input error", instanceType, hours, msg)
		}
	}

	for hours, step := range map[int]string{24: "3600", 168: "3600", 720: "86400"} {
		var raw map[string]json.RawMessage
		if err := rootWebRPC(t, router, "CapacityHistory", &CapacityHistoryArgs{Hours: hours}, &raw); err != nil {
			t.Fatalf("%s: hours %d: %v", instanceType, hours, err)
		}
		checkRawHistory(t, fmt.Sprintf("%s hours %d", instanceType, hours), raw, map[string]string{
			"since": `""`, "step": step, "used": "[]", "objects": "[]", "buckets": "[]", "free": "null", "partial": "false",
			"total": "null", "drivesOnline": "null", "drivesTotal": "null", "nodesOnline": "null", "nodesTotal": "null",
		})
	}
}

func TestWebHandlerCapacityHistoryTiers(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryTiersWebHandler)
}

// Admin sees cluster totals, other users see live bucket sums
func testCapacityHistoryTiersWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	defer func(h *capacityHistory) { globalCapacityHistory = h }(globalCapacityHistory)
	ctx := context.Background()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)

	a := makeTestBucket(t, obj, BucketOptions{})
	b := makeTestBucket(t, obj, BucketOptions{})
	c := makeTestBucket(t, obj, BucketOptions{})
	d := getRandomBucketName()

	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[
{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s","arn:aws:s3:::%s"]}]}`, a, b, d)
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("histpolicy", *p); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.CreateUser("histuser", madmin.UserInfo{SecretKey: "histuser-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet("histuser", "histpolicy", false); err != nil {
		t.Fatal(err)
	}

	// Buckets exist before samples
	now := time.Now()
	for _, bucket := range []string{a, b, c} {
		meta, err := globalBucketMetadataSys.Get(bucket)
		if err != nil {
			t.Fatalf("%s: metadata of %s: %v", instanceType, bucket, err)
		}
		meta.Created = now.Add(-3 * time.Hour)
		globalBucketMetadataSys.Set(bucket, meta)
	}

	globalCapacityHistory = newCapacityHistory()
	globalCapacityHistory.BecomeLeader(ctx, obj)
	dui := madmin.DataUsageInfo{
		ObjectsTotalSize:  700,
		ObjectsTotalCount: 7,
		BucketsCount:      3,
		BucketsUsage: map[string]madmin.BucketUsageInfo{
			a: {Size: 100, ObjectsCount: 1},
			b: {Size: 200, ObjectsCount: 2},
			c: {Size: 400, ObjectsCount: 4},
		},
	}
	globalCapacityHistory.Sample(ctx, obj, dui, now.Add(-2*time.Hour))
	globalCapacityHistory.Sample(ctx, obj, dui, now.Add(-time.Hour))

	hist := func(accessKey, secretKey string) CapacityHistoryRep {
		t.Helper()
		var rep CapacityHistoryRep
		if err := userWebRPC(t, router, accessKey, secretKey, "CapacityHistory", &CapacityHistoryArgs{Hours: 24}, &rep); err != nil {
			t.Fatalf("%s: CapacityHistory as %s: %v", instanceType, accessKey, err)
		}
		if len(rep.Used) < 2 || len(rep.Objects) != len(rep.Used) || len(rep.Buckets) != len(rep.Used) {
			t.Fatalf("%s: %s series lengths used %d objects %d buckets %d, want equal and at least 2", instanceType, accessKey, len(rep.Used), len(rep.Objects), len(rep.Buckets))
		}
		if rep.Since == "" || rep.Step != 3600 {
			t.Fatalf("%s: %s since %q step %d, want a start time and 3600", instanceType, accessKey, rep.Since, rep.Step)
		}
		return rep
	}
	last := func(v []uint64) uint64 { return v[len(v)-1] }

	rep := hist(globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if last(rep.Used) != 700 || last(rep.Objects) != 7 || last(rep.Buckets) != 3 {
		t.Errorf("%s: root last point used %d objects %d buckets %d, want 700 7 3", instanceType, last(rep.Used), last(rep.Objects), last(rep.Buckets))
	}
	if rep.Free == nil || len(rep.Free) != len(rep.Used) || rep.Partial {
		t.Errorf("%s: root free %v partial %v, want one free value per point and partial false", instanceType, rep.Free, rep.Partial)
	}
	for name, v := range driveSeries(rep) {
		if len(v) != len(rep.Used) || last(v) == 0 {
			t.Errorf("%s: root %s = %v, want one value per point ending above 0", instanceType, name, v)
		}
	}

	rep = hist("histuser", "histuser-secret")
	for i, used := range rep.Used {
		if used > 300 {
			t.Errorf("%s: scoped used[%d] = %d, includes a bucket the user cannot list", instanceType, i, used)
		}
	}
	if last(rep.Used) != 300 || last(rep.Objects) != 3 || last(rep.Buckets) != 2 {
		t.Errorf("%s: scoped last point used %d objects %d buckets %d, want 300 3 2", instanceType, last(rep.Used), last(rep.Objects), last(rep.Buckets))
	}
	if rep.Free != nil || rep.Partial {
		t.Errorf("%s: scoped free %v partial %v, want null free and partial false", instanceType, rep.Free, rep.Partial)
	}
	for name, v := range driveSeries(rep) {
		if v != nil {
			t.Errorf("%s: scoped %s = %v, want null", instanceType, name, v)
		}
	}

	if err = obj.MakeBucketWithLocation(ctx, d, BucketOptions{}); err != nil {
		t.Fatalf("make bucket %s: %v", d, err)
	}
	rep = hist("histuser", "histuser-secret")
	if !rep.Partial || last(rep.Used) != 300 || last(rep.Buckets) != 2 {
		t.Errorf("%s: scoped with untracked %s: partial %v used %d buckets %d, want true 300 2", instanceType, d, rep.Partial, last(rep.Used), last(rep.Buckets))
	}
	if rep = hist(globalActiveCred.AccessKey, globalActiveCred.SecretKey); rep.Partial {
		t.Errorf("%s: root partial true, want false (partial is scoped only)", instanceType)
	}

	if err = obj.DeleteBucket(ctx, b, false); err != nil {
		t.Fatalf("delete bucket %s: %v", b, err)
	}
	rep = hist("histuser", "histuser-secret")
	if last(rep.Used) != 100 || last(rep.Objects) != 1 || last(rep.Buckets) != 1 {
		t.Errorf("%s: scoped after deleting %s: used %d objects %d buckets %d, want 100 1 1", instanceType, b, last(rep.Used), last(rep.Objects), last(rep.Buckets))
	}
	if !rep.Partial || rep.Free != nil {
		t.Errorf("%s: scoped after delete: partial %v free %v, want true and null", instanceType, rep.Partial, rep.Free)
	}

	if err = obj.MakeBucketWithLocation(ctx, b, BucketOptions{}); err != nil {
		t.Fatalf("recreate bucket %s: %v", b, err)
	}
	rep = hist("histuser", "histuser-secret")
	for i, used := range rep.Used {
		if used > 100 || rep.Buckets[i] > 1 {
			t.Errorf("%s: scoped after recreating %s: point %d used %d buckets %d, includes the deleted bucket's history", instanceType, b, i, used, rep.Buckets[i])
		}
	}
}

func TestWebHandlerCapacityHistoryBackend(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryBackendWebHandler)
}

// Backend returns empty history
func testCapacityHistoryBackendWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	defer func(h *capacityHistory) { globalCapacityHistory = h }(globalCapacityHistory)
	ctx := context.Background()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})

	globalCapacityHistory = newCapacityHistory()
	globalCapacityHistory.BecomeLeader(ctx, obj)
	dui := madmin.DataUsageInfo{
		ObjectsTotalSize:  100,
		ObjectsTotalCount: 1,
		BucketsCount:      1,
		BucketsUsage:      map[string]madmin.BucketUsageInfo{bucket: {Size: 100, ObjectsCount: 1}},
	}
	now := time.Now()
	globalCapacityHistory.Sample(ctx, obj, dui, now.Add(-2*time.Hour))
	globalCapacityHistory.Sample(ctx, obj, dui, now.Add(-time.Hour))

	globalIsBackend = true
	for hours, step := range map[int]string{24: "3600", 720: "86400"} {
		var raw map[string]json.RawMessage
		if err := rootWebRPC(t, router, "CapacityHistory", &CapacityHistoryArgs{Hours: hours}, &raw); err != nil {
			t.Fatalf("%s: backend hours %d: %v", instanceType, hours, err)
		}
		checkRawHistory(t, fmt.Sprintf("%s backend hours %d", instanceType, hours), raw, map[string]string{
			"since": `""`, "step": step, "used": "[]", "objects": "[]", "buckets": "[]", "free": "null", "partial": "false",
			"total": "null", "drivesOnline": "null", "drivesTotal": "null", "nodesOnline": "null", "nodesTotal": "null",
		})
	}
}
