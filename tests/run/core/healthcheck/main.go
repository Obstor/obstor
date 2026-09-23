/*
*
*  Mint, (C) 2019 Minio, Inc.
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
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v4"
)

const (
	pass                     = "PASS" // Indicate that a test passed
	fail                     = "FAIL" // Indicate that a test failed
	livenessPath             = "/obstor/health/live"
	readinessPath            = "/obstor/health/ready"
	prometheusPathV2Cluster  = "/obstor/v2/metrics/cluster"
	prometheusPathV2Node     = "/obstor/v2/metrics/node"
	prometheusPathV2Bucket   = "/obstor/v2/metrics/bucket"
	prometheusPathV2Resource = "/obstor/v2/metrics/resource"
	timeout                  = time.Duration(30 * time.Second)
)

// log successful test runs
func successLogger(function string, args map[string]interface{}, startTime time.Time) {
	// calculate the test case duration
	duration := time.Since(startTime)
	// log with the fields as per tests
	slog.Info("test passed", "name", "healthcheck", "function", function, "args", args, "duration", duration.Nanoseconds()/1000000, "status", pass)
}

// log failed test runs and exit
func failureLog(function string, args map[string]interface{}, startTime time.Time, alert string, message string, err error) {
	// calculate the test case duration
	duration := time.Since(startTime)
	// log with the fields as per tests
	if err != nil {
		slog.Error("test failed", "name", "healthcheck", "function", function, "args", args,
			"duration", duration.Nanoseconds()/1000000, "status", fail, "alert", alert, "message", message, "error", err.Error())
	} else {
		slog.Error("test failed", "name", "healthcheck", "function", function, "args", args,
			"duration", duration.Nanoseconds()/1000000, "status", fail, "alert", alert, "message", message)
	}
	os.Exit(1)
}

func testLivenessEndpoint(endpoint string) {
	startTime := time.Now()
	function := "testLivenessEndpoint"

	u, err := url.Parse(fmt.Sprintf("%s%s", endpoint, livenessPath))
	if err != nil {
		// Could not parse URL successfully
		failureLog(function, nil, startTime, "", "URL Parsing for Healthcheck Liveness handler failed", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: u.Scheme == "https"},
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	resp, err := client.Get(u.String())
	if err != nil {
		// GET request errored
		failureLog(function, nil, startTime, "", "GET request failed", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Status not 200 OK
		failureLog(function, nil, startTime, "", fmt.Sprintf("GET /obstor/health/live returned %s", resp.Status), err)
	}

	defer resp.Body.Close()
	defer successLogger(function, nil, startTime)
}

func testReadinessEndpoint(endpoint string) {
	startTime := time.Now()
	function := "testReadinessEndpoint"

	u, err := url.Parse(fmt.Sprintf("%s%s", endpoint, readinessPath))
	if err != nil {
		// Could not parse URL successfully
		failureLog(function, nil, startTime, "", "URL Parsing for Healthcheck Readiness handler failed", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: u.Scheme == "https"},
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	resp, err := client.Get(u.String())
	if err != nil {
		// GET request errored
		failureLog(function, nil, startTime, "", "GET request to Readiness endpoint failed", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Status not 200 OK
		failureLog(function, nil, startTime, "", "GET /obstor/health/ready returned non OK status", err)
	}

	defer resp.Body.Close()
	defer successLogger(function, nil, startTime)
}

const (
	defaultPrometheusJWTExpiry = 100 * 365 * 24 * time.Hour
)

func testPrometheusEndpointV2(endpoint string, metricsPath string) {
	startTime := time.Now()
	function := "testPrometheusEndpoint"

	u, err := url.Parse(fmt.Sprintf("%s%s", endpoint, metricsPath))
	if err != nil {
		// Could not parse URL successfully
		failureLog(function, nil, startTime, "", "URL Parsing for Healthcheck Prometheus handler failed", err)
	}

	jwt := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, jwtgo.StandardClaims{
		ExpiresAt: time.Now().UTC().Add(defaultPrometheusJWTExpiry).Unix(),
		Subject:   os.Getenv("ACCESS_KEY"),
		Issuer:    "prometheus",
	})

	token, err := jwt.SignedString([]byte(os.Getenv("SECRET_KEY")))
	if err != nil {
		failureLog(function, nil, startTime, "", "jwt generation failed", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: u.Scheme == "https"},
	}
	client := &http.Client{Transport: tr, Timeout: timeout}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		failureLog(function, nil, startTime, "", "Initializing GET request to Prometheus endpoint failed", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := client.Do(req)
	if err != nil {
		// GET request errored
		failureLog(function, nil, startTime, "", "GET request to Prometheus endpoint failed", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Status not 200 OK
		failureLog(function, nil, startTime, "", "GET "+endpoint+" returned non OK status", err)
	}

	defer resp.Body.Close()
	defer successLogger(function, nil, startTime)
}

func testClusterPrometheusEndpointV2(endpoint string) {
	testPrometheusEndpointV2(endpoint, prometheusPathV2Cluster)
}

func testNodePrometheusEndpointV2(endpoint string) {
	testPrometheusEndpointV2(endpoint, prometheusPathV2Node)
}

func testBucketPrometheusEndpointV2(endpoint string) {
	testPrometheusEndpointV2(endpoint, prometheusPathV2Bucket)
}

func testResourcePrometheusEndpointV2(endpoint string) {
	testPrometheusEndpointV2(endpoint, prometheusPathV2Resource)
}

func main() {
	endpoint := os.Getenv("SERVER_ENDPOINT")
	secure := os.Getenv("ENABLE_HTTPS")
	if secure == "1" {
		endpoint = "https://" + endpoint
	} else {
		endpoint = "http://" + endpoint
	}

	// Configure slog to output JSON to stdout
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// execute tests
	testLivenessEndpoint(endpoint)
	testReadinessEndpoint(endpoint)
	testClusterPrometheusEndpointV2(endpoint)
	testNodePrometheusEndpointV2(endpoint)
	testBucketPrometheusEndpointV2(endpoint)
	testResourcePrometheusEndpointV2(endpoint)
}
