// PGG Obstor, (C) 2021-2026 PGG, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package sftp

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() {
	// Keep out-of-order read waits short so the timeout-fallback dont stall
	sftpReadSequenceWait = 250 * time.Millisecond
	sftpReadStartWait = 50 * time.Millisecond
}

func TestStreamingWriterAtRejectsBadOffsets(t *testing.T) {
	cases := []struct {
		name     string
		startOff int64
		off      int64
		size     int
	}{
		{"negative offset", 0, -1, 4},
		{"min int64 offset", 0, math.MinInt64, 1},
		{"behind the stream", 10, 3, 1},
		{"max int64 offset exceeds size cap", 0, math.MaxInt64, 1},
		{"write past max file size", sftpMaxFileSize, sftpMaxFileSize, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sink bytes.Buffer
			w := &streamingWriterAt{w: &sink, off: tc.startOff}
			n, err := w.WriteAt(make([]byte, tc.size), tc.off)
			if err == nil {
				t.Fatalf("expected error for off=%d size=%d, got nil (n=%d)", tc.off, tc.size, n)
			}
			if n != 0 {
				t.Fatalf("expected n=0 on rejected write, got %d", n)
			}
			if sink.Len() != 0 {
				t.Fatalf("sink must stay empty on rejected write, got %d bytes", sink.Len())
			}
		})
	}
}

func TestStreamingWriterAtSequential(t *testing.T) {
	var sink bytes.Buffer
	w := &streamingWriterAt{w: &sink}
	if n, err := w.WriteAt([]byte("hello"), 0); err != nil || n != 5 {
		t.Fatalf("write 1: n=%d err=%v", n, err)
	}
	if n, err := w.WriteAt([]byte("world"), 5); err != nil || n != 5 {
		t.Fatalf("write 2: n=%d err=%v", n, err)
	}
	if _, err := w.WriteAt([]byte("x"), 3); err == nil {
		t.Fatal("expected write behind the stream to be rejected")
	}
	if got := sink.String(); got != "helloworld" {
		t.Fatalf("sink = %q, want %q", got, "helloworld")
	}
}

func TestStreamingWriterAtReorder(t *testing.T) {
	var sink bytes.Buffer
	w := &streamingWriterAt{w: &sink}

	// Arrival order 8, 4, 12, 0: everything parks until offset 0 lands.
	for _, off := range []int64{8, 4, 12} {
		chunk := []byte(strings.Repeat(string(rune('a'+off/4)), 4))
		if n, err := w.WriteAt(chunk, off); err != nil || n != 4 {
			t.Fatalf("park write at %d: n=%d err=%v", off, n, err)
		}
	}
	if sink.Len() != 0 {
		t.Fatalf("nothing should reach the sink before the gap fills, got %q", sink.String())
	}
	if n, err := w.WriteAt([]byte("aaaa"), 0); err != nil || n != 4 {
		t.Fatalf("gap-filling write: n=%d err=%v", n, err)
	}
	if got := sink.String(); got != "aaaabbbbccccdddd" {
		t.Fatalf("sink = %q, want %q", got, "aaaabbbbccccdddd")
	}
	if w.parked != 0 || len(w.pending) != 0 {
		t.Fatalf("parked buffer not drained: parked=%d pending=%d", w.parked, len(w.pending))
	}

	// Duplicate parked offset is a protocol violation.
	if _, err := w.WriteAt([]byte("zzzz"), 24); err != nil {
		t.Fatalf("park write at 24: %v", err)
	}
	if _, err := w.WriteAt([]byte("zzzz"), 24); err == nil {
		t.Fatal("expected duplicate parked offset to be rejected")
	}
}

func TestStreamingWriterAtReorderCap(t *testing.T) {
	var sink bytes.Buffer
	w := &streamingWriterAt{w: &sink}
	if _, err := w.WriteAt(make([]byte, sftpWriteReorderMax+1), 4); err == nil {
		t.Fatal("expected oversized out-of-order backlog to be rejected")
	}
	// The writer is now poisoned; even a sequential write fails.
	if _, err := w.WriteAt([]byte("a"), 0); err == nil {
		t.Fatal("expected sticky error after backlog overflow")
	}
}

func TestSFTPFileWriterCloseGap(t *testing.T) {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, pr)
		done <- err
	}()
	w := &sftpFileWriter{
		streamingWriterAt: &streamingWriterAt{w: pw},
		pw:                pw,
		done:              done,
	}
	if _, err := w.WriteAt([]byte("ab"), 0); err != nil {
		t.Fatalf("sequential write: %v", err)
	}
	if _, err := w.WriteAt([]byte("ef"), 4); err != nil {
		t.Fatalf("park write: %v", err)
	}
	if err := w.Close(); err == nil {
		t.Fatal("expected Close to fail on an upload with a hole")
	}
}

func TestStreamingWriterAtZeroLength(t *testing.T) {
	var sink bytes.Buffer
	w := &streamingWriterAt{w: &sink}
	if n, err := w.WriteAt(nil, 100); n != 0 || err != nil {
		t.Fatalf("zero-length write: n=%d err=%v", n, err)
	}
	if len(w.pending) != 0 {
		t.Fatalf("zero-length write must not park, pending=%d", len(w.pending))
	}
	if _, err := w.WriteAt([]byte("abcd"), 100); err != nil {
		t.Fatalf("real write at same offset after zero-length: %v", err)
	}
}

func TestSFTPFileWriterCloseAfterOverflow(t *testing.T) {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, pr)
		done <- err
	}()
	w := &sftpFileWriter{
		streamingWriterAt: &streamingWriterAt{w: pw},
		pw:                pw,
		done:              done,
	}
	if _, err := w.WriteAt([]byte("ab"), 0); err != nil {
		t.Fatalf("sequential write: %v", err)
	}
	if _, err := w.WriteAt(make([]byte, sftpWriteReorderMax+1), 4); err == nil {
		t.Fatal("expected overflow rejection")
	}
	if err := w.Close(); err == nil {
		t.Fatal("expected Close to fail after sticky stream error")
	}
}

func TestSFTPFileWriterTransferError(t *testing.T) {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, pr)
		done <- err
	}()
	w := &sftpFileWriter{
		streamingWriterAt: &streamingWriterAt{w: pw},
		pw:                pw,
		done:              done,
	}
	if _, err := w.WriteAt([]byte("partial"), 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.TransferError(io.ErrUnexpectedEOF)
	if err := w.Close(); err == nil {
		t.Fatal("expected Close to fail after TransferError")
	}
}

func TestRangeReaderAt(t *testing.T) {
	data := []byte("0123456789")
	opens := 0
	open := func(off int64) (io.ReadCloser, error) {
		opens++
		return io.NopCloser(bytes.NewReader(data[off:])), nil
	}
	r := &rangeReaderAt{size: int64(len(data)), open: open}
	defer r.Close()

	buf := make([]byte, 4)
	if n, err := r.ReadAt(buf, 0); n != 4 || err != nil || string(buf) != "0123" {
		t.Fatalf("seq read 0: n=%d err=%v buf=%q", n, err, buf)
	}
	if n, err := r.ReadAt(buf, 4); n != 4 || err != nil || string(buf) != "4567" {
		t.Fatalf("seq read 4: n=%d err=%v buf=%q", n, err, buf)
	}
	if opens != 1 {
		t.Fatalf("sequential reads should reuse one stream, opens=%d", opens)
	}

	// Backward seek must re-open the stream at the new offset.
	if n, err := r.ReadAt(buf, 1); n != 4 || err != nil || string(buf) != "1234" {
		t.Fatalf("seek read 1: n=%d err=%v buf=%q", n, err, buf)
	}
	if opens != 2 {
		t.Fatalf("backward seek should re-open, opens=%d", opens)
	}

	tail := make([]byte, 4)
	if n, err := r.ReadAt(tail, 8); n != 2 || err != io.EOF || string(tail[:2]) != "89" {
		t.Fatalf("tail read: n=%d err=%v buf=%q", n, err, tail[:2])
	}

	// Reads at or beyond size return io.EOF with no bytes.
	if n, err := r.ReadAt(buf, 10); n != 0 || err != io.EOF {
		t.Fatalf("eof read: n=%d err=%v", n, err)
	}

	// Negative offset is rejected without touching the stream.
	if _, err := r.ReadAt(buf, -1); err == nil {
		t.Fatal("expected error on negative offset")
	}
}

func TestRangeReaderAtConcurrent(t *testing.T) {
	const (
		chunk  = 4 << 10
		chunks = 64
		total  = chunk * chunks
	)
	data := make([]byte, total)
	for i := range data {
		data[i] = byte(i % 251)
	}

	var openMu sync.Mutex
	opens := 0
	open := func(off int64) (io.ReadCloser, error) {
		openMu.Lock()
		opens++
		openMu.Unlock()
		return io.NopCloser(bytes.NewReader(data[off:])), nil
	}
	r := &rangeReaderAt{size: total, open: open}
	defer r.Close()

	// FIFO dispatch channel feeding 8 workers, mirroring SftpServerWorkerCount.
	offsets := make(chan int64, chunks)
	for i := 0; i < chunks; i++ {
		offsets <- int64(i * chunk)
	}
	close(offsets)

	got := make([]byte, total)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, chunk)
			for off := range offsets {
				n, err := r.ReadAt(buf, off)
				if err != nil && err != io.EOF {
					errs <- err
					return
				}
				copy(got[off:], buf[:n])
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("reassembled download does not match source data")
	}
	if opens != 1 {
		t.Fatalf("concurrent sequential download should use one stream, opens=%d", opens)
	}
}

func TestRangeReaderAtFarSeek(t *testing.T) {
	const total = sftpReadAheadWindow + (64 << 10)
	data := make([]byte, total)
	opens := 0
	open := func(off int64) (io.ReadCloser, error) {
		opens++
		return io.NopCloser(bytes.NewReader(data[off:])), nil
	}
	r := &rangeReaderAt{size: total, open: open}
	defer r.Close()

	buf := make([]byte, 4096)
	if _, err := r.ReadAt(buf, 0); err != nil {
		t.Fatalf("first read: %v", err)
	}

	start := time.Now()
	if n, err := r.ReadAt(buf, total-4096); n != 4096 || (err != nil && err != io.EOF) {
		t.Fatalf("far seek read: n=%d err=%v", n, err)
	}
	if elapsed := time.Since(start); elapsed > sftpReadSequenceWait/2 {
		t.Fatalf("far seek should not wait for a predecessor, took %v", elapsed)
	}
	if opens != 2 {
		t.Fatalf("far seek should re-open exactly once, opens=%d", opens)
	}
}

func TestRangeReaderAtResumeStart(t *testing.T) {
	data := []byte(strings.Repeat("y", 64<<10))
	opens := 0
	open := func(off int64) (io.ReadCloser, error) {
		opens++
		return io.NopCloser(bytes.NewReader(data[off:])), nil
	}
	r := &rangeReaderAt{size: int64(len(data)), open: open}
	defer r.Close()

	start := time.Now()
	buf := make([]byte, 4096)
	if n, err := r.ReadAt(buf, 8192); n != 4096 || err != nil {
		t.Fatalf("resume read: n=%d err=%v", n, err)
	}
	if elapsed := time.Since(start); elapsed >= sftpReadSequenceWait {
		t.Fatalf("resume read should use the short start wait, took %v", elapsed)
	}
	if opens != 1 {
		t.Fatalf("resume read should open exactly once, opens=%d", opens)
	}
}

func TestRangeReaderAtTruncatedStream(t *testing.T) {
	data := []byte("0123456789")
	open := func(off int64) (io.ReadCloser, error) {
		// Stream claims size 20 but only ever has 10 bytes.
		return io.NopCloser(bytes.NewReader(data[off:])), nil
	}
	r := &rangeReaderAt{size: 20, open: open}
	defer r.Close()

	buf := make([]byte, 16)
	n, err := r.ReadAt(buf, 0)
	if n != 10 {
		t.Fatalf("truncated read returned n=%d, want 10", n)
	}
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("mid-object truncation must surface ErrUnexpectedEOF, got %v", err)
	}
}

func TestRangeReaderAtWaitTimeout(t *testing.T) {
	data := []byte(strings.Repeat("x", 64<<10))
	opens := 0
	open := func(off int64) (io.ReadCloser, error) {
		opens++
		return io.NopCloser(bytes.NewReader(data[off:])), nil
	}
	r := &rangeReaderAt{size: int64(len(data)), open: open}
	defer r.Close()

	buf := make([]byte, 4096)
	if _, err := r.ReadAt(buf, 0); err != nil {
		t.Fatalf("first read: %v", err)
	}
	// Gap at 4096..8192 never fills; the read at 8192 must time out and re-open.
	if n, err := r.ReadAt(buf, 8192); err != nil || n != 4096 {
		t.Fatalf("timed-out read: n=%d err=%v", n, err)
	}
	if opens != 2 {
		t.Fatalf("expected timeout fallback to re-open once, opens=%d", opens)
	}
}

type recordedActivity struct {
	source, accessKey, api, bucket, object string
	status                                 int
}

// Replace activity recorder until test ends
func swapRecordActivity(t *testing.T) *[]recordedActivity {
	t.Helper()
	got := &[]recordedActivity{}
	saved := recordActivity
	recordActivity = func(source, accessKey, api, bucket, object string, status int) {
		*got = append(*got, recordedActivity{source, accessKey, api, bucket, object, status})
	}
	t.Cleanup(func() { recordActivity = saved })
	return got
}

func TestSFTPFileWriterRecordsActivity(t *testing.T) {
	got := swapRecordActivity(t)
	newWriter := func(putErr error) *sftpFileWriter {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() {
			_, err := io.Copy(io.Discard, pr)
			if putErr != nil {
				err = putErr
			}
			done <- err
		}()
		return &sftpFileWriter{
			streamingWriterAt: &streamingWriterAt{w: pw},
			pw:                pw,
			done:              done,
			accessKey:         "sftpuser",
			bucket:            "alpha",
			object:            "docs/a.txt",
		}
	}

	ok := newWriter(nil)
	if _, err := ok.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ok.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	want := []recordedActivity{{"sftp", "sftpuser", "PutObject", "alpha", "docs/a.txt", http.StatusOK}}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("recorded %+v, want %+v", *got, want)
	}

	aborted := newWriter(nil)
	if _, err := aborted.WriteAt([]byte("partial"), 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	aborted.TransferError(io.ErrUnexpectedEOF)
	if err := aborted.Close(); err == nil {
		t.Fatal("expected Close to fail after TransferError")
	}
	want = append(want, recordedActivity{"sftp", "sftpuser", "PutObject", "alpha", "docs/a.txt", http.StatusInternalServerError})
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("interrupted upload recorded %+v, want %+v", *got, want)
	}

	refused := newWriter(os.ErrPermission)
	if _, err := refused.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := refused.Close(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("close = %v, want the PutObject error", err)
	}
	want = append(want, recordedActivity{"sftp", "sftpuser", "PutObject", "alpha", "docs/a.txt", http.StatusInternalServerError})
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("storage failure recorded %+v, want %+v", *got, want)
	}

	gap := newWriter(nil)
	if _, err := gap.WriteAt([]byte("missing prefix"), 10); err != nil {
		t.Fatalf("park write: %v", err)
	}
	if err := gap.Close(); err == nil {
		t.Fatal("expected Close to fail on an upload with a hole")
	}
	want = append(want, recordedActivity{"sftp", "sftpuser", "PutObject", "alpha", "docs/a.txt", http.StatusInternalServerError})
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("incomplete upload recorded %+v, want %+v", *got, want)
	}

	overflow := newWriter(nil)
	if _, err := overflow.WriteAt(make([]byte, sftpWriteReorderMax+1), 10); err == nil {
		t.Fatal("expected overflow rejection")
	}
	if err := overflow.Close(); err == nil {
		t.Fatal("expected Close to fail after sticky stream error")
	}
	want = append(want, recordedActivity{"sftp", "sftpuser", "PutObject", "alpha", "docs/a.txt", http.StatusInternalServerError})
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("overflowed upload recorded %+v, want %+v", *got, want)
	}
}

func TestSFTPRecordStatus(t *testing.T) {
	got := swapRecordActivity(t)
	d := newSFTPDriver("sftpuser")
	if err := d.record("DeleteObject", "alpha", "a.txt", nil); err != nil {
		t.Fatalf("record success = %v, want nil", err)
	}
	boom := errors.New("boom")
	if err := d.record("RenameObject", "alpha", "b.txt", boom); err != boom {
		t.Fatalf("record failure = %v, want the operation's error", err)
	}
	want := []recordedActivity{
		{"sftp", "sftpuser", "DeleteObject", "alpha", "a.txt", http.StatusOK},
		{"sftp", "sftpuser", "RenameObject", "alpha", "b.txt", http.StatusInternalServerError},
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("recorded %+v, want %+v", *got, want)
	}
}
