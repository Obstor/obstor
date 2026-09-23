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

package cmd

import (
	"bytes"
	"context"
	"testing"
)

// All shards empty means a 0-byte payload shouldnt have an error.
func TestDecodeDataBlocksAllEmpty(t *testing.T) {
	e, err := NewErasure(context.Background(), 4, 2, blockSizeV2)
	if err != nil {
		t.Fatal(err)
	}
	data := make([][]byte, 6)
	if derr := e.DecodeDataBlocks(data); derr != nil {
		t.Fatalf("all-empty: want nil, got %v", derr)
	}
}

// A missing data shard with parity must reconstruct properly
func TestDecodeDataBlocksReconstructsMissing(t *testing.T) {
	e, err := NewErasure(context.Background(), 4, 2, blockSizeV2)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i)
	}
	enc, err := e.EncodeData(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	saved := append([]byte(nil), enc[0]...)
	enc[0] = nil
	if derr := e.DecodeDataBlocks(enc); derr != nil {
		t.Fatalf("reconstruct: %v", derr)
	}
	if !bytes.Equal(enc[0], saved) {
		t.Fatalf("reconstructed shard mismatch")
	}
}
