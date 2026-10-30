package cmd

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	xjwt "github.com/obstor/obstor/cmd/jwt"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

// Action groups accepted by ListActivity api filter
var activityGroups = map[string]bool{
	"write":   true,
	"delete":  true,
	"bucket":  true,
	"iam":     true,
	"policy":  true,
	"service": true,
	"config":  true,
}

// Caller tiers and cached prefix checks
type activityViewer struct {
	r         *http.Request
	claims    map[string]interface{}
	accessKey string
	owner     bool
	users     bool
	policies  bool
	settings  bool
	self      map[string]bool
	prefixOK  map[string]bool
}

// Cached s3:ListBucket check for bucket and prefix with delimiter /
func (v *activityViewer) canList(bucket, prefix string) bool {
	key := bucket + "/" + prefix
	if ok, seen := v.prefixOK[key]; seen {
		return ok
	}
	v.r.Header.Set("prefix", prefix)
	v.r.Header.Set("delimiter", SlashSeparator)
	ok := globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     v.accessKey,
		Action:          iampolicy.ListBucketAction,
		BucketName:      bucket,
		ConditionValues: getConditionValues(v.r, "", v.accessKey, v.claims),
		IsOwner:         v.owner,
		Claims:          v.claims,
	})
	v.prefixOK[key] = ok
	return ok
}

// Check admin action, owner always allowed
func (v *activityViewer) canAdmin(action iampolicy.AdminAction) bool {
	return globalIAMSys.IsAllowed(iampolicy.Args{
		AccountName:     v.accessKey,
		Action:          iampolicy.Action(action),
		ConditionValues: getConditionValues(v.r, "", v.accessKey, v.claims),
		IsOwner:         v.owner,
		Claims:          v.claims,
	})
}

// Parent prefix through last slash, folder keeps its own prefix
func activityParentPrefix(object string) string {
	return object[:strings.LastIndex(object, "/")+1]
}

// Build filter and viewer, request limit+1 events per source
func activityScope(ctx context.Context, r *http.Request, objAPI ObjectLayer, claims *xjwt.MapClaims, owner bool, args *ListActivityArgs, limit int) (activityFilter, *activityViewer, error) {
	v := &activityViewer{
		r:         r,
		claims:    claims.Map(),
		accessKey: claims.AccessKey,
		owner:     owner,
		self:      map[string]bool{claims.AccessKey: true},
		prefixOK:  map[string]bool{},
	}
	if temp, parent, err := globalIAMSys.IsTempUser(claims.AccessKey); err == nil && temp && parent != "" {
		v.self[parent] = true
	}
	v.users = owner || v.canAdmin(iampolicy.ListUsersAdminAction)
	v.policies = owner || v.canAdmin(iampolicy.ListUserPoliciesAdminAction)
	v.settings = canSeeSettings(r, claims.AccessKey, v.claims, owner)

	if args.AccessKey != "" && !v.users && !v.self[args.AccessKey] {
		return activityFilter{}, nil, &json2.Error{Message: "You can only filter by your own access key."}
	}

	f := activityFilter{
		Kinds:  []activityKind{activityKindObject, activityKindBucket},
		Group:  args.API,
		Before: args.Before,
		Limit:  limit + 1,
	}
	if args.AccessKey != "" {
		f.AccessKeys = []string{args.AccessKey}
	}

	// Explicit bucket name keeps deleted bucket history readable
	if args.BucketName != "" {
		if !v.canList(args.BucketName, "") {
			return activityFilter{}, nil, errAccessDenied
		}
		f.Buckets = []string{args.BucketName}
		return f, v, nil
	}

	if v.users {
		f.Kinds = append(f.Kinds, activityKindUser)
	}
	if v.policies {
		f.Kinds = append(f.Kinds, activityKindPolicy)
	}
	if v.settings {
		f.Kinds = append(f.Kinds, activityKindService, activityKindConfig)
	}
	if owner {
		f.AllBuckets = true
		return f, v, nil
	}

	buckets, err := objAPI.ListBuckets(ctx)
	if err != nil {
		return activityFilter{}, nil, err
	}
	allow := func(bucket string) bool { return v.canList(bucket, "") }
	// Only instance admins query all deleted names, S3 checks still apply
	if callerCanSeeInstanceTotals(ctx, r, claims.AccessKey, claims.Map(), owner) {
		f.AllBuckets = true
		return f, v, nil
	}
	for _, bucket := range buckets {
		if allow(bucket.Name) {
			f.Buckets = append(f.Buckets, bucket.Name)
		}
	}
	return f, v, nil
}

// Check object parent prefix or empty bucket prefix, other kinds already scoped
func (v *activityViewer) visible(e activityEvent) bool {
	if v.owner {
		return true
	}
	switch activityAPIs[e.API].Kind {
	case activityKindObject:
		// Missing full prefix means owner only, display name may be truncated
		return e.AuthorizationPrefixKnown && v.canList(e.Bucket, e.AuthorizationPrefix)
	case activityKindBucket:
		return v.canList(e.Bucket, "")
	}
	return true
}

// Hide other users' keys outside Users tier
func (v *activityViewer) mask(e activityEvent) activityEvent {
	if !v.users && !v.self[e.AccessKey] {
		e.AccessKey = ""
	}
	return e
}

// Merged node events before prefix checks and key masking
type activityResult struct {
	Events       []activityEvent
	Next         *ActivityCursor
	Partial      bool
	MissingNodes int
	Denied       uint64
	Dropped      uint64
}

// Sort by time, node and seq, keep limit and flag more events
func mergeActivity(pages []activityPage, limit int) ([]activityEvent, *ActivityCursor) {
	events := []activityEvent{}
	for _, p := range pages {
		events = append(events, p.Events...)
	}
	if limit < 1 {
		return []activityEvent{}, nil
	}
	sortActivityNewest(events)
	if len(events) <= limit {
		return events, nil
	}
	next := events[limit-1].cursor()
	return events[:limit], &next
}

// Peer query source, fake in tests
type activityPeer interface {
	ListActivity(ctx context.Context, f activityFilter) (activityPage, error)
}

// Peer timeout before partial reply
var activityPeerTimeout = 2 * time.Second

// Query peers concurrently, failed or timed out peers count as missing
func gatherPeerActivity(ctx context.Context, peers []activityPeer, f activityFilter, timeout time.Duration) (pages []activityPage, missing int) {
	got := make([]activityPage, len(peers))
	errs := make([]error, len(peers))
	var wg sync.WaitGroup
	for i, peer := range peers {
		wg.Go(func() {
			pctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			got[i], errs[i] = peer.ListActivity(pctx, f)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			missing++
			continue
		}
		pages = append(pages, got[i])
	}
	return pages, missing
}

// Merge local and peer pages, sum counters
func activityResultFrom(local activityPage, peers []activityPage, missing, limit int) activityResult {
	pages := append([]activityPage{local}, peers...)
	res := activityResult{Partial: missing > 0, MissingNodes: missing}
	for _, p := range pages {
		res.Denied += p.Denied
		res.Dropped += p.Dropped
	}
	res.Events, res.Next = mergeActivity(pages, limit)
	return res
}

// Query local and peer logs, merge by time, node and seq
func collectActivity(ctx context.Context, f activityFilter, limit int) activityResult {
	local := globalActivityLog.query(f)
	var peers []activityPage
	var missing int
	if globalNotificationSys != nil {
		peers, missing = globalNotificationSys.ListActivity(ctx, f)
	}
	return activityResultFrom(local, peers, missing, limit)
}
