package cmd

import (
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

// Inbound and outbound bytes in one hour of the daily stats
type BWBucket struct {
	In  uint64 `json:"in"`
	Out uint64 `json:"out"`
}

// Read and write operation counts in one hour of the daily stats
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
	MintEnrollToken(now time.Time) string
}

const fleetRingSize = 24

type ringBuf struct {
	bw          [fleetRingSize]BWBucket
	ops         [fleetRingSize]OpsBucket
	epochHour   int64
	initialized bool
}

// Hours since unix epoch.
func absHour(t time.Time) int64 { return t.Unix() / 3600 }

// Scroll ring so "now" is the newest slot
func (r *ringBuf) advance(now int64) {
	if !r.initialized {
		r.epochHour = now
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
		return
	}
	for h := r.epochHour + 1; h <= now; h++ {
		idx := ((h % fleetRingSize) + fleetRingSize) % fleetRingSize
		r.bw[idx] = BWBucket{}
		r.ops[idx] = OpsBucket{}
	}
	r.epochHour = now
}

func (r *ringBuf) fold(d Deltas, now int64) {
	r.advance(now)
	idx := ((now % fleetRingSize) + fleetRingSize) % fleetRingSize
	r.bw[idx].In += d.InBytes
	r.bw[idx].Out += d.OutBytes
	r.ops[idx].Read += d.Reads
	r.ops[idx].Write += d.Writes
}

// Series returns the daily oldest-first, ending at epochHour; all-zero when not yet initialized.
func (r *ringBuf) series() ([]BWBucket, []OpsBucket) {
	bw := make([]BWBucket, fleetRingSize)
	ops := make([]OpsBucket, fleetRingSize)
	if !r.initialized {
		return bw, ops
	}
	for k := 0; k < fleetRingSize; k++ {
		h := r.epochHour - int64(fleetRingSize-1) + int64(k)
		idx := ((h % fleetRingSize) + fleetRingSize) % fleetRingSize
		bw[k] = r.bw[idx]
		ops[k] = r.ops[idx]
	}
	return bw, ops
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
}

func newInMemFleetStore(staleAfter time.Duration) *inMemFleetStore {
	return &inMemFleetStore{
		members:    map[string]memberRec{},
		staleAfter: staleAfter,
		tokens:     map[string]time.Time{},
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
	return s.ring.series()
}

func (s *inMemFleetStore) MintEnrollToken(now time.Time) string {
	tok := mustGetUUID()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tok] = now.Add(15 * time.Minute)
	return tok
}
