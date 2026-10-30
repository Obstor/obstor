package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/obstor/obstor/pkg/auth"
	"github.com/tinylib/msgp/msgp"
)

// Distinct user tags
func bucketTagMap(n int) map[string]string {
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("k%d", i)] = "v"
	}
	return m
}

func TestPartitionBucketTags(t *testing.T) {
	user, reserved := partitionBucketTags(map[string]string{"team": "a", obstorTagSFTPEnabled: "true"})
	if len(user) != 1 || user["team"] != "a" {
		t.Fatalf("user = %v, want only team", user)
	}
	if len(reserved) != 1 || reserved[obstorTagSFTPEnabled] != "true" {
		t.Fatalf("reserved = %v, want only the SFTP toggle", reserved)
	}
}

func TestCheckUserBucketTags(t *testing.T) {
	if err := checkUserBucketTags(map[string]string{"team": "a", "env": ""}); err != nil {
		t.Fatalf("plain tags rejected: %v", err)
	}
	if err := checkUserBucketTags(map[string]string{obstorTagS3Enabled: "false"}); err != errReservedBucketTag {
		t.Fatalf("reserved key: err = %v, want errReservedBucketTag", err)
	}
	if err := checkUserBucketTags(bucketTagMap(maxBucketUserTags + 1)); err != errTooManyBucketUserTags {
		t.Fatalf("%d tags: err = %v, want errTooManyBucketUserTags", maxBucketUserTags+1, err)
	}
	withReserved := bucketTagMap(maxBucketUserTags + 1)
	withReserved[obstorTagS3Enabled] = "false"
	if err := checkUserBucketTags(withReserved); err != errReservedBucketTag {
		t.Fatalf("reserved key among %d tags: err = %v, want errReservedBucketTag", len(withReserved), err)
	}
	if err := checkUserBucketTags(bucketTagMap(maxBucketUserTags)); err != nil {
		t.Fatalf("%d tags rejected: %v", maxBucketUserTags, err)
	}
	if err := checkUserBucketTags(map[string]string{"bad*key": "v"}); err == nil {
		t.Fatal("a key with * was accepted")
	}
}

// Public tag writes preserve separate protocol metadata
func TestBucketTaggingKeepsReservedTags(t *testing.T) {
	ExecObjectLayerAPITest(t, testBucketTaggingKeepsReservedTags, []string{"PutBucketTagging", "GetBucketTagging", "DeleteBucketTagging"})
}

func testBucketTaggingKeepsReservedTags(obj ObjectLayer, instanceType, bucketName string, apiRouter http.Handler,
	credentials auth.Credentials, t *testing.T) {
	defer pinBucketGlobals(instanceType)()
	if err := writeBucketToggles(bucketName, true, true); err != nil {
		t.Fatalf("%s: writeBucketToggles: %v", instanceType, err)
	}
	taggingURL := makeTestTargetURL("", bucketName, "", url.Values{"tagging": {""}})
	send := func(method, body string) *httptest.ResponseRecorder {
		var rs io.ReadSeeker
		if body != "" {
			rs = bytes.NewReader([]byte(body))
		}
		req, err := newTestSignedRequestV4(method, taggingURL, int64(len(body)), rs, credentials.AccessKey, credentials.SecretKey, nil)
		if err != nil {
			t.Fatalf("%s: %s request: %v", instanceType, method, err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}
	tagXML := func(m map[string]string) string {
		var b strings.Builder
		b.WriteString("<Tagging><TagSet>")
		for k, v := range m {
			fmt.Fprintf(&b, "<Tag><Key>%s</Key><Value>%s</Value></Tag>", k, v)
		}
		b.WriteString("</TagSet></Tagging>")
		return b.String()
	}

	if rec := send(http.MethodPut, tagXML(map[string]string{"team": "a"})); rec.Code != http.StatusOK {
		t.Fatalf("%s: PUT tagging = %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if !IsBucketSFTPEnabled(bucketName) {
		t.Fatalf("%s: PUT tagging wiped the SFTP toggle", instanceType)
	}
	rec := send(http.MethodGet, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<Key>team</Key>") {
		t.Fatalf("%s: GET tagging = %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), obstorReservedTagPrefix) {
		t.Fatalf("%s: GET tagging showed reserved keys: %s", instanceType, rec.Body.String())
	}
	// Forbidden reserved key, too many tags returns bad request
	if rec := send(http.MethodPut, tagXML(map[string]string{obstorTagS3Enabled: "false"})); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "<Code>AccessDenied</Code>") {
		t.Fatalf("%s: PUT with a reserved key = %d %s, want 403 AccessDenied", instanceType, rec.Code, rec.Body.String())
	}
	for _, n := range []int{49, 50} {
		if rec := send(http.MethodPut, tagXML(bucketTagMap(n))); rec.Code != http.StatusOK {
			t.Fatalf("%s: PUT with %d tags = %d %s", instanceType, n, rec.Code, rec.Body.String())
		}
		if rec := send(http.MethodGet, ""); rec.Code != http.StatusOK || strings.Count(rec.Body.String(), "<Tag>") != n || strings.Contains(rec.Body.String(), obstorReservedTagPrefix) {
			t.Fatalf("%s: GET with %d tags = %d %s", instanceType, n, rec.Code, rec.Body.String())
		}
	}
	if rec := send(http.MethodPut, tagXML(bucketTagMap(maxBucketUserTags+1))); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "<Code>MalformedXML</Code>") {
		t.Fatalf("%s: PUT with %d tags = %d %s, want 400 MalformedXML", instanceType, maxBucketUserTags+1, rec.Code, rec.Body.String())
	}
	if rec := send(http.MethodDelete, ""); rec.Code != http.StatusOK {
		t.Fatalf("%s: DELETE tagging = %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if !IsBucketSFTPEnabled(bucketName) {
		t.Fatalf("%s: DELETE tagging wiped the SFTP toggle", instanceType)
	}
	if rec := send(http.MethodGet, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("%s: GET after DELETE = %d, want 404 NoSuchTagSet", instanceType, rec.Code)
	}
}

func TestBucketProtocolSettingsCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name   string
		legacy map[string]string
		stored string
		want   bucketProtocolConfig
	}{
		{"defaults", nil, "", bucketProtocolConfig{S3Enabled: true}},
		{"legacy", map[string]string{obstorTagS3Enabled: "FALSE", obstorTagSFTPEnabled: "true"}, "", bucketProtocolConfig{SFTPEnabled: true}},
		{"legacy truthy", map[string]string{obstorTagSFTPEnabled: "1"}, "", bucketProtocolConfig{S3Enabled: true, SFTPEnabled: true}},
		{"dedicated", nil, `{"s3Enabled":false,"sftpEnabled":true}`, bucketProtocolConfig{SFTPEnabled: true}},
		{"dedicated wins", map[string]string{obstorTagS3Enabled: "false", obstorTagSFTPEnabled: "true"}, `{"s3Enabled":true,"sftpEnabled":false}`, bucketProtocolConfig{S3Enabled: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := newBucketMetadata("bucket")
			var err error
			meta.TaggingConfigXML, err = marshalBucketTags(tt.legacy)
			if err != nil {
				t.Fatal(err)
			}
			meta.ProtocolConfigJSON = []byte(tt.stored)
			if err := meta.parseAllConfigs(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			got, err := meta.protocolSettings()
			if err != nil || got != tt.want {
				t.Fatalf("settings = %+v (%v), want %+v", got, err, tt.want)
			}
			data, err := meta.MarshalMsg(nil)
			if err != nil {
				t.Fatal(err)
			}
			reloaded := newBucketMetadata("bucket")
			if _, err := reloaded.UnmarshalMsg(data); err != nil {
				t.Fatal(err)
			}
			if err := reloaded.parseAllConfigs(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			got, err = reloaded.protocolSettings()
			if err != nil || got != tt.want {
				t.Fatalf("serialized settings = %+v (%v), want %+v", got, err, tt.want)
			}
		})
	}
}

// Older metadata omits protocol field
func TestBucketMetadataLegacyProtocolFieldOptional(t *testing.T) {
	legacyTags, err := marshalBucketTags(map[string]string{obstorTagS3Enabled: "false", obstorTagSFTPEnabled: "true"})
	if err != nil {
		t.Fatal(err)
	}
	data := msgp.AppendMapHeader(nil, 2)
	data = msgp.AppendString(data, "Name")
	data = msgp.AppendString(data, "bucket")
	data = msgp.AppendString(data, "TaggingConfigXML")
	data = msgp.AppendBytes(data, legacyTags)
	meta := newBucketMetadata("bucket")
	if _, err := meta.UnmarshalMsg(data); err != nil {
		t.Fatal(err)
	}
	if err := meta.parseAllConfigs(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := meta.protocolSettings()
	if err != nil || cfg.S3Enabled || !cfg.SFTPEnabled {
		t.Fatalf("legacy map settings = %+v (%v)", cfg, err)
	}
}

func TestBucketProtocolMetadataMigration(t *testing.T) {
	ExecObjectLayerTest(t, testBucketProtocolMetadataMigration)
}

func testBucketProtocolMetadataMigration(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	for _, migrateByRead := range []bool{true, false} {
		bucket := makeTestBucket(t, obj, BucketOptions{})
		legacy := bucketTagMap(48)
		legacy[obstorTagS3Enabled] = "false"
		legacy[obstorTagSFTPEnabled] = "true"
		data, err := marshalBucketTags(legacy)
		if err != nil {
			t.Fatal(err)
		}
		// Bypass public validation and write old metadata
		if err := globalBucketMetadataSys.Update(bucket, bucketTaggingConfig, data); err != nil {
			t.Fatal(err)
		}
		if IsBucketS3Enabled(bucket) || !IsBucketSFTPEnabled(bucket) {
			t.Fatalf("%s: legacy flags were not respected", instanceType)
		}
		if got, err := userBucketTags(bucket); err != nil || len(got) != 48 {
			t.Fatalf("%s: legacy user tags = %d (%v), want 48", instanceType, len(got), err)
		}
		if migrateByRead {
			globalBucketMetadataSys.Remove(bucket)
			if _, err := globalBucketMetadataSys.GetConfig(bucket); err != nil {
				t.Fatal(err)
			}
		}
		if err := replaceUserBucketTags(bucket, bucketTagMap(50)); err != nil {
			t.Fatalf("%s: replacing legacy tags with 50 user tags: %v", instanceType, err)
		}
		stored := newBucketMetadata(bucket)
		if err := stored.Load(context.Background(), obj, bucket); err != nil {
			t.Fatal(err)
		}
		if len(stored.ProtocolConfigJSON) == 0 || strings.Contains(string(stored.TaggingConfigXML), obstorReservedTagPrefix) {
			t.Fatalf("%s: legacy flags were not migrated away from tags", instanceType)
		}
		globalBucketMetadataSys.Remove(bucket)
		if IsBucketS3Enabled(bucket) || !IsBucketSFTPEnabled(bucket) {
			t.Fatalf("%s: migrated flags changed after a fresh metadata load", instanceType)
		}
		if got, err := userBucketTags(bucket); err != nil || len(got) != 50 {
			t.Fatalf("%s: user tags = %d (%v), want 50", instanceType, len(got), err)
		}
		if err := writeBucketToggles(bucket, true, true); err != nil {
			t.Fatalf("%s: enabling protocols with 50 tags: %v", instanceType, err)
		}
		if got, err := userBucketTags(bucket); err != nil || len(got) != 50 {
			t.Fatalf("%s: setting protocols replaced full tag set", instanceType)
		}
		if err := replaceUserBucketTags(bucket, nil); err != nil {
			t.Fatal(err)
		}
		globalBucketMetadataSys.Remove(bucket)
		if !IsBucketS3Enabled(bucket) || !IsBucketSFTPEnabled(bucket) {
			t.Fatalf("%s: deleting user tags reset migrated settings", instanceType)
		}
	}
}

// Independent node caches share namespace lock for full metadata transaction
func TestBucketMetadataConcurrentProtocolAndTagUpdates(t *testing.T) {
	ExecObjectLayerTest(t, testBucketMetadataConcurrentProtocolAndTagUpdates)
}

func testBucketMetadataConcurrentProtocolAndTagUpdates(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	bucket := makeTestBucket(t, obj, BucketOptions{})
	firstNode, secondNode := NewBucketMetadataSys(), NewBucketMetadataSys()
	firstNode.Set(bucket, newBucketMetadata(bucket))
	secondNode.Set(bucket, newBucketMetadata(bucket))
	blocking := &bucketMetadataBlockingLayer{
		ObjectLayer:       obj,
		metadataPath:      path.Join(bucketConfigPrefix, bucket, bucketMetadataFile),
		putStarted:        make(chan struct{}),
		releasePut:        make(chan struct{}),
		secondLockAttempt: make(chan struct{}),
	}
	setObjectLayer(blocking)
	defer setObjectLayer(obj)
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	data, err := marshalBucketTags(bucketTagMap(50))
	if err != nil {
		t.Fatal(err)
	}
	go func() { firstDone <- firstNode.Update(bucket, bucketTaggingConfig, data) }()
	select {
	case <-blocking.putStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first metadata write did not start")
	}
	protocolData, _ := json.Marshal(bucketProtocolConfig{SFTPEnabled: true})
	go func() { secondDone <- secondNode.Update(bucket, bucketProtocolConfigFile, protocolData) }()
	select {
	case <-blocking.secondLockAttempt:
	case <-time.After(5 * time.Second):
		close(blocking.releasePut)
		t.Fatal("second node did not use the shared metadata namespace lock")
	}
	if reads := blocking.metadataReads.Load(); reads != 1 {
		close(blocking.releasePut)
		t.Fatalf("%s: second node read metadata during the first node's write (%d reads)", instanceType, reads)
	}
	close(blocking.releasePut)
	for _, done := range []chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("metadata update did not finish")
		}
	}
	stored, err := loadBucketMetadata(context.Background(), obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := stored.protocolSettings()
	if err != nil || cfg.S3Enabled || !cfg.SFTPEnabled || len(stored.taggingConfig.ToMap()) != 50 {
		t.Fatalf("%s: concurrent updates lost data: protocols %+v (%v), tags %v", instanceType, cfg, err, stored.taggingConfig)
	}
}

func TestBucketMetadataReloadPublishesUnderLock(t *testing.T) {
	ExecObjectLayerTest(t, testBucketMetadataReloadPublishesUnderLock)
}

func testBucketMetadataReloadPublishesUnderLock(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	bucket := makeTestBucket(t, obj, BucketOptions{})
	node := NewBucketMetadataSys()
	observed := &bucketMetadataBlockingLayer{
		ObjectLayer:       obj,
		metadataPath:      path.Join(bucketConfigPrefix, bucket, bucketMetadataFile),
		firstRead:         make(chan struct{}),
		putStarted:        make(chan struct{}),
		releasePut:        make(chan struct{}),
		secondLockAttempt: make(chan struct{}),
	}
	close(observed.releasePut)
	setObjectLayer(observed)
	defer setObjectLayer(obj)
	// Delay cache update until concurrent writer attempts same lock
	node.Lock()
	cacheLocked := true
	defer func() {
		if cacheLocked {
			node.Unlock()
		}
	}()
	reloaded, updated := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := node.Reload(context.Background(), observed, bucket)
		reloaded <- err
	}()
	select {
	case <-observed.firstRead:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata reload did not read")
	}
	protocolData, _ := json.Marshal(bucketProtocolConfig{SFTPEnabled: true})
	go func() { updated <- node.Update(bucket, bucketProtocolConfigFile, protocolData) }()
	select {
	case <-observed.secondLockAttempt:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata update did not attempt the namespace lock")
	}
	if reads := observed.metadataReads.Load(); reads != 1 {
		t.Fatalf("%s: update read while reload cache publication was pending: %d reads", instanceType, reads)
	}
	node.Unlock()
	cacheLocked = false
	for _, done := range []chan error{reloaded, updated} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("metadata transaction did not finish")
		}
	}
	meta, err := node.GetConfig(bucket)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := meta.protocolSettings()
	if err != nil || cfg.S3Enabled || !cfg.SFTPEnabled {
		t.Fatalf("%s: delayed reload replaced new cached settings: %+v (%v)", instanceType, cfg, err)
	}
}

type bucketMetadataBlockingLayer struct {
	ObjectLayer
	metadataPath                              string
	putStarted, releasePut, secondLockAttempt chan struct{}
	firstRead                                 chan struct{}
	writes, locks, metadataReads              atomic.Int32
}

func (b *bucketMetadataBlockingLayer) NewNSLock(bucket string, objects ...string) RWLocker {
	lock := b.ObjectLayer.NewNSLock(bucket, objects...)
	if bucket == obstorMetaBucket && len(objects) == 1 && objects[0] == b.metadataPath+".lock" {
		return bucketMetadataObservedLock{RWLocker: lock, owner: b}
	}
	return lock
}

type bucketMetadataObservedLock struct {
	RWLocker
	owner *bucketMetadataBlockingLayer
}

func (l bucketMetadataObservedLock) GetLock(ctx context.Context, timeout *dynamicTimeout) (context.Context, error) {
	if l.owner.locks.Add(1) == 2 {
		close(l.owner.secondLockAttempt)
	}
	return l.RWLocker.GetLock(ctx, timeout)
}

func (b *bucketMetadataBlockingLayer) GetObjectNInfo(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, headers http.Header, lockType LockType, opts ObjectOptions) (*GetObjectReader, error) {
	if bucket == obstorMetaBucket && object == b.metadataPath {
		if b.metadataReads.Add(1) == 1 && b.firstRead != nil {
			close(b.firstRead)
		}
	}
	return b.ObjectLayer.GetObjectNInfo(ctx, bucket, object, rs, headers, lockType, opts)
}

func (b *bucketMetadataBlockingLayer) PutObject(ctx context.Context, bucket, object string, r *PutObjReader, opts ObjectOptions) (ObjectInfo, error) {
	if bucket == obstorMetaBucket && object == b.metadataPath && b.writes.Add(1) == 1 {
		close(b.putStarted)
		select {
		case <-b.releasePut:
		case <-ctx.Done():
			return ObjectInfo{}, ctx.Err()
		}
	}
	return b.ObjectLayer.PutObject(ctx, bucket, object, r, opts)
}
