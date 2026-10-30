package cmd

import (
	"context"
	"sync"
	"time"
)

// Latest reported state of one deployment
type MemberSnapshot struct {
	ID                         string
	Label                      string
	Region                     string
	Version                    string
	Used, Total, Free          uint64
	ObjectsCount, BucketsCount uint64
	NodesOnline, NodesTotal    int
	DrivesOnline, DrivesTotal  int
}

// Per-interval traffic folded into the aggregate ring
type Deltas struct {
	InBytes  uint64
	OutBytes uint64
	Reads    uint64
	Writes   uint64
}

// Read-only fleet-query output
type MemberView struct {
	MemberSnapshot
	Online bool
}

// Hourly inbound and outbound bytes
type BWBucket struct {
	In  uint64 `json:"in"`
	Out uint64 `json:"out"`
}

// Hourly read and write counts
type OpsBucket struct {
	Read  uint64 `json:"read"`
	Write uint64 `json:"write"`
}

// Shared database state interface
type FleetStore interface {
	Upsert(s MemberSnapshot, at time.Time)
	Fold(d Deltas, at time.Time)
	Members(now time.Time) []MemberView
	Series() ([]BWBucket, []OpsBucket)
	SeriesWindow(now time.Time, windowHours int) ([]BWBucket, []OpsBucket)
	Since(now time.Time) time.Time
	MintEnrollToken(now time.Time) string
	LoadHistory(ctx context.Context, objAPI ObjectLayer)
	SaveHistory(ctx context.Context, objAPI ObjectLayer, now time.Time, force bool) error
	HistoryStatus() historyStatus
}

const (
	fleetDefaultWindowHours = 24
	fleetRingSize           = 30 * 24
)

func fleetMetricsWindowHours(hours int) int {
	switch hours {
	case 24, 7 * 24, 30 * 24:
		return hours
	default:
		return fleetDefaultWindowHours
	}
}

type ringBuf struct {
	bw          [fleetRingSize]BWBucket
	ops         [fleetRingSize]OpsBucket
	epochHour   int64
	firstHour   int64 // Oldest reported hour since ring started
	initialized bool
}

// Hours since unix epoch.
func absHour(t time.Time) int64 { return t.Unix() / 3600 }

// Ring index for hour h, including negative hours
func ringSlot(h int64, size int) int {
	n := int64(size)
	return int(((h % n) + n) % n)
}

// Scroll ring so "now" is the newest slot
func (r *ringBuf) advance(now int64) {
	if !r.initialized {
		r.epochHour = now
		r.firstHour = now
		r.initialized = true
		return
	}
	gap := now - r.epochHour
	if gap <= 0 {
		return
	}
	if gap >= fleetRingSize {
		r.bw = [fleetRingSize]BWBucket{}
		r.ops = [fleetRingSize]OpsBucket{}
		r.epochHour = now
		r.firstHour = now
		return
	}
	for h := r.epochHour + 1; h <= now; h++ {
		idx := ringSlot(h, fleetRingSize)
		r.bw[idx] = BWBucket{}
		r.ops[idx] = OpsBucket{}
	}
	r.epochHour = now
}

func (r *ringBuf) fold(d Deltas, now int64) {
	r.advance(now)
	// Drop reports outside retained window
	if r.epochHour-now >= fleetRingSize {
		return
	}
	// Late report moves first hour back
	if now < r.firstHour {
		r.firstHour = now
	}
	idx := ringSlot(now, fleetRingSize)
	r.bw[idx].In += d.InBytes
	r.bw[idx].Out += d.OutBytes
	r.ops[idx].Read += d.Reads
	r.ops[idx].Write += d.Writes
}

// Oldest retained hour within window
func (r *ringBuf) sinceHour() int64 {
	return max(r.firstHour, r.epochHour-fleetRingSize+1)
}

// Hourly buckets oldest first, ending at epochHour
func (r *ringBuf) series(windowHours int) ([]BWBucket, []OpsBucket) {
	windowHours = fleetMetricsWindowHours(windowHours)
	bw := make([]BWBucket, windowHours)
	ops := make([]OpsBucket, windowHours)
	if !r.initialized {
		return bw, ops
	}
	for k := 0; k < windowHours; k++ {
		h := r.epochHour - int64(windowHours-1) + int64(k)
		idx := ringSlot(h, fleetRingSize)
		bw[k] = r.bw[idx]
		ops[k] = r.ops[idx]
	}
	return bw, ops
}

// Saved ring in traffic-{node}.bin
type trafficState struct {
	Bw          [fleetRingSize]BWBucket
	Ops         [fleetRingSize]OpsBucket
	EpochHour   int64
	FirstHour   int64
	Initialized bool
}

func (r *ringBuf) state() trafficState {
	return trafficState{Bw: r.bw, Ops: r.ops, EpochHour: r.epochHour, FirstHour: r.firstHour, Initialized: r.initialized}
}

func (r *ringBuf) restore(t trafficState) {
	r.bw, r.ops = t.Bw, t.Ops
	r.epochHour, r.firstHour, r.initialized = t.EpochHour, t.FirstHour, t.Initialized
}

// Restore saved ring, keep reports received since failed load
func (r *ringBuf) merge(t trafficState) {
	live := *r
	r.restore(t)
	if !live.initialized {
		return
	}
	for h := live.sinceHour(); h <= live.epochHour; h++ {
		bw, ops := live.bw[ringSlot(h, fleetRingSize)], live.ops[ringSlot(h, fleetRingSize)]
		r.fold(Deltas{InBytes: bw.In, OutBytes: bw.Out, Reads: ops.Read, Writes: ops.Write}, h)
	}
}

type memberRec struct {
	snap     MemberSnapshot
	lastSeen time.Time
}

type inMemFleetStore struct {
	mu         sync.Mutex
	members    map[string]memberRec
	ring       ringBuf
	staleAfter time.Duration
	tokens     map[string]time.Time
	file       historyFile
}

// Empty historyKey keeps traffic in memory
func newInMemFleetStore(staleAfter time.Duration, historyKey string) *inMemFleetStore {
	return &inMemFleetStore{
		members:    map[string]memberRec{},
		staleAfter: staleAfter,
		tokens:     map[string]time.Time{},
		file:       historyFile{key: historyKey},
	}
}

func (s *inMemFleetStore) Upsert(snap MemberSnapshot, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[snap.ID] = memberRec{snap: snap, lastSeen: at}
}

func (s *inMemFleetStore) Fold(d Deltas, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ring.fold(d, absHour(at))
	s.file.dirty = true
}

func (s *inMemFleetStore) Members(now time.Time) []MemberView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MemberView, 0, len(s.members))
	for _, rec := range s.members {
		out = append(out, MemberView{
			MemberSnapshot: rec.snap,
			Online:         now.Sub(rec.lastSeen) <= s.staleAfter,
		})
	}
	return out
}

func (s *inMemFleetStore) Series() ([]BWBucket, []OpsBucket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ring.series(fleetDefaultWindowHours)
}

// Expire hours even without reports
func (s *inMemFleetStore) SeriesWindow(now time.Time, windowHours int) ([]BWBucket, []OpsBucket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Empty ring keeps Since zero until first report
	if s.ring.initialized {
		s.ring.advance(absHour(now))
	}
	return s.ring.series(windowHours)
}

// Oldest retained hour, zero before first report
func (s *inMemFleetStore) Since(now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ring.initialized {
		return time.Time{}
	}
	s.ring.advance(absHour(now))
	return time.Unix(s.ring.sinceHour()*3600, 0).UTC()
}

func (s *inMemFleetStore) MintEnrollToken(now time.Time) string {
	tok := mustGetUUID()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tok] = now.Add(15 * time.Minute)
	return tok
}

// Load traffic before collector starts
func (s *inMemFleetStore) LoadHistory(ctx context.Context, objAPI ObjectLayer) {
	if s.file.key == "" {
		return
	}
	s.mu.Lock()
	f := s.file
	s.mu.Unlock()
	ctx, locker, err := lockHistory(ctx, objAPI, f.key)
	if err != nil {
		s.mu.Lock()
		s.file.loadFailed, s.file.unread = true, true
		s.mu.Unlock()
		return
	}
	defer locker.Unlock()

	var t trafficState
	ok := f.read(ctx, objAPI, &t)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.loadFailed, s.file.unread = f.loadFailed, f.unread
	if ok {
		s.ring.restore(t)
	}
}

// Save changed traffic outside mu
func (s *inMemFleetStore) SaveHistory(ctx context.Context, objAPI ObjectLayer, now time.Time, force bool) error {
	s.mu.Lock()
	key := s.file.key
	s.mu.Unlock()
	if key == "" {
		return nil
	}
	// Shared lock keeps periodic and shutdown writes in snapshot order
	ctx, locker, err := lockHistory(ctx, objAPI, key)
	if err != nil {
		s.mu.Lock()
		s.file.lastSaveErr = err
		s.mu.Unlock()
		return err
	}
	defer locker.Unlock()
	s.mu.Lock()
	if s.file.unread {
		f := s.file
		s.mu.Unlock()
		var t trafficState
		ok := f.read(ctx, objAPI, &t)
		s.mu.Lock()
		s.file.unread, s.file.loadFailed = f.unread, f.loadFailed
		if ok {
			s.ring.merge(t)
		}
	}
	// Empty ring keeps Since zero until first report
	if s.ring.initialized {
		before := s.ring.epochHour
		s.ring.advance(absHour(now))
		if s.ring.epochHour != before {
			s.file.dirty = true
		}
	}
	if !s.file.due(now, force) {
		s.mu.Unlock()
		return nil
	}
	data, err := encodeHistory(s.ring.state())
	if err != nil {
		s.file.lastSaveErr = err
		s.mu.Unlock()
		return err
	}
	f := s.file
	s.file.dirty = false
	s.mu.Unlock()

	err = f.write(ctx, objAPI, data, now)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.lastSave, s.file.lastSaveErr = f.lastSave, f.lastSaveErr
	if err != nil {
		s.file.dirty = true
	}
	return err
}

// Traffic file load and save status
func (s *inMemFleetStore) HistoryStatus() historyStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.status()
}
