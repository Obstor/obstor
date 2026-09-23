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

package ioutil

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

// Hides ReaderFrom/WriterTo to force Copy with pooled-buffer path, matches GET egress.
type plainWriter struct{ w io.Writer }

func (p plainWriter) Write(b []byte) (int, error) { return p.w.Write(b) }

type plainReader struct{ r io.Reader }

func (p plainReader) Read(b []byte) (int, error) { return p.r.Read(b) }

func TestCopyByteExact(t *testing.T) {
	for _, size := range []int{0, 1, 1024, copyBufferSize - 1, copyBufferSize, copyBufferSize + 1, 3*copyBufferSize + 123} {
		data := make([]byte, size)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		var sink bytes.Buffer
		n, err := Copy(plainWriter{&sink}, plainReader{bytes.NewReader(data)})
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if n != int64(size) {
			t.Fatalf("size %d: copied %d bytes", size, n)
		}
		if !bytes.Equal(sink.Bytes(), data) {
			t.Fatalf("size %d: output mismatch", size)
		}
	}
}

func BenchmarkCopyPooled(b *testing.B) {
	data := make([]byte, 8*copyBufferSize)
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Copy(plainWriter{io.Discard}, plainReader{bytes.NewReader(data)})
	}
}

func BenchmarkCopyStdlib32k(b *testing.B) {
	data := make([]byte, 8*copyBufferSize)
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		io.Copy(plainWriter{io.Discard}, plainReader{bytes.NewReader(data)})
	}
}
