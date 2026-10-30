package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/auth"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
	jsonrpc "github.com/obstor/obstor/pkg/rpc"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

// Fresh full-size activity log until test ends
func pinActivityLog(t *testing.T) *activityLog {
	t.Helper()
	prev := globalActivityLog
	l := newActivityLog("test-node:9000", activityRingSize, activityQueueSize)
	globalActivityLog = l
	t.Cleanup(func() { globalActivityLog = prev })
	return l
}

// Event seqs in slice order
func activitySeqs(events []activityEvent) []uint64 {
	out := make([]uint64, 0, len(events))
	for _, e := range events {
		out = append(out, e.Seq)
	}
	return out
}

var activityObjectKinds = []activityKind{activityKindObject, activityKindBucket}

func TestActivitySubmitGate(t *testing.T) {
	l := pinActivityLog(t)

	// Ignore reads and unknown APIs without counting denials
	l.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "GetObject", Bucket: "alpha", Object: "a.txt", Status: 200})
	l.submit(activityEvent{Source: "s3", API: "GetObject", Bucket: "alpha", Object: "a.txt", Status: 403})
	l.submit(activityEvent{AccessKey: "alice", Source: "console", API: "web.RemoveObject", Bucket: "alpha", Status: 200})
	if n, d := len(l.queue), l.denied.Load(); n != 0 || d != 0 {
		t.Fatalf("after reads: queued %d, denied %d; want 0, 0", n, d)
	}

	// Anonymous and denied mutations only increment denied counter
	l.submit(activityEvent{Source: "s3", API: "PutObject", Bucket: "alpha", Object: "anon.txt", Status: 200})
	l.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: "a.txt", Status: 401})
	l.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "DeleteObject", Bucket: "alpha", Object: "a.txt", Status: 403})
	if n, d := len(l.queue), l.denied.Load(); n != 0 || d != 3 {
		t.Fatalf("after denied: queued %d, denied %d; want 0, 3", n, d)
	}

	// Authenticated mutations queue regardless of status
	l.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: "a.txt", Status: 200})
	l.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: "b.txt", Status: 500})
	if n := len(l.queue); n != 2 {
		t.Fatalf("queued %d, want 2", n)
	}
	l.drain()
	got := l.snapshot()
	if len(got) != 2 {
		t.Fatalf("ring holds %d events, want 2", len(got))
	}
	for i, e := range got {
		if e.Seq != uint64(i) || e.Node != "test-node:9000" || e.Time.IsZero() {
			t.Fatalf("event %d = %+v; want seq %d, node test-node:9000, time stamped", i, e, i)
		}
	}
	if got[0].Status != 200 || got[1].Status != 500 {
		t.Fatalf("statuses = %d, %d; want 200, 500", got[0].Status, got[1].Status)
	}
	if page := l.query(activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 10}); page.Denied != 3 || page.Dropped != 0 {
		t.Fatalf("page counters = %d denied, %d dropped; want 3, 0", page.Denied, page.Dropped)
	}

	// Full queue drops without blocking request
	small := newActivityLog("n1:9000", 10, 2)
	for i := 0; i < 3; i++ {
		small.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: fmt.Sprintf("o%d", i), Status: 200})
	}
	if d := small.dropped.Load(); d != 1 {
		t.Fatalf("dropped = %d, want 1", d)
	}
	small.drain()
	if got := small.snapshot(); len(got) != 2 || got[0].Object != "o0" || got[1].Object != "o1" {
		t.Fatalf("ring after drop = %+v, want o0 and o1", got)
	}
	if page := small.query(activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 10}); page.Dropped != 1 {
		t.Fatalf("page dropped = %d, want 1", page.Dropped)
	}

	// Nil log accepts submits and returns empty queries
	var none *activityLog
	none.submit(activityEvent{AccessKey: "alice", API: "PutObject", Status: 200})
	if page := none.query(activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 10}); len(page.Events) != 0 || page.Denied != 0 {
		t.Fatalf("nil log page = %+v, want empty", page)
	}
}

func TestActivityRingWrap(t *testing.T) {
	l := newActivityLog("n1:9000", 3, 8)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		stored := l.append(activityEvent{Time: base.Add(time.Duration(i) * time.Second), AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: fmt.Sprintf("o%d", i), Status: 200})
		if stored.Seq != uint64(i) || stored.Node != "n1:9000" {
			t.Fatalf("append %d stored seq %d node %q", i, stored.Seq, stored.Node)
		}
	}
	snap := l.snapshot()
	if got := activitySeqs(snap); !reflect.DeepEqual(got, []uint64{2, 3, 4}) {
		t.Fatalf("snapshot seqs = %v, want [2 3 4]", got)
	}
	if snap[0].Object != "o2" || snap[2].Object != "o4" {
		t.Fatalf("snapshot objects = %q..%q, want o2..o4", snap[0].Object, snap[2].Object)
	}
	page := l.query(activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 10})
	if got := activitySeqs(page.Events); !reflect.DeepEqual(got, []uint64{4, 3, 2}) {
		t.Fatalf("query seqs = %v, want [4 3 2]", got)
	}
}

func TestTruncateActivityObject(t *testing.T) {
	long := strings.Repeat("a", 300)
	if got := truncateActivityObject(long); got != long[:256] {
		t.Fatalf("300 ASCII bytes -> %d bytes, want 256", len(got))
	}
	exact := strings.Repeat("b", 256)
	if got := truncateActivityObject(exact); got != exact {
		t.Fatalf("256 bytes changed to %d bytes", len(got))
	}
	if got := truncateActivityObject("docs/a.txt"); got != "docs/a.txt" {
		t.Fatalf("short name changed to %q", got)
	}

	// Cut inside 2-byte rune
	twoByte := strings.Repeat("a", 255) + "\u00e9" + "tail"
	if got := truncateActivityObject(twoByte); len(got) != 255 || !utf8.ValidString(got) {
		t.Fatalf("2-byte cut -> %d bytes, valid %v; want 255, true", len(got), utf8.ValidString(got))
	}

	// Cut on third byte of 4-byte rune
	fourByte := strings.Repeat("a", 254) + "\U0001F600" + "tail"
	if got := truncateActivityObject(fourByte); len(got) != 254 || !utf8.ValidString(got) {
		t.Fatalf("4-byte cut -> %d bytes, valid %v; want 254, true", len(got), utf8.ValidString(got))
	}

	// Truncate before queueing
	l := newActivityLog("n1:9000", 4, 4)
	l.submit(activityEvent{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: twoByte, Status: 200})
	l.drain()
	if got := l.snapshot(); len(got) != 1 || len(got[0].Object) != 255 {
		t.Fatalf("submitted object stored as %+v, want 255 bytes", got)
	}
}

func TestActivityQueryFilter(t *testing.T) {
	l := newActivityLog("n1:9000", 100, 100)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	seed := []activityEvent{
		{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: "a.txt", Status: 200},
		{AccessKey: "bob", Source: "s3", API: "DeleteObject", Bucket: "alpha", Object: "b.txt", Status: 204},
		{AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "beta", Object: "e.txt", Status: 200},
		{AccessKey: "alice", Source: "s3", API: "DeleteBucket", Bucket: "beta", Status: 204},
		{AccessKey: "root", Source: "admin", API: "AddUser", Status: 200},
		{AccessKey: "root", Source: "admin", API: "SetConfigKV", Status: 200},
		{AccessKey: "alice", Source: "s3", API: "DeleteMultipleObjects", Bucket: "alpha", Count: 2, Status: 200},
	}
	for i, e := range seed {
		e.Time = base.Add(time.Duration(i) * time.Second)
		l.append(e)
	}
	allKinds := []activityKind{activityKindObject, activityKindBucket, activityKindUser, activityKindPolicy, activityKindService, activityKindConfig}

	cases := []struct {
		name string
		f    activityFilter
		want []uint64
	}{
		{"own bucket hides beta and IAM", activityFilter{Buckets: []string{"alpha"}, Kinds: activityObjectKinds, Limit: 50}, []uint64{6, 1, 0}},
		{"all buckets shows beta", activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 50}, []uint64{6, 3, 2, 1, 0}},
		{"users tier adds AddUser", activityFilter{AllBuckets: true, Kinds: append([]activityKind{activityKindUser}, activityObjectKinds...), Limit: 50}, []uint64{6, 4, 3, 2, 1, 0}},
		{"every kind", activityFilter{AllBuckets: true, Kinds: allKinds, Limit: 50}, []uint64{6, 5, 4, 3, 2, 1, 0}},
		{"IAM events ignore the bucket set", activityFilter{Buckets: []string{"alpha"}, Kinds: []activityKind{activityKindUser}, Limit: 50}, []uint64{4}},
		{"access key", activityFilter{Buckets: []string{"alpha"}, Kinds: activityObjectKinds, AccessKeys: []string{"alice"}, Limit: 50}, []uint64{6, 0}},
		{"delete group", activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Group: "delete", Limit: 50}, []uint64{6, 1}},
		{"bucket group", activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Group: "bucket", Limit: 50}, []uint64{3}},
		{"config group outside kinds", activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Group: "config", Limit: 50}, []uint64{}},
		{"limit keeps the newest", activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 2}, []uint64{6, 3}},
		{"limit 0 returns none", activityFilter{AllBuckets: true, Kinds: activityObjectKinds}, []uint64{}},
		{"no kinds returns none", activityFilter{AllBuckets: true, Limit: 50}, []uint64{}},
	}
	for _, tc := range cases {
		page := l.query(tc.f)
		if got := activitySeqs(page.Events); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: seqs = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestActivityQueryCursorTie(t *testing.T) {
	l := newActivityLog("n1:9000", 100, 100)
	ts := time.Date(2026, 10, 9, 12, 0, 0, 123456789, time.UTC)
	for i := 0; i < 5; i++ {
		l.append(activityEvent{Time: ts, AccessKey: "alice", Source: "s3", API: "PutObject", Bucket: "alpha", Object: fmt.Sprintf("o%d", i), Status: 200})
	}

	// Page with cursor returned through JSON
	f := activityFilter{AllBuckets: true, Kinds: activityObjectKinds, Limit: 2}
	var sizes []int
	var order []uint64
	for i := 0; i < 5; i++ {
		page := l.query(f)
		if len(page.Events) == 0 {
			break
		}
		sizes = append(sizes, len(page.Events))
		order = append(order, activitySeqs(page.Events)...)
		raw, err := json.Marshal(page.Events[len(page.Events)-1].cursor())
		if err != nil {
			t.Fatal(err)
		}
		var next ActivityCursor
		if err := json.Unmarshal(raw, &next); err != nil {
			t.Fatal(err)
		}
		f.Before = &next
	}
	if !reflect.DeepEqual(sizes, []int{2, 2, 1}) {
		t.Fatalf("page sizes = %v, want [2 2 1]", sizes)
	}
	if !reflect.DeepEqual(order, []uint64{4, 3, 2, 1, 0}) {
		t.Fatalf("seqs across pages = %v, want each of 4 3 2 1 0 once", order)
	}

	// Equal times sort by node, then seq
	a := activityEvent{Time: ts, Node: "n1:9000", Seq: 9}
	b := activityEvent{Time: ts, Node: "n2:9000", Seq: 0}
	if !activityOlder(a, b.cursor()) || activityOlder(b, a.cursor()) || activityOlder(a, a.cursor()) {
		t.Fatal("activityOlder must order equal times by node, then seq, and be strict")
	}
	mixed := []activityEvent{
		{Time: ts, Node: "n1:9000", Seq: 0},
		{Time: ts.Add(-time.Second), Node: "n2:9000", Seq: 7},
		{Time: ts, Node: "n2:9000", Seq: 0},
		{Time: ts, Node: "n1:9000", Seq: 1},
	}
	sortActivityNewest(mixed)
	want := []activityEvent{
		{Time: ts, Node: "n2:9000", Seq: 0},
		{Time: ts, Node: "n1:9000", Seq: 1},
		{Time: ts, Node: "n1:9000", Seq: 0},
		{Time: ts.Add(-time.Second), Node: "n2:9000", Seq: 7},
	}
	if !reflect.DeepEqual(mixed, want) {
		t.Fatalf("sortActivityNewest = %+v, want %+v", mixed, want)
	}
}

func TestActivityS3Recording(t *testing.T) {
	ExecObjectLayerAPITest(t, testActivityS3Recording, []string{"PutObject", "GetObject", "HeadObject", "ListObjectsV2", "DeleteMultipleObjects", "PostPolicy"})
}

// Record signed writes, ignore reads, count anonymous or refused writes as denied
func testActivityS3Recording(obj ObjectLayer, instanceType, bucketName string, apiRouter http.Handler, credentials auth.Credentials, t *testing.T) {
	globalIAMSys.InitStore(obj)
	l := pinActivityLog(t)
	body := []byte("hello obstor")
	serve := func(req *http.Request, err error, want int) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: build request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: %s %s = HTTP %d, want %d", instanceType, req.Method, req.URL, rec.Code, want)
		}
	}
	signed := func(method, target string, payload []byte) (*http.Request, error) {
		return newTestSignedRequestV4(method, target, int64(len(payload)), bytes.NewReader(payload),
			credentials.AccessKey, credentials.SecretKey, nil)
	}

	for _, name := range []string{"anon-1.txt", "anon-2.txt", "anon-3.txt"} {
		req, err := newTestRequest(http.MethodPut, getPutObjectURL("", bucketName, name), int64(len(body)), bytes.NewReader(body))
		serve(req, err, http.StatusForbidden)
	}
	l.drain()
	if events := l.snapshot(); len(events) != 0 {
		t.Fatalf("%s: anonymous PUTs recorded %+v, want an empty ring", instanceType, events)
	}
	if got := l.denied.Load(); got != 3 {
		t.Fatalf("%s: denied = %d after three anonymous PUTs, want 3", instanceType, got)
	}

	req, err := signed(http.MethodPut, getPutObjectURL("", bucketName, "a.txt"), body)
	serve(req, err, http.StatusOK)
	for _, read := range []struct{ method, target string }{
		{http.MethodGet, getGetObjectURL("", bucketName, "a.txt")},
		{http.MethodHead, getHeadObjectURL("", bucketName, "a.txt")},
		{http.MethodGet, getListObjectsV2URL("", bucketName, "", "1000", "", "")},
	} {
		req, err = signed(read.method, read.target, nil)
		serve(req, err, http.StatusOK)
	}
	l.drain()
	events := l.snapshot()
	if len(events) != 1 {
		t.Fatalf("%s: events after one PUT and three reads = %+v, want one", instanceType, events)
	}
	put := events[0]
	if put.Source != "s3" || put.API != "PutObject" || put.AccessKey != credentials.AccessKey ||
		put.Bucket != bucketName || put.Object != "a.txt" || put.Status != http.StatusOK || put.Count != 0 {
		t.Fatalf("%s: PUT event = %+v, want s3 PutObject a.txt by %s with status 200", instanceType, put, credentials.AccessKey)
	}

	req, err = newPostRequestV4("", bucketName, "form", body, credentials.AccessKey, credentials.SecretKey)
	serve(req, err, http.StatusNoContent)
	l.drain()
	events = l.snapshot()
	if len(events) != 2 {
		t.Fatalf("%s: events after the POST-policy upload = %+v, want two", instanceType, events)
	}
	if post := events[1]; post.Source != "s3" || post.API != "PostPolicyBucket" || post.AccessKey != credentials.AccessKey ||
		post.Bucket != bucketName || post.Object != "form/upload.txt" || post.Status != http.StatusNoContent {
		t.Fatalf("%s: POST-policy event = %+v, want s3 PostPolicyBucket form/upload.txt by %s with status 204", instanceType, post, credentials.AccessKey)
	}
	if got := l.denied.Load(); got != 3 {
		t.Fatalf("%s: denied = %d after the POST-policy upload, want 3", instanceType, got)
	}

	var toDelete DeleteObjectsRequest
	for _, name := range []string{"d1.txt", "d2.txt", "d3.txt"} {
		if _, err = obj.PutObject(GlobalContext, bucketName, name,
			mustGetPutObjReader(t, bytes.NewReader(body), int64(len(body)), "", ""), ObjectOptions{}); err != nil {
			t.Fatalf("%s: seed %s: %v", instanceType, name, err)
		}
		toDelete.Objects = append(toDelete.Objects, ObjectToDelete{ObjectName: name})
	}
	req, err = signed(http.MethodPost, getDeleteMultipleObjectsURL("", bucketName), encodeResponse(toDelete))
	serve(req, err, http.StatusOK)
	l.drain()
	events = l.snapshot()
	if len(events) != 3 {
		t.Fatalf("%s: events after the multi-delete = %+v, want three", instanceType, events)
	}
	del := events[2]
	if del.Source != "s3" || del.API != "DeleteMultipleObjects" || del.Count != 3 ||
		del.Bucket != bucketName || del.Object != "" || del.Status != http.StatusOK {
		t.Fatalf("%s: delete-multiple event = %+v, want API DeleteMultipleObjects, Count 3, Status 200", instanceType, del)
	}

	// All keys denied still returns HTTP 200, counts as denied only
	reader := makeTestIAMUser(t, "activityreader", "s3:GetObject")
	toDelete.Objects = []ObjectToDelete{{ObjectName: "a.txt"}, {ObjectName: "form/upload.txt"}}
	payload := encodeResponse(toDelete)
	req, err = newTestSignedRequestV4(http.MethodPost, getDeleteMultipleObjectsURL("", bucketName), int64(len(payload)),
		bytes.NewReader(payload), reader.AccessKey, reader.SecretKey, nil)
	serve(req, err, http.StatusOK)
	l.drain()
	if events = l.snapshot(); len(events) != 3 {
		t.Fatalf("%s: refused multi-delete recorded %+v, want the ring unchanged at three", instanceType, events[3:])
	}

	page := l.query(activityFilter{AllBuckets: true, Kinds: []activityKind{activityKindObject}, Limit: activityDefaultLimit})
	if len(page.Events) != 3 || page.Denied != 4 || page.Dropped != 0 {
		t.Fatalf("%s: query = %d events, denied %d, dropped %d; want 3, 4, 0", instanceType, len(page.Events), page.Denied, page.Dropped)
	}
}

// Admin signer reaches ReqInfo, only listed API names record
func TestActivityAdminAccessKey(t *testing.T) {
	objLayer, fsDir, err := prepareFS()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(fsDir) }()
	if err = newTestConfig(globalObstorDefaultRegion, objLayer); err != nil {
		t.Fatalf("init config: %v", err)
	}
	savedCred := globalActiveCred
	defer func() { globalActiveCred = savedCred }()
	creds, err := auth.CreateCredentials("admin", "mypassword")
	if err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	globalActiveCred = creds
	l := pinActivityLog(t)

	ctx := logger.SetReqInfo(context.Background(), &logger.ReqInfo{API: "AddUser"})
	req := mustNewSignedRequest(http.MethodPut, "http://localhost:9000/obstor/admin/v3/add-user?accessKey=bob", 0, nil, t)
	if _, _, _, s3Err := validateAdminSignature(ctx, req, globalObstorDefaultRegion); s3Err != ErrNone {
		t.Fatalf("validateAdminSignature = %v, want ErrNone", s3Err)
	}
	reqInfo := logger.GetReqInfo(ctx)
	if reqInfo.AccessKey != "admin" {
		t.Fatalf("reqInfo.AccessKey = %q, want %q", reqInfo.AccessKey, "admin")
	}

	recordAuditActivity(ctx, reqInfo, http.StatusOK)
	for _, api := range []string{"ListUsers", "WebUpload", "web.MakeBucket", "MakeBucket"} {
		recordAuditActivity(ctx, &logger.ReqInfo{API: api, AccessKey: "admin"}, http.StatusOK)
	}
	l.drain()
	events := l.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %+v, want only AddUser", events)
	}
	if e := events[0]; e.Source != "admin" || e.API != "AddUser" || e.AccessKey != "admin" || e.Status != http.StatusOK {
		t.Fatalf("event = %+v, want admin AddUser by admin with status 200", e)
	}
	if got := l.denied.Load(); got != 0 {
		t.Fatalf("denied = %d, want 0 (names outside the audit list are ignored, not denied)", got)
	}
}

// Temporary credential records parent user
func TestActivitySTSParent(t *testing.T) {
	objLayer, fsDir, err := prepareFS()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(fsDir) }()
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(objLayer)
	l := pinActivityLog(t)

	temp := auth.Credentials{
		AccessKey:    "TEMPACCESSKEY0001",
		SecretKey:    "temp-secret-key-0001",
		SessionToken: "temp-session-token",
		Expiration:   UTCNow().Add(time.Hour),
		Status:       auth.AccountOn,
		ParentUser:   "alice",
	}
	if err = globalIAMSys.SetTempUser(temp.AccessKey, temp, ""); err != nil {
		t.Fatalf("set temp user: %v", err)
	}

	recordAuditActivity(context.Background(), &logger.ReqInfo{
		API: "PutObject", BucketName: "alpha", ObjectName: "a.txt", AccessKey: temp.AccessKey,
	}, http.StatusOK)
	l.drain()
	events := l.snapshot()
	if len(events) != 1 || events[0].AccessKey != "alice" || events[0].Source != "s3" {
		t.Fatalf("events = %+v, want one s3 event recorded as alice", events)
	}
}

// Scanner audit has nil writer and request, ignored entirely
func TestActivityNilWriterNeverRecords(t *testing.T) {
	if logger.ActivityHook == nil {
		t.Fatal("logger.ActivityHook is nil, want recordAuditActivity set at init")
	}
	l := pinActivityLog(t)
	ctx := logger.SetReqInfo(context.Background(), &logger.ReqInfo{
		API: "PutObject", BucketName: "alpha", ObjectName: "expired.txt", AccessKey: "obstoradmin",
	})
	logger.AuditLog(ctx, nil, nil, nil)
	l.drain()
	if events := l.snapshot(); len(events) != 0 {
		t.Fatalf("nil writer recorded %+v, want an empty ring", events)
	}
	if d, dr := l.denied.Load(), l.dropped.Load(); d != 0 || dr != 0 {
		t.Fatalf("denied %d, dropped %d; want 0 and 0", d, dr)
	}
}

func TestWebActivityTarget(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		args   reflect.Value
		bucket string
		object string
		count  int
	}{
		{"bucket", "MakeBucket", reflect.ValueOf(&MakeBucketArgs{BucketName: "alpha", LockEnabled: true}), "alpha", "", 0},
		{"policy prefix", "SetBucketPolicy", reflect.ValueOf(&SetBucketPolicyWebArgs{BucketName: "alpha", Prefix: "docs/", Policy: "readonly"}), "alpha", "docs/", 0},
		{"one object", "RemoveObject", reflect.ValueOf(&RemoveObjectArgs{BucketName: "alpha", Objects: []string{"docs/a.txt"}}), "alpha", "docs/a.txt", 0},
		{"three objects", "RemoveObject", reflect.ValueOf(&RemoveObjectArgs{BucketName: "alpha", Objects: []string{"a.txt", "b.txt", "docs/"}}), "alpha", "", 3},
		{"no objects", "RemoveObject", reflect.ValueOf(&RemoveObjectArgs{BucketName: "alpha"}), "alpha", "", 0},
		{"new user", "AddIAMUser", reflect.ValueOf(&AddIAMUserArgs{AccessKey: "alice", SecretKey: "alice-secret", Policy: "readwrite"}), "", "alice", 0},
		{"user status", "SetIAMUserStatus", reflect.ValueOf(&SetIAMUserStatusArgs{AccessKey: "bob"}), "", "bob", 0},
		{"user policy", "SetIAMUserPolicy", reflect.ValueOf(&SetIAMUserPolicyArgs{AccessKey: "bob", Policies: "readonly"}), "", "bob", 0},
		{"save policy", "SetCannedPolicy", reflect.ValueOf(&SetCannedPolicyArgs{Name: "team-read", Policy: "{}"}), "", "team-read", 0},
		{"delete policy", "DeleteCannedPolicy", reflect.ValueOf(&DeleteCannedPolicyArgs{Name: "team-read"}), "", "team-read", 0},
		{"no args", "MakeBucket", reflect.Value{}, "", "", 0},
		{"not a pointer", "MakeBucket", reflect.ValueOf(MakeBucketArgs{BucketName: "alpha"}), "", "", 0},
		{"nil pointer", "MakeBucket", reflect.ValueOf((*MakeBucketArgs)(nil)), "", "", 0},
	} {
		bucket, object, count := webActivityTarget(test.method, test.args)
		if bucket != test.bucket || object != test.object || count != test.count {
			t.Errorf("%s: target = %q %q %d, want %q %q %d", test.name, bucket, object, count, test.bucket, test.object, test.count)
		}
	}

	log := pinActivityLog(t)
	ctx := context.Background()
	recordWebActivity(ctx, "SetAuth", reflect.ValueOf(&SetAuthArgs{CurrentSecretKey: "old-secret", NewSecretKey: "new-secret"}), "alice", true, nil)
	recordWebActivity(ctx, "ListBuckets", reflect.ValueOf(&WebGenericArgs{}), "alice", true, nil)
	recordWebActivity(ctx, "MakeBucket", reflect.Value{}, "alice", true, toJSONError(ctx, errInvalidBucketName))
	// Failure before checks names no target, only denials count
	recordWebActivity(ctx, "RemoveObject", reflect.ValueOf(&RemoveObjectArgs{BucketName: "finance"}), "bob", false, toJSONError(ctx, errInvalidArgument))
	recordWebActivity(ctx, "RemoveObject", reflect.ValueOf(&RemoveObjectArgs{BucketName: "finance", Objects: []string{"x"}}), "bob", false, toJSONError(ctx, errAccessDenied))
	recordWebActivity(ctx, "MakeBucket", reflect.ValueOf(&MakeBucketArgs{BucketName: "finance"}), "bob", false, nil)
	// Denied walked key still records deletes after initial checks passed
	recordWebActivity(ctx, "RemoveObject", reflect.ValueOf(&RemoveObjectArgs{BucketName: "alpha", Objects: []string{"docs/"}}), "carol", true, toJSONError(ctx, errAccessDenied))
	log.drain()
	got := log.snapshot()
	if len(got) != 2 || got[0].API != "SetAuth" || got[0].Source != "console" || got[0].AccessKey != "alice" ||
		got[0].Bucket != "" || got[0].Object != "alice" || got[0].Status != http.StatusOK {
		t.Fatalf("recorded %+v, want a console SetAuth by alice naming alice, then carol's RemoveObject", got)
	}
	if e := got[1]; e.API != "RemoveObject" || e.AccessKey != "carol" || e.Bucket != "alpha" || e.Object != "docs/" || e.Status != http.StatusForbidden {
		t.Fatalf("second event = %+v, want carol's RemoveObject of alpha/docs/ with status 403", e)
	}
	if d := log.denied.Load(); d != 1 {
		t.Fatalf("denied = %d, want 1 for bob's refused RemoveObject", d)
	}
}

func TestActivityWebStatus(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, http.StatusOK},
		{"access denied", toJSONError(ctx, errAccessDenied), http.StatusForbidden},
		{"missing bucket", toJSONError(ctx, BucketNotFound{Bucket: "alpha"}, "alpha"), http.StatusNotFound},
		{"bucket not empty", toJSONError(ctx, BucketNotEmpty{Bucket: "alpha"}, "alpha"), http.StatusConflict},
		{"no token", toJSONError(ctx, errNoAuthToken), http.StatusBadRequest},
		{"expired token", toJSONError(ctx, errWebTokenInvalid), http.StatusUnauthorized},
		{"server starting", toJSONError(ctx, errServerNotInitialized), http.StatusServiceUnavailable},
		{"wrong current secret", toJSONError(ctx, errIncorrectCreds), http.StatusBadRequest},
		{"not implemented", toJSONError(ctx, NotImplemented{}), http.StatusBadRequest},
		{"sts not initialized", toJSONError(ctx, errSTSNotInitialized), http.StatusServiceUnavailable},
		{"input error", &json2.Error{Message: "quota type must be hard or fifo"}, http.StatusBadRequest},
		{"unknown code", &json2.Error{Message: "odd", Data: map[string]string{"code": "NoSuchThing"}}, http.StatusInternalServerError},
		{"raw error", errInvalidBucketName, http.StatusBadRequest},
	} {
		if got := webActivityStatus(ctx, test.err); got != test.want {
			t.Errorf("%s: status = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestActivityConsoleRecording(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		testActivityConsoleRecording(t, obj, instanceType)
	})
}

// Console mutations record once, reads and invalid or unauthenticated calls do not
func testActivityConsoleRecording(t *testing.T, obj ObjectLayer, instanceType string) {
	defer pinBucketGlobals(instanceType)()
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	log := pinActivityLog(t)
	root := globalActiveCred.AccessKey
	token, err := getWebRPCToken(router, root, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatalf("%s: authenticate: %v", instanceType, err)
	}

	call := func(method, bearer string, params, reply interface{}) error {
		t.Helper()
		req, err := newTestWebRPCRequest("web."+method, bearer, params)
		if err != nil {
			t.Fatalf("%s: %s request: %v", instanceType, method, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return getTestWebRPCResponse(rec, reply)
	}
	upload := func(bucket, object string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, "/obstor/upload/"+bucket+"/"+object, bytes.NewReader([]byte("hello obstor")))
		if err != nil {
			t.Fatalf("%s: upload request: %v", instanceType, err)
		}
		req.Header.Set("User-Agent", "Mozilla")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	seen := 0
	fresh := func() []activityEvent {
		log.drain()
		all := log.snapshot()
		events := all[seen:]
		seen = len(all)
		return events
	}

	bucket := getRandomBucketName()
	if err := call("MakeBucket", token, MakeBucketArgs{BucketName: bucket}, &WebGenericRep{}); err != nil {
		t.Fatalf("%s: MakeBucket: %v", instanceType, err)
	}
	// After-func audit uses web.MakeBucket, absent from audit table, so no duplicate
	if got := fresh(); len(got) != 1 || got[0].Source != "console" || got[0].API != "MakeBucket" || got[0].Bucket != bucket ||
		got[0].AccessKey != root || got[0].Status != http.StatusOK || got[0].Node != "test-node:9000" {
		t.Fatalf("%s: MakeBucket recorded %+v, want one console MakeBucket 200 by %s", instanceType, got, root)
	}

	if err := call("ListBuckets", token, WebGenericArgs{}, &ListBucketsRep{}); err != nil {
		t.Fatalf("%s: ListBuckets: %v", instanceType, err)
	}
	if got := fresh(); len(got) != 0 {
		t.Fatalf("%s: ListBuckets recorded %+v, want nothing", instanceType, got)
	}

	names := []string{"a.txt", "docs/b.txt", "docs/c.txt", "solo.txt"}
	for _, name := range names {
		if code := upload(bucket, name); code != http.StatusOK {
			t.Fatalf("%s: upload %s: HTTP %d", instanceType, name, code)
		}
	}
	got := fresh()
	if len(got) != len(names) {
		t.Fatalf("%s: uploads recorded %+v, want %d WebUpload events", instanceType, got, len(names))
	}
	for i, name := range names {
		if e := got[i]; e.Source != "console" || e.API != "WebUpload" || e.Bucket != bucket || e.Object != name ||
			e.AccessKey != root || e.Status != http.StatusOK {
			t.Fatalf("%s: upload %d recorded %+v, want WebUpload of %s", instanceType, i, e, name)
		}
	}
	missing := getRandomBucketName()
	if code := upload(missing, "a.txt"); code == http.StatusOK {
		t.Fatalf("%s: upload into a missing bucket answered 200", instanceType)
	}
	if got := fresh(); len(got) != 1 || got[0].API != "WebUpload" || got[0].Bucket != missing || got[0].Status < http.StatusBadRequest {
		t.Fatalf("%s: failed upload recorded %+v, want one WebUpload with its error status", instanceType, got)
	}

	if err := call("RemoveObject", token, RemoveObjectArgs{BucketName: bucket, Objects: names[:3]}, &WebGenericRep{}); err != nil {
		t.Fatalf("%s: RemoveObject of three: %v", instanceType, err)
	}
	if got := fresh(); len(got) != 1 || got[0].API != "RemoveObject" || got[0].Bucket != bucket || got[0].Object != "" ||
		got[0].Count != 3 || got[0].Status != http.StatusOK {
		t.Fatalf("%s: RemoveObject of three recorded %+v, want one event with count 3", instanceType, got)
	}
	if err := call("RemoveObject", token, RemoveObjectArgs{BucketName: bucket, Objects: []string{"solo.txt"}}, &WebGenericRep{}); err != nil {
		t.Fatalf("%s: RemoveObject of one: %v", instanceType, err)
	}
	if got := fresh(); len(got) != 1 || got[0].Object != "solo.txt" || got[0].Count != 0 {
		t.Fatalf("%s: RemoveObject of one recorded %+v, want object solo.txt and no count", instanceType, got)
	}

	if err := call("DeleteBucket", token, RemoveBucketArgs{BucketName: missing}, &WebGenericRep{}); err == nil {
		t.Fatalf("%s: DeleteBucket of a missing bucket succeeded", instanceType)
	}
	if got := fresh(); len(got) != 1 || got[0].API != "DeleteBucket" || got[0].Bucket != missing || got[0].Status != http.StatusNotFound {
		t.Fatalf("%s: DeleteBucket of a missing bucket recorded %+v, want status 404", instanceType, got)
	}

	var added AddIAMUserRep
	if err := call("AddIAMUser", token, AddIAMUserArgs{}, &added); err != nil || added.AccessKey == "" {
		t.Fatalf("%s: AddIAMUser with a generated key = %+v, %v", instanceType, added, err)
	}
	if got := fresh(); len(got) != 1 || got[0].API != "AddIAMUser" || got[0].Bucket != "" || got[0].Object != added.AccessKey {
		t.Fatalf("%s: AddIAMUser recorded %+v, want object %s", instanceType, got, added.AccessKey)
	}

	// Failed pre-check cannot add activity to inaccessible bucket
	outsider := makeTestIAMUser(t, "activityoutsider", "s3:ListBucket")
	outsiderToken, err := getWebRPCToken(router, outsider.AccessKey, outsider.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, outsider.AccessKey, err)
	}
	deniedBefore := log.denied.Load()
	if err := call("RemoveObject", outsiderToken, RemoveObjectArgs{BucketName: bucket, Objects: []string{}}, &WebGenericRep{}); err == nil {
		t.Fatalf("%s: RemoveObject with no names succeeded", instanceType)
	}
	if got := fresh(); len(got) != 0 || log.denied.Load() != deniedBefore {
		t.Fatalf("%s: empty RemoveObject recorded %+v with denied %d, want nothing and denied %d", instanceType, got, log.denied.Load(), deniedBefore)
	}

	// Check every name before deletion, denied name keeps allowed object
	for _, name := range []string{"mine/a.txt", "other/x.txt"} {
		if code := upload(bucket, name); code != http.StatusOK {
			t.Fatalf("%s: upload %s: HTTP %d", instanceType, name, code)
		}
	}
	fresh()
	scoped := makeTestIAMUserDoc(t, "activityscoped", `{"Version":"2012-10-17","Statement":[`+
		`{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]},`+
		`{"Effect":"Allow","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::`+bucket+`/mine/*"]}]}`)
	scopedToken, err := getWebRPCToken(router, scoped.AccessKey, scoped.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, scoped.AccessKey, err)
	}
	if err := call("RemoveObject", scopedToken, RemoveObjectArgs{BucketName: bucket, Objects: []string{"mine/a.txt", "other/x.txt"}}, &WebGenericRep{}); err == nil {
		t.Fatalf("%s: RemoveObject naming a refused object succeeded", instanceType)
	}
	if _, err := obj.GetObjectInfo(context.Background(), bucket, "mine/a.txt", ObjectOptions{}); err != nil {
		t.Fatalf("%s: refused RemoveObject still deleted mine/a.txt: %v", instanceType, err)
	}
	if got := fresh(); len(got) != 0 || log.denied.Load() != deniedBefore+1 {
		t.Fatalf("%s: refused RemoveObject recorded %+v with denied %d, want nothing and denied %d", instanceType, got, log.denied.Load(), deniedBefore+1)
	}

	// Check attach permission before creating user
	creator := makeTestIAMUserDoc(t, "activitycreator", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:CreateUser"]}]}`)
	creatorToken, err := getWebRPCToken(router, creator.AccessKey, creator.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, creator.AccessKey, err)
	}
	if err := call("AddIAMUser", creatorToken, AddIAMUserArgs{AccessKey: "activityghost", SecretKey: "activityghost-secret", Policy: "readwrite"}, &AddIAMUserRep{}); err == nil {
		t.Fatalf("%s: AddIAMUser with a policy and no attach permission succeeded", instanceType)
	}
	if _, ok := globalIAMSys.GetUser("activityghost"); ok {
		t.Fatalf("%s: refused AddIAMUser still created activityghost", instanceType)
	}
	if got := fresh(); len(got) != 0 || log.denied.Load() != deniedBefore+2 {
		t.Fatalf("%s: refused AddIAMUser recorded %+v with denied %d, want nothing and denied %d", instanceType, got, log.denied.Load(), deniedBefore+2)
	}

	// SetAuth records caller after rotating token's signing secret
	user := makeTestIAMUser(t, "activitysetauth", "s3:ListBucket")
	userToken, err := getWebRPCToken(router, user.AccessKey, user.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, user.AccessKey, err)
	}
	denied := log.denied.Load()
	if err := call("SetAuth", userToken, SetAuthArgs{CurrentSecretKey: user.SecretKey, NewSecretKey: "activitysetauth-new"}, &SetAuthReply{}); err != nil {
		t.Fatalf("%s: SetAuth: %v", instanceType, err)
	}
	if got := fresh(); len(got) != 1 || got[0].API != "SetAuth" || got[0].AccessKey != user.AccessKey || got[0].Object != user.AccessKey ||
		got[0].Status != http.StatusOK || log.denied.Load() != denied {
		t.Fatalf("%s: SetAuth recorded %+v with denied %d, want one SetAuth by %s and denied %d", instanceType, got, log.denied.Load(), user.AccessKey, denied)
	}

	if err := call("MakeBucket", "", MakeBucketArgs{BucketName: getRandomBucketName()}, &WebGenericRep{}); err == nil {
		t.Fatalf("%s: MakeBucket without a token succeeded", instanceType)
	}
	if got := fresh(); len(got) != 0 || log.denied.Load() != denied+1 {
		t.Fatalf("%s: tokenless MakeBucket recorded %+v with denied %d, want nothing and denied %d", instanceType, got, log.denied.Load(), denied+1)
	}

	if err := call("MakeBucket", token, "not-an-object", &WebGenericRep{}); err == nil {
		t.Fatalf("%s: MakeBucket with undecodable params succeeded", instanceType)
	}
	if got := fresh(); len(got) != 0 || log.denied.Load() != denied+1 {
		t.Fatalf("%s: undecodable MakeBucket recorded %+v with denied %d, want nothing and denied %d", instanceType, got, log.denied.Load(), denied+1)
	}

	// Wrong-secret token cannot name root in failed event
	forged, err := getTokenString(root, "not-the-root-secret")
	if err != nil {
		t.Fatalf("%s: forge token: %v", instanceType, err)
	}
	req, err := newTestWebRPCRequest("web.MakeBucket", forged, MakeBucketArgs{BucketName: bucket})
	if err != nil {
		t.Fatalf("%s: forged request: %v", instanceType, err)
	}
	webRPCAfter(&jsonrpc.RequestInfo{
		Args:           reflect.ValueOf(&MakeBucketArgs{BucketName: bucket}),
		Method:         "web.MakeBucket",
		Error:          toJSONError(context.Background(), errServerNotInitialized),
		ResponseWriter: httptest.NewRecorder(),
		Request:        withWebRPCAuth(req),
	})
	if got := fresh(); len(got) != 0 || log.denied.Load() != denied+2 {
		t.Fatalf("%s: forged token recorded %+v with denied %d, want nothing and denied %d", instanceType, got, log.denied.Load(), denied+2)
	}
}

func TestRecordActivitySFTP(t *testing.T) {
	log := pinActivityLog(t)
	RecordActivity("sftp", "obstoradmin", "PutObject", "alpha", "docs/a.txt", http.StatusOK)
	RecordActivity("sftp", "obstoradmin", "RenameObject", "alpha", "docs/b.txt", http.StatusInternalServerError)
	RecordActivity("sftp", "obstoradmin", "Readdir", "alpha", "", http.StatusOK)
	RecordActivity("sftp", "", "DeleteObject", "alpha", "docs/a.txt", http.StatusOK)
	log.drain()
	got := log.snapshot()
	if len(got) != 2 {
		t.Fatalf("recorded %+v, want the PutObject and RenameObject events only", got)
	}
	if e := got[0]; e.Source != "sftp" || e.API != "PutObject" || e.AccessKey != "obstoradmin" || e.Bucket != "alpha" ||
		e.Object != "docs/a.txt" || e.Status != http.StatusOK {
		t.Errorf("first event = %+v, want sftp PutObject of alpha/docs/a.txt by obstoradmin", e)
	}
	if e := got[1]; e.Source != "sftp" || e.API != "RenameObject" || e.Status != http.StatusInternalServerError {
		t.Errorf("second event = %+v, want sftp RenameObject with status 500", e)
	}
	if d := log.denied.Load(); d != 1 {
		t.Errorf("denied = %d, want 1 for the anonymous delete", d)
	}
}

// Create enabled IAM user with same name as policy
func makeTestIAMUserDoc(t TestErrHandler, name, doc string) auth.Credentials {
	t.Helper()
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse policy %s: %v", name, err)
	}
	if err = globalIAMSys.SetPolicy(name, *p); err != nil {
		t.Fatalf("set policy %s: %v", name, err)
	}
	cred := auth.Credentials{AccessKey: name, SecretKey: name + "-secret"}
	if err = globalIAMSys.CreateUser(cred.AccessKey, madmin.UserInfo{SecretKey: cred.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	if err = globalIAMSys.PolicyDBSet(cred.AccessKey, name, false); err != nil {
		t.Fatalf("attach policy %s: %v", name, err)
	}
	return cred
}

func TestActivityAdminSubject(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		testActivityAdminSubject(t, obj, instanceType)
	})
}

// IAM events name changed user or policy
func testActivityAdminSubject(t *testing.T, obj ObjectLayer, instanceType string) {
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(obj)
	router := mux.NewRouter()
	registerAdminRouter(router, true, true)
	user := makeTestIAMUser(t, "activitysubject", "s3:ListBucket")
	log := pinActivityLog(t)

	admin := func(method, path string, query url.Values, body []byte) {
		t.Helper()
		target := adminPathPrefix + adminAPIVersionPrefix + path + "?" + query.Encode()
		req, err := newTestSignedRequestV4(method, target, int64(len(body)), bytes.NewReader(body),
			globalActiveCred.AccessKey, globalActiveCred.SecretKey, nil)
		if err != nil {
			t.Fatalf("%s: build %s: %v", instanceType, path, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
			t.Fatalf("%s: %s %s = HTTP %d: %s", instanceType, method, path, rec.Code, rec.Body.String())
		}
	}
	doc := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::*"]}]}`)
	admin(http.MethodPut, "/add-canned-policy", url.Values{"name": {"team-read"}}, doc)
	admin(http.MethodPut, "/set-user-status", url.Values{"accessKey": {user.AccessKey}, "status": {"disabled"}}, nil)
	admin(http.MethodDelete, "/remove-canned-policy", url.Values{"name": {"team-read"}}, nil)

	log.drain()
	got := log.snapshot()
	want := []struct{ api, subject string }{
		{"AddCannedPolicy", "team-read"},
		{"SetUserStatus", user.AccessKey},
		{"RemoveCannedPolicy", "team-read"},
	}
	if len(got) != len(want) {
		t.Fatalf("%s: recorded %+v, want %d admin events", instanceType, got, len(want))
	}
	for i, w := range want {
		if e := got[i]; e.Source != "admin" || e.API != w.api || e.Object != w.subject {
			t.Errorf("%s: event %d = %+v, want admin %s naming %s", instanceType, i, e, w.api, w.subject)
		}
	}
}
