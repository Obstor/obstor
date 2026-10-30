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
	"errors"
	"io"

	bucketsse "github.com/obstor/obstor/pkg/bucket/encryption"
)

// BucketSSEConfigSys - in-memory cache of bucket encryption config
type BucketSSEConfigSys struct{}

// NewBucketSSEConfigSys - Creates an empty in-memory bucket encryption configuration cache
func NewBucketSSEConfigSys() *BucketSSEConfigSys {
	return &BucketSSEConfigSys{}
}

// Get - gets bucket encryption config for the given bucket.
func (sys *BucketSSEConfigSys) Get(bucket string) (*bucketsse.BucketSSEConfig, error) {
	if globalIsBackend {
		objAPI := newObjectLayerFn()
		if objAPI == nil {
			return nil, errServerNotInitialized
		}

		return nil, BucketSSEConfigNotFound{Bucket: bucket}
	}

	return globalBucketMetadataSys.GetSSEConfig(bucket)
}

// validateBucketSSEConfig parses bucket encryption configuration and validates if it is supported by Obstor.
func validateBucketSSEConfig(r io.Reader) (*bucketsse.BucketSSEConfig, error) {
	encConfig, err := bucketsse.ParseBucketSSEConfig(r)
	if err != nil {
		return nil, err
	}

	if len(encConfig.Rules) == 1 && encConfig.Rules[0].DefaultEncryptionAction.Algorithm == bucketsse.AES256 {
		return encConfig, nil
	}

	return nil, errors.New("unsupported bucket encryption configuration")
}

// SSE-S3 encryption accepted by validateBucketSSEConfig
var bucketSSES3Config = []byte(`<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule></ServerSideEncryptionConfiguration>`)
