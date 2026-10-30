package cmd

import (
	"net/http"
	"time"

	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

// 24, 168 or 720 hours
type CapacityHistoryArgs struct {
	Hours int `json:"hours"`
}

// Series ending now, Step in seconds, scoped get null capacity and health
type CapacityHistoryRep struct {
	Since        string   `json:"since"`
	Step         int64    `json:"step"`
	Used         []uint64 `json:"used"`
	Objects      []uint64 `json:"objects"`
	Buckets      []uint64 `json:"buckets"`
	Free         []uint64 `json:"free"`
	Total        []uint64 `json:"total"`
	DrivesOnline []uint64 `json:"drivesOnline"`
	DrivesTotal  []uint64 `json:"drivesTotal"`
	NodesOnline  []uint64 `json:"nodesOnline"`
	NodesTotal   []uint64 `json:"nodesTotal"`
	Partial      bool     `json:"partial"`
	UIVersion    string   `json:"uiVersion"`
}

// Cluster series for Fleet, listable bucket sums for others
func (web *webAPIHandlers) CapacityHistory(r *http.Request, args *CapacityHistoryArgs, reply *CapacityHistoryRep) error {
	ctx := newWebContext(r, args, "WebCapacityHistory")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	if !capacityHistoryHours(args.Hours) {
		return &json2.Error{Message: "Hours must be 24, 168 or 720."}
	}

	reply.UIVersion = Version
	reply.Step = int64(time.Hour / time.Second)
	if args.Hours == 720 {
		reply.Step = int64(24 * time.Hour / time.Second)
	}
	reply.Used, reply.Objects, reply.Buckets = []uint64{}, []uint64{}, []uint64{}
	if globalIsBackend {
		return nil
	}

	// Nil scope gets cluster totals
	var scope capacityScope
	if !callerCanSeeInstanceTotals(ctx, r, claims.AccessKey, claims.Map(), owner) {
		r.Header.Set("prefix", "")
		r.Header.Set("delimiter", SlashSeparator)
		buckets, err := objectAPI.ListBuckets(ctx)
		if err != nil {
			return toJSONError(ctx, err)
		}
		scope = make(capacityScope, len(buckets))
		for _, bucket := range buckets {
			if globalIAMSys.IsAllowed(iampolicy.Args{
				AccountName:     claims.AccessKey,
				Action:          iampolicy.ListBucketAction,
				BucketName:      bucket.Name,
				ConditionValues: getConditionValues(r, "", claims.AccessKey, claims.Map()),
				IsOwner:         owner,
				Claims:          claims.Map(),
			}) {
				scope[bucket.Name] = absHour(bucket.Created)
			}
		}
	}

	now := time.Now()
	var series capacitySeries
	globalCapacityHistory.view(ctx, objectAPI, now, func(s *capacityState) {
		series = s.series(absHour(now), args.Hours, scope)
		for bucket := range scope {
			if !s.tracked(bucket) {
				reply.Partial = true
				break
			}
		}
	})

	reply.Since = rfc3339OrEmpty(series.Since)
	reply.Used, reply.Objects, reply.Buckets, reply.Free = series.Used, series.Objects, series.Buckets, series.Free
	reply.Total, reply.DrivesOnline, reply.DrivesTotal = series.Total, series.DrivesOnline, series.DrivesTotal
	reply.NodesOnline, reply.NodesTotal = series.NodesOnline, series.NodesTotal
	return nil
}
