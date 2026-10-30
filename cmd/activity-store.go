package cmd

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/obstor/obstor/cmd/logger"
)

const (
	// Seal segment at event or byte limit
	activitySegmentEvents = 1000
	activitySegmentBytes  = 1 << 20
	// Segments kept per node, Flush removes older files
	activitySegmentsKept = 50
	// Flush interval for unsaved events
	activityFlushInterval = 30 * time.Second
	// Segment file version byte
	activityFileVersion byte = 1
)

var errActivityVersion = errors.New("activity segment has an unknown version")

// Node directory key, same as history files
func activityNodeKey(node string) string {
	return historyNodeKey(node)
}

// Node's segment directory under obstorMetaBucket
func activitySegmentDir(nodeKey string) string {
	return "activity/" + nodeKey + "/"
}

// Segment named by first seq so files sort by age
func activitySegmentPath(nodeKey string, firstSeq uint64) string {
	return fmt.Sprintf("%s%020d.bin", activitySegmentDir(nodeKey), firstSeq)
}

// First seq from segment name, false for invalid names
func parseActivitySegmentName(name string) (uint64, bool) {
	digits, ok := strings.CutSuffix(name, ".bin")
	if !ok || len(digits) != 20 {
		return 0, false
	}
	first, err := strconv.ParseUint(digits, 10, 64)
	return first, err == nil
}

// Version byte followed by gob events
func encodeActivitySegment(events []activityEvent) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(activityFileVersion)
	if err := gob.NewEncoder(&buf).Encode(events); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode segment, reject empty, unknown or damaged data
func decodeActivitySegment(b []byte) ([]activityEvent, error) {
	if len(b) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if b[0] != activityFileVersion {
		return nil, errActivityVersion
	}
	var events []activityEvent
	if err := gob.NewDecoder(bytes.NewReader(b[1:])).Decode(&events); err != nil {
		return nil, err
	}
	return events, nil
}

// Event size estimate for segment byte limit
func activityEventBytes(e activityEvent) int {
	return len(e.Node) + len(e.AccessKey) + len(e.Source) + len(e.API) + len(e.Bucket) + len(e.Object) + len(e.AuthorizationPrefix) + 64
}

// Persistence status for attention items
type activityStatus struct {
	LoadFailed   bool
	LastFlushErr string
}

// Append stored event and seal at either limit, caller holds mu
func (l *activityLog) feedSegment(e activityEvent) {
	size := activityEventBytes(e)
	if size > activitySegmentBytes {
		// Keep oversized events in memory, never truncate authorization data for persistence
		l.dropped.Add(1)
		return
	}
	if len(l.open) > 0 && l.openBytes+size > activitySegmentBytes {
		l.sealSegment()
	}
	if len(l.open) == 0 {
		l.openFirst = e.Seq
	}
	l.open = append(l.open, e)
	l.openBytes += size
	l.dirty = true
	if len(l.open) >= activitySegmentEvents || l.openBytes >= activitySegmentBytes {
		l.sealSegment()
	}
}

func (l *activityLog) sealSegment() {
	l.sealed = append(l.sealed, l.open)
	l.open, l.openBytes = nil, 0
	l.trimPendingSegments()
}

// Bound pending segments while Flush blocks, flusher holds one extra snapshot, caller holds mu
func (l *activityLog) trimPendingSegments() {
	if extra := len(l.sealed) - activitySegmentsKept; extra > 0 {
		for _, seg := range l.sealed[:extra] {
			l.dropped.Add(uint64(len(seg)))
		}
		// Clear discarded references in backing array too
		l.sealed = slices.Delete(l.sealed, 0, extra)
	}
}

// Load saved segments, start flusher if persistent, then appender
func (l *activityLog) Start(ctx context.Context, objAPI ObjectLayer) {
	if l == nil {
		return
	}
	if objAPI != nil && !globalIsBackend {
		l.flushMu.Lock()
		l.mu.Lock()
		l.objAPI, l.nodeKey = objAPI, activityNodeKey(l.node)
		l.mu.Unlock()
		err := l.load(ctx)
		l.mu.Lock()
		// Failed listing keeps run in memory to avoid overwriting unseen files
		l.persist = err == nil
		l.loadFailed = l.loadFailed || err != nil
		l.mu.Unlock()
		l.flushMu.Unlock()
		if err != nil {
			logger.LogOnceIf(ctx, fmt.Errorf("activity log kept in memory, segments could not be listed: %w", err), "activity-load")
		} else {
			go l.flusher(ctx)
		}
	}
	go l.run(ctx)
}

// Restore newest segments, continue seq past all files, caller holds flushMu
func (l *activityLog) load(ctx context.Context) error {
	dir := activitySegmentDir(l.nodeKey)
	// Use ListObjects so listing errors cannot reuse saved seq names
	var firsts []uint64
	for marker := ""; ; {
		res, err := l.objAPI.ListObjects(ctx, obstorMetaBucket, dir, marker, "", maxObjectList)
		if err != nil {
			return err
		}
		for _, oi := range res.Objects {
			if first, ok := parseActivitySegmentName(strings.TrimPrefix(oi.Name, dir)); ok {
				firsts = append(firsts, first)
			}
		}
		if !res.IsTruncated {
			break
		}
		marker = res.NextMarker
	}
	slices.Sort(firsts)
	read := firsts
	if len(read) > activitySegmentsKept {
		read = read[len(read)-activitySegmentsKept:]
	}

	var (
		events []activityEvent
		newest []activityEvent
		next   uint64
		failed bool
	)
	for i, first := range read {
		path := activitySegmentPath(l.nodeKey, first)
		data, err := readConfig(ctx, l.objAPI, path)
		var seg []activityEvent
		if err == nil {
			seg, err = decodeActivitySegment(data)
		}
		if err != nil {
			logger.LogOnceIf(ctx, fmt.Errorf("activity segment %s skipped: %w", path, err), "activity-load:"+path)
			failed = true
			// Unreadable segment reserves activitySegmentEvents seqs from its name
			next = max(next, first+activitySegmentEvents)
			continue
		}
		events = append(events, seg...)
		next = max(next, first+1)
		for _, e := range seg {
			next = max(next, e.Seq+1)
		}
		if i == len(read)-1 {
			newest = seg
		}
	}
	newestBytes := 0
	for _, e := range newest {
		newestBytes += activityEventBytes(e)
	}
	reopen := len(newest) > 0 && len(newest) < activitySegmentEvents && newestBytes < activitySegmentBytes &&
		newest[len(newest)-1].Seq+1 == next

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(events) > len(l.ring) {
		events = events[len(events)-len(l.ring):]
	}
	l.next, l.size = 0, 0
	for _, e := range events {
		l.putRing(e)
	}
	l.seq = next
	if reopen {
		l.open, l.openFirst, l.openBytes = newest, read[len(read)-1], newestBytes
	}
	l.loadFailed = failed
	l.segments = firsts
	return nil
}

// Save sealed and changed open segments, prune oldest files
func (l *activityLog) Flush(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.flushMu.Lock()
	defer l.flushMu.Unlock()
	l.drain()

	l.mu.Lock()
	if !l.persist {
		l.mu.Unlock()
		return nil
	}
	objAPI, nodeKey := l.objAPI, l.nodeKey
	sealed := l.sealed
	l.sealed = nil
	var open []activityEvent
	if l.dirty && len(l.open) > 0 {
		open = append([]activityEvent(nil), l.open...)
	}
	openFirst := l.openFirst
	l.dirty = false
	l.mu.Unlock()

	var firstErr error
	keep := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	var retry [][]activityEvent
	for _, seg := range sealed {
		if err := l.writeSegment(ctx, objAPI, nodeKey, seg[0].Seq, seg); err != nil {
			keep(err)
			retry = append(retry, seg)
		}
	}
	openFailed := false
	if len(open) > 0 {
		if err := l.writeSegment(ctx, objAPI, nodeKey, openFirst, open); err != nil {
			keep(err)
			openFailed = true
		}
	}
	for len(l.segments) > activitySegmentsKept {
		err := deleteConfig(ctx, objAPI, activitySegmentPath(nodeKey, l.segments[0]))
		if err != nil && !errors.Is(err, errConfigNotFound) {
			keep(err)
			break
		}
		l.segments = l.segments[1:]
	}

	l.mu.Lock()
	l.sealed = append(retry, l.sealed...)
	l.trimPendingSegments()
	if len(retry) > 0 || openFailed {
		l.dirty = true
	}
	l.lastFlushErr = firstErr
	l.mu.Unlock()
	return firstErr
}

// Save segment and track first seq, caller holds flushMu
func (l *activityLog) writeSegment(ctx context.Context, objAPI ObjectLayer, nodeKey string, first uint64, events []activityEvent) error {
	data, err := encodeActivitySegment(events)
	if err != nil {
		return err
	}
	if err = saveConfig(ctx, objAPI, activitySegmentPath(nodeKey, first), data); err != nil {
		return err
	}
	if i, found := slices.BinarySearch(l.segments, first); !found {
		l.segments = slices.Insert(l.segments, i, first)
	}
	return nil
}

// Flush unsaved events each interval until ctx ends
func (l *activityLog) flusher(ctx context.Context) {
	ticker := time.NewTicker(activityFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			dirty := l.dirty
			l.mu.Unlock()
			if dirty {
				logger.LogOnceIf(ctx, l.Flush(ctx), "activity-flush")
			}
		}
	}
}

// Load and save status, zero for nil or memory-only log
func (l *activityLog) Status() activityStatus {
	if l == nil {
		return activityStatus{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := activityStatus{LoadFailed: l.loadFailed}
	if l.lastFlushErr != nil {
		st.LastFlushErr = l.lastFlushErr.Error()
	}
	return st
}
