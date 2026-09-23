/*
 * MinIO Cloud Storage, (C) 2017 MinIO, Inc.
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
	"os"
	"runtime/debug"

	"github.com/obstor/obstor/pkg/env"
	"github.com/obstor/obstor/pkg/sys"
)

func setMaxResources() (err error) {
	// Set the Go runtime max threads threshold to 90% of kernel setting.
	sysMaxThreads, mErr := sys.GetMaxThreads()
	if mErr == nil {
		obstorMaxThreads := (sysMaxThreads * 90) / 100
		// Only set max threads if it is greater than the default one
		if obstorMaxThreads > 10000 {
			debug.SetMaxThreads(obstorMaxThreads)
		}
	}

	var maxLimit uint64

	// Set open files limit to maximum.
	if _, maxLimit, err = sys.GetMaxOpenFileLimit(); err != nil {
		return err
	}

	if err = sys.SetMaxOpenFileLimit(maxLimit, maxLimit); err != nil {
		return err
	}

	// Set max memory limit as current memory limit.
	if _, maxLimit, err = sys.GetMaxMemoryLimit(); err != nil {
		return err
	}

	if err = sys.SetMaxMemoryLimit(maxLimit, maxLimit); err != nil {
		return err
	}

	// GC tuning: pace the runtime against a real memory ceiling
	_, gomemSet := os.LookupEnv("GOMEMLIMIT")
	var totalRAM uint64
	if st, sErr := sys.GetStats(); sErr == nil {
		totalRAM = st.TotalRAM
	}
	memLimit, gcPercent, setLimit, setGC, tErr := gcTuning(totalRAM, gomemSet,
		env.Get("OBSTOR_GOMEMLIMIT", ""), env.Get("OBSTOR_GOGC", ""))
	if tErr != nil {
		return tErr
	}
	if setLimit {
		debug.SetMemoryLimit(memLimit)
	}
	if setGC {
		debug.SetGCPercent(gcPercent)
	}
	return nil
}
