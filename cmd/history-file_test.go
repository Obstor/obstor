package cmd

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/obstor/obstor/pkg/madmin"
)

// Pause before disk write, ignore cancellation unless cancelAware to test shared lock
type pausedStoreLayer struct {
	ObjectLayer
	key         string
	entered     chan context.Context
	release     chan struct{}
	cancelAware bool
	paused      atomic.Bool
	resume      sync.Once
}

func (p *pausedStoreLayer) PutObject(ctx context.Context, bucket, object string, data *PutObjReader, opts ObjectOptions) (ObjectInfo, error) {
	if object == p.key && p.paused.CompareAndSwap(false, true) {
		p.entered <- ctx
		if p.cancelAware {
			select {
			case <-p.release:
			case <-ctx.Done():
				return ObjectInfo{}, ctx.Err()
			}
		} else {
			<-p.release
			ctx = context.Background()
		}
	}
	return p.ObjectLayer.PutObject(ctx, bucket, object, data, opts)
}

func (p *pausedStoreLayer) unblock() { p.resume.Do(func() { close(p.release) }) }

func awaitStoreWrite(t testing.TB, entered <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-entered:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("save never entered the paused write")
		return nil
	}
}

func awaitStoreSave(t testing.TB, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("save never finished")
		return nil
	}
}

type observedHistoryLayer struct {
	ObjectLayer
	locking chan context.Context
}

func (l observedHistoryLayer) NewNSLock(bucket string, objects ...string) RWLocker {
	return observedHistoryLock{RWLocker: l.ObjectLayer.NewNSLock(bucket, objects...), locking: l.locking}
}

type observedHistoryLock struct {
	RWLocker
	locking chan context.Context
}

func (l observedHistoryLock) GetLock(ctx context.Context, timeout *dynamicTimeout) (context.Context, error) {
	l.locking <- ctx
	return l.RWLocker.GetLock(ctx, timeout)
}

// Bucket sizes with one object per 10 bytes
func historyTestUsage(sizes map[string]uint64) madmin.DataUsageInfo {
	dui := madmin.DataUsageInfo{
		BucketsCount: uint64(len(sizes)),
		BucketsUsage: make(map[string]madmin.BucketUsageInfo, len(sizes)),
	}
	for name, size := range sizes {
		dui.BucketsUsage[name] = madmin.BucketUsageInfo{Size: size, ObjectsCount: size / 10}
		dui.ObjectsTotalSize += size
		dui.ObjectsTotalCount += size / 10
	}
	return dui
}

func TestHistoryCodec(t *testing.T) {
	hour := absHour(time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
	all := func(string) bool { return true }
	s := newCapacityState()
	s.record(hour-1, historyTestUsage(map[string]uint64{"alpha": 100, "beta": 300}), capacityBucket{Free: 1 << 30}, all)
	s.record(hour, historyTestUsage(map[string]uint64{"alpha": 200}), capacityBucket{Free: 1 << 29}, all)

	data, err := encodeHistory(&s)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if data[0] != historyFormatVersion {
		t.Fatalf("version byte = %d, want %d", data[0], historyFormatVersion)
	}
	got := newCapacityState()
	if err := decodeHistory(data, &got); err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip = %v, equal %v; want the encoded state back", err, reflect.DeepEqual(got, s))
	}

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"version 2", append([]byte{2}, data[1:]...)},
	} {
		v := newCapacityState()
		if err := decodeHistory(tc.data, &v); !errors.Is(err, errHistoryVersion) {
			t.Fatalf("%s: decode = %v, want %v", tc.name, err, errHistoryVersion)
		}
	}

	v := newCapacityState()
	if err := decodeHistory(data[:len(data)/2], &v); err == nil || errors.Is(err, errHistoryVersion) {
		t.Fatalf("truncated: decode = %v, want a gob error", err)
	}
}

func TestHistoryNodeKey(t *testing.T) {
	for node, want := range map[string]string{
		"node1:9000":     "history/traffic-node1_9000.bin",
		"127.0.0.1:9200": "history/traffic-127.0.0.1_9200.bin",
		"localhost":      "history/traffic-localhost.bin",
	} {
		if got := trafficHistoryKey(node); got != want {
			t.Fatalf("trafficHistoryKey(%q) = %q, want %q", node, got, want)
		}
	}
	if got := historyNodeKey("[::1]:9000/x"); got != "[__1]_9000_x" {
		t.Fatalf("historyNodeKey([::1]:9000/x) = %q, want [__1]_9000_x", got)
	}
}

func TestHistoryFileDue(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	key := capacityHistoryKey
	for _, tc := range []struct {
		name  string
		f     historyFile
		force bool
		want  bool
	}{
		{"clean", historyFile{key: key, lastSave: now.Add(-2 * time.Hour)}, false, false},
		{"clean forced", historyFile{key: key}, true, false},
		{"dirty never saved", historyFile{key: key, dirty: true}, false, true},
		{"dirty 30 min after save", historyFile{key: key, dirty: true, lastSave: now.Add(-30 * time.Minute)}, false, false},
		{"dirty 60 min after save", historyFile{key: key, dirty: true, lastSave: now.Add(-time.Hour)}, false, true},
		{"dirty 61 min after save", historyFile{key: key, dirty: true, lastSave: now.Add(-61 * time.Minute)}, false, true},
		{"dirty forced", historyFile{key: key, dirty: true, lastSave: now.Add(-30 * time.Minute)}, true, true},
		{"empty key", historyFile{dirty: true}, true, false},
		{"dirty forced, load unread", historyFile{key: key, dirty: true, unread: true}, true, false},
	} {
		if got := tc.f.due(now, tc.force); got != tc.want {
			t.Fatalf("%s: due = %v, want %v", tc.name, got, tc.want)
		}
	}

	saveErr := errors.New("drive offline")
	f := historyFile{key: key, loadFailed: true, lastSaveErr: saveErr}
	if got := f.status(); got != (historyStatus{LoadFailed: true, LastSaveErr: saveErr}) {
		t.Fatalf("status = %+v, want load failed and the save error", got)
	}
}

func TestSaveHistoriesBackend(t *testing.T) {
	ExecObjectLayerTest(t, testSaveHistoriesBackend)
}

// Backend keeps history in memory, others write both files
func testSaveHistoriesBackend(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	savedCapacity, savedFleet := globalCapacityHistory, globalFleetStore
	defer func() { globalCapacityHistory, globalFleetStore = savedCapacity, savedFleet }()
	ctx := context.Background()
	now := time.Now()

	globalCapacityHistory = newCapacityHistory()
	globalCapacityHistory.BecomeLeader(ctx, obj)
	globalCapacityHistory.Sample(ctx, obj, madmin.DataUsageInfo{ObjectsTotalSize: 10, ObjectsTotalCount: 1}, now)
	fleet := newInMemFleetStore(45*time.Second, trafficHistoryKey("node1:9000"))
	fleet.Fold(Deltas{InBytes: 10}, now)
	globalFleetStore = fleet
	keys := []string{capacityHistoryKey, trafficHistoryKey("node1:9000")}

	globalIsBackend = true
	if got := historyPersisters(); len(got) != 0 {
		t.Fatalf("%s: backend persisters = %d, want 0", instanceType, len(got))
	}
	saveHistories(ctx, obj, now, true)
	for _, key := range keys {
		if _, err := readConfig(ctx, obj, key); !errors.Is(err, errConfigNotFound) {
			t.Fatalf("%s: backend mode wrote %s: %v", instanceType, key, err)
		}
	}

	globalIsBackend = false
	if got := historyPersisters(); len(got) != 2 {
		t.Fatalf("%s: persisters = %d, want 2", instanceType, len(got))
	}
	saveHistories(ctx, obj, now, true)
	for _, key := range keys {
		if _, err := readConfig(ctx, obj, key); err != nil {
			t.Fatalf("%s: %s not written: %v", instanceType, key, err)
		}
	}
}

// Read quorum failure, writes still reach backend
type historyReadFailLayer struct{ ObjectLayer }

func (historyReadFailLayer) GetObjectNInfo(context.Context, string, string, *HTTPRangeSpec, http.Header, LockType, ObjectOptions) (*GetObjectReader, error) {
	return nil, InsufficientReadQuorum{}
}

func TestCapacityHistoryLoadTransient(t *testing.T) {
	ExecObjectLayerTest(t, testCapacityHistoryLoadTransient)
}

// Failed load blocks saves until successful read, keeps saved history
func testCapacityHistoryLoadTransient(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC)
	good := newCapacityState()
	good.record(absHour(now)-5, historyTestUsage(map[string]uint64{"alpha": 50}), capacityBucket{Free: 1 << 30}, func(string) bool { return true })
	data, err := encodeHistory(&good)
	if err == nil {
		err = saveConfig(ctx, obj, capacityHistoryKey, data)
	}
	if err != nil {
		t.Fatalf("%s: seed capacity.bin: %v", instanceType, err)
	}
	saved := func(step string) capacityState {
		t.Helper()
		s := newCapacityState()
		data, err := readConfig(ctx, obj, capacityHistoryKey)
		if err == nil {
			err = decodeHistory(data, &s)
		}
		if err != nil {
			t.Fatalf("%s: %s: read capacity.bin: %v", instanceType, step, err)
		}
		return s
	}

	h := newCapacityHistory()
	h.BecomeLeader(ctx, historyReadFailLayer{obj})
	if !h.HistoryStatus().LoadFailed {
		t.Fatalf("%s: LoadFailed = false after a read error, want true", instanceType)
	}
	h.Sample(ctx, obj, historyTestUsage(map[string]uint64{"beta": 70}), now)
	if err := h.SaveHistory(ctx, historyReadFailLayer{obj}, now, true); err != nil {
		t.Fatalf("%s: save while unreadable: %v", instanceType, err)
	}
	if s := saved("save while unreadable"); !s.tracked("alpha") || s.tracked("beta") {
		t.Fatalf("%s: save while unreadable overwrote capacity.bin (alpha %v beta %v)", instanceType, s.tracked("alpha"), s.tracked("beta"))
	}

	if err := h.SaveHistory(ctx, obj, now.Add(time.Minute), true); err != nil {
		t.Fatalf("%s: save after recovery: %v", instanceType, err)
	}
	if st := h.HistoryStatus(); st.LoadFailed || st.LastSaveErr != nil {
		t.Fatalf("%s: status after recovery = %+v, want clean", instanceType, st)
	}
	if s := saved("save after recovery"); !s.tracked("alpha") || s.EpochHour != absHour(now) {
		t.Fatalf("%s: capacity.bin after recovery: alpha %v epoch %d, want alpha kept and epoch %d", instanceType, s.tracked("alpha"), s.EpochHour, absHour(now))
	}
}

func TestTrafficHistoryLoadTransient(t *testing.T) {
	ExecObjectLayerTest(t, testTrafficHistoryLoadTransient)
}

// Failed load blocks saves, successful read keeps saved traffic and new reports
func testTrafficHistoryLoadTransient(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx := context.Background()
	now := hourBase()
	key := trafficHistoryKey("transient:9000")
	good := newInMemFleetStore(45*time.Second, key)
	good.Fold(Deltas{InBytes: 100}, now.Add(-2*time.Hour))
	if err := good.SaveHistory(ctx, obj, now.Add(-2*time.Hour), true); err != nil {
		t.Fatalf("%s: seed traffic file: %v", instanceType, err)
	}
	savedSum := func(step string) uint64 {
		t.Helper()
		f := newInMemFleetStore(45*time.Second, key)
		f.LoadHistory(ctx, obj)
		if f.HistoryStatus().LoadFailed {
			t.Fatalf("%s: %s: traffic file unreadable", instanceType, step)
		}
		bw, _ := f.SeriesWindow(now, 24)
		return trafficInSum(bw)
	}

	s := newInMemFleetStore(45*time.Second, key)
	s.LoadHistory(ctx, historyReadFailLayer{obj})
	if !s.HistoryStatus().LoadFailed {
		t.Fatalf("%s: LoadFailed = false after a read error, want true", instanceType)
	}
	s.Fold(Deltas{InBytes: 5}, now)
	if err := s.SaveHistory(ctx, historyReadFailLayer{obj}, now, true); err != nil {
		t.Fatalf("%s: save while unreadable: %v", instanceType, err)
	}
	if got := savedSum("save while unreadable"); got != 100 {
		t.Fatalf("%s: traffic file In sum after save while unreadable = %d, want 100", instanceType, got)
	}

	if err := s.SaveHistory(ctx, obj, now, true); err != nil {
		t.Fatalf("%s: save after recovery: %v", instanceType, err)
	}
	if st := s.HistoryStatus(); st.LoadFailed || st.LastSaveErr != nil {
		t.Fatalf("%s: status after recovery = %+v, want clean", instanceType, st)
	}
	bw, _ := s.SeriesWindow(now, 24)
	if got := trafficInSum(bw); got != 105 {
		t.Fatalf("%s: ring In sum after recovery = %d, want 105", instanceType, got)
	}
	if got := savedSum("save after recovery"); got != 105 {
		t.Fatalf("%s: traffic file In sum after recovery = %d, want 105", instanceType, got)
	}
	if got := s.Since(now); !got.Equal(now.Add(-2 * time.Hour)) {
		t.Fatalf("%s: Since after recovery = %v, want %v", instanceType, got, now.Add(-2*time.Hour))
	}
}
