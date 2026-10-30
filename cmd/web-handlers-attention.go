package cmd

import (
	"context"
	"net/http"
	"time"

	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
)

// AttentionRep - caller's bell items, Items never null
type AttentionRep struct {
	Items     []AttentionItem `json:"items"`
	UIVersion string          `json:"uiVersion"`
}

// Attention - drive faults for Fleet, scoped quotas, stale usage and history faults for root
func (web *webAPIHandlers) Attention(r *http.Request, args *WebGenericArgs, reply *AttentionRep) error {
	ctx := newWebContext(r, args, "WebAttention")
	objectAPI := web.ObjectAPI()
	if objectAPI == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return toJSONError(ctx, authErr)
	}
	claimsMap := claims.Map()
	fleet := callerCanSeeInstanceTotals(ctx, r, claims.AccessKey, claimsMap, owner)

	now := time.Now()
	in := attentionInput{Fleet: fleet, Root: owner, Backend: globalIsBackend, BootTime: globalBootTime, Now: now}
	if fleet {
		in.OfflineNodes, in.OfflineDrives = offlineDisks(globalAttentionDisks.get(ctx, objectAPI, now))
	}
	if !globalIsBackend {
		quotas, asOf, err := attentionQuotas(ctx, r, objectAPI, claims.AccessKey, claimsMap, owner)
		switch {
		case err == nil:
			in.Quotas, in.UsageAsOf = quotas, asOf
		case len(in.OfflineNodes)+len(in.OfflineDrives) == 0:
			// Failed call keeps console items and seen IDs
			return toJSONError(ctx, err)
		default:
			// Report drive outage without quota or stale items from failed usage read
			in.UsageUnknown = true
		}
	}
	if owner {
		in.Activity = globalActivityLog.Status()
		if globalCapacityHistory != nil {
			in.Histories = append(in.Histories, namedHistory{ID: "capacity", Label: "Capacity history", Status: globalCapacityHistory.HistoryStatus()})
		}
		if globalFleetStore != nil {
			in.Histories = append(in.Histories, namedHistory{ID: "traffic", Label: "Traffic history", Status: globalFleetStore.HistoryStatus()})
		}
	}

	reply.Items = attentionItems(in)
	reply.UIVersion = Version
	return nil
}

// Hard quota usage for listable buckets and scan time
func attentionQuotas(ctx context.Context, r *http.Request, objectAPI ObjectLayer, accessKey string, claimsMap map[string]interface{}, owner bool) ([]quotaUse, time.Time, error) {
	dui, err := globalBucketQuotaSys.Usage(objectAPI)
	if err != nil {
		return nil, time.Time{}, err
	}
	// Prefix and delimiter for policy conditions
	r.Header.Set("prefix", "")
	r.Header.Set("delimiter", SlashSeparator)
	buckets, err := objectAPI.ListBuckets(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	var quotas []quotaUse
	conditions := getConditionValues(r, "", accessKey, claimsMap)
	for _, b := range buckets {
		if !globalIAMSys.IsAllowed(iampolicy.Args{
			AccountName:     accessKey,
			Action:          iampolicy.ListBucketAction,
			BucketName:      b.Name,
			ConditionValues: conditions,
			IsOwner:         owner,
			Claims:          claimsMap,
		}) {
			continue
		}
		q, qErr := globalBucketQuotaSys.Get(b.Name)
		if qErr != nil || q == nil || q.Type != madmin.HardQuota || q.Quota == 0 {
			continue
		}
		if use, ok := dui.BucketsUsage[b.Name]; ok {
			quotas = append(quotas, quotaUse{Bucket: b.Name, Used: use.Size, Quota: q.Quota})
		}
	}
	return quotas, dui.LastUpdate, nil
}
