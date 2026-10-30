package cmd

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/obstor/obstor/pkg/madmin"
)

// Item IDs in order
func attentionIDs(items []AttentionItem) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

// Equal strings, nil matches empty
func sameStrings(a, b []string) bool {
	return len(a) == len(b) && (len(a) == 0 || reflect.DeepEqual(a, b))
}

func TestAttentionItems(t *testing.T) {
	if got := attentionItems(attentionInput{}); got == nil || len(got) != 0 {
		t.Fatalf("empty input = %#v, want a non-nil empty slice", got)
	}

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	saveErr := errors.New("disk full")
	every := func() attentionInput {
		return attentionInput{
			Fleet:         true,
			Root:          true,
			OfflineNodes:  []string{"node2:9000"},
			OfflineDrives: []string{"http://node1:9000/mnt/d2"},
			Quotas: []quotaUse{
				{Bucket: "zeta", Used: 95, Quota: 100},
				{Bucket: "alpha", Used: 100, Quota: 100},
			},
			UsageAsOf: now.Add(-25 * time.Hour),
			BootTime:  now.Add(-48 * time.Hour),
			Now:       now,
			Histories: []namedHistory{
				{ID: "capacity", Label: "Capacity history", Status: historyStatus{LastSaveErr: saveErr}},
				{ID: "traffic", Label: "Traffic history", Status: historyStatus{LoadFailed: true}},
			},
		}
	}
	want := []AttentionItem{
		{ID: "drive-offline:http://node1:9000/mnt/d2", Kind: "drive-offline", Severity: attentionCritical, Title: "Drive http://node1:9000/mnt/d2 is offline", Detail: "The drive is unreachable or unformatted.", Href: "/cluster"},
		{ID: "node-offline:node2:9000", Kind: "node-offline", Severity: attentionCritical, Title: "Node node2:9000 is offline", Detail: "Every drive on this node is unreachable.", Href: "/cluster"},
		{ID: "quota-full:alpha", Kind: "quota-full", Severity: attentionCritical, Title: "Bucket alpha reached its quota", Detail: "Uploads are refused until objects are deleted or the quota is raised.", Href: "/buckets/alpha?tab=settings"},
		{ID: "history-save:capacity", Kind: "history-save", Severity: attentionWarning, Title: "Capacity history is not being saved", Detail: "Last save failed: disk full"},
		{ID: "quota-near:zeta", Kind: "quota-near", Severity: attentionWarning, Title: "Bucket zeta is at 95% of its quota", Detail: "Uploads are refused once the bucket reaches its hard quota.", Href: "/buckets/zeta?tab=settings"},
		{ID: "history-load:traffic", Kind: "history-load", Severity: attentionInfo, Title: "Traffic history could not be read at startup", Detail: "History started empty. The server retries the read; a corrupt file is replaced at the next save."},
		{ID: "usage-stale", Kind: "usage-stale", Severity: attentionInfo, Title: "Usage data is more than a day old", Detail: "The usage scanner has not finished a pass in 24 hours, so sizes, quotas and history may be out of date.", Href: "/"},
	}
	first := attentionItems(every())
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("every rule:\n got %#v\nwant %#v", first, want)
	}
	if again := attentionIDs(attentionItems(every())); !sameStrings(again, attentionIDs(first)) {
		t.Fatalf("second call ids = %q, want %q", again, attentionIDs(first))
	}
	shuffled := every()
	shuffled.Quotas[0], shuffled.Quotas[1] = shuffled.Quotas[1], shuffled.Quotas[0]
	shuffled.Histories[0], shuffled.Histories[1] = shuffled.Histories[1], shuffled.Histories[0]
	if got := attentionIDs(attentionItems(shuffled)); !sameStrings(got, attentionIDs(first)) {
		t.Fatalf("shuffled input ids = %q, want %q", got, attentionIDs(first))
	}

	with := func(f func(in *attentionInput)) attentionInput {
		in := every()
		f(&in)
		return in
	}
	quota := func(used, limit uint64) attentionInput {
		return attentionInput{Quotas: []quotaUse{{Bucket: "b", Used: used, Quota: limit}}, BootTime: now, Now: now}
	}
	usage := func(asOf, boot time.Time) attentionInput {
		return attentionInput{Root: true, UsageAsOf: asOf, BootTime: boot, Now: now}
	}
	histories := func(capacity historyStatus) attentionInput {
		return attentionInput{Root: true, BootTime: now, Now: now, Histories: []namedHistory{
			{ID: "capacity", Label: "Capacity history", Status: capacity},
			{ID: "traffic", Label: "Traffic history"},
		}}
	}
	for _, test := range []struct {
		name string
		in   attentionInput
		ids  []string
	}{
		{"no fleet tier drops node and drive items", with(func(in *attentionInput) { in.Fleet = false }),
			[]string{"quota-full:alpha", "history-save:capacity", "quota-near:zeta", "history-load:traffic", "usage-stale"}},
		{"non-root drops usage and history items", with(func(in *attentionInput) { in.Root = false }),
			[]string{"drive-offline:http://node1:9000/mnt/d2", "node-offline:node2:9000", "quota-full:alpha", "quota-near:zeta"}},
		{"scoped caller sees only its quota items", with(func(in *attentionInput) { in.Fleet, in.Root = false, false }),
			[]string{"quota-full:alpha", "quota-near:zeta"}},
		{"backend mode skips usage-stale", with(func(in *attentionInput) { in.Backend = true }),
			[]string{"drive-offline:http://node1:9000/mnt/d2", "node-offline:node2:9000", "quota-full:alpha", "history-save:capacity", "quota-near:zeta", "history-load:traffic"}},
		{"zero quota", quota(500, 0), nil},
		{"89 of 100", quota(89, 100), nil},
		{"90 of 100", quota(90, 100), []string{"quota-near:b"}},
		{"100 of 100", quota(100, 100), []string{"quota-full:b"}},
		{"150 of 100", quota(150, 100), []string{"quota-full:b"}},
		{"fresh server without a scan", usage(time.Time{}, now.Add(-time.Hour)), nil},
		{"booted 25h ago without a scan", usage(time.Time{}, now.Add(-25*time.Hour)), []string{"usage-stale"}},
		{"last scan 25h ago", usage(now.Add(-25*time.Hour), now.Add(-48*time.Hour)), []string{"usage-stale"}},
		{"last scan 23h ago on an old server", usage(now.Add(-23*time.Hour), now.Add(-48*time.Hour)), nil},
		{"last scan exactly 24h ago", usage(now.Add(-24*time.Hour), now.Add(-48*time.Hour)), []string{"usage-stale"}},
		{"healthy histories", histories(historyStatus{}), nil},
		{"one store failing both ways", histories(historyStatus{LoadFailed: true, LastSaveErr: saveErr}),
			[]string{"history-save:capacity", "history-load:capacity"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := attentionIDs(attentionItems(test.in)); !sameStrings(got, test.ids) {
				t.Fatalf("ids = %q, want %q", got, test.ids)
			}
		})
	}

	one := func(used, limit uint64) AttentionItem {
		items := attentionItems(quota(used, limit))
		if len(items) != 1 {
			t.Fatalf("%d of %d: %d items, want 1", used, limit, len(items))
		}
		return items[0]
	}
	near, full := one(90, 100), one(100, 100)
	if near.Severity != attentionWarning || near.Title != "Bucket b is at 90% of its quota" || near.Href != "/buckets/b?tab=settings" {
		t.Fatalf("90 of 100 = %#v", near)
	}
	if full.Severity != attentionCritical || full.Title != "Bucket b reached its quota" || full.Href != "/buckets/b?tab=settings" {
		t.Fatalf("100 of 100 = %#v", full)
	}
	if near.ID == full.ID {
		t.Fatalf("near and full share id %q", near.ID)
	}
	if got := one(52, 56).Title; got != "Bucket b is at 92% of its quota" {
		t.Fatalf("52 of 56 title = %q, want the floored 92%%", got)
	}
}

func TestOfflineDisks(t *testing.T) {
	ok, off := madmin.DriveStateOk, madmin.DriveStateOffline
	for _, test := range []struct {
		name          string
		disks         []madmin.Disk
		nodes, drives []string
	}{
		{
			name: "whole node down is one node item",
			disks: []madmin.Disk{
				{Endpoint: "http://node1:9000/mnt/d1", State: ok},
				{Endpoint: "http://node1:9000/mnt/d2", State: off},
				{Endpoint: "http://node2:9000/mnt/d1", State: off},
				{Endpoint: "http://node2:9000/mnt/d2", State: madmin.DriveStateFaulty},
			},
			nodes:  []string{"node2:9000"},
			drives: []string{"http://node1:9000/mnt/d2"},
		},
		{
			name:   "plain path drive is never a node",
			disks:  []madmin.Disk{{Endpoint: "/data", State: off}},
			drives: []string{"/data"},
		},
		{
			name: "plain path drives next to a healthy one",
			disks: []madmin.Disk{
				{Endpoint: "/data/d1", State: ok},
				{Endpoint: "/data/d2", State: madmin.DriveStateUnformatted},
			},
			drives: []string{"/data/d2"},
		},
		{
			name: "output is sorted",
			disks: []madmin.Disk{
				{Endpoint: "http://node3:9000/mnt/d1", State: off},
				{Endpoint: "http://node1:9000/mnt/d3", State: off},
				{Endpoint: "http://node2:9000/mnt/d1", State: off},
				{Endpoint: "http://node1:9000/mnt/d1", State: ok},
				{Endpoint: "http://node1:9000/mnt/d2", State: off},
			},
			nodes:  []string{"node2:9000", "node3:9000"},
			drives: []string{"http://node1:9000/mnt/d2", "http://node1:9000/mnt/d3"},
		},
		{
			name: "all drives ok",
			disks: []madmin.Disk{
				{Endpoint: "http://node1:9000/mnt/d1", State: ok},
				{Endpoint: "http://node2:9000/mnt/d1", State: ok},
			},
		},
		{name: "no drives"},
	} {
		t.Run(test.name, func(t *testing.T) {
			nodes, drives := offlineDisks(test.disks)
			if !sameStrings(nodes, test.nodes) || !sameStrings(drives, test.drives) {
				t.Fatalf("nodes %q drives %q, want nodes %q drives %q", nodes, drives, test.nodes, test.drives)
			}
		})
	}
}

// Count StorageInfo calls
type countingLayer struct {
	ObjectLayer
	calls int
	disks []madmin.Disk
}

func (l *countingLayer) StorageInfo(ctx context.Context) (StorageInfo, []error) {
	l.calls++
	return StorageInfo{Disks: l.disks}, nil
}

func TestAttentionDiskCache(t *testing.T) {
	if globalAttentionDisks == nil {
		t.Fatal("globalAttentionDisks is nil")
	}
	ctx := context.Background()
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	down := []madmin.Disk{{Endpoint: "http://node1:9000/mnt/d1", State: madmin.DriveStateOffline}}
	up := []madmin.Disk{{Endpoint: "http://node1:9000/mnt/d1", State: madmin.DriveStateOk}}
	layer := &countingLayer{disks: down}
	c := &attentionDiskCache{}
	if got := c.get(ctx, layer, t0); !reflect.DeepEqual(got, down) || layer.calls != 1 {
		t.Fatalf("first get = %v after %d calls, want %v after 1", got, layer.calls, down)
	}
	layer.disks = up
	if got := c.get(ctx, layer, t0.Add(14*time.Second)); !reflect.DeepEqual(got, down) || layer.calls != 1 {
		t.Fatalf("get at 14s = %v after %d calls, want the cached %v after 1", got, layer.calls, down)
	}
	if got := c.get(ctx, layer, t0.Add(attentionDiskTTL)); !reflect.DeepEqual(got, up) || layer.calls != 2 {
		t.Fatalf("get at 15s = %v after %d calls, want %v after 2", got, layer.calls, up)
	}
	if got := c.get(ctx, layer, t0.Add(29*time.Second)); !reflect.DeepEqual(got, up) || layer.calls != 2 {
		t.Fatalf("get at 29s = %v after %d calls, want the cached %v after 2", got, layer.calls, up)
	}
}

// Activity attention items with disk full error
var (
	wantActivityLoadItem = AttentionItem{
		ID:       "activity-load",
		Kind:     "activity",
		Severity: attentionInfo,
		Title:    "Activity log could not be read at startup",
		Detail:   "Some saved events were skipped. New events are recorded.",
		Href:     "/activity",
	}
	wantActivityFlushItem = AttentionItem{
		ID:       "activity-flush",
		Kind:     "activity",
		Severity: attentionWarning,
		Title:    "Activity log is not being saved",
		Detail:   "disk full",
		Href:     "/activity",
	}
)

// Item by ID, false if missing
func findAttention(items []AttentionItem, id string) (AttentionItem, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return AttentionItem{}, false
}

func TestAttentionActivityRules(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	both := activityStatus{LoadFailed: true, LastFlushErr: "disk full"}
	for _, test := range []struct {
		name   string
		root   bool
		status activityStatus
		want   []AttentionItem
	}{
		{"root, healthy log", true, activityStatus{}, []AttentionItem{}},
		{"root, load failed", true, activityStatus{LoadFailed: true}, []AttentionItem{wantActivityLoadItem}},
		{"root, flush failing", true, activityStatus{LastFlushErr: "disk full"}, []AttentionItem{wantActivityFlushItem}},
		{"root, both", true, both, []AttentionItem{wantActivityFlushItem, wantActivityLoadItem}},
		{"not root", false, both, []AttentionItem{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := attentionItems(attentionInput{Root: test.root, BootTime: now, Now: now, Activity: test.status})
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("items = %#v, want %#v", got, test.want)
			}
		})
	}

	pinActivityLog(t)
	ExecObjectLayerTest(t, testAttentionActivityWebHandler)
}

// Root sees activity faults, bucket user sees neither
func testAttentionActivityWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer pinAttentionGlobals(ctx, obj)()
	router := initTestWebRPCEndPoint(obj)
	scoped := makeScopedIAMUser(t, "actattn", makeTestBucket(t, obj, BucketOptions{}))
	// newAllSubsystems replaced pinned log, outer cleanup restores original
	globalActivityLog = newActivityLog("test-node:9000", 16, 16)
	globalActivityLog.loadFailed = true
	globalActivityLog.lastFlushErr = errors.New("disk full")

	items, err := callWebAttention(t, router, globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatalf("%s: root: %v", instanceType, err)
	}
	for _, want := range []AttentionItem{wantActivityFlushItem, wantActivityLoadItem} {
		got, ok := findAttention(items, want.ID)
		if !ok {
			t.Fatalf("%s: root items %q lack %s", instanceType, attentionIDs(items), want.ID)
		}
		if got != want {
			t.Fatalf("%s: root %s = %#v, want %#v", instanceType, want.ID, got, want)
		}
	}

	items, err = callWebAttention(t, router, scoped.AccessKey, scoped.SecretKey)
	if err != nil {
		t.Fatalf("%s: scoped: %v", instanceType, err)
	}
	for _, id := range []string{wantActivityFlushItem.ID, wantActivityLoadItem.ID} {
		if _, ok := findAttention(items, id); ok {
			t.Fatalf("%s: scoped items %q include %s", instanceType, attentionIDs(items), id)
		}
	}
}
