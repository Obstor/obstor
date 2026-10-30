/*
 * PGG Obstor, (C) 2021-2026 PGG, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package sftp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	obstor "github.com/obstor/obstor/cmd"
	"github.com/obstor/obstor/pkg/hash"
	xsftp "github.com/pkg/sftp"
)

// 5GB file max for uploads
const sftpMaxFileSize = 5 << 30

// Records SFTP mutations
var recordActivity = obstor.RecordActivity

// sftp.FileReader, sftp.FileWriter, sftp.FileCmder, sftp.FileLister.
type sftpDriver struct {
	accessKey string
}

func newSFTPDriver(accessKey string) *sftpDriver {
	return &sftpDriver{accessKey: accessKey}
}

// Split an SFTP path into bucket and object key.
func parseSFTPPath(p string) (bucket, object string) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", ""
	}
	parts := strings.SplitN(p, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		object = parts[1]
	}
	return
}

func (d *sftpDriver) getObjectLayer() (obstor.ObjectLayer, error) {
	objAPI := obstor.NewObjectLayerFn()
	if objAPI == nil {
		return nil, fmt.Errorf("object layer not initialized")
	}
	return objAPI, nil
}

// Fileread implements sftp.FileReader.
func (d *sftpDriver) Fileread(r *xsftp.Request) (io.ReaderAt, error) {
	bucket, object := parseSFTPPath(r.Filepath)
	if bucket == "" || object == "" {
		return nil, os.ErrInvalid
	}
	if err := obstor.CheckSFTPAccess(d.accessKey, bucket, object, obstor.SFTPActionGetObject); err != nil {
		return nil, os.ErrPermission
	}

	objAPI, err := d.getObjectLayer()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	oi, err := objAPI.GetObjectInfo(ctx, bucket, object, obstor.ObjectOptions{})
	if err != nil {
		return nil, sftpErrorMap(err)
	}

	return &rangeReaderAt{
		size: oi.Size,
		open: func(off int64) (io.ReadCloser, error) {
			var rs *obstor.HTTPRangeSpec
			if off > 0 {
				rs = &obstor.HTTPRangeSpec{Start: off, End: -1}
			}
			gr, gerr := objAPI.GetObjectNInfo(ctx, bucket, object, rs, nil, obstor.ReadLock, obstor.ObjectOptions{})
			if gerr != nil {
				return nil, sftpErrorMap(gerr)
			}
			return gr, nil
		},
	}, nil
}

// Filewrite implements sftp.FileWriter.
func (d *sftpDriver) Filewrite(r *xsftp.Request) (io.WriterAt, error) {
	bucket, object := parseSFTPPath(r.Filepath)
	if bucket == "" || object == "" {
		return nil, os.ErrInvalid
	}
	if err := obstor.CheckSFTPAccess(d.accessKey, bucket, object, obstor.SFTPActionPutObject); err != nil {
		return nil, os.ErrPermission
	}

	objAPI, err := d.getObjectLayer()
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		hashReader, herr := hash.NewReader(pr, -1, "", "", -1)
		if herr != nil {
			_ = pr.CloseWithError(herr)
			done <- herr
			return
		}
		_, perr := objAPI.PutObject(context.Background(), bucket, object, obstor.NewPutObjReader(hashReader), obstor.ObjectOptions{})
		_ = pr.CloseWithError(perr)
		done <- sftpErrorMap(perr)
	}()

	return &sftpFileWriter{
		streamingWriterAt: &streamingWriterAt{w: pw},
		pw:                pw,
		done:              done,
		accessKey:         d.accessKey,
		bucket:            bucket,
		object:            object,
	}, nil
}

// Filecmd implements sftp.FileCmder.
func (d *sftpDriver) Filecmd(r *xsftp.Request) error {
	ctx := context.Background()

	objAPI, err := d.getObjectLayer()
	if err != nil {
		return err
	}

	switch r.Method {
	case "Setstat":
		return nil // No-op, S3 doesn't support chmod/chown/chtimes.

	case "Rename":
		srcBucket, srcObject := parseSFTPPath(r.Filepath)
		dstBucket, dstObject := parseSFTPPath(r.Target)
		if srcBucket == "" || srcObject == "" || dstBucket == "" || dstObject == "" {
			return os.ErrInvalid
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, srcBucket, srcObject, obstor.SFTPActionGetObject); err != nil {
			return os.ErrPermission
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, dstBucket, dstObject, obstor.SFTPActionPutObject); err != nil {
			return os.ErrPermission
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, srcBucket, srcObject, obstor.SFTPActionDeleteObject); err != nil {
			return os.ErrPermission
		}
		if srcBucket == dstBucket && srcObject == dstObject {
			if _, err := objAPI.GetObjectInfo(ctx, srcBucket, srcObject, obstor.ObjectOptions{}); err != nil {
				return sftpErrorMap(err)
			}
			return nil
		}
		return d.record("RenameObject", dstBucket, dstObject, renameObject(ctx, objAPI, srcBucket, srcObject, dstBucket, dstObject))

	case "Rmdir":
		bucket, prefix := parseSFTPPath(r.Filepath)
		if bucket == "" {
			return os.ErrPermission
		}
		if prefix == "" {
			// Deleting a bucket.
			if err := obstor.CheckSFTPAccess(d.accessKey, bucket, "", obstor.SFTPActionDeleteBucket); err != nil {
				return os.ErrPermission
			}
			return d.record("DeleteBucket", bucket, "", sftpErrorMap(objAPI.DeleteBucket(ctx, bucket, false)))
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, bucket, prefix, obstor.SFTPActionDeleteObject); err != nil {
			return os.ErrPermission
		}
		// Delete directory marker if it exists.
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		_, _ = objAPI.DeleteObject(ctx, bucket, prefix, obstor.ObjectOptions{})
		return d.record("DeleteObject", bucket, prefix, nil)

	case "Mkdir":
		bucket, prefix := parseSFTPPath(r.Filepath)
		if bucket == "" {
			return os.ErrInvalid
		}
		if prefix == "" {
			// Creating a bucket.
			if err := obstor.CheckSFTPAccess(d.accessKey, bucket, "", obstor.SFTPActionCreateBucket); err != nil {
				return os.ErrPermission
			}
			return d.record("PutBucket", bucket, "", sftpErrorMap(objAPI.MakeBucketWithLocation(ctx, bucket, obstor.BucketOptions{})))
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, bucket, prefix, obstor.SFTPActionPutObject); err != nil {
			return os.ErrPermission
		}
		// Create a directory marker object.
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		hashReader, err := hash.NewReader(bytes.NewReader(nil), 0, "", "", 0)
		if err != nil {
			return d.record("PutObject", bucket, prefix, err)
		}
		_, err = objAPI.PutObject(ctx, bucket, prefix, obstor.NewPutObjReader(hashReader), obstor.ObjectOptions{})
		return d.record("PutObject", bucket, prefix, sftpErrorMap(err))

	case "Remove":
		bucket, object := parseSFTPPath(r.Filepath)
		if bucket == "" || object == "" {
			return os.ErrInvalid
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, bucket, object, obstor.SFTPActionDeleteObject); err != nil {
			return os.ErrPermission
		}
		_, err := objAPI.DeleteObject(ctx, bucket, object, obstor.ObjectOptions{})
		return d.record("DeleteObject", bucket, object, sftpErrorMap(err))

	case "Symlink":
		return fmt.Errorf("symlinks are not supported")

	case "Link":
		return fmt.Errorf("hard links are not supported")
	}

	return fmt.Errorf("unsupported command: %s", r.Method)
}

// Record authorized SFTP mutation
func (d *sftpDriver) record(api, bucket, object string, err error) error {
	status := http.StatusOK
	if err != nil {
		status = http.StatusInternalServerError
	}
	recordActivity("sftp", d.accessKey, api, bucket, object, status)
	return err
}

// Copy source then delete it
func renameObject(ctx context.Context, objAPI obstor.ObjectLayer, srcBucket, srcObject, dstBucket, dstObject string) error {
	reader, err := objAPI.GetObjectNInfo(ctx, srcBucket, srcObject, nil, nil, obstor.ReadLock, obstor.ObjectOptions{})
	if err != nil {
		return sftpErrorMap(err)
	}
	srcSize := reader.ObjInfo.Size
	hashReader, err := hash.NewReader(reader, srcSize, "", "", srcSize)
	if err != nil {
		_ = reader.Close()
		return err
	}
	_, perr := objAPI.PutObject(ctx, dstBucket, dstObject, obstor.NewPutObjReader(hashReader), obstor.ObjectOptions{})
	_ = reader.Close()
	if perr != nil {
		return sftpErrorMap(perr)
	}
	if _, err := objAPI.DeleteObject(ctx, srcBucket, srcObject, obstor.ObjectOptions{}); err != nil {
		return sftpErrorMap(err)
	}
	return nil
}

// Filelist implements sftp.FileLister.
func (d *sftpDriver) Filelist(r *xsftp.Request) (xsftp.ListerAt, error) {
	ctx := context.Background()

	objAPI, err := d.getObjectLayer()
	if err != nil {
		return nil, err
	}

	bucket, prefix := parseSFTPPath(r.Filepath)

	switch r.Method {
	case "List":
		if bucket == "" {
			// List buckets.
			buckets, err := objAPI.ListBuckets(ctx)
			if err != nil {
				return nil, sftpErrorMap(err)
			}
			entries := make([]os.FileInfo, 0, len(buckets))
			for _, b := range buckets {
				if err := obstor.CheckSFTPAccess(d.accessKey, b.Name, "", obstor.SFTPActionListBucket); err != nil {
					continue
				}
				entries = append(entries, &sftpFileInfo{
					name:    b.Name,
					size:    0,
					mode:    os.ModeDir | 0755,
					modTime: b.Created,
					isDir:   true,
				})
			}
			return listerAt(entries), nil
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, bucket, "", obstor.SFTPActionListBucket); err != nil {
			return nil, os.ErrPermission
		}

		// List objects in bucket with prefix.
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		result, err := objAPI.ListObjects(ctx, bucket, prefix, "", "/", 10000)
		if err != nil {
			return nil, sftpErrorMap(err)
		}

		entries := make([]os.FileInfo, 0, len(result.Prefixes)+len(result.Objects))
		// Directories (common prefixes).
		for _, p := range result.Prefixes {
			name := strings.TrimPrefix(p, prefix)
			name = strings.TrimSuffix(name, "/")
			if name == "" {
				continue
			}
			entries = append(entries, &sftpFileInfo{
				name:    name,
				size:    0,
				mode:    os.ModeDir | 0755,
				modTime: time.Time{},
				isDir:   true,
			})
		}
		// Objects.
		for _, obj := range result.Objects {
			name := strings.TrimPrefix(obj.Name, prefix)
			if name == "" || strings.HasSuffix(name, "/") {
				continue // skip directory markers
			}
			entries = append(entries, &sftpFileInfo{
				name:    name,
				size:    obj.Size,
				mode:    0644,
				modTime: obj.ModTime,
				isDir:   false,
			})
		}
		return listerAt(entries), nil

	case "Stat":
		if bucket == "" {
			// Stat root.
			return listerAt([]os.FileInfo{&sftpFileInfo{
				name:    "/",
				size:    0,
				mode:    os.ModeDir | 0755,
				modTime: time.Now(),
				isDir:   true,
			}}), nil
		}
		if err := obstor.CheckSFTPAccess(d.accessKey, bucket, prefix, obstor.SFTPActionListBucket); err != nil {
			return nil, os.ErrPermission
		}
		if prefix == "" {
			// Stat a bucket.
			bi, err := objAPI.GetBucketInfo(ctx, bucket)
			if err != nil {
				return nil, sftpErrorMap(err)
			}
			return listerAt([]os.FileInfo{&sftpFileInfo{
				name:    bi.Name,
				size:    0,
				mode:    os.ModeDir | 0755,
				modTime: bi.Created,
				isDir:   true,
			}}), nil
		}
		// Stat an object.
		oi, err := objAPI.GetObjectInfo(ctx, bucket, prefix, obstor.ObjectOptions{})
		if err != nil {
			// Maybe it's a directory prefix.
			dirPrefix := prefix
			if !strings.HasSuffix(dirPrefix, "/") {
				dirPrefix += "/"
			}
			result, listErr := objAPI.ListObjects(ctx, bucket, dirPrefix, "", "/", 1)
			if listErr != nil {
				return nil, sftpErrorMap(err)
			}
			if len(result.Objects) > 0 || len(result.Prefixes) > 0 {
				name := strings.TrimSuffix(prefix, "/")
				if idx := strings.LastIndex(name, "/"); idx >= 0 {
					name = name[idx+1:]
				}
				return listerAt([]os.FileInfo{&sftpFileInfo{
					name:    name,
					size:    0,
					mode:    os.ModeDir | 0755,
					modTime: time.Time{},
					isDir:   true,
				}}), nil
			}
			return nil, os.ErrNotExist
		}
		name := oi.Name
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		return listerAt([]os.FileInfo{&sftpFileInfo{
			name:    name,
			size:    oi.Size,
			mode:    0644,
			modTime: oi.ModTime,
			isDir:   false,
		}}), nil

	case "Readlink":
		return nil, fmt.Errorf("readlink is not supported")
	}

	return nil, fmt.Errorf("unsupported list method: %s", r.Method)
}

const sftpWriteReorderMax = 16 << 20

type streamingWriterAt struct {
	mu      sync.Mutex
	w       io.Writer
	off     int64            // next offset the pipe expects
	pending map[int64][]byte // parked out-of-order chunks keyed by offset
	parked  int64            // total bytes currently parked
	err     error            // sticky stream error
}

func (s *streamingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("invalid negative offset: %d", off)
	}
	if len(p) == 0 {
		// Parking an empty chunk would poison its offset for the real write.
		return 0, nil
	}
	if int64(len(p)) > sftpMaxFileSize-off {
		return 0, fmt.Errorf("file size exceeds maximum allowed (%d bytes)", sftpMaxFileSize)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return 0, s.err
	}

	switch {
	case off == s.off:
		n, err := s.w.Write(p)
		s.off += int64(n)
		if err != nil {
			s.err = err
			return n, err
		}
		if err := s.flushParkedLocked(); err != nil {
			return n, err
		}
		return n, nil

	case off > s.off:
		// Concurrent worker is ahead of the stream; park until gap fills.
		if _, dup := s.pending[off]; dup {
			return 0, fmt.Errorf("duplicate SFTP write at offset %d", off)
		}
		if s.parked+int64(len(p)) > sftpWriteReorderMax {
			s.err = fmt.Errorf("out-of-order SFTP write backlog exceeds %d bytes (offset %d, expected %d)", int64(sftpWriteReorderMax), off, s.off)
			return 0, s.err
		}
		buf := make([]byte, len(p))
		copy(buf, p)
		if s.pending == nil {
			s.pending = make(map[int64][]byte)
		}
		s.pending[off] = buf
		s.parked += int64(len(p))
		return len(p), nil

	default: // off < s.off: rewriting already-streamed bytes is impossible
		return 0, fmt.Errorf("non-sequential SFTP write at offset %d (expected %d): streaming uploads cannot rewrite earlier bytes", off, s.off)
	}
}

// flushParkedLocked drains parked chunks that have become sequential.
func (s *streamingWriterAt) flushParkedLocked() error {
	for {
		p, ok := s.pending[s.off]
		if !ok {
			return nil
		}
		delete(s.pending, s.off)
		s.parked -= int64(len(p))
		n, err := s.w.Write(p)
		s.off += int64(n)
		if err != nil {
			s.err = err
			return err
		}
	}
}

type sftpFileWriter struct {
	*streamingWriterAt
	pw        *io.PipeWriter
	done      chan error
	accessKey string
	bucket    string
	object    string
}

func (w *sftpFileWriter) Close() (err error) {
	defer func() {
		status := http.StatusOK
		if err != nil {
			status = http.StatusInternalServerError
		}
		recordActivity("sftp", w.accessKey, "PutObject", w.bucket, w.object, status)
	}()

	w.mu.Lock()
	leftover := w.parked
	streamErr := w.err
	w.mu.Unlock()

	if streamErr != nil {
		_ = w.pw.CloseWithError(streamErr)
		<-w.done
		return streamErr
	}
	if leftover > 0 {
		err := fmt.Errorf("incomplete SFTP upload: %d bytes of out-of-order writes never joined the stream", leftover)
		_ = w.pw.CloseWithError(err)
		<-w.done
		return err
	}

	// Signal EOF to PutObject, then wait for it to finish.
	if err := w.pw.Close(); err != nil {
		return err
	}
	return <-w.done
}

func (w *sftpFileWriter) TransferError(err error) {
	w.mu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.mu.Unlock()
	_ = w.pw.CloseWithError(err)
}

const (
	sftpReadAheadWindow = 8 << 20
)

var sftpReadSequenceWait = 2 * time.Second

var sftpReadStartWait = 250 * time.Millisecond

type rangeReaderAt struct {
	mu     sync.Mutex
	seq    sync.Cond // lazily bound to mu; signaled whenever curOff changes
	size   int64
	open   func(off int64) (io.ReadCloser, error)
	cur    io.ReadCloser
	curOff int64
	closed bool
}

func (r *rangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("invalid negative offset: %d", off)
	}
	if len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	if r.seq.L == nil {
		r.seq.L = &r.mu
	}
	defer r.mu.Unlock()
	defer r.seq.Broadcast()

	if r.closed {
		return 0, os.ErrClosed
	}
	if off >= r.size {
		return 0, io.EOF
	}

	if off > r.curOff && off-r.curOff <= sftpReadAheadWindow {
		wait := sftpReadSequenceWait
		if r.cur == nil && r.curOff == 0 {
			wait = sftpReadStartWait
		}
		expired := false
		t := time.AfterFunc(wait, func() {
			r.mu.Lock()
			expired = true
			r.mu.Unlock()
			r.seq.Broadcast()
		})
		for !expired && !r.closed && off > r.curOff && off-r.curOff <= sftpReadAheadWindow {
			r.seq.Wait()
		}
		t.Stop()
		if r.closed {
			return 0, os.ErrClosed
		}
	}

	if r.cur == nil || off != r.curOff {
		if r.cur != nil {
			_ = r.cur.Close()
			r.cur = nil
		}
		rc, err := r.open(off)
		if err != nil {
			return 0, err
		}
		r.cur = rc
		r.curOff = off
	}

	// Never read past the logical end of the object.
	want := int64(len(p))
	atEnd := false
	if off+want >= r.size {
		want = r.size - off
		atEnd = true
	}

	n, err := io.ReadFull(r.cur, p[:want])
	r.curOff += int64(n)
	if err == io.ErrUnexpectedEOF && atEnd {
		err = io.EOF
	}
	if err == nil && atEnd {
		// Caller asked for more than remained; signal EOF per io.ReaderAt.
		err = io.EOF
	}
	if err != nil && err != io.EOF {
		// Stream is in an unknown state; drop it so the next call re-opens.
		_ = r.cur.Close()
		r.cur = nil
	}
	return n, err
}

func (r *rangeReaderAt) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.seq.L != nil {
		r.seq.Broadcast() // release readers waiting for a predecessor
	}
	if r.cur != nil {
		err := r.cur.Close()
		r.cur = nil
		return err
	}
	return nil
}

// Implement sftp.ListerAt
type listerAt []os.FileInfo

func (l listerAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n+int(offset) >= len(l) {
		return n, io.EOF
	}
	return n, nil
}

// Implement os.FileInfo
type sftpFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *sftpFileInfo) Name() string       { return fi.name }
func (fi *sftpFileInfo) Size() int64        { return fi.size }
func (fi *sftpFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *sftpFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *sftpFileInfo) IsDir() bool        { return fi.isDir }
func (fi *sftpFileInfo) Sys() interface{}   { return nil }

// Map object layer errors to OS-level errors that SFTP clients understand.
func sftpErrorMap(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case obstor.IsErrObjectNotFound(err), obstor.IsErrVersionNotFound(err):
		return os.ErrNotExist
	case obstor.IsErrBucketNotFound(err):
		return os.ErrNotExist
	}
	switch err.(type) {
	case obstor.BucketAlreadyExists, obstor.BucketAlreadyOwnedByYou:
		return os.ErrExist
	case obstor.BucketNotEmpty:
		return fmt.Errorf("directory not empty")
	}
	return err
}
