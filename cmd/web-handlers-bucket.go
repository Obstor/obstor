package cmd

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"

	"github.com/obstor/obstor/pkg/bucket/versioning"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

// BucketFeaturesRep - settings supported by server mode
type BucketFeaturesRep struct {
	UIVersion      string `json:"uiVersion"`
	Versioning     bool   `json:"versioning"`
	ObjectLock     bool   `json:"objectLock"`
	Quota          bool   `json:"quota"`
	Encryption     bool   `json:"encryption"`
	AutoEncryption bool   `json:"autoEncryption"`
	Tags           bool   `json:"tags"`
	Backend        bool   `json:"backend"`
}

// Match metadata and SSE gates so console offers supported controls
func bucketFeatures() BucketFeaturesRep {
	return BucketFeaturesRep{
		Versioning:     globalIsErasure,
		ObjectLock:     globalIsErasure,
		Quota:          !globalIsBackend,
		Encryption:     !globalIsBackend && GlobalKMS != nil,
		AutoEncryption: globalAutoEncryption,
		Tags:           !globalIsBackend, // NAS stores tags but cannot read them
		Backend:        globalIsBackend,
	}
}

// BucketFeatures - server support for bucket settings
func (web *webAPIHandlers) BucketFeatures(r *http.Request, args *WebGenericArgs, reply *BucketFeaturesRep) error {
	ctx := newWebContext(r, args, "WebBucketFeatures")
	if web.ObjectAPI() == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	if _, _, authErr := webRequestAuthenticate(r); authErr != nil {
		return toJSONError(ctx, authErr)
	}
	*reply = bucketFeatures()
	reply.UIVersion = Version
	return nil
}

// GetBucketConfigArgs - bucket whose settings to read
type GetBucketConfigArgs struct {
	BucketName string `json:"bucketName"`
}

// WebBucketQuota - byte limit, zero means none
type WebBucketQuota struct {
	Type  madmin.QuotaType `json:"type"`
	Bytes uint64           `json:"bytes"`
}

// WebTag - user bucket tag
type WebTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GetBucketConfigRep - nil fields were unreadable
type GetBucketConfigRep struct {
	UIVersion  string          `json:"uiVersion"`
	Versioning *string         `json:"versioning"`
	ObjectLock *bool           `json:"objectLock"`
	Quota      *WebBucketQuota `json:"quota"`
	Encryption *bool           `json:"encryption"`
	Tags       []WebTag        `json:"tags"`
}

// Authenticate once, admin actions use empty bucket as admin API does
func webAuthorizer(r *http.Request) (func(action iampolicy.Action, bucket string) bool, error) {
	claims, owner, err := webRequestAuthenticate(r)
	if err != nil {
		return nil, err
	}
	return func(action iampolicy.Action, bucket string) bool {
		return globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     claims.AccessKey,
			Action:          action,
			BucketName:      bucket,
			ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
			IsOwner:         owner,
			Claims:          claims.Map(),
		})
	}, nil
}

// Tags by key, empty encodes as []
func sortedWebTags(m map[string]string) []WebTag {
	out := make([]WebTag, 0, len(m))
	for k, v := range m {
		out = append(out, WebTag{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// GetBucketConfig - versioning, lock, quota, encryption and tags with separate read checks
func (web *webAPIHandlers) GetBucketConfig(r *http.Request, args *GetBucketConfigArgs, reply *GetBucketConfigRep) error {
	ctx := newWebContext(r, args, "WebGetBucketConfig")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	allowed, authErr := webAuthorizer(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	bucket := args.BucketName
	reply.UIVersion = Version

	canVersioning := allowed(iampolicy.GetBucketVersioningAction, bucket)
	canLock := allowed(iampolicy.GetBucketObjectLockConfigurationAction, bucket)
	canQuota := allowed(iampolicy.GetBucketQuotaAdminAction, "")
	canSSE := allowed(iampolicy.GetBucketEncryptionAction, bucket)
	canTags := allowed(iampolicy.GetBucketTaggingAction, bucket)
	// No readable fields, return before revealing bucket existence
	if !canVersioning && !canLock && !canQuota && !canSSE && !canTags {
		return nil
	}
	if isReservedOrInvalidBucket(bucket, false) {
		return toJSONError(ctx, errInvalidBucketName, bucket)
	}
	if _, err := objectAPI.GetBucketInfo(ctx, bucket); err != nil {
		return toJSONError(ctx, err, bucket)
	}
	// Backend settings are unreadable
	if globalIsBackend {
		return nil
	}

	if canVersioning {
		v, err := globalBucketVersioningSys.Get(bucket)
		if err != nil {
			return toJSONError(ctx, err, bucket)
		}
		status := string(v.Status)
		reply.Versioning = &status
	}
	if canLock {
		rcfg, err := globalBucketObjectLockSys.Get(bucket)
		if err != nil {
			return toJSONError(ctx, err, bucket)
		}
		reply.ObjectLock = &rcfg.LockEnabled
	}
	if canQuota {
		q, err := globalBucketQuotaSys.Get(bucket)
		if err != nil {
			return toJSONError(ctx, err, bucket)
		}
		reply.Quota = &WebBucketQuota{Type: q.Type, Bytes: q.Quota}
	}
	if canSSE {
		_, err := globalBucketSSEConfigSys.Get(bucket)
		if _, missing := err.(BucketSSEConfigNotFound); err != nil && !missing {
			return toJSONError(ctx, err, bucket)
		}
		on := err == nil
		reply.Encryption = &on
	}
	if canTags {
		user, err := userBucketTags(bucket)
		if err != nil {
			return toJSONError(ctx, err, bucket)
		}
		reply.Tags = sortedWebTags(user)
	}
	return nil
}

// SetBucketVersioningArgs - Enabled or Suspended
type SetBucketVersioningArgs struct {
	BucketName string `json:"bucketName"`
	Enabled    bool   `json:"enabled"`
}

// SetBucketVersioning - same as S3 handler, Suspended keeps stored versions reachable
func (web *webAPIHandlers) SetBucketVersioning(r *http.Request, args *SetBucketVersioningArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketVersioning")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	allowed, authErr := webAuthorizer(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !allowed(iampolicy.PutBucketVersioningAction, args.BucketName) {
		return toJSONError(ctx, errAccessDenied)
	}
	passWebActivityGate(r)
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}
	if _, err := objectAPI.GetBucketInfo(ctx, args.BucketName); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	v := versioning.Versioning{XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/", Status: versioning.Enabled}
	if !args.Enabled {
		if err := checkVersioningSuspend(ctx, args.BucketName); err != nil {
			return &json2.Error{Message: err.Error()}
		}
		v.Status = versioning.Suspended
	}
	configData, err := xml.Marshal(v)
	if err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	// FS and backends refuse versioning
	if err = globalBucketMetadataSys.Update(args.BucketName, bucketVersioningConfig, configData); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	reply.UIVersion = Version
	return nil
}

// SetBucketQuotaArgs - zero clears quota
type SetBucketQuotaArgs struct {
	BucketName string           `json:"bucketName"`
	Type       madmin.QuotaType `json:"type"`
	Bytes      uint64           `json:"bytes"`
}

// SetBucketQuota - same permission as S3 handler, backends refuse metadata update
func (web *webAPIHandlers) SetBucketQuota(r *http.Request, args *SetBucketQuotaArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketQuota")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	if _, err := webAdminAuth(r, iampolicy.SetBucketQuotaAdminAction); err != nil {
		return toJSONError(ctx, err)
	}
	passWebActivityGate(r)
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}
	if _, err := objectAPI.GetBucketInfo(ctx, args.BucketName); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	var configData []byte
	if args.Bytes > 0 {
		if !args.Type.IsValid() {
			return &json2.Error{Message: "quota type must be hard or fifo"}
		}
		data, err := json.Marshal(madmin.BucketQuota{Quota: args.Bytes, Type: args.Type})
		if err != nil {
			return toJSONError(ctx, err, args.BucketName)
		}
		configData = data
	}
	if err := globalBucketMetadataSys.Update(args.BucketName, bucketQuotaConfigFile, configData); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	reply.UIVersion = Version
	return nil
}

// SetBucketEncryptionArgs - SSE-S3 on or off
type SetBucketEncryptionArgs struct {
	BucketName string `json:"bucketName"`
	Enabled    bool   `json:"enabled"`
}

// SetBucketEncryption - same as S3 handlers, only enabling needs KMS
func (web *webAPIHandlers) SetBucketEncryption(r *http.Request, args *SetBucketEncryptionArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketEncryption")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	allowed, authErr := webAuthorizer(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !allowed(iampolicy.PutBucketEncryptionAction, args.BucketName) {
		return toJSONError(ctx, errAccessDenied)
	}
	passWebActivityGate(r)
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}
	if _, err := objectAPI.GetBucketInfo(ctx, args.BucketName); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	// Backends cannot apply bucket SSE
	if globalIsBackend {
		return toJSONError(ctx, NotImplemented{})
	}
	var configData []byte
	if args.Enabled {
		if GlobalKMS == nil {
			return toJSONError(ctx, errKMSNotConfigured)
		}
		configData = bucketSSES3Config
	}
	if err := globalBucketMetadataSys.Update(args.BucketName, bucketSSEConfig, configData); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	reply.UIVersion = Version
	return nil
}

// SetBucketTagsArgs - public tag set, protocol metadata stays server side
type SetBucketTagsArgs struct {
	BucketName string   `json:"bucketName"`
	Tags       []WebTag `json:"tags"`
}

// SetBucketTags - same reserved key and 50-tag checks as S3 handler
func (web *webAPIHandlers) SetBucketTags(r *http.Request, args *SetBucketTagsArgs, reply *WebGenericRep) error {
	ctx := newWebContext(r, args, "WebSetBucketTags")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	allowed, authErr := webAuthorizer(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !allowed(iampolicy.PutBucketTaggingAction, args.BucketName) {
		return toJSONError(ctx, errAccessDenied)
	}
	passWebActivityGate(r)
	if isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}
	if _, err := objectAPI.GetBucketInfo(ctx, args.BucketName); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	user := make(map[string]string, len(args.Tags))
	for _, t := range args.Tags {
		if _, dup := user[t.Key]; dup {
			return &json2.Error{Message: fmt.Sprintf("tag key %q is used twice", t.Key)}
		}
		user[t.Key] = t.Value
	}
	if err := checkUserBucketTags(user); err != nil {
		return &json2.Error{Message: err.Error()}
	}
	// Backend tag read fails before write
	if err := replaceUserBucketTags(args.BucketName, user); err != nil {
		return toJSONError(ctx, err, args.BucketName)
	}
	reply.UIVersion = Version
	return nil
}
