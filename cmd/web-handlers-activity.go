package cmd

import (
	"net/http"

	"github.com/obstor/obstor/pkg/rpc/json2"
)

// Filters and cursor for activity page
type ListActivityArgs struct {
	BucketName string          `json:"bucket"`
	AccessKey  string          `json:"accessKey"`
	API        string          `json:"api"`
	Before     *ActivityCursor `json:"before"`
	Limit      int             `json:"limit"`
}

// Activity page, newest first
type ListActivityRep struct {
	Events       []activityEvent `json:"events"`
	Next         *ActivityCursor `json:"next"`
	Partial      bool            `json:"partial"`
	MissingNodes int             `json:"missingNodes"`
	Denied       uint64          `json:"denied"`
	Dropped      uint64          `json:"dropped"`
	UIVersion    string          `json:"uiVersion"`
}

// Recorded mutations caller may read, newest first
func (web *webAPIHandlers) ListActivity(r *http.Request, args *ListActivityArgs, reply *ListActivityRep) error {
	ctx := newWebContext(r, args, "WebListActivity")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}

	if args.Limit < 0 || args.Limit > activityMaxLimit {
		return &json2.Error{Message: "Ask for at most 200 events per call."}
	}
	limit := args.Limit
	if limit == 0 {
		limit = activityDefaultLimit
	}
	if args.API != "" && !activityGroups[args.API] {
		return &json2.Error{Message: "Unknown action type: " + args.API + "."}
	}
	if args.BucketName != "" && isReservedOrInvalidBucket(args.BucketName, false) {
		return toJSONError(ctx, errInvalidBucketName, args.BucketName)
	}

	f, v, err := activityScope(ctx, r, objectAPI, claims, owner, args, limit)
	if err != nil {
		if jerr, ok := err.(*json2.Error); ok {
			return jerr
		}
		return toJSONError(ctx, err, args.BucketName)
	}

	res := collectActivity(ctx, f, limit)
	reply.Events = make([]activityEvent, 0, len(res.Events))
	for _, e := range res.Events {
		if v.visible(e) {
			reply.Events = append(reply.Events, v.mask(e))
		}
	}
	reply.Next = res.Next
	reply.Partial = res.Partial
	reply.MissingNodes = res.MissingNodes
	if v.settings {
		reply.Denied = res.Denied
		reply.Dropped = res.Dropped
	}
	reply.UIVersion = Version
	return nil
}
