package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestActivitySegmentCodec(t *testing.T) {
	events := []activityEvent{
		storeTestEvent("test-node:9000", 7),
		{Time: time.Date(2026, 10, 9, 12, 0, 0, 5, time.UTC), Node: "test-node:9000", Seq: 8, AccessKey: "obstoradmin", Source: "console", API: "RemoveObject", Bucket: "alpha", Count: 3, Status: 200},
	}
	b, err := encodeActivitySegment(events)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if b[0] != activityFileVersion {
		t.Fatalf("first byte = %d, want version %d", b[0], activityFileVersion)
	}
	got, err := decodeActivitySegment(b)
	if err != nil || !sameStoreEvents(got, events) {
		t.Fatalf("round trip = %+v, %v; want %+v", got, err, events)
	}
	if _, err = decodeActivitySegment(nil); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("empty = %v, want %v", err, io.ErrUnexpectedEOF)
	}
	if _, err = decodeActivitySegment(append([]byte{2}, b[1:]...)); !errors.Is(err, errActivityVersion) {
		t.Fatalf("version 2 = %v, want %v", err, errActivityVersion)
	}
	if _, err = decodeActivitySegment(b[:len(b)/2]); err == nil {
		t.Fatal("truncated body decoded without error")
	}
	if _, err = decodeActivitySegment(b[:1]); err == nil {
		t.Fatal("version byte alone decoded without error")
	}
}

func TestActivitySegmentRollover(t *testing.T) {
	const node = "test-node:9000"
	l := newActivityLog(node, 4096, 16)
	l.persist = true
	for i := 0; i < 2500; i++ {
		l.append(storeTestEvent(node, uint64(i)))
	}
	if len(l.sealed) != 2 || len(l.sealed[0]) != activitySegmentEvents || len(l.sealed[1]) != activitySegmentEvents {
		t.Fatalf("sealed segments = %d, want two of %d events", len(l.sealed), activitySegmentEvents)
	}
	if l.sealed[0][0].Seq != 0 || l.sealed[1][0].Seq != 1000 {
		t.Fatalf("sealed first seqs = %d, %d; want 0, 1000", l.sealed[0][0].Seq, l.sealed[1][0].Seq)
	}
	if len(l.open) != 500 || l.openFirst != 2000 || !l.dirty {
		t.Fatalf("open = %d events from %d, dirty %v; want 500 from 2000, dirty", len(l.open), l.openFirst, l.dirty)
	}
	sum := 0
	for _, e := range l.open {
		sum += activityEventBytes(e)
	}
	if l.openBytes != sum {
		t.Fatalf("openBytes = %d, want %d", l.openBytes, sum)
	}

	// Large access keys reach byte limit before event limit
	big := newActivityLog(node, 4096, 16)
	big.persist = true
	e := activityEvent{AccessKey: strings.Repeat("k", 1000), Source: "s3", API: "PutObject", Bucket: "alpha", Object: "o", Status: 200}
	stored := e
	stored.Node = node
	per := activityEventBytes(stored)
	want := activitySegmentBytes / per
	if want >= activitySegmentEvents {
		t.Fatalf("%d events of %d bytes fill a segment, want fewer than %d", want, per, activitySegmentEvents)
	}
	for i := 0; i < want; i++ {
		big.append(e)
	}
	if len(big.sealed) != 0 {
		t.Fatalf("sealed after %d events of %d bytes, want none", want, per)
	}
	next := big.append(e)
	if len(big.sealed) != 1 || len(big.sealed[0]) != want || len(big.open) != 1 || big.openBytes != per || big.openFirst != next.Seq {
		t.Fatalf("after %d events: %d sealed, open %d with %d bytes from %d; want one sealed of %d and one open of %d bytes from %d",
			want+1, len(big.sealed), len(big.open), big.openBytes, big.openFirst, want, per, next.Seq)
	}
}

func TestActivityOversizedPersistenceEventDropped(t *testing.T) {
	l := newActivityLog("node", 8, 8)
	l.persist = true
	e := storeTestEvent("node", 0)
	l.append(e)
	e.AuthorizationPrefix = strings.Repeat("a", activitySegmentBytes)
	l.append(e)
	e.AuthorizationPrefix = "alpha/"
	l.append(e)
	if len(l.open) != 2 || len(l.sealed) != 0 || l.openBytes > activitySegmentBytes || l.dropped.Load() != 1 {
		t.Fatalf("oversized event: open %d, sealed %d, bytes %d, dropped %d", len(l.open), len(l.sealed), l.openBytes, l.dropped.Load())
	}
	if l.open[1].Seq != 2 || len(l.snapshot()) != 3 {
		t.Fatal("oversized persistence drop lost sequence progress or the in-memory event")
	}
}

func TestActivityPersistReload(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj, dir := newStoreTestFS(t)
	const node = "127.0.0.1:9200"

	first := startStoreLog(ctx, obj, node, activityRingSize)
	for i := 0; i < 1200; i++ {
		first.append(storeTestEvent(node, uint64(i)))
	}
	if err := first.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if st := first.Status(); st != (activityStatus{}) {
		t.Fatalf("status after a good flush = %+v, want zero", st)
	}
	wantNames := []string{storeSegmentName(0), storeSegmentName(1000)}
	if got := storeSegmentNames(t, dir, node); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("segment files = %q, want %q", got, wantNames)
	}

	second := startStoreLog(ctx, obj, node, activityRingSize)
	got := second.snapshot()
	if len(got) != 1200 || !sameStoreEvents([]activityEvent{got[0], got[1199]}, []activityEvent{storeTestEvent(node, 0), storeTestEvent(node, 1199)}) {
		t.Fatalf("reloaded %d events, want 1200 from seq 0 to 1199", len(got))
	}
	second.mu.Lock()
	seq, open, openFirst, persist := second.seq, len(second.open), second.openFirst, second.persist
	second.mu.Unlock()
	if seq != 1200 || open != 200 || openFirst != 1000 || !persist {
		t.Fatalf("reloaded seq %d, open %d from %d, persist %v; want 1200, 200 from 1000, true", seq, open, openFirst, persist)
	}
	if next := second.append(storeTestEvent(node, 0)); next.Seq != 1200 {
		t.Fatalf("next seq = %d, want 1200", next.Seq)
	}
	if err := second.Flush(ctx); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if got := storeSegmentNames(t, dir, node); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("segment files after reopen = %q, want %q", got, wantNames)
	}
	reopened, err := decodeActivitySegment(readStoreFile(t, dir, node, storeSegmentName(1000)))
	if err != nil || len(reopened) != 201 || reopened[200].Seq != 1200 {
		t.Fatalf("reopened segment = %d events, %v; want 201 ending at seq 1200", len(reopened), err)
	}
}

func TestActivityCorruptSegmentSkipped(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	const node = "127.0.0.1:9200"
	for _, test := range []struct {
		name    string
		bad     uint64
		corrupt func(good []byte) []byte
	}{
		{"middle zero bytes", 1000, func([]byte) []byte { return []byte{} }},
		{"middle truncated", 1000, func(b []byte) []byte { return b[:len(b)/2] }},
		{"middle version 2", 1000, func(b []byte) []byte { return append([]byte{2}, b[1:]...) }},
		{"newest zero bytes", 2000, func([]byte) []byte { return []byte{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			obj, dir := newStoreTestFS(t)
			for _, first := range []uint64{0, 1000, 2000} {
				good := writeStoreSegment(t, obj, node, first, activitySegmentEvents)
				if first == test.bad {
					path := filepath.Join(storeSegmentDir(dir, node), storeSegmentName(first))
					if err := os.WriteFile(path, test.corrupt(good), 0o644); err != nil {
						t.Fatalf("corrupt %s: %v", path, err)
					}
				}
			}
			before := map[string][]byte{}
			for _, name := range storeSegmentNames(t, dir, node) {
				before[name] = readStoreFile(t, dir, node, name)
			}

			l := startStoreLog(ctx, obj, node, activityRingSize)
			if st := l.Status(); !st.LoadFailed || st.LastFlushErr != "" {
				t.Fatalf("status = %+v, want LoadFailed only", st)
			}
			got := l.snapshot()
			if len(got) != 2*activitySegmentEvents {
				t.Fatalf("loaded %d events, want %d", len(got), 2*activitySegmentEvents)
			}
			for _, e := range got {
				if e.Seq >= test.bad && e.Seq < test.bad+activitySegmentEvents {
					t.Fatalf("seq %d came from the corrupt segment", e.Seq)
				}
			}
			if next := l.append(storeTestEvent(node, 0)); next.Seq != 3000 {
				t.Fatalf("next seq = %d, want 3000 (above every file)", next.Seq)
			}
			if err := l.Flush(ctx); err != nil {
				t.Fatalf("flush: %v", err)
			}
			want := []string{storeSegmentName(0), storeSegmentName(1000), storeSegmentName(2000), storeSegmentName(3000)}
			if names := storeSegmentNames(t, dir, node); !reflect.DeepEqual(names, want) {
				t.Fatalf("segment files = %q, want %q", names, want)
			}
			for name, b := range before {
				if !bytes.Equal(readStoreFile(t, dir, node, name), b) {
					t.Fatalf("existing file %s changed", name)
				}
			}
		})
	}
}

func TestActivityTwoNodesPersist(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj, dir := newStoreTestFS(t)
	nodes := []string{"10.0.0.1:9000", "10.0.0.2:9000"}
	for i, node := range nodes {
		l := startStoreLog(ctx, obj, node, 1024)
		for j := 0; j < 3; j++ {
			e := storeTestEvent(node, uint64(j))
			e.Bucket = fmt.Sprintf("node%d", i+1)
			l.append(e)
		}
		if err := l.Flush(ctx); err != nil {
			t.Fatalf("%s flush: %v", node, err)
		}
	}
	for i, node := range nodes {
		if fi, err := os.Stat(storeSegmentDir(dir, node)); err != nil || !fi.IsDir() {
			t.Fatalf("%s segment directory: %v", node, err)
		}
		if names := storeSegmentNames(t, dir, node); !reflect.DeepEqual(names, []string{storeSegmentName(0)}) {
			t.Fatalf("%s segment files = %q, want one file from seq 0", node, names)
		}
		got := startStoreLog(ctx, obj, node, 1024).snapshot()
		if len(got) != 3 {
			t.Fatalf("%s reloaded %d events, want 3", node, len(got))
		}
		for _, e := range got {
			if e.Node != node || e.Bucket != fmt.Sprintf("node%d", i+1) {
				t.Fatalf("%s reloaded %+v, want only its own events", node, e)
			}
		}
	}
}

func TestActivityPruneSegments(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj, dir := newStoreTestFS(t)
	const node = "127.0.0.1:9200"
	total := (activitySegmentsKept + 1) * activitySegmentEvents

	l := startStoreLog(ctx, obj, node, activityRingSize)
	for i := 0; i < total; i++ {
		l.append(storeTestEvent(node, uint64(i)))
	}
	if len(l.sealed) != activitySegmentsKept || len(l.open) != 0 {
		t.Fatalf("%d sealed and %d open events, want %d sealed and none open", len(l.sealed), len(l.open), activitySegmentsKept)
	}
	if err := l.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	names := storeSegmentNames(t, dir, node)
	if len(names) != activitySegmentsKept || names[0] != storeSegmentName(activitySegmentEvents) ||
		names[len(names)-1] != storeSegmentName(uint64(total-activitySegmentEvents)) {
		t.Fatalf("segment files = %q, want %d from %s", names, activitySegmentsKept, storeSegmentName(activitySegmentEvents))
	}
	if len(l.segments) != activitySegmentsKept || l.segments[0] != activitySegmentEvents {
		t.Fatalf("tracked segments = %v, want %d from %d", l.segments, activitySegmentsKept, activitySegmentEvents)
	}

	reloaded := startStoreLog(ctx, obj, node, activityRingSize)
	got := reloaded.snapshot()
	if len(got) != activitySegmentsKept*activitySegmentEvents || got[0].Seq != activitySegmentEvents {
		t.Fatalf("reloaded %d events, want %d from seq %d", len(got), activitySegmentsKept*activitySegmentEvents, activitySegmentEvents)
	}
	if next := reloaded.append(storeTestEvent(node, 0)); next.Seq != uint64(total) {
		t.Fatalf("next seq = %d, want %d", next.Seq, total)
	}
}

func TestActivityBackendMemoryOnly(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	globalIsBackend = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj, dir := newStoreTestFS(t)
	const node = "127.0.0.1:9200"

	l := startStoreLog(ctx, obj, node, 1024)
	for i := 0; i < 3; i++ {
		l.append(storeTestEvent(node, uint64(i)))
	}
	if err := l.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, obstorMetaBucket, "activity")); !os.IsNotExist(err) {
		t.Fatalf("activity directory stat = %v, want not exist", err)
	}
	if got := l.snapshot(); len(got) != 3 {
		t.Fatalf("memory holds %d events, want 3", len(got))
	}
	if st := l.Status(); st != (activityStatus{}) || l.persist {
		t.Fatalf("status %+v, persist %v; want zero status and no persistence", st, l.persist)
	}
}

func TestActivityListFailureMemoryOnly(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj, dir := newStoreTestFS(t)
	const node = "127.0.0.1:9200"
	saved := writeStoreSegment(t, obj, node, 0, 5)
	writeStoreSegment(t, obj, node, 1000, 5)

	// First listing page succeeds, next page fails
	layer := &storeFailLayer{ObjectLayer: obj, listFailAfter: 1}
	l := startStoreLog(ctx, layer, node, 1024)
	l.mu.Lock()
	persist := l.persist
	l.mu.Unlock()
	if layer.listCalls != 2 {
		t.Fatalf("listing made %d calls, want 2 (one page, then the failure)", layer.listCalls)
	}
	if persist || !l.Status().LoadFailed {
		t.Fatalf("persist %v, status %+v after a failed listing; want memory only and LoadFailed", persist, l.Status())
	}
	l.append(storeTestEvent(node, 0))
	if err := l.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := readStoreFile(t, dir, node, storeSegmentName(0)); !bytes.Equal(got, saved) {
		t.Fatal("segment 0 was rewritten after a failed listing")
	}
}

func TestActivitySealedRetryCapped(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj, _ := newStoreTestFS(t)
	const node = "127.0.0.1:9200"

	l := startStoreLog(ctx, &storeFailLayer{ObjectLayer: obj, failPut: true}, node, 1024)
	extra := 2
	for i := 0; i < (activitySegmentsKept+extra)*activitySegmentEvents; i++ {
		l.append(storeTestEvent(node, uint64(i)))
	}
	if err := l.Flush(ctx); err == nil {
		t.Fatal("flush with failing writes returned nil")
	}
	l.mu.Lock()
	sealed, firstSeq := len(l.sealed), l.sealed[0][0].Seq
	l.mu.Unlock()
	if sealed != activitySegmentsKept || firstSeq != uint64(extra*activitySegmentEvents) {
		t.Fatalf("%d sealed from seq %d, want %d from %d", sealed, firstSeq, activitySegmentsKept, extra*activitySegmentEvents)
	}
	if d := l.dropped.Load(); d != uint64(extra*activitySegmentEvents) {
		t.Fatalf("dropped = %d, want %d", d, extra*activitySegmentEvents)
	}
	if l.Status().LastFlushErr == "" {
		t.Fatal("status lacks the flush error")
	}
}

func TestActivityPendingBoundDuringBlockedFlush(t *testing.T) {
	defer pinBucketGlobals(FSTestStr)()
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("writeFailure=%t", fail), func(t *testing.T) {
			obj, _ := newStoreTestFS(t)
			const node = "blocked:9000"
			layer := &pausedStoreLayer{
				ObjectLayer: &storeFailLayer{ObjectLayer: obj, failPut: fail},
				key:         activitySegmentPath(activityNodeKey(node), 0),
				entered:     make(chan context.Context, 1), release: make(chan struct{}),
			}
			l := newActivityLog(node, 1024, 16)
			l.objAPI, l.nodeKey, l.persist = layer, activityNodeKey(node), true
			for i := 0; i < activitySegmentsKept*activitySegmentEvents; i++ {
				l.append(storeTestEvent(node, uint64(i)))
			}
			flushed := make(chan error, 1)
			go func() { flushed <- l.Flush(context.Background()) }()
			awaitStoreWrite(t, layer.entered)
			defer layer.unblock()

			// Check every rollover while disk write blocks so backlog stays bounded before flush ends
			arrivals := make(chan error, 1)
			const extra = 10
			go func() {
				for i := 0; i < (activitySegmentsKept+extra)*activitySegmentEvents; i++ {
					l.append(storeTestEvent(node, uint64(i)))
					if i%activitySegmentEvents == activitySegmentEvents-1 {
						l.mu.Lock()
						n := len(l.sealed)
						pendingBytes := l.openBytes
						for _, seg := range l.sealed {
							for _, e := range seg {
								pendingBytes += activityEventBytes(e)
							}
						}
						l.mu.Unlock()
						if n > activitySegmentsKept || pendingBytes > (activitySegmentsKept+1)*activitySegmentBytes {
							arrivals <- fmt.Errorf("pending segments = %d, bytes = %d; limits %d and %d", n, pendingBytes, activitySegmentsKept, (activitySegmentsKept+1)*activitySegmentBytes)
							return
						}
					}
				}
				arrivals <- nil
			}()
			select {
			case err := <-arrivals:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("appending waited on the blocked persistence write")
			}
			if got := l.dropped.Load(); got != extra*activitySegmentEvents {
				t.Fatalf("dropped while blocked = %d, want %d", got, extra*activitySegmentEvents)
			}
			layer.unblock()
			err := awaitStoreSave(t, flushed)
			if (err != nil) != fail {
				t.Fatalf("flush error = %v, want failure %t", err, fail)
			}
			wantDropped := uint64(extra * activitySegmentEvents)
			if fail {
				wantDropped += activitySegmentsKept * activitySegmentEvents
			}
			l.mu.Lock()
			n, first, dirty := len(l.sealed), l.sealed[0][0].Seq, l.dirty
			l.objAPI = obj
			l.mu.Unlock()
			wantFirst := uint64((activitySegmentsKept + extra) * activitySegmentEvents)
			if n != activitySegmentsKept || first != wantFirst || !dirty || l.dropped.Load() != wantDropped {
				t.Fatalf("after flush: pending %d from %d, dirty %t, dropped %d; want %d from %d, dirty, dropped %d",
					n, first, dirty, l.dropped.Load(), activitySegmentsKept, wantFirst, wantDropped)
			}
			if err := l.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(l.sealed) != 0 || len(l.segments) != activitySegmentsKept || l.dropped.Load() != wantDropped {
				t.Fatalf("recovery: pending %d, saved %d, dropped %d", len(l.sealed), len(l.segments), l.dropped.Load())
			}
		})
	}
}

// Object layer with configurable listing and write failures
type storeFailLayer struct {
	ObjectLayer
	listFailAfter int
	listCalls     int
	failPut       bool
}

var errStoreFail = errors.New("store test failure")

func (l *storeFailLayer) ListObjects(ctx context.Context, bucket, prefix, marker, delimiter string, maxKeys int) (ListObjectsInfo, error) {
	l.listCalls++
	if l.listFailAfter > 0 {
		if l.listCalls > l.listFailAfter {
			return ListObjectsInfo{}, errStoreFail
		}
		maxKeys = 1
	}
	return l.ObjectLayer.ListObjects(ctx, bucket, prefix, marker, delimiter, maxKeys)
}

func (l *storeFailLayer) PutObject(ctx context.Context, bucket, object string, data *PutObjReader, opts ObjectOptions) (ObjectInfo, error) {
	if l.failPut {
		return ObjectInfo{}, errStoreFail
	}
	return l.ObjectLayer.PutObject(ctx, bucket, object, data, opts)
}

// Stored event with node and seq, time advances one second per seq
func storeTestEvent(node string, seq uint64) activityEvent {
	return activityEvent{
		Time:      time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).Add(time.Duration(seq) * time.Second),
		Node:      node,
		Seq:       seq,
		AccessKey: "obstoradmin",
		Source:    "s3",
		API:       "PutObject",
		Bucket:    "alpha",
		Object:    fmt.Sprintf("obj-%d.txt", seq),
		Status:    200,
	}
}

// Equal events, times compared with Equal
func sameStoreEvents(a, b []activityEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if !x.Time.Equal(y.Time) {
			return false
		}
		x.Time = y.Time
		if x != y {
			return false
		}
	}
	return true
}

// FS object layer cleaned up after test
func newStoreTestFS(t *testing.T) (ObjectLayer, string) {
	t.Helper()
	obj, dir, err := prepareFS()
	if err != nil {
		t.Fatalf("prepare FS: %v", err)
	}
	t.Cleanup(func() {
		_ = obj.Shutdown(context.Background())
		_ = os.RemoveAll(dir)
	})
	return obj, dir
}

// Node segment directory on FS
func storeSegmentDir(dir, node string) string {
	return filepath.Join(dir, obstorMetaBucket, "activity", activityNodeKey(node))
}

// Segment filename from first seq
func storeSegmentName(first uint64) string {
	return fmt.Sprintf("%020d.bin", first)
}

// Sorted segment names, nil if directory missing
func storeSegmentNames(t *testing.T, dir, node string) []string {
	t.Helper()
	entries, err := os.ReadDir(storeSegmentDir(dir, node))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read segment directory: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// Raw segment bytes
func readStoreFile(t *testing.T, dir, node, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(storeSegmentDir(dir, node), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// Save n events from first seq as one segment, return bytes
func writeStoreSegment(t *testing.T, obj ObjectLayer, node string, first uint64, n int) []byte {
	t.Helper()
	events := make([]activityEvent, n)
	for i := range events {
		events[i] = storeTestEvent(node, first+uint64(i))
	}
	b, err := encodeActivitySegment(events)
	if err != nil {
		t.Fatalf("encode segment %d: %v", first, err)
	}
	if err = saveConfig(context.Background(), obj, activitySegmentPath(activityNodeKey(node), first), b); err != nil {
		t.Fatalf("save segment %d: %v", first, err)
	}
	return b
}

// Start node log on obj, ctx stops goroutines
func startStoreLog(ctx context.Context, obj ObjectLayer, node string, ringSize int) *activityLog {
	l := newActivityLog(node, ringSize, 16)
	l.Start(ctx, obj)
	return l
}
