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
	"fmt"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
)

// Decide soft memory limit and GC percentage for startup
func gcTuning(totalRAM uint64, gomemlimitSet bool, obstorLimit, obstorGOGC string) (memLimit int64, gcPercent int, setLimit, setGC bool, err error) {
	obstorLimit = strings.TrimSpace(obstorLimit)
	obstorGOGC = strings.TrimSpace(obstorGOGC)

	if obstorGOGC != "" {
		gcPercent, err = strconv.Atoi(obstorGOGC)
		if err != nil {
			return 0, 0, false, false, fmt.Errorf("invalid OBSTOR_GOGC %q: %w", obstorGOGC, err)
		}
		if gcPercent != -1 && gcPercent <= 0 {
			return 0, 0, false, false, fmt.Errorf("invalid OBSTOR_GOGC %q: must be -1 (off) or a positive percent", obstorGOGC)
		}
		setGC = true
	}

	switch {
	case obstorLimit != "":
		if strings.HasSuffix(obstorLimit, "%") {
			pct, perr := strconv.ParseFloat(strings.TrimSuffix(obstorLimit, "%"), 64)
			if perr != nil || pct <= 0 || pct > 100 {
				return 0, 0, false, false, fmt.Errorf("invalid OBSTOR_GOMEMLIMIT percent %q", obstorLimit)
			}
			if totalRAM == 0 {
				return 0, 0, false, false, fmt.Errorf("OBSTOR_GOMEMLIMIT %q needs detectable system RAM", obstorLimit)
			}
			memLimit = int64(float64(totalRAM) * pct / 100)
		} else {
			b, berr := humanize.ParseBytes(obstorLimit)
			if berr != nil {
				return 0, 0, false, false, fmt.Errorf("invalid OBSTOR_GOMEMLIMIT %q: %w", obstorLimit, berr)
			}
			memLimit = int64(b)
		}
		setLimit = true
	case !gomemlimitSet && totalRAM > 0:
		memLimit = int64(float64(totalRAM) * 0.8)
		setLimit = true
	}

	if memLimit < 0 {
		return 0, 0, false, false, fmt.Errorf("computed memory limit overflowed int64")
	}

	if setLimit && !setGC {
		gcPercent = 200
		setGC = true
	}

	return memLimit, gcPercent, setLimit, setGC, nil
}
