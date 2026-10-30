package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/kms"
	"github.com/obstor/obstor/pkg/madmin"
	xnet "github.com/obstor/obstor/pkg/net"
)

// Call RPC as root, return error
func rootWebRPC(t TestErrHandler, router http.Handler, method string, args, reply interface{}) error {
	t.Helper()
	return userWebRPC(t, router, globalActiveCred.AccessKey, globalActiveCred.SecretKey, method, args, reply)
}

// Call RPC as user, return error
func userWebRPC(t TestErrHandler, router http.Handler, accessKey, secretKey, method string, args, reply interface{}) error {
	t.Helper()
	token, err := getWebRPCToken(router, accessKey, secretKey)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	req, err := newTestWebRPCRequest("web."+method, token, args)
	if err != nil {
		t.Fatalf("%s request: %v", method, err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d", method, rec.Code)
	}
	return getTestWebRPCResponse(rec, reply)
}

// Create random bucket or fail test
func makeTestBucket(t TestErrHandler, obj ObjectLayer, opts BucketOptions) string {
	t.Helper()
	bucket := getRandomBucketName()
	if err := obj.MakeBucketWithLocation(context.Background(), bucket, opts); err != nil {
		t.Fatalf("make bucket: %v", err)
	}
	return bucket
}

// Pin mode and KMS globals, erasure suite leaves flag false and earlier tests leave state
func pinBucketGlobals(instanceType string) func() {
	erasure, dist, backend, kmsKey, auto := globalIsErasure, globalIsDistErasure, globalIsBackend, GlobalKMS, globalAutoEncryption
	globalIsErasure, globalIsDistErasure, globalIsBackend = instanceType == ErasureTestStr, false, false
	GlobalKMS, globalAutoEncryption = nil, false
	return func() {
		globalIsErasure, globalIsDistErasure, globalIsBackend = erasure, dist, backend
		GlobalKMS, globalAutoEncryption = kmsKey, auto
	}
}

func TestWebHandlerGetBucketToggles(t *testing.T) {
	ExecObjectLayerTest(t, testGetBucketTogglesWebHandler)
}

// Needs object layer and valid bucket, same as SetBucketToggles
func testGetBucketTogglesWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})

	var rep GetBucketTogglesRep
	err := rootWebRPC(t, router, "GetBucketToggles", &GetBucketTogglesArgs{BucketName: bucket}, &rep)
	if err != nil || !rep.S3Enabled || rep.SFTPEnabled {
		t.Fatalf("%s: defaults = %+v, %v; want S3 on, SFTP off", instanceType, rep, err)
	}
	err = rootWebRPC(t, router, "GetBucketToggles", &GetBucketTogglesArgs{BucketName: "ab"}, &rep)
	if err == nil || !strings.Contains(err.Error(), "is invalid") {
		t.Fatalf("%s: bucket ab = %v, want an invalid name error", instanceType, err)
	}
	resetGlobalObjectAPI()
	defer setObjectLayer(obj)
	err = rootWebRPC(t, router, "GetBucketToggles", &GetBucketTogglesArgs{BucketName: bucket}, &rep)
	if err == nil || !strings.Contains(err.Error(), errServerNotInitialized.Error()) {
		t.Fatalf("%s: no object layer = %v, want %v", instanceType, err, errServerNotInitialized)
	}
}

func TestWebHandlerBucketTogglesMissingBucket(t *testing.T) {
	ExecObjectLayerTest(t, testBucketTogglesMissingBucketWebHandler)
}

func testBucketTogglesMissingBucketWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	var rep GetBucketTogglesRep
	err := rootWebRPC(t, router, "GetBucketToggles", &GetBucketTogglesArgs{BucketName: "missing-bucket"}, &rep)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("%s: Get missing = %v, want bucket not found", instanceType, err)
	}
	var gen WebGenericRep
	err = rootWebRPC(t, router, "SetBucketToggles", &SetBucketTogglesArgs{BucketName: "missing-bucket", S3Enabled: true}, &gen)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("%s: Set missing = %v, want bucket not found", instanceType, err)
	}
}

// Install test KMS, cleanup restores previous one
func useTestKMS(t TestErrHandler) func() {
	t.Helper()
	key, err := kms.Parse("my-obstor-key:5lF+0pJM0OWwlQrvK2S/I7W9mO4a6rJJI7wzj7v09cw=")
	if err != nil {
		t.Fatalf("test KMS: %v", err)
	}
	prev := GlobalKMS
	GlobalKMS = key
	return func() { GlobalKMS = prev }
}

func TestWebHandlerBucketFeatures(t *testing.T) {
	ExecObjectLayerTest(t, testBucketFeaturesWebHandler)
}

func testBucketFeaturesWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	erasure := instanceType == ErasureTestStr

	var rep BucketFeaturesRep
	if err := rootWebRPC(t, router, "BucketFeatures", &WebGenericArgs{}, &rep); err != nil {
		t.Fatalf("%s: %v", instanceType, err)
	}
	want := BucketFeaturesRep{UIVersion: Version, Versioning: erasure, ObjectLock: erasure, Quota: true, Tags: true}
	if rep != want {
		t.Fatalf("%s: features = %+v, want %+v", instanceType, rep, want)
	}

	defer useTestKMS(t)()
	if err := rootWebRPC(t, router, "BucketFeatures", &WebGenericArgs{}, &rep); err != nil || !rep.Encryption {
		t.Fatalf("%s: with a KMS, encryption = %v (%v), want true", instanceType, rep.Encryption, err)
	}
}

// Backend stores no settings, even with KMS
func TestBucketFeaturesBackend(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	defer useTestKMS(t)()
	globalIsBackend, globalAutoEncryption = true, true

	want := BucketFeaturesRep{AutoEncryption: true, Backend: true}
	if got := bucketFeatures(); got != want {
		t.Fatalf("backend features = %+v, want %+v", got, want)
	}
}

func TestWebHandlerGetBucketConfig(t *testing.T) {
	ExecObjectLayerTest(t, testGetBucketConfigWebHandler)
}

func testGetBucketConfigWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	read := func(name string) (GetBucketConfigRep, error) {
		var rep GetBucketConfigRep
		err := rootWebRPC(t, router, "GetBucketConfig", &GetBucketConfigArgs{BucketName: name}, &rep)
		return rep, err
	}

	rep, err := read(bucket)
	if err != nil {
		t.Fatalf("%s: fresh bucket: %v", instanceType, err)
	}
	if rep.Versioning == nil || *rep.Versioning != "" || rep.ObjectLock == nil || *rep.ObjectLock ||
		rep.Quota == nil || rep.Quota.Bytes != 0 || rep.Encryption == nil || *rep.Encryption ||
		rep.Tags == nil || len(rep.Tags) != 0 {
		t.Fatalf("%s: fresh bucket config = %+v", instanceType, rep)
	}

	sse := []byte(`<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule></ServerSideEncryptionConfiguration>`)
	for name, data := range map[string][]byte{
		bucketQuotaConfigFile: []byte(`{"quota":1073741824,"quotatype":"hard"}`),
		bucketSSEConfig:       sse,
	} {
		if err = globalBucketMetadataSys.Update(bucket, name, data); err != nil {
			t.Fatalf("%s: store %s: %v", instanceType, name, err)
		}
	}
	if err = replaceUserBucketTags(bucket, map[string]string{"team": "a"}); err != nil {
		t.Fatalf("%s: tags: %v", instanceType, err)
	}
	if err = writeBucketToggles(bucket, true, true); err != nil {
		t.Fatalf("%s: toggles: %v", instanceType, err)
	}
	rep, err = read(bucket)
	if err != nil || *rep.Quota != (WebBucketQuota{Type: madmin.HardQuota, Bytes: 1 << 30}) || !*rep.Encryption ||
		len(rep.Tags) != 1 || rep.Tags[0] != (WebTag{Key: "team", Value: "a"}) {
		t.Fatalf("%s: stored config = %+v, %v; want a 1 GiB hard quota, SSE on, only the team tag", instanceType, rep, err)
	}

	if instanceType == ErasureTestStr {
		locked := makeTestBucket(t, obj, BucketOptions{LockEnabled: true})
		rep, err = read(locked)
		if err != nil || !*rep.ObjectLock || *rep.Versioning != "Enabled" {
			t.Fatalf("locked bucket config = %+v, %v; want lock on and versioning Enabled", rep, err)
		}
	}

	if _, err = read("ab"); err == nil || !strings.Contains(err.Error(), "is invalid") {
		t.Fatalf("%s: bucket ab = %v, want an invalid name error", instanceType, err)
	}
	if _, err = read(getRandomBucketName()); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("%s: missing bucket = %v, want does not exist", instanceType, err)
	}

	prevBackend := globalIsBackend
	globalIsBackend = true
	rep, err = read(bucket)
	globalIsBackend = prevBackend
	if err != nil || rep.Versioning != nil || rep.ObjectLock != nil || rep.Quota != nil || rep.Encryption != nil || rep.Tags != nil {
		t.Fatalf("%s: backend config = %+v, %v; want every field null", instanceType, rep, err)
	}
}

func TestWebHandlerGetBucketConfigPermissions(t *testing.T) {
	ExecObjectLayerTest(t, testGetBucketConfigPermissionsWebHandler)
}

// Create user allowing given actions on all resources
func makeTestPolicyUser(t TestErrHandler, name string, actions ...string) {
	t.Helper()
	doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["` + strings.Join(actions, `","`) + `"],"Resource":["arn:aws:s3:::*"]}]}`
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy(name, *p); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.CreateUser(name, madmin.UserInfo{SecretKey: name + "-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(name, name, false); err != nil {
		t.Fatal(err)
	}
}

func testGetBucketConfigPermissionsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func(s *IAMSys) { globalIAMSys = s }(globalIAMSys)
	globalIAMSys = NewIAMSys()
	globalIAMSys.Init(ctx, obj)

	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	makeTestPolicyUser(t, "tagsonly", string(iampolicy.GetBucketTaggingAction))
	makeTestPolicyUser(t, "quotaonly", string(iampolicy.GetBucketQuotaAdminAction))
	makeTestPolicyUser(t, "nothing", string(iampolicy.ListBucketAction))
	read := func(user, name string) (GetBucketConfigRep, error) {
		var rep GetBucketConfigRep
		err := userWebRPC(t, router, user, user+"-secret", "GetBucketConfig", &GetBucketConfigArgs{BucketName: name}, &rep)
		return rep, err
	}

	rep, err := read("tagsonly", bucket)
	if err != nil || rep.Tags == nil || rep.Versioning != nil || rep.ObjectLock != nil || rep.Quota != nil || rep.Encryption != nil {
		t.Fatalf("%s: tags-only user = %+v, %v; want only Tags set", instanceType, rep, err)
	}
	rep, err = read("quotaonly", bucket)
	if err != nil || rep.Quota == nil || rep.Versioning != nil || rep.ObjectLock != nil || rep.Encryption != nil || rep.Tags != nil {
		t.Fatalf("%s: quota-only user = %+v, %v; want only Quota set", instanceType, rep, err)
	}
	// No readable settings, missing and invalid buckets match real ones
	for _, name := range []string{bucket, getRandomBucketName(), "ab"} {
		rep, err = read("nothing", name)
		if err != nil || rep.Versioning != nil || rep.ObjectLock != nil || rep.Quota != nil || rep.Encryption != nil || rep.Tags != nil {
			t.Fatalf("%s: no-read user on %q = %+v, %v; want no error and every field null", instanceType, name, rep, err)
		}
	}
}

func TestWebHandlerSetBucketVersioning(t *testing.T) {
	ExecObjectLayerTest(t, testSetBucketVersioningWebHandler)
}

func testSetBucketVersioningWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	set := func(name string, enabled bool) error {
		return rootWebRPC(t, router, "SetBucketVersioning", &SetBucketVersioningArgs{BucketName: name, Enabled: enabled}, &WebGenericRep{})
	}

	if instanceType == FSTestStr {
		if err := set(bucket, true); err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("FS: versioning = %v, want not implemented", err)
		}
		return
	}
	if err := set(bucket, true); err != nil || !globalBucketVersioningSys.Enabled(bucket) {
		t.Fatalf("enable = %v, enabled %v", err, globalBucketVersioningSys.Enabled(bucket))
	}
	// Suspend versioning, keep stored versions reachable
	if err := set(bucket, false); err != nil || !globalBucketVersioningSys.Suspended(bucket) {
		t.Fatalf("disable = %v, suspended %v", err, globalBucketVersioningSys.Suspended(bucket))
	}
	locked := makeTestBucket(t, obj, BucketOptions{LockEnabled: true})
	if err := set(locked, false); err == nil || !strings.Contains(err.Error(), "Object Lock") {
		t.Fatalf("suspend on a locked bucket = %v, want the object lock refusal", err)
	}
	if !globalBucketVersioningSys.Enabled(locked) {
		t.Fatal("locked bucket lost versioning")
	}
	if err := set(getRandomBucketName(), true); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing bucket = %v, want does not exist", err)
	}
}

func TestWebHandlerSetBucketQuota(t *testing.T) {
	ExecObjectLayerTest(t, testSetBucketQuotaWebHandler)
}

func testSetBucketQuotaWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	set := func(name string, typ madmin.QuotaType, bytes uint64) error {
		return rootWebRPC(t, router, "SetBucketQuota", &SetBucketQuotaArgs{BucketName: name, Type: typ, Bytes: bytes}, &WebGenericRep{})
	}
	quota := func() madmin.BucketQuota {
		q, err := globalBucketQuotaSys.Get(bucket)
		if err != nil {
			t.Fatalf("%s: read quota: %v", instanceType, err)
		}
		return *q
	}

	if err := set(bucket, madmin.HardQuota, 1<<30); err != nil || quota() != (madmin.BucketQuota{Quota: 1 << 30, Type: madmin.HardQuota}) {
		t.Fatalf("%s: hard 1 GiB = %v, stored %+v", instanceType, err, quota())
	}
	if err := set(bucket, madmin.FIFOQuota, 5<<40); err != nil || quota() != (madmin.BucketQuota{Quota: 5 << 40, Type: madmin.FIFOQuota}) {
		t.Fatalf("%s: fifo 5 TiB = %v, stored %+v", instanceType, err, quota())
	}
	// Zero clears quota and cached limit
	if err := set(bucket, "", 0); err != nil || quota().Quota != 0 {
		t.Fatalf("%s: clear = %v, stored %+v", instanceType, err, quota())
	}
	if err := set(bucket, "soft", 1); err == nil || !strings.Contains(err.Error(), "hard or fifo") {
		t.Fatalf("%s: quota type soft = %v, want rejected", instanceType, err)
	}
	if err := set(getRandomBucketName(), madmin.HardQuota, 1); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("%s: missing bucket = %v, want does not exist", instanceType, err)
	}
}

func TestWebHandlerSetBucketEncryption(t *testing.T) {
	ExecObjectLayerTest(t, testSetBucketEncryptionWebHandler)
}

func testSetBucketEncryptionWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	set := func(enabled bool) error {
		return rootWebRPC(t, router, "SetBucketEncryption", &SetBucketEncryptionArgs{BucketName: bucket, Enabled: enabled}, &WebGenericRep{})
	}
	encrypted := func() bool {
		_, err := globalBucketSSEConfigSys.Get(bucket)
		return err == nil
	}

	if _, err := validateBucketSSEConfig(bytes.NewReader(bucketSSES3Config)); err != nil {
		t.Fatalf("bucketSSES3Config is not a valid SSE-S3 rule: %v", err)
	}
	if err := set(true); err == nil || !strings.Contains(err.Error(), "KMS is not configured") || encrypted() {
		t.Fatalf("%s: enable without a KMS = %v, want refused", instanceType, err)
	}
	removeKMS := useTestKMS(t)
	if err := set(true); err != nil || !encrypted() {
		t.Fatalf("%s: enable = %v, encrypted %v", instanceType, err, encrypted())
	}
	removeKMS()
	// Disabling encryption needs no KMS
	if err := set(false); err != nil || encrypted() {
		t.Fatalf("%s: disable without a KMS = %v, encrypted %v", instanceType, err, encrypted())
	}
}

func TestWebHandlerSetBucketTags(t *testing.T) {
	ExecObjectLayerTest(t, testSetBucketTagsWebHandler)
}

func testSetBucketTagsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	set := func(list []WebTag) error {
		return rootWebRPC(t, router, "SetBucketTags", &SetBucketTagsArgs{BucketName: bucket, Tags: list}, &WebGenericRep{})
	}
	if err := writeBucketToggles(bucket, true, true); err != nil {
		t.Fatalf("%s: toggles: %v", instanceType, err)
	}

	if err := set([]WebTag{{Key: "team", Value: "a"}, {Key: "env", Value: "prod"}}); err != nil {
		t.Fatalf("%s: set tags: %v", instanceType, err)
	}
	user, err := userBucketTags(bucket)
	if err != nil || len(user) != 2 || user["env"] != "prod" || !IsBucketSFTPEnabled(bucket) {
		t.Fatalf("%s: after set: user %v (%v), SFTP %v; want both tags and SFTP still on", instanceType, user, err, IsBucketSFTPEnabled(bucket))
	}
	if err = writeBucketToggles(bucket, true, true); err != nil {
		t.Fatalf("%s: toggles again: %v", instanceType, err)
	}
	if user, _ = userBucketTags(bucket); len(user) != 2 {
		t.Fatalf("%s: SetBucketToggles dropped user tags: %v", instanceType, user)
	}

	full := make([]WebTag, 0, 50)
	for k := range bucketTagMap(50) {
		full = append(full, WebTag{Key: k, Value: "v"})
	}
	if err := set(full); err != nil {
		t.Fatalf("%s: setting 50 tags: %v", instanceType, err)
	}
	if err := writeBucketToggles(bucket, true, true); err != nil {
		t.Fatalf("%s: setting protocols alongside 50 tags: %v", instanceType, err)
	}
	if user, _ = userBucketTags(bucket); len(user) != 50 || !IsBucketSFTPEnabled(bucket) {
		t.Fatalf("%s: full tag set or protocol settings changed", instanceType)
	}

	many := make([]WebTag, 0, maxBucketUserTags+1)
	for k := range bucketTagMap(maxBucketUserTags + 1) {
		many = append(many, WebTag{Key: k, Value: "v"})
	}
	for _, bad := range []struct {
		tags []WebTag
		want string
	}{
		{[]WebTag{{Key: obstorTagS3Enabled, Value: "false"}}, "reserved"},
		{[]WebTag{{Key: "a", Value: "1"}, {Key: "a", Value: "2"}}, "twice"},
		{[]WebTag{{Key: "bad*key", Value: "v"}}, "TagKey"},
		{many, "at most 50"},
	} {
		if err = set(bad.tags); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("%s: tags %v = %v, want an error containing %q", instanceType, bad.tags, err, bad.want)
		}
	}

	if err = set(nil); err != nil {
		t.Fatalf("%s: clear tags: %v", instanceType, err)
	}
	if user, _ = userBucketTags(bucket); len(user) != 0 || !IsBucketSFTPEnabled(bucket) {
		t.Fatalf("%s: after clear: user %v, SFTP %v; want no user tags and SFTP still on", instanceType, user, IsBucketSFTPEnabled(bucket))
	}
}

func TestWebHandlerMakeBucketLock(t *testing.T) {
	ExecObjectLayerTest(t, testMakeBucketLockWebHandler)
}

// Erasure locks bucket and enables versioning, FS refuses
func testMakeBucketLockWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	create := func(name string, lock bool) error {
		return rootWebRPC(t, router, "MakeBucket", &MakeBucketArgs{BucketName: name, LockEnabled: lock}, &WebGenericRep{})
	}
	locked := getRandomBucketName()
	err := create(locked, true)
	if instanceType == FSTestStr {
		if err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("FS: locked MakeBucket = %v, want not implemented", err)
		}
		return
	}
	rcfg, lerr := globalBucketObjectLockSys.Get(locked)
	if err != nil || lerr != nil || !rcfg.LockEnabled || !globalBucketVersioningSys.Enabled(locked) {
		t.Fatalf("locked MakeBucket = %v; lock %v (%v), versioning %v", err, rcfg.LockEnabled, lerr, globalBucketVersioningSys.Enabled(locked))
	}
	plain := getRandomBucketName()
	if err = create(plain, false); err != nil {
		t.Fatalf("plain MakeBucket: %v", err)
	}
	if rcfg, _ = globalBucketObjectLockSys.Get(plain); rcfg.LockEnabled {
		t.Fatal("plain bucket came back locked")
	}
}

func TestWebHandlerMakeBucketBroadcast(t *testing.T) {
	ExecObjectLayerTest(t, testMakeBucketBroadcastWebHandler)
}

// MakeBucket reloads peers that cached metadata before creation
func testMakeBucketBroadcastWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	var mu sync.Mutex
	var reloaded []string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, peerRESTMethodLoadBucketMetadata) {
			mu.Lock()
			reloaded = append(reloaded, r.URL.Query().Get(peerRESTBucket))
			mu.Unlock()
		}
	}))
	defer peer.Close()
	host, err := xnet.ParseHost(peer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	router := initTestWebRPCEndPoint(obj)
	defer func(p []*peerRESTClient) { globalNotificationSys.peerClients = p }(globalNotificationSys.peerClients)
	globalNotificationSys.peerClients = []*peerRESTClient{newPeerRESTClient(host)}

	bucket := getRandomBucketName()
	if err = rootWebRPC(t, router, "MakeBucket", &MakeBucketArgs{BucketName: bucket, LockEnabled: instanceType == ErasureTestStr}, &WebGenericRep{}); err != nil {
		t.Fatalf("%s: MakeBucket: %v", instanceType, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reloaded) != 1 || reloaded[0] != bucket {
		t.Fatalf("%s: peer reloads = %v, want [%s]", instanceType, reloaded, bucket)
	}
}
