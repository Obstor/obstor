package cmd

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/obstor/obstor-go/v7/pkg/tags"
)

// Reserve old protocol tag names, store flags separately
const (
	obstorTagS3Enabled      = "__obstor_s3_enabled"
	obstorTagSFTPEnabled    = "__obstor_sftp_enabled"
	obstorReservedTagPrefix = "__obstor_"

	// Protocol settings use no public tag slots
	maxBucketUserTags = 50
)

var (
	errReservedBucketTag     = errors.New("tag keys starting with " + obstorReservedTagPrefix + " are reserved")
	errTooManyBucketUserTags = fmt.Errorf("a bucket can have at most %d tags", maxBucketUserTags)
)

// Protocol config stored separately from S3 tags
type bucketProtocolConfig struct {
	S3Enabled   bool `json:"s3Enabled"`
	SFTPEnabled bool `json:"sftpEnabled"`
}

func defaultBucketProtocols() bucketProtocolConfig {
	return bucketProtocolConfig{S3Enabled: true}
}

// Read protocol config or old reserved tags, dedicated metadata wins
func (b *BucketMetadata) protocolSettings() (bucketProtocolConfig, error) {
	cfg := defaultBucketProtocols()
	if len(b.ProtocolConfigJSON) != 0 {
		err := json.Unmarshal(b.ProtocolConfigJSON, &cfg)
		return cfg, err
	}
	if b.taggingConfig != nil {
		legacy := b.taggingConfig.ToMap()
		if v, ok := legacy[obstorTagS3Enabled]; ok {
			cfg.S3Enabled = !strings.EqualFold(v, "false")
		}
		if v, ok := legacy[obstorTagSFTPEnabled]; ok {
			cfg.SFTPEnabled = !strings.EqualFold(v, "false")
		}
	}
	return cfg, nil
}

func bucketProtocols(bucket string) bucketProtocolConfig {
	cfg := defaultBucketProtocols()
	if bucket == "" {
		return cfg
	}
	meta, err := globalBucketMetadataSys.GetConfig(bucket)
	if err != nil {
		return cfg
	}
	settings, err := meta.protocolSettings()
	if err != nil {
		return cfg
	}
	return settings
}

// S3 enabled by default
func IsBucketS3Enabled(bucket string) bool {
	return bucketProtocols(bucket).S3Enabled
}

// SFTP disabled by default
func IsBucketSFTPEnabled(bucket string) bool {
	return bucketProtocols(bucket).SFTPEnabled
}

// Set protocol metadata, keep public tags
func writeBucketToggles(bucket string, s3Enabled, sftpEnabled bool) error {
	configData, err := json.Marshal(bucketProtocolConfig{S3Enabled: s3Enabled, SFTPEnabled: sftpEnabled})
	if err != nil {
		return err
	}
	return globalBucketMetadataSys.Update(bucket, bucketProtocolConfigFile, configData)
}

// Migrate old flags under metadata lock before replacing user tags
func (b *BucketMetadata) migrateBucketProtocolTags() (bool, error) {
	if b.taggingConfig == nil {
		return false, nil
	}
	legacy := b.taggingConfig.ToMap()
	_, hasS3 := legacy[obstorTagS3Enabled]
	_, hasSFTP := legacy[obstorTagSFTPEnabled]
	if !hasS3 && !hasSFTP {
		return false, nil
	}
	cfg, err := b.protocolSettings()
	if err != nil {
		return false, err
	}
	if len(b.ProtocolConfigJSON) == 0 {
		b.ProtocolConfigJSON, err = json.Marshal(cfg)
		if err != nil {
			return false, err
		}
	}
	delete(legacy, obstorTagS3Enabled)
	delete(legacy, obstorTagSFTPEnabled)
	b.TaggingConfigXML, err = marshalBucketTags(legacy)
	return true, err
}

// Splits a tag map into user keys and reserved toggle keys
func partitionBucketTags(m map[string]string) (user, reserved map[string]string) {
	user, reserved = map[string]string{}, map[string]string{}
	for k, v := range m {
		if strings.HasPrefix(k, obstorReservedTagPrefix) {
			reserved[k] = v
		} else {
			user[k] = v
		}
	}
	return user, reserved
}

// Public tags reject reserved prefix and enforce 50-tag limit
func checkUserBucketTags(user map[string]string) error {
	for k := range user {
		if strings.HasPrefix(k, obstorReservedTagPrefix) {
			return errReservedBucketTag
		}
	}
	if len(user) > maxBucketUserTags {
		return errTooManyBucketUserTags
	}
	_, err := tags.NewTags(user, false)
	return err
}

// Public tags only, empty when bucket has none
func userBucketTags(bucket string) (map[string]string, error) {
	cfg, err := globalBucketMetadataSys.GetTaggingConfig(bucket)
	if err != nil {
		if _, ok := err.(BucketTaggingNotFound); ok {
			return map[string]string{}, nil
		}
		return nil, err
	}
	user, _ := partitionBucketTags(cfg.ToMap())
	return user, nil
}

// Replace public tags after migrating old flags under same distributed lock
func replaceUserBucketTags(bucket string, user map[string]string) error {
	if err := checkUserBucketTags(user); err != nil {
		return err
	}
	configData, err := marshalBucketTags(user)
	if err != nil {
		return err
	}
	return globalBucketMetadataSys.Update(bucket, bucketTaggingConfig, configData)
}

func marshalBucketTags(user map[string]string) ([]byte, error) {
	if len(user) == 0 {
		return nil, nil
	}
	t, err := tags.NewTags(user, false)
	if err != nil {
		return nil, err
	}
	return xml.Marshal(t)
}
