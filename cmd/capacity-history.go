package cmd

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/obstor/obstor/pkg/madmin"
)

const (
	capacityHourlySlots = 168
	capacityDailySlots  = 30
	maxTrackedBuckets   = 1000
)

// Hourly capacity and health, Set marks sampled hours
type capacityBucket struct {
	Used, Objects, Buckets, Free, Total                uint64
	DrivesOnline, DrivesTotal, NodesOnline, NodesTotal uint64
	Set                                                bool
}

// Bucket bytes and object count
type usagePoint struct {
	Size, Objects uint64
}

// Bucket history, DailyDay tracks UTC day per daily slot
type bucketSeries struct {
	FirstHour int64
	Hourly    [capacityHourlySlots]usagePoint
	Daily     [capacityDailySlots]usagePoint
	DailyDay  [capacityDailySlots]int64
}

// Capacity state, exported fields persist in gob
type capacityState struct {
	CapRing     [fleetRingSize]capacityBucket
	EpochHour   int64
	Initialized bool
	Buckets     map[string]*bucketSeries
}

// Visible buckets mapped to creation hour, nil means cluster totals
type capacityScope map[string]int64

// Read series, capacity and health nil for scoped callers
type capacitySeries struct {
	Since                                              time.Time
	Step                                               time.Duration
	Used, Objects, Buckets                             []uint64
	Free, Total                                        []uint64
	DrivesOnline, DrivesTotal, NodesOnline, NodesTotal []uint64
}

// UTC day of hour, rounded down
func hourDay(h int64) int64 {
	if h < 0 {
		return (h - 23) / 24
	}
	return h / 24
}

// Supported capacity windows
func capacityHistoryHours(hours int) bool {
	switch hours {
	case 24, 7 * 24, 30 * 24:
		return true
	default:
		return false
	}
}

// Advance hour, report initialization or movement
func (s *capacityState) advance(nowHour int64) bool {
	if !s.Initialized {
		s.CapRing = [fleetRingSize]capacityBucket{}
		s.EpochHour = nowHour
		s.Initialized = true
		return true
	}
	gap := nowHour - s.EpochHour
	if gap <= 0 {
		return false
	}
	if gap >= fleetRingSize {
		s.CapRing = [fleetRingSize]capacityBucket{}
	} else {
		for h := s.EpochHour + 1; h <= nowHour; h++ {
			s.CapRing[ringSlot(h, fleetRingSize)] = capacityBucket{}
		}
	}
	s.EpochHour = nowHour
	return true
}

// Record usage and drive health in every ring retaining this hour
func (s *capacityState) record(nowHour int64, dui madmin.DataUsageInfo, drives capacityBucket, exists func(bucket string) bool) {
	s.advance(nowHour)
	if s.Buckets == nil {
		s.Buckets = map[string]*bucketSeries{}
	}
	if s.EpochHour-nowHour < fleetRingSize {
		b := drives
		b.Used, b.Objects, b.Buckets, b.Set = dui.ObjectsTotalSize, dui.ObjectsTotalCount, dui.BucketsCount, true
		s.CapRing[ringSlot(nowHour, fleetRingSize)] = b
	}
	for name := range s.Buckets {
		if _, ok := dui.BucketsUsage[name]; !ok && !exists(name) {
			delete(s.Buckets, name)
		}
	}
	fresh := make([]string, 0, len(dui.BucketsUsage))
	for name := range dui.BucketsUsage {
		if _, ok := s.Buckets[name]; !ok {
			fresh = append(fresh, name)
		}
	}
	sort.Strings(fresh)
	for _, name := range fresh {
		if len(s.Buckets) >= maxTrackedBuckets {
			break
		}
		s.Buckets[name] = &bucketSeries{FirstHour: nowHour}
	}
	day := hourDay(nowHour)
	hourly := s.EpochHour-nowHour < capacityHourlySlots
	daily := hourDay(s.EpochHour)-day < capacityDailySlots
	hourSlot, daySlot := ringSlot(nowHour, capacityHourlySlots), ringSlot(day, capacityDailySlots)
	for name, b := range s.Buckets {
		u := dui.BucketsUsage[name]
		p := usagePoint{Size: u.Size, Objects: u.ObjectsCount}
		if hourly {
			b.Hourly[hourSlot] = p
		}
		if daily {
			b.Daily[daySlot] = p
			b.DailyDay[daySlot] = day
		}
	}
}

// Bucket has a series
func (s *capacityState) tracked(bucket string) bool {
	_, ok := s.Buckets[bucket]
	return ok
}

// Hour has retained cluster sample
func (s *capacityState) sampled(h int64) bool {
	return s.Initialized && h <= s.EpochHour && s.EpochHour-h < fleetRingSize && s.CapRing[ringSlot(h, fleetRingSize)].Set
}

// Sum scoped hourly usage from bucket creation, recreated names hide old history
func (s *capacityState) bucketHour(h int64, scope capacityScope) (used, objects, count uint64) {
	slot := ringSlot(h, capacityHourlySlots)
	for name, b := range s.Buckets {
		created, ok := scope[name]
		if !ok || h < max(b.FirstHour, created) {
			continue
		}
		used += b.Hourly[slot].Size
		objects += b.Hourly[slot].Objects
		count++
	}
	return used, objects, count
}

// Sum scoped daily usage, carry last value when daily slot missing
func (s *capacityState) bucketDay(day, at int64, scope capacityScope, carried map[string]usagePoint) (used, objects, count uint64) {
	slot := ringSlot(day, capacityDailySlots)
	for name, b := range s.Buckets {
		created, ok := scope[name]
		if !ok || at < max(b.FirstHour, created) {
			continue
		}
		p := carried[name]
		if b.DailyDay[slot] == day {
			p = b.Daily[slot]
			carried[name] = p
		}
		used += p.Size
		objects += p.Objects
		count++
	}
	return used, objects, count
}

// Step series ending now, read only for follower caches
func (s *capacityState) series(nowHour int64, hours int, scope capacityScope) capacitySeries {
	daily := hours == capacityDailySlots*24
	scoped := scope != nil
	out := capacitySeries{Step: time.Hour, Used: []uint64{}, Objects: []uint64{}, Buckets: []uint64{}}
	var starts, ats []int64
	if daily {
		out.Step = 24 * time.Hour
		today := hourDay(nowHour)
		for d := today - capacityDailySlots + 1; d <= today; d++ {
			starts = append(starts, d*24)
			ats = append(ats, min(d*24+23, nowHour))
		}
	} else {
		for i := 0; i < hours; i++ {
			h := nowHour - int64(hours-1) + int64(i)
			starts = append(starts, h)
			ats = append(ats, h)
		}
	}
	if !s.Initialized {
		return out
	}
	var cur capacityBucket
	have := false
	carried := map[string]usagePoint{}
	h := s.EpochHour - fleetRingSize + 1
	for i, at := range ats {
		for ; h <= min(at, s.EpochHour); h++ {
			if !s.sampled(h) || (scoped && !daily && s.EpochHour-h >= capacityHourlySlots) {
				continue
			}
			cur = s.CapRing[ringSlot(h, fleetRingSize)]
			if scoped && !daily {
				cur.Used, cur.Objects, cur.Buckets = s.bucketHour(h, scope)
			}
			have = true
		}
		if !have {
			continue
		}
		pt := cur
		if scoped && daily {
			pt.Used, pt.Objects, pt.Buckets = s.bucketDay(hourDay(starts[i]), at, scope, carried)
		}
		if len(out.Used) == 0 {
			out.Since = time.Unix(starts[i]*3600, 0).UTC()
		}
		out.Used = append(out.Used, pt.Used)
		out.Objects = append(out.Objects, pt.Objects)
		out.Buckets = append(out.Buckets, pt.Buckets)
		if !scoped {
			out.Free = append(out.Free, pt.Free)
			out.Total = append(out.Total, pt.Total)
			out.DrivesOnline = append(out.DrivesOnline, pt.DrivesOnline)
			out.DrivesTotal = append(out.DrivesTotal, pt.DrivesTotal)
			out.NodesOnline = append(out.NodesOnline, pt.NodesOnline)
			out.NodesTotal = append(out.NodesTotal, pt.NodesTotal)
		}
	}
	return out
}

// Follower cache lifetime
const capacityFollowerTTL = time.Minute

// Scanner leader samples and saves, followers read file
type capacityHistory struct {
	mu         sync.Mutex
	state      capacityState
	file       historyFile
	leader     bool
	leaderCtx  context.Context
	leaderStop context.CancelFunc
	follower   *capacityState
	followerAt time.Time
}

var _ historyPersister = (*capacityHistory)(nil)

func newCapacityState() capacityState {
	return capacityState{Buckets: map[string]*bucketSeries{}}
}

func newCapacityHistory() *capacityHistory {
	return &capacityHistory{state: newCapacityState(), file: historyFile{key: capacityHistoryKey}}
}

// Load saved capacity before first leader sample
func (h *capacityHistory) BecomeLeader(ctx context.Context, objAPI ObjectLayer) error {
	h.ResignLeader()
	lease, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.leaderCtx, h.leaderStop = lease, cancel
	f := h.file
	h.mu.Unlock()

	ctx, locker, err := lockHistory(lease, objAPI, f.key)
	if err != nil {
		h.mu.Lock()
		if h.leaderCtx == lease {
			h.file.loadFailed, h.file.unread = true, true
		}
		h.mu.Unlock()
		cancel()
		return err
	}
	defer locker.Unlock()
	s := newCapacityState()
	if !f.read(ctx, objAPI, &s) {
		s = newCapacityState()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.leaderCtx != lease || ctx.Err() != nil {
		cancel()
		return context.Canceled
	}
	h.file = f
	h.state, h.leader, h.follower = s, true, nil
	return nil
}

// Drop leadership and file status after lease loss to protect next leader's history
func (h *capacityHistory) ResignLeader() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.leaderStop != nil {
		h.leaderStop()
	}
	h.leaderCtx, h.leaderStop = nil, nil
	h.state, h.leader, h.follower = newCapacityState(), false, nil
	h.file = historyFile{key: h.file.key}
}

// Leader records usage and drive health from same tick
func (h *capacityHistory) Sample(ctx context.Context, objAPI ObjectLayer, dui madmin.DataUsageInfo, now time.Time) {
	h.mu.Lock()
	lease := h.leaderCtx
	leader := h.leader && lease != nil && lease.Err() == nil
	var missing []string
	if leader {
		for name := range h.state.Buckets {
			if _, ok := dui.BucketsUsage[name]; !ok {
				missing = append(missing, name)
			}
		}
	}
	h.mu.Unlock()
	if !leader {
		return
	}
	ctx, cancel := historyLeaseContext(ctx, lease)
	defer cancel()

	si, _ := objAPI.StorageInfo(ctx)
	total, free, dOn, dTot, nOn, nTot := storageTotals(si.Disks)
	drives := capacityBucket{
		Total: total, Free: free,
		DrivesOnline: uint64(dOn), DrivesTotal: uint64(dTot), NodesOnline: uint64(nOn), NodesTotal: uint64(nTot),
	}
	// Look up buckets outside lock so drive I/O cannot block history reads
	gone := map[string]bool{}
	for _, name := range missing {
		if _, err := objAPI.GetBucketInfo(ctx, name); isErrBucketNotFound(err) {
			gone[name] = true
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.leader || h.leaderCtx != lease || ctx.Err() != nil {
		return
	}
	h.state.record(absHour(now), dui, drives, func(name string) bool { return !gone[name] })
	h.file.dirty = true
}

// Read leader state or follower cache within capacityFollowerTTL
func (h *capacityHistory) view(ctx context.Context, objAPI ObjectLayer, now time.Time, fn func(s *capacityState)) {
	h.mu.Lock()
	if h.leader && h.leaderCtx != nil && h.leaderCtx.Err() == nil {
		defer h.mu.Unlock()
		fn(&h.state)
		return
	}
	s := h.follower
	if s != nil && now.Sub(h.followerAt) >= capacityFollowerTTL {
		s = nil
	}
	h.mu.Unlock()
	if s == nil {
		s = readCapacityState(ctx, objAPI)
		h.mu.Lock()
		h.follower, h.followerAt = s, now
		h.mu.Unlock()
	}
	fn(s)
}

// Follower reads file, missing or failed read gives empty history, only leader reports faults
func readCapacityState(ctx context.Context, objAPI ObjectLayer) *capacityState {
	s := newCapacityState()
	data, err := readConfig(ctx, objAPI, capacityHistoryKey)
	if err == nil {
		err = decodeHistory(data, &s)
	}
	if err != nil {
		s = newCapacityState()
	}
	return &s
}

// Leader saves dirty capacity hourly unless forced
func (h *capacityHistory) SaveHistory(ctx context.Context, objAPI ObjectLayer, now time.Time, force bool) error {
	h.mu.Lock()
	lease := h.leaderCtx
	if !h.leader || lease == nil || lease.Err() != nil {
		h.mu.Unlock()
		return nil
	}
	key := h.file.key
	h.mu.Unlock()
	ctx, cancel := historyLeaseContext(ctx, lease)
	defer cancel()
	ctx, locker, err := lockHistory(ctx, objAPI, key)
	if err != nil {
		h.mu.Lock()
		active := h.leader && h.leaderCtx == lease && lease.Err() == nil
		if active {
			h.file.lastSaveErr = err
		}
		h.mu.Unlock()
		if !active {
			return nil
		}
		return err
	}
	defer locker.Unlock()

	h.mu.Lock()
	if !h.leader || h.leaderCtx != lease || ctx.Err() != nil {
		h.mu.Unlock()
		return nil
	}
	if h.file.unread {
		f := h.file
		h.mu.Unlock()
		s := newCapacityState()
		ok := f.read(ctx, objAPI, &s)
		h.mu.Lock()
		if !h.leader || h.leaderCtx != lease || ctx.Err() != nil {
			h.mu.Unlock()
			return nil
		}
		h.file.unread, h.file.loadFailed = f.unread, f.loadFailed
		// Saved history replaces samples since failed load, next scan fills current hour
		if ok {
			h.state = s
		}
	}
	// No samples means no save, advancing sampled ring does
	if h.state.Initialized && h.state.advance(absHour(now)) {
		h.file.dirty = true
	}
	if !h.file.due(now, force) {
		h.mu.Unlock()
		return nil
	}
	data, err := encodeHistory(&h.state)
	if err != nil {
		h.file.lastSaveErr = err
		h.mu.Unlock()
		return err
	}
	// Write copy outside lock, concurrent sample marks dirty again
	f := h.file
	h.file.dirty = false
	h.mu.Unlock()

	err = f.write(ctx, objAPI, data, now)

	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.leader || h.leaderCtx != lease || lease.Err() != nil {
		// Lost lease stops former leader's write without flagging current leader's file
		return nil
	}
	h.file.lastSave, h.file.lastSaveErr = f.lastSave, f.lastSaveErr
	if err != nil {
		h.file.dirty = true
	}
	return err
}

func (h *capacityHistory) HistoryStatus() historyStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.file.status()
}
