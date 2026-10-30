package cmd

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/obstor/obstor/cmd/logger"
)

const (
	historyFormatVersion byte = 1
	// Scanner leader's history object under obstorMetaBucket
	capacityHistoryKey = "history/capacity.bin"
	historySaveEvery   = time.Hour
)

var errHistoryVersion = errors.New("history file has an unknown format version")

var historyLockTimeout = newDynamicTimeout(30*time.Second, 10*time.Second)

type historyLocker struct {
	RWLocker
	cancel context.CancelFunc
}

func (l historyLocker) Unlock() {
	l.RWLocker.Unlock()
	l.cancel()
}

// Shared namespace lock orders snapshots and leader handover, separate resource avoids nested object lock
func lockHistory(ctx context.Context, objAPI ObjectLayer, key string) (context.Context, RWLocker, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	locker := objAPI.NewNSLock(obstorMetaBucket, key+".lock")
	lockedCtx, err := locker.GetLock(ctx, historyLockTimeout)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		cancel()
		return nil, nil, err
	}
	if err = lockedCtx.Err(); err != nil {
		locker.Unlock()
		cancel()
		return nil, nil, err
	}
	return lockedCtx, historyLocker{RWLocker: locker, cancel: cancel}, nil
}

// Stop save on caller deadline or scanner lease loss
func historyLeaseContext(ctx, lease context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lease, cancel)
	if lease.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

// Version byte followed by gob
func encodeHistory(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(historyFormatVersion)
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode fresh value, gob would merge existing fields
func decodeHistory(data []byte, v interface{}) error {
	if len(data) == 0 || data[0] != historyFormatVersion {
		return errHistoryVersion
	}
	return gob.NewDecoder(bytes.NewReader(data[1:])).Decode(v)
}

// Local node name safe for object path segment
func historyNodeKey(node string) string {
	return strings.NewReplacer(":", "_", "/", "_").Replace(node)
}

func trafficHistoryKey(node string) string {
	return "history/traffic-" + historyNodeKey(node) + ".bin"
}

// History file load and save status
type historyStatus struct {
	LoadFailed  bool
	LastSaveErr error
}

// Save state guarded by owning store's lock
type historyFile struct {
	key         string
	dirty       bool
	lastSave    time.Time
	loadFailed  bool
	unread      bool // Read failed, block saves until successful read
	lastSaveErr error
}

func (f *historyFile) due(now time.Time, force bool) bool {
	return f.key != "" && f.dirty && !f.unread && (force || f.lastSave.IsZero() || now.Sub(f.lastSave) >= historySaveEvery)
}

func (f *historyFile) status() historyStatus {
	return historyStatus{LoadFailed: f.loadFailed, LastSaveErr: f.lastSaveErr}
}

// Load file, missing starts fresh, corrupt flags error, read failure blocks saves
func (f *historyFile) read(ctx context.Context, objAPI ObjectLayer, v interface{}) bool {
	data, err := readConfig(ctx, objAPI, f.key)
	if errors.Is(err, errConfigNotFound) {
		// Empty history object is torn save, readConfig treats it as missing
		if err = checkConfig(ctx, objAPI, f.key); errors.Is(err, errConfigNotFound) {
			f.loadFailed, f.unread = false, false
			return false
		}
	}
	// Read failure blocks saves, decode failure allows replacement
	f.unread = err != nil
	if err == nil {
		err = decodeHistory(data, v)
	}
	f.loadFailed = err != nil
	if err != nil {
		logger.LogIf(ctx, fmt.Errorf("load %s: %w", f.key, err))
		return false
	}
	return true
}

// Save and record status, failed writes stay dirty for next interval
func (f *historyFile) write(ctx context.Context, objAPI ObjectLayer, data []byte, now time.Time) error {
	err := ctx.Err()
	if err == nil {
		err = saveConfig(ctx, objAPI, f.key, data)
	}
	f.lastSave, f.lastSaveErr = now, err
	if err == nil {
		f.dirty = false
	}
	return err
}

// History store flushed periodically and at shutdown
type historyPersister interface {
	SaveHistory(ctx context.Context, objAPI ObjectLayer, now time.Time, force bool) error
	HistoryStatus() historyStatus
}

// Save check interval
const historySaveCheck = time.Minute

// Persistent stores for this node, backend keeps history in memory
func historyPersisters() []historyPersister {
	if globalIsBackend {
		return nil
	}
	var out []historyPersister
	if globalCapacityHistory != nil {
		out = append(out, globalCapacityHistory)
	}
	if globalFleetStore != nil {
		out = append(out, globalFleetStore)
	}
	return out
}

// Save due stores, force skips hourly limit at shutdown
func saveHistories(ctx context.Context, objAPI ObjectLayer, now time.Time, force bool) {
	for _, p := range historyPersisters() {
		logger.LogIf(ctx, p.SaveHistory(ctx, objAPI, now, force))
	}
}

// Save dirty history hourly until ctx ends
func runHistorySaver(ctx context.Context, objAPI ObjectLayer) {
	ticker := time.NewTicker(historySaveCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			saveHistories(ctx, objAPI, time.Now(), false)
		}
	}
}
