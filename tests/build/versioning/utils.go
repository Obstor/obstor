/*
*
*  Mint, (C) 2021 Minio, Inc.
*  PGG Obstor, (C) 2021-2026 PGG, Inc.
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software

*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
 */

package main

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyz01234569"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// different kinds of test failures
const (
	PASS = "PASS" // Indicate that a test passed
	FAIL = "FAIL" // Indicate that a test failed
)

type errorResponse struct {
	XMLName    xml.Name `xml:"Error" json:"-"`
	Code       string
	Message    string
	BucketName string
	Key        string
	RequestID  string `xml:"RequestId"`
	HostID     string `xml:"HostId"`

	// Region where the bucket is located. This header is returned
	// only in HEAD bucket and ListObjects response.
	Region string

	// Headers of the returned S3 XML error
	Headers http.Header `xml:"-" json:"-"`
}

// log successful test runs
func successLogger(function string, args map[string]interface{}, startTime time.Time) {
	// calculate the test case duration
	duration := time.Since(startTime)
	// log with the fields as per tests
	slog.Info("test passed", "name", "versioning", "function", function, "args", args, "duration", duration.Nanoseconds()/1000000, "status", PASS)
}

// log not applicable test runs
func ignoreLog(function string, args map[string]interface{}, startTime time.Time, alert string) {
	// calculate the test case duration
	duration := time.Since(startTime)
	// log with the fields as per tests
	slog.Info("test skipped", "name", "versioning", "function", function, "args", args,
		"duration", duration.Nanoseconds()/1000000, "status", "NA", "alert", strings.Split(alert, " ")[0]+" is NotImplemented")
}

// log failed test runs and exit
func failureLog(function string, args map[string]interface{}, startTime time.Time, alert string, message string, err error) {
	// calculate the test case duration
	duration := time.Since(startTime)
	// log with the fields as per tests
	if pc, file, line, ok := runtime.Caller(1); ok {
		function = fmt.Sprintf("%s:%d: %s", file, line, runtime.FuncForPC(pc).Name())
	}
	if err != nil {
		slog.Error("test failed", "name", "versioning", "function", function, "args", args,
			"duration", duration.Nanoseconds()/1000000, "status", FAIL, "alert", alert, "message", message, "error", err.Error())
	} else {
		slog.Error("test failed", "name", "versioning", "function", function, "args", args,
			"duration", duration.Nanoseconds()/1000000, "status", FAIL, "alert", alert, "message", message)
	}
	os.Exit(1)
}

func randString(n int, src rand.Source, prefix string) string {
	b := make([]byte, n)
	// A rand.Int63() generates 63 random bits, enough for letterIdxMax letters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}
	return prefix + string(b[0:30-len(prefix)])
}
