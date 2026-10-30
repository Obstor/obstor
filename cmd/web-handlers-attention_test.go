package cmd

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/obstor/obstor/pkg/madmin"
)

// Fresh attention globals, cleanup restores originals
func pinAttentionGlobals(ctx context.Context, obj ObjectLayer) func() {
	iam, quota, capacity, fleet, disks := globalIAMSys, globalBucketQuotaSys, globalCapacityHistory, globalFleetStore, globalAttentionDisks
	globalIAMSys = NewIAMSys()
	globalIAMSys.Init(ctx, obj)
	globalBucketQuotaSys = NewBucketQuotaSys()
	globalCapacityHistory = newCapacityHistory()
	globalFleetStore = nil
	globalAttentionDisks = &attentionDiskCache{}
	return func() {
		globalIAMSys, globalBucketQuotaSys, globalCapacityHistory, globalFleetStore, globalAttentionDisks = iam, quota, capacity, fleet, disks
	}
}

// Near, full, FIFO and unscanned quotas, stale usage and one offline drive
func seedWebAttention(t TestErrHandler, obj ObjectLayer) (near, full string) {
	t.Helper()
	near, full = makeTestBucket(t, obj, BucketOptions{}), makeTestBucket(t, obj, BucketOptions{})
	fifo, unscanned := makeTestBucket(t, obj, BucketOptions{}), makeTestBucket(t, obj, BucketOptions{})
	for b, cfg := range map[string]string{
		near:      `{"quota":100,"quotatype":"hard"}`,
		full:      `{"quota":100,"quotatype":"hard"}`,
		fifo:      `{"quota":100,"quotatype":"fifo"}`,
		unscanned: `{"quota":100,"quotatype":"hard"}`,
	} {
		if err := globalBucketMetadataSys.Update(b, bucketQuotaConfigFile, []byte(cfg)); err != nil {
			t.Fatalf("quota on %s: %v", b, err)
		}
	}
	usage := make(chan madmin.DataUsageInfo, 1)
	usage <- madmin.DataUsageInfo{
		LastUpdate:   time.Now().Add(-48 * time.Hour),
		BucketsCount: 3,
		BucketsUsage: map[string]madmin.BucketUsageInfo{near: {Size: 95}, full: {Size: 120}, fifo: {Size: 120}},
	}
	close(usage)
	storeDataUsageInBackend(context.Background(), obj, usage)
	if dui, err := loadDataUsageFromBackend(context.Background(), obj); err != nil || len(dui.BucketsUsage) != 3 {
		t.Fatalf("usage snapshot = %+v, %v; want three buckets", dui.BucketsUsage, err)
	}
	// Future cache time prevents StorageInfo reads
	globalAttentionDisks = &attentionDiskCache{at: time.Now().Add(time.Hour), ok: true, disks: []madmin.Disk{
		{Endpoint: "http://node1:9000/d1", State: madmin.DriveStateOk},
		{Endpoint: "http://node1:9000/d2", State: madmin.DriveStateOffline},
	}}
	return near, full
}

// Call Attention as user, null items decode to nil
func callWebAttention(t TestErrHandler, router http.Handler, accessKey, secretKey string) ([]AttentionItem, error) {
	t.Helper()
	var rep struct {
		Items []AttentionItem `json:"items"`
	}
	err := userWebRPC(t, router, accessKey, secretKey, "Attention", &WebGenericArgs{}, &rep)
	return rep.Items, err
}

func webAttentionIDs(items []AttentionItem) string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return strings.Join(ids, " ")
}

func TestWebHandlerAttentionTiers(t *testing.T) {
	ExecObjectLayerTest(t, testAttentionTiersWebHandler)
}

// Root sees all faults, scoped user sees own quotas only
func testAttentionTiersWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer pinAttentionGlobals(ctx, obj)()
	router := initTestWebRPCEndPoint(obj)
	near, full := seedWebAttention(t, obj)
	globalCapacityHistory.file.loadFailed = true
	scoped := makeScopedIAMUser(t, "attnscoped", near)
	none := makeScopedIAMUser(t, "attnnone", "attn-missing-bucket")
	root, rootSecret := globalActiveCred.AccessKey, globalActiveCred.SecretKey

	want := strings.Join([]string{
		"drive-offline:http://node1:9000/d2",
		"quota-full:" + full,
		"quota-near:" + near,
		"history-load:capacity",
		"usage-stale",
	}, " ")
	// Stable IDs keep seen state across reloads
	for call := 1; call <= 2; call++ {
		items, err := callWebAttention(t, router, root, rootSecret)
		if err != nil {
			t.Fatalf("%s: root call %d: %v", instanceType, call, err)
		}
		if got := webAttentionIDs(items); got != want {
			t.Fatalf("%s: root call %d ids = %q, want %q", instanceType, call, got, want)
		}
		nearItem := items[2]
		if nearItem.Kind != "quota-near" || nearItem.Severity != attentionWarning ||
			nearItem.Title != "Bucket "+near+" is at 95% of its quota" || nearItem.Href != "/buckets/"+near+"?tab=settings" {
			t.Fatalf("%s: quota-near item = %+v", instanceType, nearItem)
		}
	}

	items, err := callWebAttention(t, router, scoped.AccessKey, scoped.SecretKey)
	if err != nil || webAttentionIDs(items) != "quota-near:"+near {
		t.Fatalf("%s: scoped ids = %q, %v; want only quota-near:%s", instanceType, webAttentionIDs(items), err, near)
	}
	items, err = callWebAttention(t, router, none.AccessKey, none.SecretKey)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("%s: no-bucket user items = %#v, %v; want an empty array, not null", instanceType, items, err)
	}

	// Fleet sees drives and quotas, root alone sees usage and history faults
	fleetUser := makeDocIAMUser(t, "attnfleet", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}]}`)
	items, err = callWebAttention(t, router, fleetUser.AccessKey, fleetUser.SecretKey)
	wantFleet := strings.Join([]string{"drive-offline:http://node1:9000/d2", "quota-full:" + full, "quota-near:" + near}, " ")
	if err != nil || webAttentionIDs(items) != wantFleet {
		t.Fatalf("%s: fleet user ids = %q, %v; want %q", instanceType, webAttentionIDs(items), err, wantFleet)
	}

	resetGlobalObjectAPI()
	defer setObjectLayer(obj)
	if _, err = callWebAttention(t, router, root, rootSecret); err == nil || !strings.Contains(err.Error(), errServerNotInitialized.Error()) {
		t.Fatalf("%s: no object layer = %v, want %v", instanceType, err, errServerNotInitialized)
	}
}

func TestWebHandlerAttentionBackend(t *testing.T) {
	ExecObjectLayerTest(t, testAttentionBackendWebHandler)
}

// Backend has no quota or stale usage items
func testAttentionBackendWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer pinAttentionGlobals(ctx, obj)()
	router := initTestWebRPCEndPoint(obj)
	seedWebAttention(t, obj)
	globalIsBackend = true

	items, err := callWebAttention(t, router, globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatalf("%s: backend root: %v", instanceType, err)
	}
	if got, want := webAttentionIDs(items), "drive-offline:http://node1:9000/d2"; got != want {
		t.Fatalf("%s: backend root ids = %q, want %q", instanceType, got, want)
	}
}

// ListBuckets fails like lost read quorum
type listBucketsFailLayer struct{ ObjectLayer }

func (listBucketsFailLayer) ListBuckets(context.Context) ([]BucketInfo, error) {
	return nil, errErasureReadQuorum
}

// Healthy drives leave quota rules only
func pinHealthyAttentionDisks() {
	globalAttentionDisks = &attentionDiskCache{at: time.Now().Add(time.Hour), ok: true, disks: []madmin.Disk{
		{Endpoint: "http://node1:9000/d1", State: madmin.DriveStateOk},
		{Endpoint: "http://node1:9000/d2", State: madmin.DriveStateOk},
	}}
}

func TestWebHandlerAttentionUsageError(t *testing.T) {
	ExecObjectLayerTest(t, testAttentionUsageErrorWebHandler)
}

// Failed usage read keeps prior console items without false stale warning
func testAttentionUsageErrorWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer pinAttentionGlobals(ctx, obj)()
	boot := globalBootTime
	defer func() { globalBootTime = boot }()
	globalBootTime = time.Now().Add(-48 * time.Hour)
	router := initTestWebRPCEndPoint(obj)
	seedWebAttention(t, obj)
	globalBucketQuotaSys.bucketStorageCache.Once.Do(func() {
		globalBucketQuotaSys.bucketStorageCache.Update = func() (interface{}, error) { return nil, errErasureReadQuorum }
	})
	root, rootSecret := globalActiveCred.AccessKey, globalActiveCred.SecretKey

	// Report offline drive without quota or stale items from empty snapshot
	items, err := callWebAttention(t, router, root, rootSecret)
	if got, want := webAttentionIDs(items), "drive-offline:http://node1:9000/d2"; err != nil || got != want {
		t.Fatalf("%s: outage root ids = %q, %v; want %q", instanceType, got, err, want)
	}

	// No drive fault, failed call keeps console items
	pinHealthyAttentionDisks()
	if items, err = callWebAttention(t, router, root, rootSecret); err == nil {
		t.Fatalf("%s: healthy root ids = %q, want an error", instanceType, webAttentionIDs(items))
	}
}

func TestWebHandlerAttentionListBucketsError(t *testing.T) {
	ExecObjectLayerTest(t, testAttentionListBucketsErrorWebHandler)
}

// Failed bucket listing still reports drive faults
func testAttentionListBucketsErrorWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer pinAttentionGlobals(ctx, obj)()
	seedWebAttention(t, obj)
	router := initTestWebRPCEndPoint(listBucketsFailLayer{obj})
	defer setObjectLayer(obj)
	root, rootSecret := globalActiveCred.AccessKey, globalActiveCred.SecretKey

	items, err := callWebAttention(t, router, root, rootSecret)
	if got, want := webAttentionIDs(items), "drive-offline:http://node1:9000/d2"; err != nil || got != want {
		t.Fatalf("%s: outage root ids = %q, %v; want %q", instanceType, got, err, want)
	}

	pinHealthyAttentionDisks()
	if items, err = callWebAttention(t, router, root, rootSecret); err == nil {
		t.Fatalf("%s: healthy root ids = %q, want an error", instanceType, webAttentionIDs(items))
	}
}
