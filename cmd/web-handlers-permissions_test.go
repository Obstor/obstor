package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/obstor/obstor/pkg/auth"
	"github.com/obstor/obstor/pkg/madmin"
)

// S3 access grants no instance access as bucket inventory changes
func TestWebHandlerInstancePermissionIndependentOfBuckets(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		t.Run(instanceType, func(t *testing.T) {
			defer pinBucketGlobals(instanceType)()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer pinAttentionGlobals(ctx, obj)()
			router := initTestWebRPCEndPoint(obj)
			a, b := makeTestBucket(t, obj, BucketOptions{}), makeTestBucket(t, obj, BucketOptions{})
			now := UTCNow()
			for _, bucket := range []string{a, b} {
				meta, err := globalBucketMetadataSys.Get(bucket)
				if err != nil {
					t.Fatal(err)
				}
				meta.Created = now.Add(-3 * time.Hour)
				globalBucketMetadataSys.Set(bucket, meta)
			}
			globalCapacityHistory.BecomeLeader(ctx, obj)
			globalCapacityHistory.Sample(ctx, obj, madmin.DataUsageInfo{
				ObjectsTotalSize: 1000, ObjectsTotalCount: 10, BucketsCount: 2,
				BucketsUsage: map[string]madmin.BucketUsageInfo{a: {Size: 100, ObjectsCount: 1}, b: {Size: 900, ObjectsCount: 9}},
			}, now.Add(-time.Hour))
			globalFleetStore = newInMemFleetStore(45*time.Second, "")
			globalAttentionDisks = &attentionDiskCache{at: now.Add(time.Hour), ok: true, disks: []madmin.Disk{
				{Endpoint: "http://private-node:9000/drive", State: madmin.DriveStateOffline},
			}}
			admin := makeDocIAMUser(t, "instanceadmin", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}]}`)
			alpha := makeScopedIAMUser(t, "instancealpha", a)
			beta := makeScopedIAMUser(t, "instancebeta", b)
			all := makeTestIAMUser(t, "instanceallbuckets", "s3:ListBucket")
			// Explicit admin Deny overrides Allow
			denied := makeDocIAMUser(t, "instancedenied", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"]},{"Effect":"Deny","Action":["admin:ServerInfo"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}]}`)
			bucketAdmin := makeDocIAMUser(t, "instancebucketadmin", fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"],"Resource":["arn:aws:s3:::%s"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}]}`, a))
			if err := obj.DeleteBucket(ctx, b, false); err != nil {
				t.Fatal(err)
			}
			for phase := 0; phase < 4; phase++ {
				switch phase {
				case 1, 3:
					if err := obj.MakeBucketWithLocation(ctx, b, BucketOptions{}); err != nil {
						t.Fatal(err)
					}
					meta, err := globalBucketMetadataSys.Get(b)
					if err != nil {
						t.Fatal(err)
					}
					meta.Created = now
					globalBucketMetadataSys.Set(b, meta)
				case 2:
					if err := obj.DeleteBucket(ctx, b, false); err != nil {
						t.Fatal(err)
					}
				}
				other := phase == 1 || phase == 3
				usage := map[string]madmin.BucketUsageInfo{a: {Size: 100, ObjectsCount: 1}}
				if other {
					usage[b] = madmin.BucketUsageInfo{Size: 900, ObjectsCount: 9}
				}
				// Restore usage volume pruned by empty erasure bucket deletion so every set has fixture
				if pools, ok := obj.(*erasureServerPools); ok {
					for _, pool := range pools.serverPools {
						for _, set := range pool.sets {
							for _, disk := range set.getDisks() {
								if err := disk.MakeVol(ctx, dataUsageBucket); err != nil && err != errVolumeExists {
									t.Fatal(err)
								}
							}
						}
					}
				}
				dui := madmin.DataUsageInfo{LastUpdate: now, BucketsUsage: usage}
				data, err := json.Marshal(dui)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := obj.PutObject(ctx, dataUsageBucket, dataUsageObjName, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{}); err != nil {
					t.Fatalf("phase %d usage fixture write: %v", phase, err)
				}
				globalCapacityHistory.Sample(ctx, obj, dui, now)
				stored, err := loadDataUsageFromBackend(ctx, obj)
				if err != nil || len(stored.BucketsUsage) != len(usage) {
					t.Fatalf("phase %d usage fixture read = %+v, %v; want %d buckets", phase, stored, err, len(usage))
				}
				globalBucketQuotaSys = NewBucketQuotaSys()
				for _, caller := range []struct {
					cred     auth.Credentials
					instance bool
					used     uint64
				}{
					{globalActiveCred, true, 100}, {admin, true, 100},
					{alpha, false, 100}, {beta, false, 0}, {all, false, 100}, {denied, false, 100},
					{bucketAdmin, false, 100},
				} {
					t.Run(fmt.Sprintf("phase-%d/%s", phase, caller.cred.AccessKey), func(t *testing.T) {
						wantUsed := caller.used
						if other && caller.cred.AccessKey != alpha.AccessKey {
							wantUsed += 900
						}
						call := func(method string, args, rep interface{}) error {
							t.Helper()
							return userWebRPC(t, router, caller.cred.AccessKey, caller.cred.SecretKey, method, args, rep)
						}
						var info ServerInfoRep
						if err := call("ServerInfo", &WebGenericArgs{}, &info); err != nil || info.CanSeeFleet != caller.instance {
							t.Fatalf("ServerInfo = %+v, %v; instance access should be %v", info, err, caller.instance)
						}
						var storage StorageInfoRep
						if err := call("StorageInfo", &WebGenericArgs{}, &storage); err != nil || storage.Used != wantUsed {
							t.Fatalf("StorageInfo = %+v, %v; want scoped used %d", storage, err, wantUsed)
						}
						if caller.instance != (storage.Total > 0) || (!caller.instance && (storage.Free != 0 || storage.DisksOnline != 0 || storage.DisksOffline != 0 || storage.ParityRatio != 0)) {
							t.Fatalf("hardware leaked or missing: %+v", storage)
						}
						var history CapacityHistoryRep
						if err := call("CapacityHistory", &CapacityHistoryArgs{Hours: 24}, &history); err != nil || len(history.Used) == 0 {
							t.Fatalf("CapacityHistory = %+v, %v", history, err)
						}
						if caller.instance != (history.Total != nil && history.Free != nil) {
							t.Fatalf("capacity hardware series leaked or missing: %+v", history)
						}
						if !caller.instance {
							for name, values := range driveSeries(history) {
								if values != nil {
									t.Errorf("%s exposed %v", name, values)
								}
							}
							since, err := time.Parse(time.RFC3339, history.Since)
							if err != nil {
								t.Fatal(err)
							}
							for i, used := range history.Used {
								// Recreated buckets hide old history even from wildcard S3 users
								allowed := wantUsed
								if absHour(since.Add(time.Duration(i)*time.Hour)) < absHour(now) {
									allowed = caller.used
								}
								if used > allowed {
									t.Errorf("historical usage[%d] %d exceeds authorized buckets' %d", i, used, allowed)
								}
							}
						}
						var attention AttentionRep
						if err := call("Attention", &WebGenericArgs{}, &attention); err != nil {
							t.Fatal(err)
						}
						hasHardware := false
						for _, item := range attention.Items {
							hasHardware = hasHardware || strings.HasPrefix(item.ID, "drive-offline:") || strings.HasPrefix(item.ID, "node-offline:")
						}
						if hasHardware != caller.instance {
							t.Fatalf("attention hardware visibility = %v; want %v", hasHardware, caller.instance)
						}
						for _, rpc := range []struct {
							method string
							args   interface{}
						}{
							{"FleetStorageInfo", &WebGenericArgs{}}, {"FleetServers", &ListServersArgs{}},
							{"FleetRegions", &WebGenericArgs{}}, {"FleetMetrics", &FleetMetricsArgs{WindowHours: 24}},
							{"ListServers", &ListServersArgs{}}, {"GetObjectLocations", &GetObjectLocationsArgs{BucketName: a, Objects: []string{"example.txt"}}},
						} {
							token, err := getWebRPCToken(router, caller.cred.AccessKey, caller.cred.SecretKey)
							if err != nil {
								t.Fatal(err)
							}
							req, err := newTestWebRPCRequest("web."+rpc.method, token, rpc.args)
							if err != nil {
								t.Fatal(err)
							}
							rec := httptest.NewRecorder()
							router.ServeHTTP(rec, req)
							code, message := webRPCError(t, rec)
							if caller.instance && code != "" {
								t.Errorf("%s authorized administrator: %q %q", rpc.method, code, message)
							}
							if !caller.instance && code != "AccessDenied" {
								t.Errorf("%s as bucket user = %q %q; want AccessDenied", rpc.method, code, message)
							}
						}
					})
				}
			}
		})
	})
}

func TestWebHandlerScopedUserCannotMutateOtherBucket(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		t.Run(instanceType, func(t *testing.T) {
			defer pinBucketGlobals(instanceType)()
			globalIAMSys.InitStore(obj)
			router := initTestWebRPCEndPoint(obj)
			mine, other := makeTestBucket(t, obj, BucketOptions{}), makeTestBucket(t, obj, BucketOptions{})
			user := makeDocIAMUser(t, "scoped-editor", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::`+mine+`","arn:aws:s3:::`+mine+`/*"]}]}`)
			var reply WebGenericRep
			if err := userWebRPC(t, router, user.AccessKey, user.SecretKey, "SetBucketTags", &SetBucketTagsArgs{BucketName: mine, Tags: []WebTag{{Key: "tenant", Value: "mine"}}}, &reply); err != nil {
				t.Fatalf("allowed bucket tags: %v", err)
			}
			var config GetBucketConfigRep
			if err := userWebRPC(t, router, user.AccessKey, user.SecretKey, "GetBucketConfig", &GetBucketConfigArgs{BucketName: mine}, &config); err != nil || len(config.Tags) != 1 || config.Tags[0].Value != "mine" || config.Quota != nil {
				t.Fatalf("allowed bucket settings = %+v, %v; want own tags and no globally protected quota", config, err)
			}
			token, err := getWebRPCToken(router, user.AccessKey, user.SecretKey)
			if err != nil {
				t.Fatal(err)
			}
			for _, rpc := range []struct {
				method string
				args   interface{}
			}{
				{"SetBucketTags", &SetBucketTagsArgs{BucketName: other}},
				{"SetBucketVersioning", &SetBucketVersioningArgs{BucketName: other, Enabled: true}},
				{"SetBucketEncryption", &SetBucketEncryptionArgs{BucketName: other}},
				{"SetBucketToggles", &SetBucketTogglesArgs{BucketName: other, S3Enabled: true}},
				{"SetBucketQuota", &SetBucketQuotaArgs{BucketName: other}},
				{"SetBucketPolicy", &SetBucketPolicyWebArgs{BucketName: other, Policy: "none"}},
				{"SetBucketPolicyDoc", &SetBucketPolicyDocArgs{BucketName: other, Policy: `{}`}},
				{"DeleteBucket", &RemoveBucketArgs{BucketName: other}},
				{"RemoveObject", &RemoveObjectArgs{BucketName: other, Objects: []string{"x.txt"}}},
				{"SetCannedPolicy", &SetCannedPolicyArgs{Name: "other-policy", Policy: `{}`}},
				{"ListIAMUsers", &WebGenericArgs{}}, {"ListCannedPolicies", &WebGenericArgs{}},
			} {
				req, err := newTestWebRPCRequest("web."+rpc.method, token, rpc.args)
				if err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if code, message := webRPCError(t, rec); code != "AccessDenied" {
					t.Errorf("%s returned %q (%s); want AccessDenied", rpc.method, code, message)
				}
			}
		})
	})
}
