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

import "testing"

func TestGCTuning(t *testing.T) {
	const GiB = int64(1) << 30
	cases := []struct {
		name         string
		totalRAM     uint64
		gomemSet     bool
		limit        string
		gogc         string
		wantLimit    int64
		wantGC       int
		wantSetLimit bool
		wantSetGC    bool
		wantErr      bool
	}{
		{"default 80pct of RAM, paired GOGC 200", uint64(10 * GiB), false, "", "", 10 * GiB * 80 / 100, 200, true, true, false},
		{"explicit bytes pairs GOGC 200", 0, false, "8GiB", "", 8 * GiB, 200, true, true, false},
		{"explicit percent of RAM", uint64(16 * GiB), false, "50%", "", 8 * GiB, 200, true, true, false},
		{"native GOMEMLIMIT set: do not override", uint64(10 * GiB), true, "", "", 0, 0, false, false, false},
		{"GOGC override still defaults limit", uint64(10 * GiB), false, "", "300", 10 * GiB * 80 / 100, 300, true, true, false},
		{"GOGC override with explicit limit", 0, false, "4GiB", "150", 4 * GiB, 150, true, true, false},
		{"GOGC -1 disables", uint64(10 * GiB), false, "", "-1", 10 * GiB * 80 / 100, -1, true, true, false},
		{"no RAM, no overrides: nothing", 0, false, "", "", 0, 0, false, false, false},
		{"bad limit errors", 0, false, "notabyte", "", 0, 0, false, false, true},
		{"percent needs RAM", 0, false, "50%", "", 0, 0, false, false, true},
		{"percent over 100 errors", uint64(10 * GiB), false, "150%", "", 0, 0, false, false, true},
		{"bad GOGC errors", uint64(10 * GiB), false, "", "abc", 0, 0, false, false, true},
		{"GOGC zero errors", uint64(10 * GiB), false, "", "0", 0, 0, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lim, gc, setLim, setGC, err := gcTuning(c.totalRAM, c.gomemSet, c.limit, c.gogc)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (lim=%d gc=%d)", lim, gc)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if setLim != c.wantSetLimit || setGC != c.wantSetGC {
				t.Fatalf("set flags: got setLimit=%v setGC=%v, want %v %v", setLim, setGC, c.wantSetLimit, c.wantSetGC)
			}
			if setLim && lim != c.wantLimit {
				t.Fatalf("memLimit: got %d, want %d", lim, c.wantLimit)
			}
			if setGC && gc != c.wantGC {
				t.Fatalf("gcPercent: got %d, want %d", gc, c.wantGC)
			}
		})
	}
}
