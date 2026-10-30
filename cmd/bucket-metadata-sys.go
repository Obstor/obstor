/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
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
	"errors"
	"fmt"
	"sync"

	"github.com/obstor/obstor-go/v7/pkg/tags"
	"github.com/obstor/obstor/cmd/crypto"
	"github.com/obstor/obstor/cmd/logger"
	bucketsse "github.com/obstor/obstor/pkg/bucket/encryption"
	"github.com/obstor/obstor/pkg/bucket/lifecycle"
	objectlock "github.com/obstor/obstor/pkg/bucket/object/lock"
	"github.com/obstor/obstor/pkg/bucket/policy"
	"github.com/obstor/obstor/pkg/bucket/replication"
	"github.com/obstor/obstor/pkg/bucket/versioning"
	"github.com/obstor/obstor/pkg/event"
	"github.com/obstor/obstor/pkg/madmin"
	"github.com/obstor/obstor/pkg/sync/errgroup"
)

// BucketMetadataSys captures all bucket metadata for a given cluster.
type BucketMetadataSys struct {
	sync.RWMutex
	metadataMap map[string]BucketMetadata
}

// Remove bucket metadata from memory.
func (sys *BucketMetadataSys) Remove(bucket string) {
	if globalIsBackend {
		return
	}
	sys.Lock()
	delete(sys.metadataMap, bucket)
	globalBucketMonitor.DeleteBucket(bucket)
	sys.Unlock()
}

// Set - sets a new metadata in-memory.
// Only a shallow copy is saved and fields with references
// cannot be modified without causing a race condition,
// so they should be replaced atomically and not appended to, etc.
// Data is not persisted to disk.
func (sys *BucketMetadataSys) Set(bucket string, meta BucketMetadata) {
	if globalIsBackend {
		return
	}

	if bucket != obstorMetaBucket {
		sys.Lock()
		sys.metadataMap[bucket] = meta
		sys.Unlock()
	}
}

// Update update bucket metadata for the specified config file.
// The configData data should not be modified after being sent here.
func (sys *BucketMetadataSys) Update(bucket string, configFile string, configData []byte) error {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return errServerNotInitialized
	}

	if globalIsBackend {
		switch configFile {
		case bucketPolicyConfig:
			if configData == nil {
				return objAPI.DeleteBucketPolicy(GlobalContext, bucket)
			}
			config, err := policy.ParseConfig(bytes.NewReader(configData), bucket)
			if err != nil {
				return err
			}
			return objAPI.SetBucketPolicy(GlobalContext, bucket, config)
		case bucketSSEConfig, bucketLifecycleConfig, bucketTaggingConfig, bucketNotificationConfig:
			if globalBackendName != NASBackend {
				return NotImplemented{}
			}
		default:
			return NotImplemented{}
		}
	}

	if bucket == obstorMetaBucket {
		return errInvalidArgument
	}

	lock := bucketMetadataLock(objAPI, bucket)
	ctx, err := lock.GetLock(GlobalContext, globalOperationTimeout)
	if err != nil {
		return err
	}
	// Release lock before peers reload with same lock
	locked := true
	defer func() {
		if locked {
			lock.Unlock()
		}
	}()
	meta, err := loadBucketMetadataLocked(ctx, objAPI, bucket)
	if err != nil {
		return err
	}

	switch configFile {
	case bucketPolicyConfig:
		meta.PolicyConfigJSON = configData
	case bucketNotificationConfig:
		meta.NotificationConfigXML = configData
	case bucketLifecycleConfig:
		meta.LifecycleConfigXML = configData
	case bucketSSEConfig:
		meta.EncryptionConfigXML = configData
	case bucketTaggingConfig:
		meta.TaggingConfigXML = configData
	case bucketProtocolConfigFile:
		meta.ProtocolConfigJSON = configData
	case bucketQuotaConfigFile:
		meta.QuotaConfigJSON = configData
	case objectLockConfig:
		if !globalIsErasure && !globalIsDistErasure {
			return NotImplemented{}
		}
		meta.ObjectLockConfigXML = configData
	case bucketVersioningConfig:
		if !globalIsErasure && !globalIsDistErasure {
			return NotImplemented{}
		}
		meta.VersioningConfigXML = configData
	case bucketReplicationConfig:
		if !globalIsErasure && !globalIsDistErasure {
			return NotImplemented{}
		}
		meta.ReplicationConfigXML = configData
	case bucketTargetsFile:
		meta.BucketTargetsConfigJSON, meta.BucketTargetsConfigMetaJSON, err = encryptBucketMetadata(meta.Name, configData, crypto.Context{
			bucket:            meta.Name,
			bucketTargetsFile: bucketTargetsFile,
		})
		if err != nil {
			return fmt.Errorf("error encrypting bucket target metadata %w", err)
		}
	default:
		return fmt.Errorf("unknown bucket %s metadata update requested %s", bucket, configFile)
	}

	if err := meta.Save(ctx, objAPI); err != nil {
		return err
	}

	sys.Set(bucket, meta)
	lock.Unlock()
	locked = false
	if !globalIsBackend {
		globalNotificationSys.LoadBucketMetadata(GlobalContext, bucket)
	}

	return nil
}

// Get metadata for a bucket.
// If no metadata exists errConfigNotFound is returned and a new metadata is returned.
// Only a shallow copy is returned, so referenced data should not be modified,
// but can be replaced atomically.
//
// This function should only be used with
// - GetBucketInfo
// - ListBuckets
// For all other bucket specific metadata, use the relevant
// calls implemented specifically for each of those features.
func (sys *BucketMetadataSys) Get(bucket string) (BucketMetadata, error) {
	if globalIsBackend || bucket == obstorMetaBucket {
		return newBucketMetadata(bucket), errConfigNotFound
	}

	sys.RLock()
	defer sys.RUnlock()

	meta, ok := sys.metadataMap[bucket]
	if !ok {
		return newBucketMetadata(bucket), errConfigNotFound
	}

	return meta, nil
}

// GetVersioningConfig returns configured versioning config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetVersioningConfig(bucket string) (*versioning.Versioning, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	return meta.versioningConfig, nil
}

// GetTaggingConfig returns configured tagging config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetTaggingConfig(bucket string) (*tags.Tags, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketTaggingNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.taggingConfig == nil {
		return nil, BucketTaggingNotFound{Bucket: bucket}
	}
	return meta.taggingConfig, nil
}

// GetObjectLockConfig returns configured object lock config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetObjectLockConfig(bucket string) (*objectlock.Config, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketObjectLockConfigNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.objectLockConfig == nil {
		return nil, BucketObjectLockConfigNotFound{Bucket: bucket}
	}
	return meta.objectLockConfig, nil
}

// GetLifecycleConfig returns configured lifecycle config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetLifecycleConfig(bucket string) (*lifecycle.Lifecycle, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketLifecycleNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.lifecycleConfig == nil {
		return nil, BucketLifecycleNotFound{Bucket: bucket}
	}
	return meta.lifecycleConfig, nil
}

// GetNotificationConfig returns configured notification config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetNotificationConfig(bucket string) (*event.Config, error) {
	if globalIsBackend && globalBackendName == NASBackend {
		// Only needed in case of NAS backend.
		objAPI := newObjectLayerFn()
		if objAPI == nil {
			return nil, errServerNotInitialized
		}
		meta, err := loadBucketMetadata(GlobalContext, objAPI, bucket)
		if err != nil {
			return nil, err
		}
		return meta.notificationConfig, nil
	}

	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	return meta.notificationConfig, nil
}

// GetSSEConfig returns configured SSE config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetSSEConfig(bucket string) (*bucketsse.BucketSSEConfig, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketSSEConfigNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.sseConfig == nil {
		return nil, BucketSSEConfigNotFound{Bucket: bucket}
	}
	return meta.sseConfig, nil
}

// GetPolicyConfig returns configured bucket policy
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetPolicyConfig(bucket string) (*policy.Policy, error) {
	if globalIsBackend {
		objAPI := newObjectLayerFn()
		if objAPI == nil {
			return nil, errServerNotInitialized
		}
		return objAPI.GetBucketPolicy(GlobalContext, bucket)
	}

	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketPolicyNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.policyConfig == nil {
		return nil, BucketPolicyNotFound{Bucket: bucket}
	}
	return meta.policyConfig, nil
}

// GetQuotaConfig returns configured bucket quota
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetQuotaConfig(bucket string) (*madmin.BucketQuota, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	return meta.quotaConfig, nil
}

// GetReplicationConfig returns configured bucket replication config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetReplicationConfig(ctx context.Context, bucket string) (*replication.Config, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketReplicationConfigNotFound{Bucket: bucket}
		}
		return nil, err
	}

	if meta.replicationConfig == nil {
		return nil, BucketReplicationConfigNotFound{Bucket: bucket}
	}
	return meta.replicationConfig, nil
}

// GetBucketTargetsConfig returns configured bucket targets for this bucket
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetBucketTargetsConfig(bucket string) (*madmin.BucketTargets, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	if meta.bucketTargetConfig == nil {
		return nil, BucketRemoteTargetNotFound{Bucket: bucket}
	}
	return meta.bucketTargetConfig, nil
}

// GetBucketTarget returns the target for the bucket and arn.
func (sys *BucketMetadataSys) GetBucketTarget(bucket string, arn string) (madmin.BucketTarget, error) {
	targets, err := sys.GetBucketTargetsConfig(bucket)
	if err != nil {
		return madmin.BucketTarget{}, err
	}
	for _, t := range targets.Targets {
		if t.Arn == arn {
			return t, nil
		}
	}
	return madmin.BucketTarget{}, errConfigNotFound
}

// GetConfig returns a specific configuration from the bucket metadata.
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetConfig(bucket string) (BucketMetadata, error) {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return newBucketMetadata(bucket), errServerNotInitialized
	}

	if globalIsBackend {
		return newBucketMetadata(bucket), NotImplemented{}
	}

	if bucket == obstorMetaBucket {
		return newBucketMetadata(bucket), errInvalidArgument
	}

	sys.RLock()
	meta, ok := sys.metadataMap[bucket]
	sys.RUnlock()
	if ok {
		return meta, nil
	}
	return sys.Reload(GlobalContext, objAPI, bucket)
}

// Delayed reads cant replace newer cache
func (sys *BucketMetadataSys) Reload(ctx context.Context, objAPI ObjectLayer, bucket string) (BucketMetadata, error) {
	lock := bucketMetadataLock(objAPI, bucket)
	ctx, err := lock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return newBucketMetadata(bucket), err
	}
	defer lock.Unlock()
	meta, err := loadBucketMetadataLocked(ctx, objAPI, bucket)
	if err != nil {
		return meta, err
	}
	sys.Set(bucket, meta)
	return meta, nil
}

// Init - initializes bucket metadata system for all buckets.
func (sys *BucketMetadataSys) Init(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) error {
	if objAPI == nil {
		return errServerNotInitialized
	}

	// In backend mode, we don't need to load the policies
	// from the backend.
	if globalIsBackend {
		return nil
	}

	// Load bucket metadata sys in background
	go sys.load(ctx, buckets, objAPI)
	return nil
}

// Concurrently load bucket metadata to speed up loading bucket metadata.
func (sys *BucketMetadataSys) concurrentLoad(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) {
	g := errgroup.WithNErrs(len(buckets))
	for index := range buckets {
		index := index
		g.Go(func() error {
			_, _ = objAPI.HealBucket(ctx, buckets[index].Name, madmin.HealOpts{
				// Ensure heal opts for bucket metadata be deep healed all the time.
				ScanMode: madmin.HealDeepScan,
			})
			_, err := sys.Reload(ctx, objAPI, buckets[index].Name)
			if err != nil {
				return err
			}
			return nil
		}, index)
	}
	for _, err := range g.Wait() {
		if err != nil {
			logger.LogIf(ctx, err)
		}
	}
}

// Loads bucket metadata for all buckets into BucketMetadataSys.
func (sys *BucketMetadataSys) load(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) {
	count := 100 // load 100 bucket metadata at a time.
	for {
		if len(buckets) < count {
			sys.concurrentLoad(ctx, buckets, objAPI)
			return
		}
		sys.concurrentLoad(ctx, buckets[:count], objAPI)
		buckets = buckets[count:]
	}
}

// Reset the state of the BucketMetadataSys.
func (sys *BucketMetadataSys) Reset() {
	sys.Lock()
	for k := range sys.metadataMap {
		delete(sys.metadataMap, k)
	}
	sys.Unlock()
}

// NewBucketMetadataSys - creates new policy system.
func NewBucketMetadataSys() *BucketMetadataSys {
	return &BucketMetadataSys{
		metadataMap: make(map[string]BucketMetadata),
	}
}
