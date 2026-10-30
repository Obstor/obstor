package cmd

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/obstor/obstor/pkg/madmin"
)

// Severities, most urgent first
const (
	attentionCritical = "critical"
	attentionWarning  = "warning"
	attentionInfo     = "info"
)

const (
	// Refresh drive state after this age
	attentionDiskTTL = 15 * time.Second
	// Usage age that raises stale item
	attentionStaleAfter = 24 * time.Hour
)

// Condition needing attention, stable ID by kind and subject
type AttentionItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Href     string `json:"href"`
}

// Scanned bucket size and hard quota
type quotaUse struct {
	Bucket      string
	Used, Quota uint64
}

// History persistence status, item ID and label
type namedHistory struct {
	ID     string
	Label  string
	Status historyStatus
}

// Attention inputs allowed by caller's tier
type attentionInput struct {
	Fleet, Root, Backend        bool
	UsageUnknown                bool
	OfflineNodes, OfflineDrives []string
	Quotas                      []quotaUse
	UsageAsOf, BootTime, Now    time.Time
	Histories                   []namedHistory
	Activity                    activityStatus
}

// Severity rank, critical first
var attentionRank = map[string]int{attentionCritical: 0, attentionWarning: 1, attentionInfo: 2}

// Items by severity then ID, never nil
func attentionItems(in attentionInput) []AttentionItem {
	items := []AttentionItem{}
	if in.Fleet {
		for _, host := range in.OfflineNodes {
			items = append(items, AttentionItem{
				ID:       "node-offline:" + host,
				Kind:     "node-offline",
				Severity: attentionCritical,
				Title:    "Node " + host + " is offline",
				Detail:   "Every drive on this node is unreachable.",
				Href:     "/cluster",
			})
		}
		for _, endpoint := range in.OfflineDrives {
			items = append(items, AttentionItem{
				ID:       "drive-offline:" + endpoint,
				Kind:     "drive-offline",
				Severity: attentionCritical,
				Title:    "Drive " + endpoint + " is offline",
				Detail:   "The drive is unreachable or unformatted.",
				Href:     "/cluster",
			})
		}
	}
	for _, q := range in.Quotas {
		if q.Quota == 0 {
			continue
		}
		used, quota := float64(q.Used), float64(q.Quota)
		href := "/buckets/" + url.PathEscape(q.Bucket) + "?tab=settings"
		switch {
		case used >= quota:
			items = append(items, AttentionItem{
				ID:       "quota-full:" + q.Bucket,
				Kind:     "quota-full",
				Severity: attentionCritical,
				Title:    "Bucket " + q.Bucket + " reached its quota",
				Detail:   "Uploads are refused until objects are deleted or the quota is raised.",
				Href:     href,
			})
		case used >= 0.9*quota:
			items = append(items, AttentionItem{
				ID:       "quota-near:" + q.Bucket,
				Kind:     "quota-near",
				Severity: attentionWarning,
				Title:    fmt.Sprintf("Bucket %s is at %d%% of its quota", q.Bucket, int(math.Floor(used*100/quota))),
				Detail:   "Uploads are refused once the bucket reaches its hard quota.",
				Href:     href,
			})
		}
	}
	if in.Root && !in.Backend && !in.UsageUnknown && usageStale(in.UsageAsOf, in.BootTime, in.Now) {
		items = append(items, AttentionItem{
			ID:       "usage-stale",
			Kind:     "usage-stale",
			Severity: attentionInfo,
			Title:    "Usage data is more than a day old",
			Detail:   "The usage scanner has not finished a pass in 24 hours, so sizes, quotas and history may be out of date.",
			Href:     "/",
		})
	}
	if in.Root {
		for _, h := range in.Histories {
			if err := h.Status.LastSaveErr; err != nil {
				items = append(items, AttentionItem{
					ID:       "history-save:" + h.ID,
					Kind:     "history-save",
					Severity: attentionWarning,
					Title:    h.Label + " is not being saved",
					Detail:   "Last save failed: " + err.Error(),
				})
			}
			if h.Status.LoadFailed {
				items = append(items, AttentionItem{
					ID:       "history-load:" + h.ID,
					Kind:     "history-load",
					Severity: attentionInfo,
					Title:    h.Label + " could not be read at startup",
					Detail:   "History started empty. The server retries the read; a corrupt file is replaced at the next save.",
				})
			}
		}
	}
	if in.Root && in.Activity.LastFlushErr != "" {
		items = append(items, AttentionItem{
			ID:       "activity-flush",
			Kind:     "activity",
			Severity: attentionWarning,
			Title:    "Activity log is not being saved",
			Detail:   in.Activity.LastFlushErr,
			Href:     "/activity",
		})
	}
	if in.Root && in.Activity.LoadFailed {
		items = append(items, AttentionItem{
			ID:       "activity-load",
			Kind:     "activity",
			Severity: attentionInfo,
			Title:    "Activity log could not be read at startup",
			Detail:   "Some saved events were skipped. New events are recorded.",
			Href:     "/activity",
		})
	}
	sort.Slice(items, func(i, j int) bool {
		ri, rj := attentionRank[items[i].Severity], attentionRank[items[j].Severity]
		if ri != rj {
			return ri < rj
		}
		return items[i].ID < items[j].ID
	})
	return items
}

// Stale after attentionStaleAfter since scan or boot
func usageStale(asOf, boot, now time.Time) bool {
	if asOf.IsZero() {
		return now.Sub(boot) >= attentionStaleAfter
	}
	return now.Sub(asOf) >= attentionStaleAfter
}

// Drive URL host, plain path means local node
func diskNode(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// Offline nodes and remaining offline drives, both sorted
func offlineDisks(disks []madmin.Disk) (nodes, drives []string) {
	nodeUp := map[string]bool{}
	for _, d := range disks {
		node := diskNode(d.Endpoint)
		nodeUp[node] = nodeUp[node] || d.State == madmin.DriveStateOk
	}
	down := map[string]bool{}
	for node, up := range nodeUp {
		if node != "" && !up {
			down[node] = true
			nodes = append(nodes, node)
		}
	}
	for _, d := range disks {
		if d.State != madmin.DriveStateOk && !down[diskNode(d.Endpoint)] {
			drives = append(drives, d.Endpoint)
		}
	}
	sort.Strings(nodes)
	sort.Strings(drives)
	return nodes, drives
}

// Drive state cached for attentionDiskTTL
type attentionDiskCache struct {
	mu    sync.Mutex
	at    time.Time
	disks []madmin.Disk
	ok    bool
}

// Cached drives or fresh StorageInfo, callers must not modify slice
func (c *attentionDiskCache) get(ctx context.Context, objAPI ObjectLayer, now time.Time) []madmin.Disk {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok && now.Sub(c.at) < attentionDiskTTL {
		return c.disks
	}
	si, _ := objAPI.StorageInfo(ctx)
	c.disks, c.at, c.ok = si.Disks, now, true
	return c.disks
}

// Shared drive cache for Attention calls
var globalAttentionDisks = &attentionDiskCache{}
