package cmd

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

// Event class for permission tiers
type activityKind string

const (
	activityKindObject  activityKind = "object"
	activityKindBucket  activityKind = "bucket"
	activityKindUser    activityKind = "user"
	activityKindPolicy  activityKind = "policy"
	activityKindService activityKind = "service"
	activityKindConfig  activityKind = "config"
)

// Kind, filter group and S3 or admin audit source
type activityAPI struct {
	Kind  activityKind
	Group string
	Audit string
}

// API names allowed in activity log
var activityAPIs = map[string]activityAPI{
	"PutObject":               {activityKindObject, "write", "s3"},
	"PutObjectExtract":        {activityKindObject, "write", "s3"},
	"PostPolicyBucket":        {activityKindObject, "write", "s3"},
	"CopyObject":              {activityKindObject, "write", "s3"},
	"CompleteMultipartUpload": {activityKindObject, "write", "s3"},
	"WebUpload":               {activityKindObject, "write", ""},
	"RenameObject":            {activityKindObject, "write", ""},

	"DeleteObject":          {activityKindObject, "delete", "s3"},
	"DeleteMultipleObjects": {activityKindObject, "delete", "s3"},
	"RemoveObject":          {activityKindObject, "delete", ""},

	"PutBucket":                     {activityKindBucket, "bucket", "s3"},
	"DeleteBucket":                  {activityKindBucket, "bucket", "s3"},
	"PutBucketObjectLockConfig":     {activityKindBucket, "bucket", "s3"},
	"PutBucketTagging":              {activityKindBucket, "bucket", "s3"},
	"DeleteBucketTagging":           {activityKindBucket, "bucket", "s3"},
	"PutBucketReplicationConfig":    {activityKindBucket, "bucket", "s3"},
	"DeleteBucketReplicationConfig": {activityKindBucket, "bucket", "s3"},
	"PutBucketACL":                  {activityKindBucket, "bucket", "s3"},
	"PutBucketEncryption":           {activityKindBucket, "bucket", "s3"},
	"DeleteBucketEncryption":        {activityKindBucket, "bucket", "s3"},
	"PutBucketLifecycle":            {activityKindBucket, "bucket", "s3"},
	"DeleteBucketLifecycle":         {activityKindBucket, "bucket", "s3"},
	"PutBucketNotification":         {activityKindBucket, "bucket", "s3"},
	"PutBucketPolicy":               {activityKindBucket, "bucket", "s3"},
	"DeleteBucketPolicy":            {activityKindBucket, "bucket", "s3"},
	"PutBucketVersioning":           {activityKindBucket, "bucket", "s3"},
	"PutBucketQuotaConfig":          {activityKindBucket, "bucket", "admin"},
	"SetBucketTarget":               {activityKindBucket, "bucket", "admin"},
	"RemoveBucketTarget":            {activityKindBucket, "bucket", "admin"},
	"MakeBucket":                    {activityKindBucket, "bucket", ""},
	"SetBucketPolicy":               {activityKindBucket, "bucket", ""},
	"SetBucketPolicyDoc":            {activityKindBucket, "bucket", ""},
	"SetBucketToggles":              {activityKindBucket, "bucket", ""},
	"SetBucketVersioning":           {activityKindBucket, "bucket", ""},
	"SetBucketQuota":                {activityKindBucket, "bucket", ""},
	"SetBucketEncryption":           {activityKindBucket, "bucket", ""},
	"SetBucketTags":                 {activityKindBucket, "bucket", ""},

	"AddUser":                 {activityKindUser, "iam", "admin"},
	"RemoveUser":              {activityKindUser, "iam", "admin"},
	"SetUserStatus":           {activityKindUser, "iam", "admin"},
	"UpdateGroupMembers":      {activityKindUser, "iam", "admin"},
	"SetGroupStatus":          {activityKindUser, "iam", "admin"},
	"AddServiceAccount":       {activityKindUser, "iam", "admin"},
	"UpdateServiceAccount":    {activityKindUser, "iam", "admin"},
	"DeleteServiceAccount":    {activityKindUser, "iam", "admin"},
	"SetPolicyForUserOrGroup": {activityKindUser, "iam", "admin"},
	"AddIAMUser":              {activityKindUser, "iam", ""},
	"RemoveIAMUser":           {activityKindUser, "iam", ""},
	"SetIAMUserStatus":        {activityKindUser, "iam", ""},
	"SetIAMUserPolicy":        {activityKindUser, "iam", ""},
	"SetAuth":                 {activityKindUser, "iam", ""},

	"AddCannedPolicy":    {activityKindPolicy, "policy", "admin"},
	"RemoveCannedPolicy": {activityKindPolicy, "policy", "admin"},
	"SetCannedPolicy":    {activityKindPolicy, "policy", ""},
	"DeleteCannedPolicy": {activityKindPolicy, "policy", ""},

	"Service":      {activityKindService, "service", "admin"},
	"ServerUpdate": {activityKindService, "service", "admin"},

	"SetConfigKV":            {activityKindConfig, "config", "admin"},
	"DeleteConfigKV":         {activityKindConfig, "config", "admin"},
	"SetConfig":              {activityKindConfig, "config", "admin"},
	"RestoreConfigHistoryKV": {activityKindConfig, "config", "admin"},
	"ClearConfigHistoryKV":   {activityKindConfig, "config", "admin"},
	"KMSCreateKey":           {activityKindConfig, "config", "admin"},
}

// Web RPCs recorded after handler
var webActivityMethods = map[string]bool{
	"MakeBucket":          true,
	"DeleteBucket":        true,
	"RemoveObject":        true,
	"SetBucketPolicy":     true,
	"SetBucketPolicyDoc":  true,
	"SetBucketToggles":    true,
	"SetBucketVersioning": true,
	"SetBucketQuota":      true,
	"SetBucketEncryption": true,
	"SetBucketTags":       true,
	"SetCannedPolicy":     true,
	"DeleteCannedPolicy":  true,
	"AddIAMUser":          true,
	"RemoveIAMUser":       true,
	"SetIAMUserStatus":    true,
	"SetIAMUserPolicy":    true,
	"SetAuth":             true,
}

const (
	activityRingSize     = 50000
	activityQueueSize    = 4096
	activityObjectMax    = 256
	activityDefaultLimit = 50
	activityMaxLimit     = 200
)

// Recorded mutation
type activityEvent struct {
	Time      time.Time `json:"time"`
	Node      string    `json:"node"`
	Seq       uint64    `json:"seq"`
	AccessKey string    `json:"accessKey"`
	Source    string    `json:"source"`
	API       string    `json:"api"`
	Bucket    string    `json:"bucket"`
	Object    string    `json:"object"`
	Count     int       `json:"count"`
	Status    int       `json:"status"`

	// Full prefix stays in gob only, bool distinguishes root prefix from missing legacy data
	AuthorizationPrefix      string `json:"-"`
	AuthorizationPrefixKnown bool   `json:"-"`
}

// Last displayed event's time, node and seq
type ActivityCursor struct {
	Time time.Time `json:"time"`
	Node string    `json:"node"`
	Seq  uint64    `json:"seq"`
}

func (e activityEvent) cursor() ActivityCursor {
	return ActivityCursor{Time: e.Time, Node: e.Node, Seq: e.Seq}
}

// True when e sorts before cursor
func activityOlder(e activityEvent, c ActivityCursor) bool {
	if !e.Time.Equal(c.Time) {
		return e.Time.Before(c.Time)
	}
	if e.Node != c.Node {
		return e.Node < c.Node
	}
	return e.Seq < c.Seq
}

// Newest first by time, node and seq
func sortActivityNewest(events []activityEvent) {
	sort.Slice(events, func(i, j int) bool { return activityOlder(events[j], events[i].cursor()) })
}

// Limit bytes without splitting UTF-8 rune
func truncateActivityObject(s string) string {
	if len(s) <= activityObjectMax {
		return s
	}
	cut := activityObjectMax
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Per-node ring with buffered appender, persists in segments once started
type activityLog struct {
	mu      sync.Mutex
	node    string
	ring    []activityEvent
	next    int
	size    int
	seq     uint64
	queue   chan activityEvent
	denied  atomic.Uint64
	dropped atomic.Uint64

	// Persistence state set by Start, guarded by mu
	objAPI       ObjectLayer
	nodeKey      string
	persist      bool
	open         []activityEvent
	openFirst    uint64
	openBytes    int
	sealed       [][]activityEvent
	dirty        bool
	loadFailed   bool
	lastFlushErr error

	// Lock load, Flush and saved segment seqs
	flushMu  sync.Mutex
	segments []uint64
}

func newActivityLog(node string, ringSize, queueSize int) *activityLog {
	return &activityLog{
		node:  node,
		ring:  make([]activityEvent, ringSize),
		queue: make(chan activityEvent, queueSize),
	}
}

// Queue authenticated mutation from API list without blocking
func (l *activityLog) submit(e activityEvent) {
	if l == nil {
		return
	}
	if _, ok := activityAPIs[e.API]; !ok {
		return
	}
	if e.AccessKey == "" || e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		l.denied.Add(1)
		return
	}
	l.enqueue(e)
}

// Queue mutation after permission checks, regardless of final status
func (l *activityLog) submitPermitted(e activityEvent) {
	if l == nil {
		return
	}
	if _, ok := activityAPIs[e.API]; !ok {
		return
	}
	if e.AccessKey == "" {
		l.denied.Add(1)
		return
	}
	l.enqueue(e)
}

func (l *activityLog) enqueue(e activityEvent) {
	if e.Time.IsZero() {
		e.Time = UTCNow()
	}
	if activityAPIs[e.API].Kind == activityKindObject {
		e.AuthorizationPrefix = activityParentPrefix(e.Object)
		e.AuthorizationPrefixKnown = true
	}
	e.Object = truncateActivityObject(e.Object)
	select {
	case l.queue <- e:
	default:
		l.dropped.Add(1)
	}
}

// Add event with node name and next seq
func (l *activityLog) append(e activityEvent) activityEvent {
	if globalIAMSys != nil && globalIAMSys.Initialized() {
		if ok, parent, err := globalIAMSys.IsTempUser(e.AccessKey); err == nil && ok && parent != "" {
			e.AccessKey = parent
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e.Node = l.node
	e.Seq = l.seq
	l.seq++
	l.putRing(e)
	if l.persist {
		l.feedSegment(e)
	}
	return e
}

// Write next slot, overwrite oldest when full, caller holds mu
func (l *activityLog) putRing(e activityEvent) {
	l.ring[l.next] = e
	l.next = (l.next + 1) % len(l.ring)
	if l.size < len(l.ring) {
		l.size++
	}
}

// Append events already queued
func (l *activityLog) drain() {
	for n := len(l.queue); n > 0; n-- {
		select {
		case e := <-l.queue:
			l.append(e)
		default:
			return
		}
	}
}

// Append queued events until ctx ends
func (l *activityLog) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-l.queue:
			l.append(e)
		}
	}
}

// Ring copy, oldest first
func (l *activityLog) snapshot() []activityEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]activityEvent, 0, l.size)
	start := (l.next - l.size + len(l.ring)) % len(l.ring)
	for i := 0; i < l.size; i++ {
		out = append(out, l.ring[(start+i)%len(l.ring)])
	}
	return out
}

// ListActivity filter sent whole to peers
type activityFilter struct {
	AllBuckets bool
	Buckets    []string
	Kinds      []activityKind
	AccessKeys []string
	Group      string
	Before     *ActivityCursor
	Limit      int
}

func (f activityFilter) matches(e activityEvent) bool {
	api, ok := activityAPIs[e.API]
	if !ok || !slices.Contains(f.Kinds, api.Kind) {
		return false
	}
	if (api.Kind == activityKindObject || api.Kind == activityKindBucket) && !f.AllBuckets && !slices.Contains(f.Buckets, e.Bucket) {
		return false
	}
	if len(f.AccessKeys) > 0 && !slices.Contains(f.AccessKeys, e.AccessKey) {
		return false
	}
	if f.Group != "" && api.Group != f.Group {
		return false
	}
	return f.Before == nil || activityOlder(e, *f.Before)
}

// One node's filtered page
type activityPage struct {
	Events  []activityEvent
	Denied  uint64
	Dropped uint64
}

// Matching events newest first, at most f.Limit, with local counters
func (l *activityLog) query(f activityFilter) activityPage {
	if l == nil {
		return activityPage{}
	}
	page := activityPage{Denied: l.denied.Load(), Dropped: l.dropped.Load()}
	if f.Limit <= 0 {
		return page
	}
	l.mu.Lock()
	for _, e := range l.ring[:l.size] {
		if f.matches(e) {
			page.Events = append(page.Events, e)
		}
	}
	l.mu.Unlock()
	sortActivityNewest(page.Events)
	if len(page.Events) > f.Limit {
		page.Events = page.Events[:f.Limit]
	}
	return page
}

// Record listed S3 and admin APIs from ActivityHook
func recordAuditActivity(_ context.Context, reqInfo *logger.ReqInfo, statusCode int) {
	if reqInfo == nil {
		return
	}
	api, ok := activityAPIs[reqInfo.API]
	if !ok || api.Audit == "" {
		return
	}
	tags := reqInfo.GetTagsMap()
	count, _ := tags["count"].(int)
	if denied, _ := tags["denied"].(bool); denied {
		statusCode = http.StatusForbidden
	}
	object := reqInfo.ObjectName
	if subject, _ := tags["subject"].(string); subject != "" {
		object = subject
	}
	globalActivityLog.submit(activityEvent{
		AccessKey: reqInfo.AccessKey,
		Source:    api.Audit,
		API:       reqInfo.API,
		Bucket:    reqInfo.BucketName,
		Object:    object,
		Count:     count,
		Status:    statusCode,
	})
}

func init() {
	logger.ActivityHook = recordAuditActivity
}

// HTTP status from toWebAPIError tables
var webErrorStatuses = sync.OnceValue(func() map[string]int {
	statuses := make(map[string]int, len(errorCodes)+8)
	for _, apiErr := range errorCodes {
		if status, ok := statuses[apiErr.Code]; !ok || apiErr.HTTPStatusCode < status {
			statuses[apiErr.Code] = apiErr.HTTPStatusCode
		}
	}
	for _, err := range []error{errNoAuthToken, errWebTokenInvalid, errServerNotInitialized, errFleetStoreDisabled, errAuthentication, errSizeUnspecified,
		errChangeCredNotAllowed, errInvalidBucketName, errInvalidArgument, errIncorrectCreds, errSTSNotInitialized, NotImplemented{}} {
		apiErr := toWebAPIError(GlobalContext, err)
		statuses[apiErr.Code] = apiErr.HTTPStatusCode
	}
	return statuses
})

// Web error status from data.code
func webActivityStatus(ctx context.Context, err error) int {
	if err == nil {
		return http.StatusOK
	}
	var jerr *json2.Error
	if !errors.As(err, &jerr) {
		return toWebAPIError(ctx, err).HTTPStatusCode
	}
	data, _ := jerr.Data.(map[string]string)
	code := data["code"]
	if code == "" {
		return http.StatusBadRequest
	}
	if status, ok := webErrorStatuses()[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// Bucket, object, subject and delete count from RPC args
func webActivityTarget(method string, args reflect.Value) (bucket, object string, count int) {
	if !args.IsValid() || args.Kind() != reflect.Pointer || args.IsNil() || args.Elem().Kind() != reflect.Struct {
		return "", "", 0
	}
	a := args.Elem()
	field := func(name string) string {
		if f := a.FieldByName(name); f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
		return ""
	}
	bucket = field("BucketName")
	object = field("ObjectName")
	if object == "" {
		object = field("Prefix")
	}
	if f := a.FieldByName("Objects"); f.IsValid() && f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String {
		switch n := f.Len(); {
		case n == 1:
			object = f.Index(0).String()
		case n > 1:
			count = n
		}
	}
	switch method {
	case "AddIAMUser", "RemoveIAMUser", "SetIAMUserStatus", "SetIAMUserPolicy":
		object = field("AccessKey")
	case "SetCannedPolicy", "DeleteCannedPolicy":
		object = field("Name")
	}
	return bucket, object, count
}

type webActivityGateKey struct{}

// Add request flag for passed permission checks
func withWebActivityGate(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), webActivityGateKey{}, new(atomic.Bool)))
}

// Mark permission checks passed so activity records result
func passWebActivityGate(r *http.Request) {
	if gate, ok := r.Context().Value(webActivityGateKey{}).(*atomic.Bool); ok {
		gate.Store(true)
	}
}

func webActivityGatePassed(r *http.Request) bool {
	gate, ok := r.Context().Value(webActivityGateKey{}).(*atomic.Bool)
	return ok && gate.Load()
}

// Record mutations after permission checks, count only denials before them
func recordConsoleActivity(e activityEvent, permitted bool) {
	e.Source = "console"
	if permitted {
		globalActivityLog.submitPermitted(e)
		return
	}
	if e.AccessKey == "" || e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		globalActivityLog.submit(e)
	}
}

// Record listed web RPCs that reached handler
func recordWebActivity(ctx context.Context, method string, args reflect.Value, accessKey string, permitted bool, err error) {
	if !webActivityMethods[method] || !args.IsValid() {
		return
	}
	bucket, object, count := webActivityTarget(method, args)
	if method == "SetAuth" {
		object = accessKey
	}
	recordConsoleActivity(activityEvent{
		AccessKey: accessKey,
		API:       method,
		Bucket:    bucket,
		Object:    object,
		Count:     count,
		Status:    webActivityStatus(ctx, err),
	}, permitted)
}

// RecordActivity - records SFTP mutations outside S3 and web routers
func RecordActivity(source, accessKey, api, bucket, object string, status int) {
	globalActivityLog.submit(activityEvent{
		AccessKey: accessKey,
		Source:    source,
		API:       api,
		Bucket:    bucket,
		Object:    object,
		Status:    status,
	})
}
