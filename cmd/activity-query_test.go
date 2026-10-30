package cmd

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMergeActivityOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	at := func(sec int, node string, seq uint64) activityEvent {
		return activityEvent{Time: t0.Add(time.Duration(sec) * time.Second), Node: node, Seq: seq, API: "PutObject", Bucket: "alpha"}
	}
	// Unsorted peer page with ties across and within nodes
	pages := []activityPage{
		{Events: []activityEvent{at(3, "a", 6), at(3, "a", 5), at(1, "a", 4)}},
		{Events: []activityEvent{at(0, "b", 1), at(3, "b", 2), at(2, "b", 9)}},
	}
	keys := func(events []activityEvent) string {
		out := make([]string, 0, len(events))
		for _, e := range events {
			out = append(out, fmt.Sprintf("%d/%s/%d", int(e.Time.Sub(t0)/time.Second), e.Node, e.Seq))
		}
		return strings.Join(out, " ")
	}
	all := "3/b/2 3/a/6 3/a/5 2/b/9 1/a/4 0/b/1"

	events, next := mergeActivity(pages, 10)
	if got := keys(events); got != all || next != nil {
		t.Fatalf("limit 10 = %q next %v, want %q and nil", got, next, all)
	}
	events, next = mergeActivity(pages, 6)
	if got := keys(events); got != all || next != nil {
		t.Fatalf("limit 6 = %q next %v, want %q and nil", got, next, all)
	}
	events, next = mergeActivity(pages, 3)
	if got := keys(events); got != "3/b/2 3/a/6 3/a/5" {
		t.Fatalf("limit 3 = %q, want the three newest", got)
	}
	if next == nil || !next.Time.Equal(t0.Add(3*time.Second)) || next.Node != "a" || next.Seq != 5 {
		t.Fatalf("limit 3 next = %+v, want the cursor of 3/a/5", next)
	}
	events, next = mergeActivity(nil, 50)
	if events == nil || len(events) != 0 || next != nil {
		t.Fatalf("no pages = %v next %v, want an empty non-nil slice and nil", events, next)
	}
}

// Seven equal timestamps across two nodes, three per page, no repeats or gaps
func TestMergeActivityTieBoundary(t *testing.T) {
	stamp := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	a := newActivityLog("node-a:9000", 16, 16)
	b := newActivityLog("node-b:9000", 16, 16)
	for i := 0; i < 4; i++ {
		a.append(activityEvent{Time: stamp, AccessKey: "k", Source: "s3", API: "PutObject", Bucket: "alpha", Object: fmt.Sprintf("a%d", i), Status: 200})
	}
	for i := 0; i < 3; i++ {
		b.append(activityEvent{Time: stamp, AccessKey: "k", Source: "s3", API: "PutObject", Bucket: "alpha", Object: fmt.Sprintf("b%d", i), Status: 200})
	}

	const limit = 3
	f := activityFilter{AllBuckets: true, Kinds: []activityKind{activityKindObject}, Limit: limit + 1}
	var sizes []int
	var got []string
	for page := 0; page < 10; page++ {
		events, next := mergeActivity([]activityPage{a.query(f), b.query(f)}, limit)
		sizes = append(sizes, len(events))
		for _, e := range events {
			got = append(got, fmt.Sprintf("%s/%d", e.Node, e.Seq))
		}
		if next == nil {
			break
		}
		f.Before = next
	}
	if fmt.Sprint(sizes) != "[3 3 1]" {
		t.Fatalf("page sizes = %v, want [3 3 1]", sizes)
	}
	want := "node-b:9000/2 node-b:9000/1 node-b:9000/0 node-a:9000/3 node-a:9000/2 node-a:9000/1 node-a:9000/0"
	if strings.Join(got, " ") != want {
		t.Fatalf("pages = %q, want %q", strings.Join(got, " "), want)
	}
}

// Fake peer returns page, error or timeout
type fakeActivityPeer struct {
	page  activityPage
	err   error
	block bool
	got   activityFilter
}

func (p *fakeActivityPeer) ListActivity(ctx context.Context, f activityFilter) (activityPage, error) {
	p.got = f
	if p.block {
		<-ctx.Done()
		return activityPage{}, ctx.Err()
	}
	return p.page, p.err
}

func TestActivityFilterGob(t *testing.T) {
	at := time.Date(2026, 10, 9, 12, 30, 0, 123456789, time.UTC)
	in := activityFilter{
		Buckets:    []string{"alpha", "beta"},
		Kinds:      []activityKind{activityKindObject, activityKindBucket, activityKindUser},
		AccessKeys: []string{"scoped"},
		Group:      "delete",
		Before:     &ActivityCursor{Time: at, Node: "127.0.0.1:9300", Seq: 41},
		Limit:      51,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(in); err != nil {
		t.Fatalf("encode filter: %v", err)
	}
	var out activityFilter
	if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
		t.Fatalf("decode filter: %v", err)
	}
	if out.AllBuckets || out.Group != in.Group || out.Limit != in.Limit ||
		!reflect.DeepEqual(out.Buckets, in.Buckets) || !reflect.DeepEqual(out.Kinds, in.Kinds) ||
		!reflect.DeepEqual(out.AccessKeys, in.AccessKeys) {
		t.Fatalf("filter = %+v, want %+v", out, in)
	}
	if out.Before == nil || !out.Before.Time.Equal(at) || out.Before.Time.Nanosecond() != 123456789 ||
		out.Before.Node != in.Before.Node || out.Before.Seq != in.Before.Seq {
		t.Fatalf("cursor = %+v, want %+v", out.Before, in.Before)
	}

	buf.Reset()
	if err := gob.NewEncoder(&buf).Encode(activityFilter{AllBuckets: true, Kinds: []activityKind{activityKindObject}, Limit: 201}); err != nil {
		t.Fatalf("encode all-buckets filter: %v", err)
	}
	var all activityFilter
	if err := gob.NewDecoder(&buf).Decode(&all); err != nil {
		t.Fatalf("decode all-buckets filter: %v", err)
	}
	if !all.AllBuckets || all.Before != nil || all.Limit != 201 || len(all.Buckets) != 0 {
		t.Fatalf("all-buckets filter = %+v", all)
	}

	page := activityPage{
		Events: []activityEvent{{Time: at, Node: "127.0.0.1:9300", Seq: 41, AccessKey: "scoped", Source: "s3",
			API: "DeleteMultipleObjects", Bucket: "alpha", Count: 3, Status: 200}},
		Denied:  2,
		Dropped: 1,
	}
	buf.Reset()
	if err := gob.NewEncoder(&buf).Encode(page); err != nil {
		t.Fatalf("encode page: %v", err)
	}
	var back activityPage
	if err := gob.NewDecoder(&buf).Decode(&back); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(back.Events) != 1 || back.Denied != 2 || back.Dropped != 1 {
		t.Fatalf("page = %+v, want %+v", back, page)
	}
	got, want := back.Events[0], page.Events[0]
	if !got.Time.Equal(want.Time) || got.Time.Nanosecond() != want.Time.Nanosecond() {
		t.Fatalf("event time = %v, want %v", got.Time, want.Time)
	}
	got.Time = want.Time
	if got != want {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
}

func TestGatherPeerActivity(t *testing.T) {
	l := pinActivityLog(t)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	l.append(activityEvent{Time: sec(3), AccessKey: "obstoradmin", Source: "s3", API: "PutObject", Bucket: "alpha", Object: "a.txt", Status: 200})
	l.append(activityEvent{Time: sec(1), AccessKey: "obstoradmin", Source: "s3", API: "PutObject", Bucket: "alpha", Object: "b.txt", Status: 200})
	l.submit(activityEvent{API: "PutObject", Bucket: "alpha", Object: "anon.txt", Status: 403})

	ev := func(at time.Time, node string, seq uint64) activityEvent {
		return activityEvent{Time: at, Node: node, Seq: seq, AccessKey: "obstoradmin", Source: "s3",
			API: "PutObject", Bucket: "alpha", Object: "x.txt", Status: 200}
	}
	b := &fakeActivityPeer{page: activityPage{Events: []activityEvent{ev(sec(2), "b", 0), ev(sec(1), "b", 1)}, Denied: 2}}
	c := &fakeActivityPeer{page: activityPage{Events: []activityEvent{ev(sec(3), "c", 5), ev(sec(1), "c", 4)}, Denied: 3, Dropped: 1}}
	f := activityFilter{AllBuckets: true, Kinds: []activityKind{activityKindObject, activityKindBucket}, Limit: 11}

	pages, missing := gatherPeerActivity(context.Background(), []activityPeer{b, c}, f, time.Second)
	if missing != 0 || len(pages) != 2 {
		t.Fatalf("gather = %d pages, %d missing; want 2 pages, 0 missing", len(pages), missing)
	}
	if b.got.Limit != 11 || !c.got.AllBuckets || !reflect.DeepEqual(c.got.Kinds, f.Kinds) {
		t.Fatalf("peers got filters %+v and %+v, want %+v", b.got, c.got, f)
	}

	order := func(events []activityEvent) []string {
		out := make([]string, len(events))
		for i, e := range events {
			out[i] = fmt.Sprintf("%s/%d", e.Node, e.Seq)
		}
		return out
	}
	// Equal times sort node names descending, local node leads
	want := []string{"test-node:9000/0", "c/5", "b/0", "test-node:9000/1", "c/4", "b/1"}
	res := activityResultFrom(globalActivityLog.query(f), pages, missing, 10)
	if got := order(res.Events); !reflect.DeepEqual(got, want) || res.Next != nil {
		t.Fatalf("merged = %v next %+v, want %v and no next", got, res.Next, want)
	}
	if res.Partial || res.MissingNodes != 0 || res.Denied != 6 || res.Dropped != 1 {
		t.Fatalf("result partial %v missing %d denied %d dropped %d; want false 0 6 1",
			res.Partial, res.MissingNodes, res.Denied, res.Dropped)
	}

	// Cursor splits three-node tie without repeats or gaps
	cut := activityResultFrom(globalActivityLog.query(f), pages, missing, 4)
	if got := order(cut.Events); !reflect.DeepEqual(got, want[:4]) || cut.Next == nil || *cut.Next != res.Events[3].cursor() {
		t.Fatalf("cut = %v next %+v, want %v and next at %s", got, cut.Next, want[:4], want[3])
	}
	for i, e := range res.Events {
		if older := activityOlder(e, *cut.Next); older != (i >= 4) {
			t.Fatalf("event %s older than the cursor = %v, want %v", want[i], older, i >= 4)
		}
	}
}

func TestGatherPeerActivityTimeout(t *testing.T) {
	if activityPeerTimeout != 2*time.Second {
		t.Fatalf("activityPeerTimeout = %v, want 2s", activityPeerTimeout)
	}
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	answer := &fakeActivityPeer{page: activityPage{Events: []activityEvent{{Time: at, Node: "b", AccessKey: "obstoradmin",
		Source: "s3", API: "PutObject", Bucket: "alpha", Object: "a.txt", Status: 200}}, Denied: 1}}
	slow := &fakeActivityPeer{block: true}
	f := activityFilter{AllBuckets: true, Kinds: []activityKind{activityKindObject}, Limit: 51}

	start := time.Now()
	pages, missing := gatherPeerActivity(context.Background(), []activityPeer{slow, answer}, f, 50*time.Millisecond)
	if took := time.Since(start); took >= time.Second {
		t.Fatalf("gather took %v with a 50ms timeout", took)
	}
	if missing != 1 || len(pages) != 1 || len(pages[0].Events) != 1 || pages[0].Events[0].Node != "b" {
		t.Fatalf("gather = %+v, %d missing; want the answering page and 1 missing", pages, missing)
	}
	res := activityResultFrom(activityPage{}, pages, missing, 50)
	if !res.Partial || res.MissingNodes != 1 || len(res.Events) != 1 || res.Denied != 1 || res.Next != nil {
		t.Fatalf("result = %+v; want partial, 1 missing node, 1 event, denied 1", res)
	}

	failing := &fakeActivityPeer{err: errors.New("connection refused")}
	pages, missing = gatherPeerActivity(context.Background(), []activityPeer{answer, failing}, f, time.Second)
	if missing != 1 || len(pages) != 1 || pages[0].Denied != 1 {
		t.Fatalf("with a failing peer: %+v, %d missing; want the answering page and 1 missing", pages, missing)
	}

	pages, missing = gatherPeerActivity(context.Background(), nil, f, time.Second)
	if missing != 0 || len(pages) != 0 {
		t.Fatalf("no peers: %+v, %d missing; want nothing", pages, missing)
	}
}
