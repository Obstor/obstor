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

// Skip the runtime.Caller stack return empty unless lock
func TestGetSourceGated(t *testing.T) {
	lockSourceTrace.Store(false)
	if s := getSource(1); s != "" {
		t.Fatalf("trace off: want empty source, got %q", s)
	}
	lockSourceTrace.Store(true)
	defer lockSourceTrace.Store(false)
	if s := getSource(1); s == "" {
		t.Fatalf("trace on: want non-empty source, got empty")
	}
}
