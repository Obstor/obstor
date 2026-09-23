/*
 * MinIO Cloud Storage, (C) 2016-2019 MinIO, Inc.
 * PGG Obstor, (C) 2021-2026 PGG, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/klauspost/compress/zip"
	obstor "github.com/obstor/obstor-go/v7"
	obstorpolicy "github.com/obstor/obstor-go/v7/pkg/policy"
	"github.com/obstor/obstor-go/v7/pkg/s3utils"
	"github.com/obstor/obstor-go/v7/pkg/tags"

	"github.com/obstor/obstor/cmd/config/dns"
	"github.com/obstor/obstor/cmd/config/identity/openid"
	"github.com/obstor/obstor/cmd/crypto"
	xhttp "github.com/obstor/obstor/cmd/http"
	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/auth"
	"github.com/obstor/obstor/pkg/bucket/lifecycle"
	objectlock "github.com/obstor/obstor/pkg/bucket/object/lock"
	"github.com/obstor/obstor/pkg/bucket/policy"
	"github.com/obstor/obstor/pkg/bucket/replication"
	"github.com/obstor/obstor/pkg/etag"
	"github.com/obstor/obstor/pkg/event"
	"github.com/obstor/obstor/pkg/handlers"
	"github.com/obstor/obstor/pkg/hash"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/ioutil"
	"github.com/obstor/obstor/pkg/madmin"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

func extractBucketObject(args reflect.Value) (bucketName, objectName string) {
	switch args.Kind() {
	case reflect.Pointer:
		a := args.Elem()
		for i := 0; i < a.NumField(); i++ {
			switch a.Type().Field(i).Name {
			case "BucketName":
				bucketName = a.Field(i).String()
			case "Prefix":
				objectName = a.Field(i).String()
			case "ObjectName":
				objectName = a.Field(i).String()
			}
		}
	}
	return bucketName, objectName
}

// WebGenericArgs - empty struct for calls that don't accept arguments
// for ex. ServerInfo
type WebGenericArgs struct{}

// WebGenericRep - reply structure for calls for which reply is success/failure
// for ex. RemoveObject MakeBucket
type WebGenericRep struct {
	UIVersion string `json:"uiVersion"`
}

// ServerInfoRep - server info reply.
type ServerInfoRep struct {
	ObstorVersion  string
	ObstorPlatform string
	ObstorRuntime  string
	CanSeeFleet    bool   `json:"canSeeFleet"`
	UIVersion      string `json:"uiVersion"`
}

// ServerInfo - get server info.
func (web *webAPIHandlers) ServerInfo(r *http.Request, args *WebGenericArgs, reply *ServerInfoRep) error {
	ctx := newWebContext(r, args, "WebServerInfo")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	reply.CanSeeFleet = callerCanSeeInstanceTotals(ctx, r, claims.AccessKey, claims.Map(), owner)
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	platform := fmt.Sprintf("Host: %s | OS: %s | Arch: %s",
		host,
		runtime.GOOS,
		runtime.GOARCH)
	goruntime := fmt.Sprintf("Version: %s | CPUs: %d", runtime.Version(), runtime.NumCPU())

	reply.ObstorVersion = Version
	reply.ObstorPlatform = platform
	reply.ObstorRuntime = goruntime
	reply.UIVersion = Version
	return nil
}

// WebServerNode - per-node summary for the dashboard nodes table.
type WebServerNode struct {
	Endpoint     string `json:"endpoint"`
	Online       bool   `json:"online"`
	DrivesOnline int    `json:"drivesOnline"`
	DrivesTotal  int    `json:"drivesTotal"`
	Used         uint64 `json:"used"`
	Total        uint64 `json:"total"`
	Pool         int    `json:"pool"`
}

// ListServersArgs - pagination arguments for ListServers.
type ListServersArgs struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// ToKeyValue implementation for ListServersArgs.
func (args *ListServersArgs) ToKeyValue() KeyValueMap {
	return KeyValueMap{}
}

// ServerListRep - list per-node cluster info.
type ServerListRep struct {
	Servers   []WebServerNode `json:"servers"`
	Total     int             `json:"total"`
	UIVersion string          `json:"uiVersion"`
}

// ListServers - per-node info for the dashboard nodes table from broadcasts
func (web *webAPIHandlers) ListServers(r *http.Request, args *ListServersArgs, reply *ServerListRep) error {
	ctx := newWebContext(r, args, "WebListServers")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	// Node topology isn't bucket-scopable, so gate like fleetGuard.
	if !callerCanSeeInstanceTotals(ctx, r, claims.AccessKey, claims.Map(), owner) {
		return toJSONError(ctx, errAccessDenied)
	}

	servers := globalNotificationSys.ServerInfo()
	servers = append(servers, getLocalServerProperty(globalEndpoints, r))
	assignPoolNumbers(servers)
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Endpoint < servers[j].Endpoint
	})

	reply.Total = len(servers)

	offset := args.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(servers) {
		offset = len(servers)
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	end := offset + limit
	if end > len(servers) {
		end = len(servers)
	}

	for _, node := range servers[offset:end] {
		n := WebServerNode{
			Endpoint:    node.Endpoint,
			Online:      node.State == string(madmin.ItemOnline),
			DrivesTotal: len(node.Disks),
			Pool:        node.PoolNumber,
		}
		for _, disk := range node.Disks {
			if disk.State == madmin.DriveStateOk {
				n.DrivesOnline++
			}
			n.Total += disk.TotalSpace
			n.Used += disk.UsedSpace
		}
		reply.Servers = append(reply.Servers, n)
	}

	reply.UIVersion = Version
	return nil
}

// StorageInfoRep - contains storage usage statistics.
type StorageInfoRep struct {
	Used         uint64 `json:"used"`
	Total        uint64 `json:"total"`
	Free         uint64 `json:"free"`
	DisksOnline  int    `json:"disksOnline"`
	DisksOffline int    `json:"disksOffline"`
	BucketsCount uint64 `json:"bucketsCount"`
	ObjectsCount uint64 `json:"objectsCount"`
	UIVersion    string `json:"uiVersion"`
}

func scopedUsage(bucketsUsage map[string]madmin.BucketUsageInfo, allow func(bucket string) bool) (used, objects, buckets uint64) {
	for bucket, usage := range bucketsUsage {
		if !allow(bucket) {
			continue
		}
		used += usage.Size
		objects += usage.ObjectsCount
		buckets++
	}
	return used, objects, buckets
}

// canSeeInstanceTotals reports whether the caller may view instance-wide hardware totals; not bucket-scopable, so exposed only to a caller allowed on every bucket, derived from bucket access not a hard-coded admin/owner flag.
func canSeeInstanceTotals(buckets []BucketInfo, allow func(bucket string) bool) bool {
	if len(buckets) == 0 {
		return false
	}
	for _, bucket := range buckets {
		if !allow(bucket.Name) {
			return false
		}
	}
	return true
}

// callerCanSeeInstanceTotals reports whether the caller may view instance-wide totals: allowed to list every bucket (owner passes via the core IAM short-circuit).
func callerCanSeeInstanceTotals(ctx context.Context, r *http.Request, accessKey string, claimsMap map[string]interface{}, owner bool) bool {
	if owner {
		return true
	}
	objectAPI := newObjectLayerFn()
	if objectAPI == nil {
		return false
	}
	r.Header.Set("prefix", "")
	r.Header.Set("delimiter", SlashSeparator)
	allow := func(bucket string) bool {
		return globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     accessKey,
			Action:          iampolicy.ListBucketAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", accessKey, claimsMap),
			IsOwner:         owner,
			Claims:          claimsMap,
		})
	}
	buckets, err := objectAPI.ListBuckets(ctx)
	if err != nil {
		return false
	}
	return canSeeInstanceTotals(buckets, allow)
}

// StorageInfo - web call to gather storage usage statistics.
func (web *webAPIHandlers) StorageInfo(r *http.Request, args *WebGenericArgs, reply *StorageInfoRep) error {
	ctx := newWebContext(r, args, "WebStorageInfo")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// Set prefix/delimiter for any s3:prefix / s3:delimiter policy conditionals.
	r.Header.Set("prefix", "")
	r.Header.Set("delimiter", SlashSeparator)

	// allow reports whether this caller can list the bucket; usage is always scoped to allowed buckets, no admin/owner shortcut (owner still sees all via the core IAM short-circuit).
	allow := func(bucket string) bool {
		return globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.ListBucketAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			Claims:          claims.Map(),
		})
	}

	dataUsageInfo, _ := loadDataUsageFromBackend(ctx, objectAPI)
	reply.Used, reply.ObjectsCount, reply.BucketsCount = scopedUsage(dataUsageInfo.BucketsUsage, allow)

	// Instance-wide hardware totals aren't bucket-scopable: expose only to a caller allowed on every bucket, checked against the live bucket list (not the periodic usage snapshot above).
	if buckets, err := objectAPI.ListBuckets(ctx); err == nil && canSeeInstanceTotals(buckets, allow) {
		storageInfo, _ := objectAPI.StorageInfo(ctx)
		for _, disk := range storageInfo.Disks {
			reply.Total += disk.TotalSpace
			reply.Free += disk.AvailableSpace
			if disk.State == madmin.DriveStateOk {
				reply.DisksOnline++
			} else {
				reply.DisksOffline++
			}
		}
	}

	reply.UIVersion = Version
	return nil
}

// MakeBucketArgs - make bucket args.
type MakeBucketArgs struct {
	BucketName string `json:"bucketName"`
}

// MakeBucket - creates a new bucket.
func (web *webAPIHandlers) MakeBucket(r *http.Request, args *MakeBucketArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebMakeBucket")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// For authenticated users apply IAM policy.
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.CreateBucketAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, true) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	opts := BucketOptions{
		Location:    globalServerRegion,
		LockEnabled: false,
	}

	if globalDNSConfig != nil {
		if _, err := globalDNSConfig.Get(args.BucketName); err != nil {
			if err == dns.ErrNoEntriesFound || err == dns.ErrNotImplemented {
				// Proceed to creating a bucket.
				if err = objectAPI.MakeBucketWithLocation(ctx, args.BucketName, opts); err != nil {
					return toJSONError(ctx, err)
				}

				if err = globalDNSConfig.Put(args.BucketName); err != nil {
					_ = objectAPI.DeleteBucket(ctx, args.BucketName, false)
					return toJSONError(ctx, err)
				}

				reply.UIVersion = Version
				return nil
			}
			return toJSONError(ctx, err)
		}
		return toJSONError(ctx, errBucketAlreadyExists)
	}

	if err := objectAPI.MakeBucketWithLocation(ctx, args.BucketName, opts); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}

	reply.UIVersion = Version

	reqParams := extractReqParams(r)
	reqParams["accessKey"] = claims.GetAccessKey()

	sendEvent(eventArgs{
		EventName:  event.BucketCreated,
		BucketName: args.BucketName,
		ReqParams:  reqParams,
		UserAgent:  r.UserAgent(),
		Host:       handlers.GetSourceIP(r),
	})

	return nil
}

// RemoveBucketArgs - remove bucket args.
type RemoveBucketArgs struct {
	BucketName string `json:"bucketName"`
}

// DeleteBucket - removes a bucket, must be empty.
func (web *webAPIHandlers) DeleteBucket(r *http.Request, args *RemoveBucketArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebDeleteBucket")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// For authenticated users apply IAM policy.
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.DeleteBucketAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	reply.UIVersion = Version

	if IsRemoteCallRequired(ctx, args.BucketName, objectAPI) {
		sr, err := globalDNSConfig.Get(args.BucketName)
		if err != nil {
			if err == dns.ErrNoEntriesFound {
				return toJSONError(ctx, BucketNotFound{
					Bucket: args.BucketName,
				}, args.BucketName)
			}
			return toJSONError(ctx, err, args.BucketName)
		}
		core, err := GetRemoteInstanceClient(r, getHostFromSrv(sr))
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
		if err = core.RemoveBucket(ctx, args.BucketName); err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
		return nil
	}

	deleteBucket := objectAPI.DeleteBucket

	if err := deleteBucket(ctx, args.BucketName, false); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}

	globalNotificationSys.DeleteBucketMetadata(ctx, args.BucketName)

	if globalDNSConfig != nil {
		if err := globalDNSConfig.Delete(args.BucketName); err != nil {
			logger.LogIf(ctx, fmt.Errorf("unable to delete bucket DNS entry %w, please delete it manually", err))
			return toJSONError(ctx, err)
		}
	}

	reqParams := extractReqParams(r)
	reqParams["accessKey"] = claims.AccessKey

	sendEvent(eventArgs{
		EventName:  event.BucketRemoved,
		BucketName: args.BucketName,
		ReqParams:  reqParams,
		UserAgent:  r.UserAgent(),
		Host:       handlers.GetSourceIP(r),
	})

	return nil
}

// ListBucketsRep - list buckets response
type ListBucketsRep struct {
	Buckets   []WebBucketInfo `json:"buckets"`
	UIVersion string          `json:"uiVersion"`
}

// WebBucketInfo container for list buckets metadata.
type WebBucketInfo struct {
	// The name of the bucket.
	Name string `json:"name"`
	// Date the bucket was created.
	CreationDate time.Time `json:"creationDate"`
}

// ListBuckets - list buckets api.
func (web *webAPIHandlers) ListBuckets(r *http.Request, args *WebGenericArgs, reply *ListBucketsRep) error {
	ctx := newWebContext(r, args, "WebListBuckets")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	listBuckets := objectAPI.ListBuckets

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// Set prefix value for "s3:prefix" policy conditionals.
	r.Header.Set("prefix", "")

	// Set delimiter value for "s3:delimiter" policy conditionals.
	r.Header.Set("delimiter", SlashSeparator)

	// If etcd, dns federation configured list buckets from etcd.
	if globalDNSConfig != nil && globalBucketFederation {
		dnsBuckets, err := globalDNSConfig.List()
		if err != nil && !IsErrIgnored(err,
			dns.ErrNoEntriesFound,
			dns.ErrDomainMissing) {
			return toJSONError(ctx, err)
		}
		for _, dnsRecords := range dnsBuckets {
			if globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.ListBucketAction,
				BucketName:      dnsRecords[0].Key,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				ObjectName:      "",
				Claims:          claims.Map(),
			}) {
				reply.Buckets = append(reply.Buckets, WebBucketInfo{
					Name:         dnsRecords[0].Key,
					CreationDate: dnsRecords[0].CreationDate,
				})
			}
		}
	} else {
		buckets, err := listBuckets(ctx)
		if err != nil {
			return toJSONError(ctx, err)
		}
		for _, bucket := range buckets {
			if globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.ListBucketAction,
				BucketName:      bucket.Name,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				ObjectName:      "",
				Claims:          claims.Map(),
			}) {
				reply.Buckets = append(reply.Buckets, WebBucketInfo{
					Name:         bucket.Name,
					CreationDate: bucket.Created,
				})
			}
		}
	}

	reply.UIVersion = Version
	return nil
}

// ListObjectsArgs - list object args.
type ListObjectsArgs struct {
	BucketName string `json:"bucketName"`
	Prefix     string `json:"prefix"`
	Marker     string `json:"marker"`
}

// ListObjectsRep - list objects response.
type ListObjectsRep struct {
	Objects   []WebObjectInfo `json:"objects"`
	Writable  bool            `json:"writable"` // Used by client to show "upload file" button.
	UIVersion string          `json:"uiVersion"`
}

// WebObjectInfo container for list objects metadata.
type WebObjectInfo struct {
	// Name of the object
	Key string `json:"name"`
	// Date and time the object was last modified.
	LastModified time.Time `json:"lastModified"`
	// Size in bytes of the object.
	Size int64 `json:"size"`
	// ContentType is mime type of the object.
	ContentType string `json:"contentType"`
	// ETag (Usually MD5 for non-multipart uploads)
	ETag string `json:"etag,omitempty"`
}

// ListObjects - list objects api.
func (web *webAPIHandlers) ListObjects(r *http.Request, args *ListObjectsArgs, reply *ListObjectsRep) error {
	ctx := newWebContext(r, args, "WebListObjects")
	reply.UIVersion = Version
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	listObjects := objectAPI.ListObjects

	if IsRemoteCallRequired(ctx, args.BucketName, objectAPI) {
		sr, err := globalDNSConfig.Get(args.BucketName)
		if err != nil {
			if err == dns.ErrNoEntriesFound {
				return toJSONError(ctx, BucketNotFound{
					Bucket: args.BucketName,
				}, args.BucketName)
			}
			return toJSONError(ctx, err, args.BucketName)
		}
		core, err := GetRemoteInstanceClient(r, getHostFromSrv(sr))
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}

		nextMarker := ""
		// Fetch all the objects
		for {
			// Let listObjects reply back the maximum from server implementation
			result, err := core.ListObjects(args.BucketName, args.Prefix, nextMarker, SlashSeparator, 1000)
			if err != nil {
				return toJSONError(ctx, err, args.BucketName)
			}

			for _, obj := range result.Contents {
				reply.Objects = append(reply.Objects, WebObjectInfo{
					Key:          obj.Key,
					LastModified: obj.LastModified,
					Size:         obj.Size,
					ContentType:  obj.ContentType,
					ETag:         strings.Trim(obj.ETag, "\""),
				})
			}
			for _, p := range result.CommonPrefixes {
				reply.Objects = append(reply.Objects, WebObjectInfo{
					Key: p.Prefix,
				})
			}

			nextMarker = result.NextMarker

			// Return when there are no more objects
			if !result.IsTruncated {
				return nil
			}
		}
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		if authErr == errNoAuthToken {
			// Set prefix value for "s3:prefix" policy conditionals.
			r.Header.Set("prefix", args.Prefix)

			// Set delimiter value for "s3:delimiter" policy conditionals.
			r.Header.Set("delimiter", SlashSeparator)

			// Check if anonymous (non-owner) has access to download objects.
			readable := globalPolicySys.IsAllowed(policy.Args{
				Action:          policy.ListBucketAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
			})

			// Check if anonymous (non-owner) has access to upload objects.
			writable := globalPolicySys.IsAllowed(policy.Args{
				Action:          policy.PutObjectAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
				ObjectName:      args.Prefix + SlashSeparator,
			})

			reply.Writable = writable
			if !readable {
				// Error out if anonymous user (non-owner) has no access to download or upload objects
				if !writable {
					return errAccessDenied
				}
				// return empty object list if access is write only
				return nil
			}
		} else {
			return toJSONError(ctx, authErr)
		}
	}

	// For authenticated users apply IAM policy.
	if authErr == nil {
		// Set prefix value for "s3:prefix" policy conditionals.
		r.Header.Set("prefix", args.Prefix)

		// Set delimiter value for "s3:delimiter" policy conditionals.
		r.Header.Set("delimiter", SlashSeparator)

		readable := globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.ListBucketAction,
			BucketName:      args.BucketName,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			Claims:          claims.Map(),
		})

		writable := globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.PutObjectAction,
			BucketName:      args.BucketName,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      args.Prefix + SlashSeparator,
			Claims:          claims.Map(),
		})

		reply.Writable = writable
		if !readable {
			// Error out if anonymous user (non-owner) has no access to download or upload objects
			if !writable {
				return errAccessDenied
			}
			// return empty object list if access is write only
			return nil
		}
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	nextMarker := ""
	// Fetch all the objects
	for {
		// Limit browser to '1000' batches to be more responsive, scrolling friendly.
		// Also don't change the maxKeys value silly GCS SDKs do not honor maxKeys
		// values to be '-1'
		lo, err := listObjects(ctx, args.BucketName, args.Prefix, nextMarker, SlashSeparator, 1000)
		if err != nil {
			return &json2.Error{Message: err.Error()}
		}

		nextMarker = lo.NextMarker
		for i := range lo.Objects {
			lo.Objects[i].Size, err = lo.Objects[i].GetActualSize()
			if err != nil {
				return toJSONError(ctx, err)
			}
		}

		for _, obj := range lo.Objects {
			reply.Objects = append(reply.Objects, WebObjectInfo{
				Key:          obj.Name,
				LastModified: obj.ModTime,
				Size:         obj.Size,
				ContentType:  obj.ContentType,
				ETag:         strings.Trim(obj.ETag, "\""),
			})
		}
		for _, prefix := range lo.Prefixes {
			reply.Objects = append(reply.Objects, WebObjectInfo{
				Key: prefix,
			})
		}

		// Return when there are no more objects
		if !lo.IsTruncated {
			return nil
		}
	}
}

// RemoveObjectArgs - args to remove an object, JSON will look like.
//
//	{
//	    "bucketname": "testbucket",
//	    "objects": [
//	        "photos/hawaii/",
//	        "photos/maldives/",
//	        "photos/sanjose.jpg"
//	    ]
//	}
type RemoveObjectArgs struct {
	Objects    []string `json:"objects"`    // Contains objects, prefixes.
	BucketName string   `json:"bucketname"` // Contains bucket name.
}

// RemoveObject - removes an object, or all the objects at a given prefix.
func (web *webAPIHandlers) RemoveObject(r *http.Request, args *RemoveObjectArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebRemoveObject")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	deleteObjects := objectAPI.DeleteObjects
	if web.CacheAPI() != nil {
		deleteObjects = web.CacheAPI().DeleteObjects
	}
	getObjectInfoFn := objectAPI.GetObjectInfo
	if web.CacheAPI() != nil {
		getObjectInfoFn = web.CacheAPI().GetObjectInfo
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		if authErr == errNoAuthToken {
			// Check if all objects are allowed to be deleted anonymously
			for _, object := range args.Objects {
				if !globalPolicySys.IsAllowed(policy.Args{
					Action:          policy.DeleteObjectAction,
					BucketName:      args.BucketName,
					ConditionValues: getConditionValues(r, "", "", nil),
					IsOwner:         false,
					ObjectName:      object,
				}) {
					return toJSONError(ctx, errAuthentication)
				}
			}
		} else {
			return toJSONError(ctx, authErr)
		}
	}

	if args.BucketName == "" || len(args.Objects) == 0 {
		return toJSONError(ctx, errInvalidArgument)
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	reply.UIVersion = Version
	if IsRemoteCallRequired(ctx, args.BucketName, objectAPI) {
		sr, err := globalDNSConfig.Get(args.BucketName)
		if err != nil {
			if err == dns.ErrNoEntriesFound {
				return toJSONError(ctx, BucketNotFound{
					Bucket: args.BucketName,
				}, args.BucketName)
			}
			return toJSONError(ctx, err, args.BucketName)
		}
		core, err := GetRemoteInstanceClient(r, getHostFromSrv(sr))
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
		objectsCh := make(chan obstor.ObjectInfo)

		// Send object names that are needed to be removed to objectsCh
		go func() {
			defer close(objectsCh)

			for _, objectName := range args.Objects {
				objectsCh <- obstor.ObjectInfo{
					Key: objectName,
				}
			}
		}()

		for resp := range core.RemoveObjects(ctx, args.BucketName, objectsCh, obstor.RemoveObjectsOptions{}) {
			if resp.Err != nil {
				return toJSONError(ctx, resp.Err, args.BucketName, resp.ObjectName)
			}
		}
		return nil
	}

	opts := ObjectOptions{
		Versioned:        globalBucketVersioningSys.Enabled(args.BucketName),
		VersionSuspended: globalBucketVersioningSys.Suspended(args.BucketName),
	}
	var (
		err           error
		replicateSync bool
	)

	reqParams := extractReqParams(r)
	reqParams["accessKey"] = claims.GetAccessKey()
	sourceIP := handlers.GetSourceIP(r)

next:
	for _, objectName := range args.Objects {
		// If not a directory, remove the object.
		if !HasSuffix(objectName, SlashSeparator) && objectName != "" {
			// Check permissions for non-anonymous user.
			if authErr != errNoAuthToken {
				if !globalIAMSys.IsAllowed(iampolicy.Args{
					AccountName:     claims.AccessKey,
					Action:          iampolicy.DeleteObjectAction,
					BucketName:      args.BucketName,
					ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
					IsOwner:         owner,
					ObjectName:      objectName,
					Claims:          claims.Map(),
				}) {
					return toJSONError(ctx, errAccessDenied)
				}
			}

			if authErr == errNoAuthToken {
				// Check if object is allowed to be deleted anonymously.
				if !globalPolicySys.IsAllowed(policy.Args{
					Action:          policy.DeleteObjectAction,
					BucketName:      args.BucketName,
					ConditionValues: getConditionValues(r, "", "", nil),
					IsOwner:         false,
					ObjectName:      objectName,
				}) {
					return toJSONError(ctx, errAccessDenied)
				}
			}
			var (
				replicateDel, hasLifecycleConfig bool
				goi                              ObjectInfo
				gerr                             error
			)
			if _, err := globalBucketMetadataSys.GetLifecycleConfig(args.BucketName); err == nil {
				hasLifecycleConfig = true
			}
			if hasReplicationRules(ctx, args.BucketName, []ObjectToDelete{{ObjectName: objectName}}) || hasLifecycleConfig {
				goi, gerr = getObjectInfoFn(ctx, args.BucketName, objectName, opts)
				if replicateDel, replicateSync = checkReplicateDelete(ctx, args.BucketName, ObjectToDelete{
					ObjectName: objectName,
					VersionID:  goi.VersionID,
				}, goi, gerr); replicateDel {
					opts.DeleteMarkerReplicationStatus = string(replication.Pending)
					opts.DeleteMarker = true
				}
			}

			deleteObject := objectAPI.DeleteObject
			if web.CacheAPI() != nil {
				deleteObject = web.CacheAPI().DeleteObject
			}

			oi, err := deleteObject(ctx, args.BucketName, objectName, opts)
			if err != nil {
				switch err.(type) {
				case BucketNotFound:
					return toJSONError(ctx, err)
				}
			}
			if oi.Name == "" {
				logger.LogIf(ctx, err)
				continue
			}

			eventName := event.ObjectRemovedDelete
			if oi.DeleteMarker {
				eventName = event.ObjectRemovedDeleteMarkerCreated
			}

			// Notify object deleted event.
			sendEvent(eventArgs{
				EventName:  eventName,
				BucketName: args.BucketName,
				Object:     oi,
				ReqParams:  reqParams,
				UserAgent:  r.UserAgent(),
				Host:       sourceIP,
			})

			if replicateDel {
				dobj := DeletedObjectVersionInfo{
					DeletedObject: DeletedObject{
						ObjectName:                    objectName,
						DeleteMarkerVersionID:         oi.VersionID,
						DeleteMarkerReplicationStatus: string(oi.ReplicationStatus),
						DeleteMarkerMTime:             DeleteMarkerMTime{oi.ModTime},
						DeleteMarker:                  oi.DeleteMarker,
						VersionPurgeStatus:            oi.VersionPurgeStatus,
					},
					Bucket: args.BucketName,
				}
				scheduleReplicationDelete(ctx, dobj, objectAPI, replicateSync)
			}
			if goi.TransitionStatus == lifecycle.TransitionComplete {
				deleteTransitionedObject(ctx, objectAPI, args.BucketName, objectName, lifecycle.ObjectOpts{
					Name:             objectName,
					UserTags:         goi.UserTags,
					VersionID:        goi.VersionID,
					DeleteMarker:     goi.DeleteMarker,
					TransitionStatus: goi.TransitionStatus,
					IsLatest:         goi.IsLatest,
				}, false, true)
			}

			logger.LogIf(ctx, err)
			continue
		}

		if authErr == errNoAuthToken {
			// Check if object is allowed to be deleted anonymously
			if !globalPolicySys.IsAllowed(policy.Args{
				Action:          iampolicy.DeleteObjectAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
				ObjectName:      objectName,
			}) {
				return toJSONError(ctx, errAccessDenied)
			}
		} else {
			if !globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.DeleteObjectAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				ObjectName:      objectName,
				Claims:          claims.Map(),
			}) {
				return toJSONError(ctx, errAccessDenied)
			}
		}

		// Allocate new results channel to receive ObjectInfo.
		objInfoCh := make(chan ObjectInfo)

		// Walk through all objects
		if err = objectAPI.Walk(ctx, args.BucketName, objectName, objInfoCh, ObjectOptions{}); err != nil {
			break next
		}

		for {
			var objects []ObjectToDelete
			for obj := range objInfoCh {
				if len(objects) == maxDeleteList {
					// Reached maximum delete requests, attempt a delete for now.
					break
				}
				if obj.ReplicationStatus == replication.Replica {
					if authErr == errNoAuthToken {
						// Check if object is allowed to be deleted anonymously
						if !globalPolicySys.IsAllowed(policy.Args{
							Action:          iampolicy.ReplicateDeleteAction,
							BucketName:      args.BucketName,
							ConditionValues: getConditionValues(r, "", "", nil),
							IsOwner:         false,
							ObjectName:      objectName,
						}) {
							return toJSONError(ctx, errAccessDenied)
						}
					} else {
						if !globalIAMSys.IsAllowed(iampolicy.Args{
							AccountName:     claims.AccessKey,
							Action:          iampolicy.ReplicateDeleteAction,
							BucketName:      args.BucketName,
							ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
							IsOwner:         owner,
							ObjectName:      objectName,
							Claims:          claims.Map(),
						}) {
							return toJSONError(ctx, errAccessDenied)
						}
					}
				}
				replicateDel, _ := checkReplicateDelete(ctx, args.BucketName, ObjectToDelete{ObjectName: obj.Name, VersionID: obj.VersionID}, obj, nil)
				// Since versioned delete is not available on web browser, yet - this is a simple DeleteMarker replication
				objToDel := ObjectToDelete{ObjectName: obj.Name}
				if replicateDel {
					objToDel.DeleteMarkerReplicationStatus = string(replication.Pending)
				}

				objects = append(objects, objToDel)
			}

			// Nothing to do.
			if len(objects) == 0 {
				break next
			}

			// Deletes a list of objects.
			deletedObjects, errs := deleteObjects(ctx, args.BucketName, objects, opts)
			for i, err := range errs {
				if err != nil && !isErrObjectNotFound(err) {
					deletedObjects[i].DeleteMarkerReplicationStatus = objects[i].DeleteMarkerReplicationStatus
					deletedObjects[i].VersionPurgeStatus = objects[i].VersionPurgeStatus
				}
				if err != nil {
					logger.LogIf(ctx, err)
					break next
				}
			}
			// Notify deleted event for objects.
			for _, dobj := range deletedObjects {
				objInfo := ObjectInfo{
					Name:      dobj.ObjectName,
					VersionID: dobj.VersionID,
				}
				if dobj.DeleteMarker {
					objInfo = ObjectInfo{
						Name:         dobj.ObjectName,
						DeleteMarker: dobj.DeleteMarker,
						VersionID:    dobj.DeleteMarkerVersionID,
					}
				}
				sendEvent(eventArgs{
					EventName:  event.ObjectRemovedDelete,
					BucketName: args.BucketName,
					Object:     objInfo,
					ReqParams:  reqParams,
					UserAgent:  r.UserAgent(),
					Host:       sourceIP,
				})
				if dobj.DeleteMarkerReplicationStatus == string(replication.Pending) || dobj.VersionPurgeStatus == Pending {
					dv := DeletedObjectVersionInfo{
						DeletedObject: dobj,
						Bucket:        args.BucketName,
					}
					scheduleReplicationDelete(ctx, dv, objectAPI, replicateSync)
				}
			}
		}
	}

	if err != nil && !isErrObjectNotFound(err) && !isErrVersionNotFound(err) {
		// Ignore object not found error.
		return toJSONError(ctx, err, args.BucketName, "")
	}

	return nil
}

// LoginArgs - login arguments.
type LoginArgs struct {
	Username string `json:"username" form:"username"`
	Password string `json:"password" form:"password"`
}

// LoginRep - login reply.
type LoginRep struct {
	Token     string `json:"token"`
	UIVersion string `json:"uiVersion"`
}

// Login - user login handler.
func (web *webAPIHandlers) Login(r *http.Request, args *LoginArgs, reply *LoginRep) error {
	ctx := newWebContext(r, args, "WebLogin")
	token, err := authenticateWeb(args.Username, args.Password)
	if err != nil {
		return toJSONError(ctx, err)
	}

	reply.Token = token
	reply.UIVersion = Version
	return nil
}

// SetAuthArgs - argument for SetAuth
type SetAuthArgs struct {
	CurrentSecretKey string `json:"currentSecretKey"`
	NewSecretKey     string `json:"newSecretKey"`
}

// SetAuthReply - reply for SetAuth
type SetAuthReply struct {
	Token     string `json:"token"`
	UIVersion string `json:"uiVersion"`
}

// SetAuth - Set accessKey and secretKey credentials.
func (web *webAPIHandlers) SetAuth(r *http.Request, args *SetAuthArgs, reply *SetAuthReply) error {
	ctx := newWebContext(r, args, "WebSetAuth")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	if owner {
		// Owner is not allowed to change credentials through browser.
		return toJSONError(ctx, errChangeCredNotAllowed)
	}

	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.CreateUserAdminAction,
		IsOwner:         false,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		Claims:          claims.Map(),
		DenyOnly:        true,
	}) {
		return toJSONError(ctx, errChangeCredNotAllowed)
	}

	// for IAM users, access key cannot be updated
	// claims.AccessKey is used instead of accesskey from args
	prevCred, ok := globalIAMSys.GetUser(claims.AccessKey)
	if !ok {
		return errInvalidAccessKeyID
	}

	// Throw error when wrong secret key is provided
	if subtle.ConstantTimeCompare([]byte(prevCred.SecretKey), []byte(args.CurrentSecretKey)) != 1 {
		return errIncorrectCreds
	}

	creds, err := auth.CreateCredentials(claims.AccessKey, args.NewSecretKey)
	if err != nil {
		return toJSONError(ctx, err)
	}

	err = globalIAMSys.SetUserSecretKey(creds.AccessKey, creds.SecretKey)
	if err != nil {
		return toJSONError(ctx, err)
	}

	reply.Token, err = authenticateWeb(creds.AccessKey, creds.SecretKey)
	if err != nil {
		return toJSONError(ctx, err)
	}

	reply.UIVersion = Version

	return nil
}

// URLTokenReply contains the reply for CreateURLToken.
type URLTokenReply struct {
	Token     string `json:"token"`
	UIVersion string `json:"uiVersion"`
}

// CreateURLToken creates a URL token (short-lived) for GET requests.
func (web *webAPIHandlers) CreateURLToken(r *http.Request, args *WebGenericArgs, reply *URLTokenReply) error {
	ctx := newWebContext(r, args, "WebCreateURLToken")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	creds := globalActiveCred
	if !owner {
		var ok bool
		creds, ok = globalIAMSys.GetUser(claims.AccessKey)
		if !ok {
			return toJSONError(ctx, errInvalidAccessKeyID)
		}
	}

	if creds.SessionToken != "" {
		// Use the same session token for URL token.
		reply.Token = creds.SessionToken
	} else {
		token, err := authenticateURL(creds.AccessKey, creds.SecretKey)
		if err != nil {
			return toJSONError(ctx, err)
		}
		reply.Token = token
	}

	reply.UIVersion = Version
	return nil
}

// Upload - file upload handler.
func (web *webAPIHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "WebUpload")

	// Obtain the claims here if possible, for audit logging.
	claims, owner, authErr := webRequestAuthenticate(r)

	defer logger.AuditLog(ctx, w, r, claims.Map())

	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		writeWebErrorResponse(w, errServerNotInitialized)
		return
	}

	vars := mux.Vars(r)
	bucket := vars["bucket"]
	object, err := unescapePath(vars["object"])
	if err != nil {
		writeWebErrorResponse(w, err)
		return
	}

	retPerms := ErrAccessDenied
	holdPerms := ErrAccessDenied
	replPerms := ErrAccessDenied
	if authErr != nil {
		if authErr == errNoAuthToken {
			// Check if anonymous (non-owner) has access to upload objects.
			if !globalPolicySys.IsAllowed(policy.Args{
				Action:          policy.PutObjectAction,
				BucketName:      bucket,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
				ObjectName:      object,
			}) {
				writeWebErrorResponse(w, errAuthentication)
				return
			}
		} else {
			writeWebErrorResponse(w, authErr)
			return
		}
	}

	// For authenticated users apply IAM policy.
	if authErr == nil {
		if !globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.PutObjectAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      object,
			Claims:          claims.Map(),
		}) {
			writeWebErrorResponse(w, errAuthentication)
			return
		}
		if globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.PutObjectRetentionAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      object,
			Claims:          claims.Map(),
		}) {
			retPerms = ErrNone
		}
		if globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.PutObjectLegalHoldAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      object,
			Claims:          claims.Map(),
		}) {
			holdPerms = ErrNone
		}
		if globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.GetReplicationConfigurationAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      "",
			Claims:          claims.Map(),
		}) {
			replPerms = ErrNone
		}
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(bucket, false) {
		writeWebErrorResponse(w, errInvalidBucketName)
		return
	}

	// Check if bucket encryption is enabled
	_, err = globalBucketSSEConfigSys.Get(bucket)
	if (globalAutoEncryption || err == nil) && !crypto.SSEC.IsRequested(r.Header) {
		r.Header.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionAES)
	}

	// Require Content-Length to be set in the request
	size := r.ContentLength
	if size < 0 {
		writeWebErrorResponse(w, errSizeUnspecified)
		return
	}

	if err := enforceBucketQuota(ctx, bucket, size); err != nil {
		writeWebErrorResponse(w, err)
		return
	}

	// Extract incoming metadata if any.
	metadata, err := extractMetadata(ctx, r)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
		return
	}

	var pReader *PutObjReader
	var reader io.Reader = r.Body
	actualSize := size

	hashReader, err := hash.NewReader(reader, size, "", "", actualSize)
	if err != nil {
		writeWebErrorResponse(w, err)
		return
	}

	if objectAPI.IsCompressionSupported() && isCompressible(r.Header, object) && size > 0 {
		// Storing the compression metadata.
		metadata[ReservedMetadataPrefix+"compression"] = compressionAlgorithmV2
		metadata[ReservedMetadataPrefix+"actual-size"] = strconv.FormatInt(actualSize, 10)

		actualReader, err := hash.NewReader(reader, actualSize, "", "", actualSize)
		if err != nil {
			writeWebErrorResponse(w, err)
			return
		}

		// Set compression metrics.
		size = -1 // Since compressed size is un-predictable.
		s2c := newS2CompressReader(actualReader, actualSize)
		defer s2c.Close()
		reader = etag.Wrap(s2c, actualReader)
		hashReader, err = hash.NewReader(reader, size, "", "", actualSize)
		if err != nil {
			writeWebErrorResponse(w, err)
			return
		}
	}

	mustReplicate, sync := mustReplicateWeb(ctx, r, bucket, object, metadata, "", replPerms)
	if mustReplicate {
		metadata[xhttp.AmzBucketReplicationStatus] = string(replication.Pending)
	}
	pReader = NewPutObjReader(hashReader)
	// Get backend encryption options
	opts, err := putOpts(ctx, r, bucket, object, metadata)
	if err != nil {
		writeErrorResponseHeadersOnly(w, toAPIError(ctx, err))
		return
	}

	if objectAPI.IsEncryptionSupported() {
		if _, ok := crypto.IsRequested(r.Header); ok && !HasSuffix(object, SlashSeparator) { // handle SSE requests
			var (
				objectEncryptionKey crypto.ObjectKey
				encReader           io.Reader
			)
			encReader, objectEncryptionKey, err = EncryptRequest(hashReader, r, bucket, object, metadata)
			if err != nil {
				writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
				return
			}
			info := ObjectInfo{Size: size}
			// Do not try to verify encrypted content
			hashReader, err = hash.NewReader(etag.Wrap(encReader, hashReader), info.EncryptedSize(), "", "", size)
			if err != nil {
				writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
				return
			}
			pReader, err = pReader.WithEncryption(hashReader, &objectEncryptionKey)
			if err != nil {
				writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
				return
			}
		}
	}

	// Ensure that metadata does not contain sensitive information
	crypto.RemoveSensitiveEntries(metadata)

	putObject := objectAPI.PutObject
	getObjectInfo := objectAPI.GetObjectInfo
	if web.CacheAPI() != nil {
		putObject = web.CacheAPI().PutObject
		getObjectInfo = web.CacheAPI().GetObjectInfo
	}

	// Enforce object retention rules
	retentionMode, retentionDate, _, s3Err := checkPutObjectLockAllowed(ctx, r, bucket, object, getObjectInfo, retPerms, holdPerms)
	if s3Err != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Err), r.URL, guessIsBrowserReq(r))
		return
	}
	if retentionMode != "" {
		opts.UserDefined[strings.ToLower(xhttp.AmzObjectLockMode)] = string(retentionMode)
		opts.UserDefined[strings.ToLower(xhttp.AmzObjectLockRetainUntilDate)] = retentionDate.UTC().Format(iso8601TimeFormat)
	}

	objInfo, err := putObject(GlobalContext, bucket, object, pReader, opts)
	if err != nil {
		writeWebErrorResponse(w, err)
		return
	}
	if objectAPI.IsEncryptionSupported() {
		switch kind, _ := crypto.IsEncrypted(objInfo.UserDefined); kind {
		case crypto.S3:
			w.Header().Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionAES)
		case crypto.SSEC:
			w.Header().Set(xhttp.AmzServerSideEncryptionCustomerAlgorithm, r.Header.Get(xhttp.AmzServerSideEncryptionCustomerAlgorithm))
			w.Header().Set(xhttp.AmzServerSideEncryptionCustomerKeyMD5, r.Header.Get(xhttp.AmzServerSideEncryptionCustomerKeyMD5))
		}
	}
	if mustReplicate {
		scheduleReplication(ctx, objInfo.Clone(), objectAPI, sync, replication.ObjectReplicationType)
	}

	reqParams := extractReqParams(r)
	reqParams["accessKey"] = claims.GetAccessKey()

	// Notify object created event.
	sendEvent(eventArgs{
		EventName:    event.ObjectCreatedPut,
		BucketName:   bucket,
		Object:       objInfo,
		ReqParams:    reqParams,
		RespElements: extractRespElements(w),
		UserAgent:    r.UserAgent(),
		Host:         handlers.GetSourceIP(r),
	})
}

// Download - file download handler.
func (web *webAPIHandlers) Download(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "WebDownload")

	claims, owner, authErr := webTokenAuthenticate(r.URL.Query().Get("token"))
	defer logger.AuditLog(ctx, w, r, claims.Map())

	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		writeWebErrorResponse(w, errServerNotInitialized)
		return
	}

	vars := mux.Vars(r)

	bucket := vars["bucket"]
	object, err := unescapePath(vars["object"])
	if err != nil {
		writeWebErrorResponse(w, err)
		return
	}

	getRetPerms := ErrAccessDenied
	legalHoldPerms := ErrAccessDenied

	if authErr != nil {
		if authErr == errNoAuthToken {
			// Check if anonymous (non-owner) has access to download objects.
			if !globalPolicySys.IsAllowed(policy.Args{
				Action:          policy.GetObjectAction,
				BucketName:      bucket,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
				ObjectName:      object,
			}) {
				writeWebErrorResponse(w, errAuthentication)
				return
			}
			if globalPolicySys.IsAllowed(policy.Args{
				Action:          policy.GetObjectRetentionAction,
				BucketName:      bucket,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
				ObjectName:      object,
			}) {
				getRetPerms = ErrNone
			}
			if globalPolicySys.IsAllowed(policy.Args{
				Action:          policy.GetObjectLegalHoldAction,
				BucketName:      bucket,
				ConditionValues: getConditionValues(r, "", "", nil),
				IsOwner:         false,
				ObjectName:      object,
			}) {
				legalHoldPerms = ErrNone
			}
		} else {
			writeWebErrorResponse(w, authErr)
			return
		}
	}

	// For authenticated users apply IAM policy.
	if authErr == nil {
		if !globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.GetObjectAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      object,
			Claims:          claims.Map(),
		}) {
			writeWebErrorResponse(w, errAuthentication)
			return
		}
		if globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.GetObjectRetentionAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      object,
			Claims:          claims.Map(),
		}) {
			getRetPerms = ErrNone
		}
		if globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          iampolicy.GetObjectLegalHoldAction,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			ObjectName:      object,
			Claims:          claims.Map(),
		}) {
			legalHoldPerms = ErrNone
		}
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(bucket, false) {
		writeWebErrorResponse(w, errInvalidBucketName)
		return
	}

	getObjectNInfo := objectAPI.GetObjectNInfo
	if web.CacheAPI() != nil {
		getObjectNInfo = web.CacheAPI().GetObjectNInfo
	}

	var opts ObjectOptions
	gr, err := getObjectNInfo(ctx, bucket, object, nil, r.Header, readLock, opts)
	if err != nil {
		writeWebErrorResponse(w, err)
		return
	}
	defer func() { _ = gr.Close() }()

	objInfo := gr.ObjInfo

	// Filter object lock metadata if permission does not permit
	objInfo.UserDefined = objectlock.FilterObjectLockMetadata(objInfo.UserDefined, getRetPerms != ErrNone, legalHoldPerms != ErrNone)

	if objectAPI.IsEncryptionSupported() {
		if _, err = DecryptObjectInfo(&objInfo, r); err != nil {
			writeWebErrorResponse(w, err)
			return
		}
	}

	// Set encryption response headers
	if objectAPI.IsEncryptionSupported() {
		switch kind, _ := crypto.IsEncrypted(objInfo.UserDefined); kind {
		case crypto.S3:
			w.Header().Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionAES)
		case crypto.SSEC:
			w.Header().Set(xhttp.AmzServerSideEncryptionCustomerAlgorithm, r.Header.Get(xhttp.AmzServerSideEncryptionCustomerAlgorithm))
			w.Header().Set(xhttp.AmzServerSideEncryptionCustomerKeyMD5, r.Header.Get(xhttp.AmzServerSideEncryptionCustomerKeyMD5))
		}
	}

	// Set Parts Count Header
	if opts.PartNumber > 0 && len(objInfo.Parts) > 0 {
		setPartsCountHeaders(w, objInfo)
	}

	if err = setObjectHeaders(w, objInfo, nil, opts); err != nil {
		writeWebErrorResponse(w, err)
		return
	}

	// Add content disposition.
	w.Header().Set(xhttp.ContentDisposition, fmt.Sprintf("attachment; filename=\"%s\"", safeDisplayName(path.Base(objInfo.Name))))

	SetHeadGetRespHeaders(w, r.URL.Query())

	httpWriter := ioutil.WriteOnClose(w)

	// Write object content to response body
	if _, err = io.Copy(httpWriter, gr); err != nil {
		if !httpWriter.HasWritten() { // write error response only if no data or headers has been written to client yet
			writeWebErrorResponse(w, err)
		}
		return
	}

	if err = httpWriter.Close(); err != nil {
		if !httpWriter.HasWritten() { // write error response only if no data or headers has been written to client yet
			writeWebErrorResponse(w, err)
			return
		}
	}

	reqParams := extractReqParams(r)
	reqParams["accessKey"] = claims.GetAccessKey()

	// Notify object accessed via a GET request.
	sendEvent(eventArgs{
		EventName:    event.ObjectAccessedGet,
		BucketName:   bucket,
		Object:       objInfo,
		ReqParams:    reqParams,
		RespElements: extractRespElements(w),
		UserAgent:    r.UserAgent(),
		Host:         handlers.GetSourceIP(r),
	})
}

// DownloadZipArgs - Argument for downloading a bunch of files as a zip file.
// JSON will look like:
// '{"bucketname":"testbucket","prefix":"john/pics/","objects":["hawaii/","maldives/","sanjose.jpg"]}'
type DownloadZipArgs struct {
	Objects    []string `json:"objects"`    // can be files or sub-directories
	Prefix     string   `json:"prefix"`     // current directory in the browser-ui
	BucketName string   `json:"bucketname"` // bucket name.
}

// Takes a list of objects and creates a zip file that sent as the response body.
func (web *webAPIHandlers) DownloadZip(w http.ResponseWriter, r *http.Request) {
	host := handlers.GetSourceIP(r)

	claims, owner, authErr := webTokenAuthenticate(r.URL.Query().Get("token"))

	ctx := newContext(r, w, "WebDownloadZip")
	defer logger.AuditLog(ctx, w, r, claims.Map())

	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		writeWebErrorResponse(w, errServerNotInitialized)
		return
	}

	// Auth is done after reading the body to accommodate for anonymous requests
	// when bucket policy is enabled.
	var args DownloadZipArgs
	tenKB := 10 * 1024 // To limit r.Body to take care of misbehaving anonymous client.
	decodeErr := json.NewDecoder(io.LimitReader(r.Body, int64(tenKB))).Decode(&args)
	if decodeErr != nil {
		writeWebErrorResponse(w, decodeErr)
		return
	}

	var getRetPerms []APIErrorCode
	var legalHoldPerms []APIErrorCode

	if authErr != nil {
		if authErr == errNoAuthToken {
			for _, object := range args.Objects {
				// Check if anonymous (non-owner) has access to download objects.
				if !globalPolicySys.IsAllowed(policy.Args{
					Action:          policy.GetObjectAction,
					BucketName:      args.BucketName,
					ConditionValues: getConditionValues(r, "", "", nil),
					IsOwner:         false,
					ObjectName:      pathJoin(args.Prefix, object),
				}) {
					writeWebErrorResponse(w, errAuthentication)
					return
				}
				retentionPerm := ErrAccessDenied
				if globalPolicySys.IsAllowed(policy.Args{
					Action:          policy.GetObjectRetentionAction,
					BucketName:      args.BucketName,
					ConditionValues: getConditionValues(r, "", "", nil),
					IsOwner:         false,
					ObjectName:      pathJoin(args.Prefix, object),
				}) {
					retentionPerm = ErrNone
				}
				getRetPerms = append(getRetPerms, retentionPerm)

				legalHoldPerm := ErrAccessDenied
				if globalPolicySys.IsAllowed(policy.Args{
					Action:          policy.GetObjectLegalHoldAction,
					BucketName:      args.BucketName,
					ConditionValues: getConditionValues(r, "", "", nil),
					IsOwner:         false,
					ObjectName:      pathJoin(args.Prefix, object),
				}) {
					legalHoldPerm = ErrNone
				}
				legalHoldPerms = append(legalHoldPerms, legalHoldPerm)
			}
		} else {
			writeWebErrorResponse(w, authErr)
			return
		}
	}

	// For authenticated users apply IAM policy.
	if authErr == nil {
		for _, object := range args.Objects {
			if !globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.GetObjectAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				ObjectName:      pathJoin(args.Prefix, object),
				Claims:          claims.Map(),
			}) {
				writeWebErrorResponse(w, errAuthentication)
				return
			}
			retentionPerm := ErrAccessDenied
			if globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.GetObjectRetentionAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				ObjectName:      pathJoin(args.Prefix, object),
				Claims:          claims.Map(),
			}) {
				retentionPerm = ErrNone
			}
			getRetPerms = append(getRetPerms, retentionPerm)

			legalHoldPerm := ErrAccessDenied
			if globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.GetObjectLegalHoldAction,
				BucketName:      args.BucketName,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				ObjectName:      pathJoin(args.Prefix, object),
				Claims:          claims.Map(),
			}) {
				legalHoldPerm = ErrNone
			}
			legalHoldPerms = append(legalHoldPerms, legalHoldPerm)
		}
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		writeWebErrorResponse(w, errInvalidBucketName)
		return
	}

	getObjectNInfo := objectAPI.GetObjectNInfo
	if web.CacheAPI() != nil {
		getObjectNInfo = web.CacheAPI().GetObjectNInfo
	}

	archive := zip.NewWriter(w)
	defer func() { _ = archive.Close() }()

	reqParams := extractReqParams(r)
	reqParams["accessKey"] = claims.GetAccessKey()
	respElements := extractRespElements(w)

	for i, object := range args.Objects {
		if contextCanceled(ctx) {
			return
		}
		// Writes compressed object file to the response.
		zipit := func(objectName string) error {
			var opts ObjectOptions
			gr, err := getObjectNInfo(ctx, args.BucketName, objectName, nil, r.Header, readLock, opts)
			if err != nil {
				return err
			}
			defer func() { _ = gr.Close() }()

			info := gr.ObjInfo
			// Filter object lock metadata if permission does not permit
			info.UserDefined = objectlock.FilterObjectLockMetadata(info.UserDefined, getRetPerms[i] != ErrNone, legalHoldPerms[i] != ErrNone)
			// For reporting, set the file size to the uncompressed size.
			info.Size, err = info.GetActualSize()
			if err != nil {
				return err
			}
			header := &zip.FileHeader{
				Name:               strings.TrimPrefix(objectName, args.Prefix),
				Method:             zip.Deflate,
				Flags:              1 << 11,
				Modified:           info.ModTime,
				UncompressedSize64: uint64(info.Size),
			}
			if info.Size < 20 || hasStringSuffixInSlice(info.Name, standardExcludeCompressExtensions) || hasPattern(standardExcludeCompressContentTypes, info.ContentType) {
				// We strictly disable compression for standard extensions/content-types.
				header.Method = zip.Store
			}
			writer, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}

			// Write object content to response body
			if _, err = io.Copy(writer, gr); err != nil {
				return err
			}

			// Notify object accessed via a GET request.
			sendEvent(eventArgs{
				EventName:    event.ObjectAccessedGet,
				BucketName:   args.BucketName,
				Object:       info,
				ReqParams:    reqParams,
				RespElements: respElements,
				UserAgent:    r.UserAgent(),
				Host:         host,
			})

			return nil
		}

		if !HasSuffix(object, SlashSeparator) {
			// If not a directory, compress the file and write it to response.
			err := zipit(pathJoin(args.Prefix, object))
			if err != nil {
				logger.LogIf(ctx, err)
				return
			}
			continue
		}

		objInfoCh := make(chan ObjectInfo)

		// Walk through all objects
		if err := objectAPI.Walk(ctx, args.BucketName, pathJoin(args.Prefix, object), objInfoCh, ObjectOptions{}); err != nil {
			logger.LogIf(ctx, err)
			continue
		}

		for obj := range objInfoCh {
			if err := zipit(obj.Name); err != nil {
				logger.LogIf(ctx, err)
				continue
			}
		}
	}
}

// GetBucketPolicyArgs - get bucket policy args.
type GetBucketPolicyArgs struct {
	BucketName string `json:"bucketName"`
	Prefix     string `json:"prefix"`
}

// GetBucketPolicyRep - get bucket policy reply.
type GetBucketPolicyRep struct {
	UIVersion string                    `json:"uiVersion"`
	Policy    obstorpolicy.BucketPolicy `json:"policy"`
}

// GetBucketPolicy - get bucket policy for the requested prefix.
func (web *webAPIHandlers) GetBucketPolicy(r *http.Request, args *GetBucketPolicyArgs, reply *GetBucketPolicyRep) error {
	ctx := newWebContext(r, args, "WebGetBucketPolicy")

	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// For authenticated users apply IAM policy.
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.GetBucketPolicyAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	var policyInfo = &obstorpolicy.BucketAccessPolicy{Version: "2012-10-17"}
	if IsRemoteCallRequired(ctx, args.BucketName, objectAPI) {
		sr, err := globalDNSConfig.Get(args.BucketName)
		if err != nil {
			if err == dns.ErrNoEntriesFound {
				return toJSONError(ctx, BucketNotFound{
					Bucket: args.BucketName,
				}, args.BucketName)
			}
			return toJSONError(ctx, err, args.BucketName)
		}
		client, rerr := GetRemoteInstanceClient(r, getHostFromSrv(sr))
		if rerr != nil {
			return toJSONError(ctx, rerr, args.BucketName)
		}
		policyStr, err := client.GetBucketPolicy(ctx, args.BucketName)
		if err != nil {
			return toJSONError(ctx, rerr, args.BucketName)
		}
		bucketPolicy, err := policy.ParseConfig(strings.NewReader(policyStr), args.BucketName)
		if err != nil {
			return toJSONError(ctx, rerr, args.BucketName)
		}
		policyInfo, err = PolicyToBucketAccessPolicy(bucketPolicy)
		if err != nil {
			// This should not happen.
			return toJSONError(ctx, err, args.BucketName)
		}
	} else {
		bucketPolicy, err := globalPolicySys.Get(args.BucketName)
		if err != nil {
			if _, ok := err.(BucketPolicyNotFound); !ok {
				return toJSONError(ctx, err, args.BucketName)
			}
		}

		policyInfo, err = PolicyToBucketAccessPolicy(bucketPolicy)
		if err != nil {
			// This should not happen.
			return toJSONError(ctx, err, args.BucketName)
		}
	}

	reply.UIVersion = Version
	reply.Policy = obstorpolicy.GetPolicy(policyInfo.Statements, args.BucketName, args.Prefix)

	return nil
}

// ListAllBucketPoliciesArgs - get all bucket policies.
type ListAllBucketPoliciesArgs struct {
	BucketName string `json:"bucketName"`
}

// BucketAccessPolicy - Collection of canned bucket policy at a given prefix.
type BucketAccessPolicy struct {
	Bucket string                    `json:"bucket"`
	Prefix string                    `json:"prefix"`
	Policy obstorpolicy.BucketPolicy `json:"policy"`
}

// ListAllBucketPoliciesRep - get all bucket policy reply.
type ListAllBucketPoliciesRep struct {
	UIVersion string               `json:"uiVersion"`
	Policies  []BucketAccessPolicy `json:"policies"`
}

// ListAllBucketPolicies - get all bucket policy.
func (web *webAPIHandlers) ListAllBucketPolicies(r *http.Request, args *ListAllBucketPoliciesArgs, reply *ListAllBucketPoliciesRep) error {
	ctx := newWebContext(r, args, "WebListAllBucketPolicies")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// For authenticated users apply IAM policy.
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.GetBucketPolicyAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	var policyInfo = new(obstorpolicy.BucketAccessPolicy)
	if IsRemoteCallRequired(ctx, args.BucketName, objectAPI) {
		sr, err := globalDNSConfig.Get(args.BucketName)
		if err != nil {
			if err == dns.ErrNoEntriesFound {
				return toJSONError(ctx, BucketNotFound{
					Bucket: args.BucketName,
				}, args.BucketName)
			}
			return toJSONError(ctx, err, args.BucketName)
		}
		core, rerr := GetRemoteInstanceClient(r, getHostFromSrv(sr))
		if rerr != nil {
			return toJSONError(ctx, rerr, args.BucketName)
		}
		var policyStr string
		policyStr, err = core.Client.GetBucketPolicy(ctx, args.BucketName)
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
		if policyStr != "" {
			if err = json.Unmarshal([]byte(policyStr), policyInfo); err != nil {
				return toJSONError(ctx, err, args.BucketName)
			}
		}
	} else {
		bucketPolicy, err := globalPolicySys.Get(args.BucketName)
		if err != nil {
			if _, ok := err.(BucketPolicyNotFound); !ok {
				return toJSONError(ctx, err, args.BucketName)
			}
		}
		policyInfo, err = PolicyToBucketAccessPolicy(bucketPolicy)
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
	}

	reply.UIVersion = Version
	for prefix, policy := range obstorpolicy.GetPolicies(policyInfo.Statements, args.BucketName, "") {
		bucketName, objectPrefix := path2BucketObject(prefix)
		objectPrefix = strings.TrimSuffix(objectPrefix, "*")
		reply.Policies = append(reply.Policies, BucketAccessPolicy{
			Bucket: bucketName,
			Prefix: objectPrefix,
			Policy: policy,
		})
	}

	return nil
}

// SetBucketPolicyWebArgs - set bucket policy args.
type SetBucketPolicyWebArgs struct {
	BucketName string `json:"bucketName"`
	Prefix     string `json:"prefix"`
	Policy     string `json:"policy"`
}

// SetBucketPolicy - set bucket policy.
func (web *webAPIHandlers) SetBucketPolicy(r *http.Request, args *SetBucketPolicyWebArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketPolicy")
	objectAPI := web.ObjectAPI()
	reply.UIVersion = Version

	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	// For authenticated users apply IAM policy.
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.PutBucketPolicyAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	policyType := obstorpolicy.BucketPolicy(args.Policy)
	if !policyType.IsValidBucketPolicy() {
		return &json2.Error{
			Message: "Invalid policy type " + args.Policy,
		}
	}

	if IsRemoteCallRequired(ctx, args.BucketName, objectAPI) {
		sr, err := globalDNSConfig.Get(args.BucketName)
		if err != nil {
			if err == dns.ErrNoEntriesFound {
				return toJSONError(ctx, BucketNotFound{
					Bucket: args.BucketName,
				}, args.BucketName)
			}
			return toJSONError(ctx, err, args.BucketName)
		}
		core, rerr := GetRemoteInstanceClient(r, getHostFromSrv(sr))
		if rerr != nil {
			return toJSONError(ctx, rerr, args.BucketName)
		}
		var policyStr string
		// Use the abstracted API instead of core, such that
		// NoSuchBucketPolicy errors are automatically handled.
		policyStr, err = core.Client.GetBucketPolicy(ctx, args.BucketName)
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
		var policyInfo = &obstorpolicy.BucketAccessPolicy{Version: "2012-10-17"}
		if policyStr != "" {
			if err = json.Unmarshal([]byte(policyStr), policyInfo); err != nil {
				return toJSONError(ctx, err, args.BucketName)
			}
		}

		policyInfo.Statements = obstorpolicy.SetPolicy(policyInfo.Statements, policyType, args.BucketName, args.Prefix)
		if len(policyInfo.Statements) == 0 {
			if err = core.SetBucketPolicy(ctx, args.BucketName, ""); err != nil {
				return toJSONError(ctx, err, args.BucketName)
			}
			return nil
		}

		bucketPolicy, err := BucketAccessPolicyToPolicy(policyInfo)
		if err != nil {
			// This should not happen.
			return toJSONError(ctx, err, args.BucketName)
		}

		policyData, err := json.Marshal(bucketPolicy)
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}

		if err = core.SetBucketPolicy(ctx, args.BucketName, string(policyData)); err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}

	} else {
		bucketPolicy, err := globalPolicySys.Get(args.BucketName)
		if err != nil {
			if _, ok := err.(BucketPolicyNotFound); !ok {
				return toJSONError(ctx, err, args.BucketName)
			}
		}
		policyInfo, err := PolicyToBucketAccessPolicy(bucketPolicy)
		if err != nil {
			// This should not happen.
			return toJSONError(ctx, err, args.BucketName)
		}

		policyInfo.Statements = obstorpolicy.SetPolicy(policyInfo.Statements, policyType, args.BucketName, args.Prefix)
		if len(policyInfo.Statements) == 0 {
			if err = globalBucketMetadataSys.Update(args.BucketName, bucketPolicyConfig, nil); err != nil {
				return toJSONError(ctx, err, args.BucketName)
			}

			return nil
		}

		bucketPolicy, err = BucketAccessPolicyToPolicy(policyInfo)
		if err != nil {
			// This should not happen.
			return toJSONError(ctx, err, args.BucketName)
		}

		configData, err := json.Marshal(bucketPolicy)
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}

		// Parse validate and save bucket policy.
		if err = globalBucketMetadataSys.Update(args.BucketName, bucketPolicyConfig, configData); err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
	}

	return nil
}

// PresignedGetArgs - presigned-get API args.
type PresignedGetArgs struct {
	// Host header required for signed headers.
	HostName string `json:"host"`

	// Bucket name of the object to be presigned.
	BucketName string `json:"bucket"`

	// Object name to be presigned.
	ObjectName string `json:"object"`

	// Expiry in seconds.
	Expiry int64 `json:"expiry"`
}

// PresignedGetRep - presigned-get URL reply.
type PresignedGetRep struct {
	UIVersion string `json:"uiVersion"`
	// Presigned URL of the object.
	URL string `json:"url"`
}

// PresignedGET - returns presigned-Get url.
func (web *webAPIHandlers) PresignedGet(r *http.Request, args *PresignedGetArgs, reply *PresignedGetRep) error {
	ctx := newWebContext(r, args, "WebPresignedGet")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	var creds auth.Credentials
	if !owner {
		var ok bool
		creds, ok = globalIAMSys.GetUser(claims.AccessKey)
		if !ok {
			return toJSONError(ctx, errInvalidAccessKeyID)
		}
	} else {
		creds = globalActiveCred
	}

	region := globalServerRegion
	if args.BucketName == "" || args.ObjectName == "" {
		return &json2.Error{
			Message: "Bucket and Object are mandatory arguments.",
		}
	}

	// Check if bucket is a reserved bucket name or invalid.
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	// Check if the user indeed has GetObject access,
	// if not we do not need to generate presigned URLs
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.GetObjectAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		ObjectName:      args.ObjectName,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errPresignedNotAllowed)
	}

	reply.UIVersion = Version
	reply.URL = presignedGet(args.HostName, args.BucketName, args.ObjectName, args.Expiry, creds, region)
	return nil
}

func ensureScheme(host string) string {
	if host != "" && !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		return getURLScheme(globalIsTLS) + "://" + host
	}
	return host
}

// Returns presigned url for GET method.
func presignedGet(host, bucket, object string, expiry int64, creds auth.Credentials, region string) string {
	accessKey := creds.AccessKey
	secretKey := creds.SecretKey
	sessionToken := creds.SessionToken

	date := UTCNow()
	dateStr := date.Format(iso8601Format)
	credential := fmt.Sprintf("%s/%s", accessKey, getScope(date, region))

	// Cap presigned URL expiry to 5 minutes for security.
	var expiryStr = "300"
	if expiry > 0 && expiry < 300 {
		expiryStr = strconv.FormatInt(expiry, 10)
	}

	query := url.Values{}
	query.Set(xhttp.AmzAlgorithm, signV4Algorithm)
	query.Set(xhttp.AmzCredential, credential)
	query.Set(xhttp.AmzDate, dateStr)
	query.Set(xhttp.AmzExpires, expiryStr)
	query.Set(xhttp.ContentDisposition, fmt.Sprintf("attachment; filename=\"%s\"", safeDisplayName(object)))
	// Set session token if available.
	if sessionToken != "" {
		query.Set(xhttp.AmzSecurityToken, sessionToken)
	}
	query.Set(xhttp.AmzSignedHeaders, "host")
	queryStr := s3utils.QueryEncode(query)

	path := SlashSeparator + path.Join(bucket, object)

	// "host" is the only header required to be signed for Presigned URLs.
	extractedSignedHeaders := make(http.Header)
	extractedSignedHeaders.Set("host", host)
	canonicalRequest := getCanonicalRequest(extractedSignedHeaders, unsignedPayload, queryStr, path, http.MethodGet)
	stringToSign := getStringToSign(canonicalRequest, date, getScope(date, region))
	signingKey := getSigningKey(secretKey, date, region, serviceS3)
	signature := getSignature(signingKey, stringToSign)

	return ensureScheme(host) + s3utils.EncodePath(path) + "?" + queryStr + "&" + xhttp.AmzSignature + "=" + signature
}

// DiscoveryDocResp - OpenID discovery document reply.
type DiscoveryDocResp struct {
	DiscoveryDoc openid.DiscoveryDoc
	UIVersion    string `json:"uiVersion"`
	ClientID     string `json:"clientId"`
}

// GetDiscoveryDoc - returns parsed value of OpenID discovery document
func (web *webAPIHandlers) GetDiscoveryDoc(r *http.Request, args *WebGenericArgs, reply *DiscoveryDocResp) error {
	if globalOpenIDConfig.DiscoveryDoc.AuthEndpoint != "" {
		reply.DiscoveryDoc = globalOpenIDConfig.DiscoveryDoc
		reply.ClientID = globalOpenIDConfig.ClientID
	}
	reply.UIVersion = Version
	return nil
}

// LoginSTSArgs - login arguments.
type LoginSTSArgs struct {
	Token string `json:"token" form:"token"`
}

var errSTSNotInitialized = errors.New("sts API not initialized, please configure STS support")

// LoginSTS - STS user login handler.
func (web *webAPIHandlers) LoginSTS(r *http.Request, args *LoginSTSArgs, reply *LoginRep) error {
	ctx := newWebContext(r, args, "WebLoginSTS")

	if globalOpenIDValidators == nil {
		return toJSONError(ctx, errSTSNotInitialized)
	}

	v, err := globalOpenIDValidators.Get("jwt")
	if err != nil {
		logger.LogIf(ctx, err)
		return toJSONError(ctx, errSTSNotInitialized)
	}

	m, err := v.Validate(args.Token, "")
	if err != nil {
		return toJSONError(ctx, err)
	}

	// JWT has requested a custom claim with policy value set.
	// This is a Obstor STS API specific value, this value should
	// be set and configured on your identity provider as part of
	// JWT custom claims.
	var policyName string
	policySet, ok := iampolicy.GetPoliciesFromClaims(m, iamPolicyClaimNameOpenID())
	if ok {
		policyName = globalIAMSys.CurrentPolicies(strings.Join(policySet.ToSlice(), ","))
	}
	if policyName == "" && globalPolicyOPA == nil {
		return toJSONError(ctx, fmt.Errorf("%s claim missing from the JWT token, credentials will not be generated", iamPolicyClaimNameOpenID()))
	}
	m[iamPolicyClaimNameOpenID()] = policyName

	secret := globalActiveCred.SecretKey
	cred, err := auth.GetNewCredentialsWithMetadata(m, secret)
	if err != nil {
		return toJSONError(ctx, err)
	}

	// Set the newly generated credentials.
	if err = globalIAMSys.SetTempUser(cred.AccessKey, cred, policyName); err != nil {
		return toJSONError(ctx, err)
	}

	// Notify all other Obstor peers to reload temp users
	for _, nerr := range globalNotificationSys.LoadUser(cred.AccessKey, true) {
		if nerr.Err != nil {
			logger.GetReqInfo(ctx).SetTags("peerAddress", nerr.Host.String())
			logger.LogIf(ctx, nerr.Err)
		}
	}

	reply.Token = cred.SessionToken
	reply.UIVersion = Version
	return nil
}

// toJSONError converts regular errors into more user friendly
// and consumable error message for the browser UI.
func toJSONError(ctx context.Context, err error, params ...string) (jerr *json2.Error) {
	apiErr := toWebAPIError(ctx, err)
	jerr = &json2.Error{
		Message: apiErr.Description,
	}
	switch apiErr.Code {
	// Reserved bucket name provided.
	case "AllAccessDisabled":
		if len(params) > 0 {
			jerr = &json2.Error{
				Message: fmt.Sprintf("All access to this bucket %s has been disabled.", params[0]),
			}
		}
	// Bucket name invalid with custom error message.
	case "InvalidBucketName":
		if len(params) > 0 {
			jerr = &json2.Error{
				Message: fmt.Sprintf("Bucket Name %s is invalid. Lowercase letters, period, hyphen, numerals are the only allowed characters and should be minimum 3 characters in length.", params[0]),
			}
		}
	// Bucket not found custom error message.
	case "NoSuchBucket":
		if len(params) > 0 {
			jerr = &json2.Error{
				Message: fmt.Sprintf("The specified bucket %s does not exist.", params[0]),
			}
		}
	// Object not found custom error message.
	case "NoSuchKey":
		if len(params) > 1 {
			jerr = &json2.Error{
				Message: fmt.Sprintf("The specified key %s does not exist", params[1]),
			}
		}
		// Add more custom error messages here with more context.
	}
	return jerr
}

// toWebAPIError - convert into error into APIError.
func toWebAPIError(ctx context.Context, err error) APIError {
	switch err {
	case errNoAuthToken:
		return APIError{
			Code:           "WebTokenMissing",
			HTTPStatusCode: http.StatusBadRequest,
			Description:    err.Error(),
		}
	case errSTSNotInitialized:
		return APIError(stsErrCodes.ToSTSErr(ErrSTSNotInitialized))
	case errServerNotInitialized:
		return APIError{
			Code:           "XObstorServerNotInitialized",
			HTTPStatusCode: http.StatusServiceUnavailable,
			Description:    err.Error(),
		}
	case errAuthentication, auth.ErrInvalidAccessKeyLength,
		auth.ErrInvalidSecretKeyLength, errInvalidAccessKeyID, errAccessDenied, errLockedObject:
		return APIError{
			Code:           "AccessDenied",
			HTTPStatusCode: http.StatusForbidden,
			Description:    err.Error(),
		}
	case errSizeUnspecified:
		return APIError{
			Code:           "InvalidRequest",
			HTTPStatusCode: http.StatusBadRequest,
			Description:    err.Error(),
		}
	case errChangeCredNotAllowed:
		return APIError{
			Code:           "MethodNotAllowed",
			HTTPStatusCode: http.StatusMethodNotAllowed,
			Description:    err.Error(),
		}
	case errInvalidBucketName:
		return APIError{
			Code:           "InvalidBucketName",
			HTTPStatusCode: http.StatusBadRequest,
			Description:    err.Error(),
		}
	case errInvalidArgument:
		return APIError{
			Code:           "InvalidArgument",
			HTTPStatusCode: http.StatusBadRequest,
			Description:    err.Error(),
		}
	case errEncryptedObject:
		return getAPIError(ErrSSEEncryptedObject)
	case errInvalidEncryptionParameters:
		return getAPIError(ErrInvalidEncryptionParameters)
	case errObjectTampered:
		return getAPIError(ErrObjectTampered)
	case errMethodNotAllowed:
		return getAPIError(ErrMethodNotAllowed)
	}

	// Convert error type to api error code.
	switch err.(type) {
	case StorageFull:
		return getAPIError(ErrStorageFull)
	case BucketQuotaExceeded:
		return getAPIError(ErrAdminBucketQuotaExceeded)
	case BucketNotFound:
		return getAPIError(ErrNoSuchBucket)
	case BucketNotEmpty:
		return getAPIError(ErrBucketNotEmpty)
	case BucketExists:
		return getAPIError(ErrBucketAlreadyOwnedByYou)
	case BucketNameInvalid:
		return getAPIError(ErrInvalidBucketName)
	case hash.BadDigest:
		return getAPIError(ErrBadDigest)
	case IncompleteBody:
		return getAPIError(ErrIncompleteBody)
	case ObjectExistsAsDirectory:
		return getAPIError(ErrObjectExistsAsDirectory)
	case ObjectNotFound:
		return getAPIError(ErrNoSuchKey)
	case ObjectNameInvalid:
		return getAPIError(ErrNoSuchKey)
	case InsufficientWriteQuorum:
		return getAPIError(ErrWriteQuorum)
	case InsufficientReadQuorum:
		return getAPIError(ErrReadQuorum)
	case NotImplemented:
		return APIError{
			Code:           "NotImplemented",
			HTTPStatusCode: http.StatusBadRequest,
			Description:    "Functionality not implemented",
		}
	}

	// Log unexpected and unhandled errors.
	logger.LogIf(ctx, err)
	return toAPIError(ctx, err)
}

// writeWebErrorResponse - set HTTP status code and write error description to the body.
func writeWebErrorResponse(w http.ResponseWriter, err error) {
	reqInfo := &logger.ReqInfo{
		DeploymentID: globalDeploymentID,
	}
	ctx := logger.SetReqInfo(GlobalContext, reqInfo)
	apiErr := toWebAPIError(ctx, err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(apiErr.HTTPStatusCode)
	w.Write([]byte(apiErr.Description))
}

// GetObjectLocationsArgs - get object locations args.
type GetObjectLocationsArgs struct {
	BucketName string `json:"bucketName"`
	Prefix     string `json:"prefix"`
}

// ObjectLocation - a single object's placement info.
type ObjectLocation struct {
	Name      string   `json:"name"`
	Endpoints []string `json:"endpoints"`
}

// GetObjectLocationsRep - reply with per-object endpoint placements.
type GetObjectLocationsRep struct {
	Objects   []ObjectLocation `json:"objects"`
	UIVersion string           `json:"uiVersion"`
}

// GetObjectLocations returns which endpoints (nodes) hold each object in a prefix.
func (web *webAPIHandlers) GetObjectLocations(r *http.Request, args *GetObjectLocationsArgs, reply *GetObjectLocationsRep) error {
	ctx := newWebContext(r, args, "WebGetObjectLocations")
	objectAPI := web.ObjectAPI()
	reply.UIVersion = Version

	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.ListBucketAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied, args.BucketName)
	}

	reply.Objects = []ObjectLocation{}

	// List objects in the prefix
	lo, err := objectAPI.ListObjects(ctx, args.BucketName, args.Prefix, "", "/", 1000)
	if err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}

	for _, obj := range lo.Objects {
		loc := ObjectLocation{Name: obj.Name}

		// For erasure-coded deployments, read which disks have the object
		if z, ok := objectAPI.(*erasureServerPools); ok {
			for _, pool := range z.serverPools {
				for _, set := range pool.sets {
					fi, metaArr, onlineDisks, err := set.getObjectFileInfo(ctx, args.BucketName, obj.Name, ObjectOptions{}, false)
					if err != nil || fi.Name == "" {
						continue
					}
					for i, disk := range onlineDisks {
						if disk == nil {
							continue
						}
						if metaArr[i].IsValid() {
							ep := disk.Endpoint().String()
							if ep == "" {
								ep = disk.String()
							}
							loc.Endpoints = append(loc.Endpoints, ep)
						}
					}
				}
			}
		}

		// Single-node fallback
		if len(loc.Endpoints) == 0 {
			for _, z := range globalEndpoints {
				for _, ep := range z.Endpoints {
					loc.Endpoints = append(loc.Endpoints, ep.String())
				}
			}
		}

		reply.Objects = append(reply.Objects, loc)
	}

	return nil
}

// Get object checksums args.
type GetObjectChecksumsArgs struct {
	BucketName string `json:"bucketName"`
	ObjectName string `json:"objectName"`
}

// Reply with computed checksums.
type GetObjectChecksumsRep struct {
	MD5       string `json:"md5"`
	SHA1      string `json:"sha1"`
	SHA256    string `json:"sha256"`
	SHA512    string `json:"sha512"`
	UIVersion string `json:"uiVersion"`
}

// Read an object and get MD5, SHA-1, SHA-256, and SHA-512 hashes.
func (web *webAPIHandlers) GetObjectChecksums(r *http.Request, args *GetObjectChecksumsArgs, reply *GetObjectChecksumsRep) error {
	ctx := newWebContext(r, args, "WebGetObjectChecksums")
	objectAPI := web.ObjectAPI()
	reply.UIVersion = Version

	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}

	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.GetObjectAction,
		BucketName:      args.BucketName,
		ObjectName:      args.ObjectName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied, args.BucketName, args.ObjectName)
	}

	gr, err := objectAPI.GetObjectNInfo(ctx, args.BucketName, args.ObjectName, nil, r.Header, readLock, ObjectOptions{})
	if err != nil {
		return toJSONError(ctx, err, args.BucketName, args.ObjectName)
	}
	defer func() { _ = gr.Close() }()

	hMD5 := md5.New()
	hSHA1 := sha1.New()
	hSHA256 := sha256.New()
	hSHA512 := sha512.New()
	w := io.MultiWriter(hMD5, hSHA1, hSHA256, hSHA512)

	if _, err := io.Copy(w, gr); err != nil {
		return toJSONError(ctx, err, args.BucketName, args.ObjectName)
	}

	reply.MD5 = hex.EncodeToString(hMD5.Sum(nil))
	reply.SHA1 = hex.EncodeToString(hSHA1.Sum(nil))
	reply.SHA256 = hex.EncodeToString(hSHA256.Sum(nil))
	reply.SHA512 = hex.EncodeToString(hSHA512.Sum(nil))
	return nil
}

// PresignedPutArgs - presigned-put API args.
type PresignedPutArgs struct {
	// Host header value.
	HostName string `json:"host"`
	// Bucket name of the object to be uploaded.
	BucketName string `json:"bucket"`
	// Prefix for the object path.
	Prefix string `json:"prefix"`
	// Object name to be uploaded.
	ObjectName string `json:"object"`
}

// PresignedPutRep - presigned-put URL reply.
type PresignedPutRep struct {
	UIVersion string `json:"uiVersion"`
	// Presigned URL for PUT.
	URL string `json:"url"`
}

// PresignedPut returns a presigned URL for uploading an object.
func (web *webAPIHandlers) PresignedPut(r *http.Request, args *PresignedPutArgs, reply *PresignedPutRep) error {
	ctx := newWebContext(r, args, "WebPresignedPut")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	var creds auth.Credentials
	if !owner {
		var ok bool
		creds, ok = globalIAMSys.GetUser(claims.AccessKey)
		if !ok {
			return toJSONError(ctx, errInvalidAccessKeyID)
		}
	} else {
		creds = globalActiveCred
	}

	objectName := args.Prefix + args.ObjectName
	if args.BucketName == "" || objectName == "" {
		return &json2.Error{
			Message: "Bucket and Object are mandatory arguments.",
		}
	}

	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	// Check if the user has PutObject access.
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.PutObjectAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		ObjectName:      objectName,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errPresignedNotAllowed)
	}

	region := globalServerRegion
	reply.UIVersion = Version
	reply.URL = presignedPut(args.HostName, args.BucketName, objectName, creds, region)
	return nil
}

func presignedPut(host, bucket, object string, creds auth.Credentials, region string) string {
	date := UTCNow()
	dateStr := date.Format(iso8601Format)
	credential := fmt.Sprintf("%s/%s", creds.AccessKey, getScope(date, region))

	query := url.Values{}
	query.Set(xhttp.AmzAlgorithm, signV4Algorithm)
	query.Set(xhttp.AmzCredential, credential)
	query.Set(xhttp.AmzDate, dateStr)
	query.Set(xhttp.AmzExpires, "300")
	if creds.SessionToken != "" {
		query.Set(xhttp.AmzSecurityToken, creds.SessionToken)
	}
	query.Set(xhttp.AmzSignedHeaders, "host")
	queryStr := s3utils.QueryEncode(query)

	path := SlashSeparator + path.Join(bucket, object)

	extractedSignedHeaders := make(http.Header)
	extractedSignedHeaders.Set("host", host)
	canonicalRequest := getCanonicalRequest(extractedSignedHeaders, unsignedPayload, queryStr, path, http.MethodPut)
	stringToSign := getStringToSign(canonicalRequest, date, getScope(date, region))
	signingKey := getSigningKey(creds.SecretKey, date, region, serviceS3)
	signature := getSignature(signingKey, stringToSign)

	return ensureScheme(host) + s3utils.EncodePath(path) + "?" + queryStr + "&" + xhttp.AmzSignature + "=" + signature
}

// IAM admin helpers
func webAdminAuth(r *http.Request, action iampolicy.AdminAction) (context.Context, error) {
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return nil, authErr
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.Action(action),
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return nil, errAccessDenied
	}
	return r.Context(), nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Summary of a canned policy.
type WebPolicySummary struct {
	Name   string `json:"name"`
	Policy string `json:"policy"`
}

type ListCannedPoliciesRep struct {
	UIVersion string             `json:"uiVersion"`
	Policies  []WebPolicySummary `json:"policies"`
}

// List filtered policies
func (web *webAPIHandlers) ListCannedPolicies(r *http.Request, args *WebGenericArgs, reply *ListCannedPoliciesRep) error {
	ctx := newWebContext(r, args, "WebListCannedPolicies")
	if _, err := webAdminAuth(r, iampolicy.ListUserPoliciesAdminAction); err != nil {
		return toJSONError(ctx, err)
	}

	pols, err := globalIAMSys.ListPolicies()
	if err != nil {
		return toJSONError(ctx, err)
	}

	reply.Policies = []WebPolicySummary{}
	for name, p := range pols {
		buf, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			continue
		}
		reply.Policies = append(reply.Policies, WebPolicySummary{
			Name:   name,
			Policy: string(buf),
		})
	}
	reply.UIVersion = Version
	return nil
}

// Policy args
type GetCannedPolicyArgs struct {
	Name string `json:"name"`
}

// Policy reply
type GetCannedPolicyRep struct {
	UIVersion string `json:"uiVersion"`
	Name      string `json:"name"`
	Policy    string `json:"policy"`
}

// Return requested policy json
func (web *webAPIHandlers) GetCannedPolicy(r *http.Request, args *GetCannedPolicyArgs, reply *GetCannedPolicyRep) error {
	ctx := newWebContext(r, args, "WebGetCannedPolicy")
	if _, err := webAdminAuth(r, iampolicy.GetPolicyAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	p, err := globalIAMSys.InfoPolicy(args.Name)
	if err != nil {
		return toJSONError(ctx, err)
	}
	buf, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return toJSONError(ctx, err)
	}
	reply.Name = args.Name
	reply.Policy = string(buf)
	reply.UIVersion = Version
	return nil
}

// Policy args
type SetCannedPolicyArgs struct {
	Name   string `json:"name"`
	Policy string `json:"policy"`
}

// Create/update json IAM policy
func (web *webAPIHandlers) SetCannedPolicy(r *http.Request, args *SetCannedPolicyArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetCannedPolicy")
	if _, err := webAdminAuth(r, iampolicy.CreatePolicyAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	if strings.TrimSpace(args.Name) == "" {
		return toJSONError(ctx, errInvalidArgument)
	}
	p, err := iampolicy.ParseConfig(bytes.NewReader([]byte(args.Policy)))
	if err != nil {
		return toJSONError(ctx, err)
	}
	if err := globalIAMSys.SetPolicy(args.Name, *p); err != nil {
		return toJSONError(ctx, err)
	}
	reply.UIVersion = Version
	return nil
}

// Delete args
type DeleteCannedPolicyArgs struct {
	Name string `json:"name"`
}

func (web *webAPIHandlers) DeleteCannedPolicy(r *http.Request, args *DeleteCannedPolicyArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebDeleteCannedPolicy")
	if _, err := webAdminAuth(r, iampolicy.DeletePolicyAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	if err := globalIAMSys.DeletePolicy(args.Name); err != nil {
		return toJSONError(ctx, err)
	}
	reply.UIVersion = Version
	return nil
}

// UI user summary
type WebUser struct {
	AccessKey string   `json:"accessKey"`
	Status    string   `json:"status"`
	Policies  []string `json:"policies"`
}

// List reply
type ListUsersRep struct {
	UIVersion string    `json:"uiVersion"`
	Users     []WebUser `json:"users"`
}

// List all regular users
func (web *webAPIHandlers) ListIAMUsers(r *http.Request, args *WebGenericArgs, reply *ListUsersRep) error {
	ctx := newWebContext(r, args, "WebListIAMUsers")
	if _, err := webAdminAuth(r, iampolicy.ListUsersAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	users, err := globalIAMSys.ListUsers()
	if err != nil {
		return toJSONError(ctx, err)
	}

	reply.Users = []WebUser{}
	for ak, info := range users {
		policies := splitCSV(info.PolicyName)
		if policies == nil {
			policies = []string{}
		}
		reply.Users = append(reply.Users, WebUser{
			AccessKey: ak,
			Status:    string(info.Status),
			Policies:  policies,
		})
	}
	reply.UIVersion = Version
	return nil
}

// If SecretKey is empty, generate a random one
type AddIAMUserArgs struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Policy    string `json:"policy"`
}

// Return the created credentials
type AddIAMUserRep struct {
	UIVersion string `json:"uiVersion"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// Generate creds if either field is blank
func (web *webAPIHandlers) AddIAMUser(r *http.Request, args *AddIAMUserArgs, reply *AddIAMUserRep) error {
	ctx := newWebContext(r, args, "WebAddIAMUser")
	if _, err := webAdminAuth(r, iampolicy.CreateUserAdminAction); err != nil {
		return toJSONError(ctx, err)
	}

	ak := strings.TrimSpace(args.AccessKey)
	sk := strings.TrimSpace(args.SecretKey)

	if ak == "" || sk == "" {
		cred, err := auth.GetNewCredentials()
		if err != nil {
			return toJSONError(ctx, err)
		}
		if ak == "" {
			ak = cred.AccessKey
		}
		if sk == "" {
			sk = cred.SecretKey
		}
	}

	if isRootCredAccessKey(ak) {
		return toJSONError(ctx, errAccessDenied)
	}

	if err := globalIAMSys.CreateUser(ak, madmin.UserInfo{
		SecretKey: sk,
		Status:    madmin.AccountEnabled,
	}); err != nil {
		return toJSONError(ctx, err)
	}

	if strings.TrimSpace(args.Policy) != "" {
		if _, err := webAdminAuth(r, iampolicy.AttachPolicyAdminAction); err != nil {
			return toJSONError(ctx, err)
		}
		if err := globalIAMSys.PolicyDBSet(ak, args.Policy, false); err != nil {
			return toJSONError(ctx, err)
		}
	}

	reply.AccessKey = ak
	reply.SecretKey = sk
	reply.UIVersion = Version
	return nil
}

// Remove IAM user args
type RemoveIAMUserArgs struct {
	AccessKey string `json:"accessKey"`
}

// Delete a user.
func (web *webAPIHandlers) RemoveIAMUser(r *http.Request, args *RemoveIAMUserArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebRemoveIAMUser")
	if _, err := webAdminAuth(r, iampolicy.DeleteUserAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	if err := globalIAMSys.DeleteUser(args.AccessKey); err != nil {
		return toJSONError(ctx, err)
	}
	reply.UIVersion = Version
	return nil
}

// Set IAM user args
type SetIAMUserStatusArgs struct {
	AccessKey string `json:"accessKey"`
	Enabled   bool   `json:"enabled"`
}

// Enable or disable a user
func (web *webAPIHandlers) SetIAMUserStatus(r *http.Request, args *SetIAMUserStatusArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetIAMUserStatus")
	if _, err := webAdminAuth(r, iampolicy.EnableUserAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	status := madmin.AccountDisabled
	if args.Enabled {
		status = madmin.AccountEnabled
	}
	if err := globalIAMSys.SetUserStatus(args.AccessKey, status); err != nil {
		return toJSONError(ctx, err)
	}
	reply.UIVersion = Version
	return nil
}

// Replace policies attached to a user
type SetIAMUserPolicyArgs struct {
	AccessKey string `json:"accessKey"`
	Policies  string `json:"policies"`
}

// Attach policies on a user
func (web *webAPIHandlers) SetIAMUserPolicy(r *http.Request, args *SetIAMUserPolicyArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetIAMUserPolicy")
	if _, err := webAdminAuth(r, iampolicy.AttachPolicyAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	if err := globalIAMSys.PolicyDBSet(args.AccessKey, args.Policies, false); err != nil {
		return toJSONError(ctx, err)
	}
	reply.UIVersion = Version
	return nil
}

type GetBucketPolicyDocArgs struct {
	BucketName string `json:"bucketName"`
}

type GetBucketPolicyDocRep struct {
	UIVersion string `json:"uiVersion"`
	Policy    string `json:"policy"` // raw JSON, empty string if none
}

// Return anonymous-access policy status
func (web *webAPIHandlers) GetBucketPolicyDoc(r *http.Request, args *GetBucketPolicyDocArgs, reply *GetBucketPolicyDocRep) error {
	ctx := newWebContext(r, args, "WebGetBucketPolicyDoc")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.GetBucketPolicyAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	bp, err := globalPolicySys.Get(args.BucketName)
	if err != nil {
		if _, ok := err.(BucketPolicyNotFound); !ok {
			return toJSONError(ctx, err, args.BucketName)
		}
		reply.Policy = ""
		reply.UIVersion = Version
		return nil
	}
	buf, err := json.MarshalIndent(bp, "", "  ")
	if err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	reply.Policy = string(buf)
	reply.UIVersion = Version
	return nil
}

// Pass empty Policy to clear.
type SetBucketPolicyDocArgs struct {
	BucketName string `json:"bucketName"`
	Policy     string `json:"policy"`
}

// Set bucket's anonymous-access policy from raw JSON.
func (web *webAPIHandlers) SetBucketPolicyDoc(r *http.Request, args *SetBucketPolicyDocArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketPolicyDoc")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.PutBucketPolicyAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	reply.UIVersion = Version

	trimmed := strings.TrimSpace(args.Policy)
	if trimmed == "" || trimmed == "{}" {
		return globalBucketMetadataSys.Update(args.BucketName, bucketPolicyConfig, nil)
	}

	bp, err := policy.ParseConfig(strings.NewReader(trimmed), args.BucketName)
	if err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	configData, err := json.Marshal(bp)
	if err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	return globalBucketMetadataSys.Update(args.BucketName, bucketPolicyConfig, configData)
}

// Per-bucket feature toggles
const (
	obstorTagS3Enabled      = "__obstor_s3_enabled"
	obstorTagSFTPEnabled    = "__obstor_sftp_enabled"
	obstorReservedTagPrefix = "__obstor_"
)

// Check if the feature toggle is enabled
func bucketToggleOn(bucket, tagKey string, defaultOn bool) bool {
	if bucket == "" {
		return defaultOn
	}
	cfg, err := globalBucketMetadataSys.GetTaggingConfig(bucket)
	if err != nil {
		return defaultOn
	}
	m := cfg.ToMap()
	v, ok := m[tagKey]
	if !ok {
		return defaultOn
	}
	return !strings.EqualFold(v, "false")
}

// Enable S3 by feature by default
func IsBucketS3Enabled(bucket string) bool {
	return bucketToggleOn(bucket, obstorTagS3Enabled, true)
}

// Disable SFTP by feature by default
func IsBucketSFTPEnabled(bucket string) bool {
	return bucketToggleOn(bucket, obstorTagSFTPEnabled, false)
}

// Update __obstor_* tags while preserving user-set tags.
func writeBucketToggles(bucket string, s3Enabled, sftpEnabled bool) error {
	m := map[string]string{}
	if cfg, err := globalBucketMetadataSys.GetTaggingConfig(bucket); err == nil {
		m = cfg.ToMap()
	}
	m[obstorTagS3Enabled] = boolToTagValue(s3Enabled)
	m[obstorTagSFTPEnabled] = boolToTagValue(sftpEnabled)

	t, err := tags.NewTags(m, false)
	if err != nil {
		return err
	}
	configData, err := xml.Marshal(t)
	if err != nil {
		return err
	}
	return globalBucketMetadataSys.Update(bucket, bucketTaggingConfig, configData)
}

func boolToTagValue(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type GetBucketTogglesArgs struct {
	BucketName string `json:"bucketName"`
}

type GetBucketTogglesRep struct {
	UIVersion   string `json:"uiVersion"`
	S3Enabled   bool   `json:"s3Enabled"`
	SFTPEnabled bool   `json:"sftpEnabled"`
}

// Feature toggles for the given bucket
func (web *webAPIHandlers) GetBucketToggles(r *http.Request, args *GetBucketTogglesArgs, reply *GetBucketTogglesRep) error {
	ctx := newWebContext(r, args, "WebGetBucketToggles")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.GetBucketTaggingAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}
	reply.S3Enabled = IsBucketS3Enabled(args.BucketName)
	reply.SFTPEnabled = IsBucketSFTPEnabled(args.BucketName)
	reply.UIVersion = Version
	return nil
}

type SetBucketTogglesArgs struct {
	BucketName  string `json:"bucketName"`
	S3Enabled   bool   `json:"s3Enabled"`
	SFTPEnabled bool   `json:"sftpEnabled"`
}

// Per-bucket feature toggles as bucket tags.
func (web *webAPIHandlers) SetBucketToggles(r *http.Request, args *SetBucketTogglesArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketToggles")
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     claims.AccessKey,
		Action:          iampolicy.PutBucketTaggingAction,
		BucketName:      args.BucketName,
		ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
		IsOwner:         owner,
		Claims:          claims.Map(),
	}) {
		return toJSONError(ctx, errAccessDenied)
	}
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}
	if err := writeBucketToggles(args.BucketName, args.S3Enabled, args.SFTPEnabled); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	reply.UIVersion = Version
	return nil
}
