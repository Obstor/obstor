/*
*
*  Mint, (C) 2017-2025 Minio, Inc.
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
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	slog.Info("test passed", "name", "aws-sdk-go-v2", "function", function, "args", args, "duration", duration.Nanoseconds()/1000000, "status", PASS)
}

// log failed test runs and exit
func failureLog(function string, args map[string]interface{}, startTime time.Time, alert string, message string, err error) {
	// calculate the test case duration
	duration := time.Since(startTime)
	// log with the fields as per tests
	if err != nil {
		slog.Error("test failed", "name", "aws-sdk-go-v2", "function", function, "args", args,
			"duration", duration.Nanoseconds()/1000000, "status", FAIL, "alert", alert, "message", message, "error", err.Error())
	} else {
		slog.Error("test failed", "name", "aws-sdk-go-v2", "function", function, "args", args,
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

func isObjectTaggingImplemented(ctx context.Context, s3Client *s3.Client) bool {
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := randString(60, rand.NewSource(time.Now().UnixNano()), "")
	startTime := time.Now()
	function := "isObjectTaggingImplemented"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return false
	}

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Body:   strings.NewReader("testfile"),
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})

	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to success but got %v", err), err)
		return false
	}

	_, err = s3Client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			if apiErr.ErrorCode() == "NotImplemented" {
				return false
			}
		}
	}
	return true
}

func cleanup(ctx context.Context, s3Client *s3.Client, bucket string, object string, function string,
	args map[string]interface{}, startTime time.Time, deleteBucket bool,
) {
	// Deleting the object, just in case it was created. Will not check for errors.
	s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})

	if deleteBucket {
		_, err := s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteBucket Failed", err)
			return
		}
	}
}

func cleanupBucket(ctx context.Context, s3Client *s3.Client, bucket string, function string,
	args map[string]interface{}, startTime time.Time,
) {
	// List and delete all objects in bucket
	listResp, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	if err == nil && listResp.Contents != nil {
		for _, obj := range listResp.Contents {
			s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    obj.Key,
			})
		}
	}

	// Delete bucket
	_, err = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteBucket Failed", err)
		return
	}
}

func testPresignedPutInvalidHash(ctx context.Context, s3Client *s3.Client, presignClient *s3.PresignClient) {
	startTime := time.Now()
	function := "PresignedPut"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "presignedTest"
	expiry := 1 * time.Minute
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
		"expiry":     expiry,
	}

	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	presignReq, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(object),
		ContentType: aws.String("application/octet-stream"),
	}, s3.WithPresignExpires(expiry))

	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 presigned Put request creation failed", err)
		return
	}

	rreq, err := http.NewRequest(http.MethodPut, presignReq.URL, bytes.NewReader([]byte("")))
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 presigned PUT request failed", err)
		return
	}

	rreq.Header.Set("X-Amz-Content-Sha256", "invalid-sha256")
	rreq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(rreq)
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 presigned put request failed", err)
		return
	}
	defer resp.Body.Close()

	dec := xml.NewDecoder(resp.Body)
	errResp := errorResponse{}
	err = dec.Decode(&errResp)
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 unmarshalling xml failed", err)
		return
	}

	if errResp.Code != "SignatureDoesNotMatch" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 presigned PUT expected to fail with SignatureDoesNotMatch but got %v", errResp.Code), errors.New("aws S3 error code mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func testConditionalDeleteWithCorrectETag(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ConditionalDeleteWithCorrectETag"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testConditionalDelete"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Put object and capture ETag
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Body:   bytes.NewReader([]byte("test content")),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	if putResp.ETag == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject returned nil ETag", errors.New("nil ETag"))
		return
	}

	etag := *putResp.ETag

	// Delete with If-Match using correct ETag
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(object),
		IfMatch: aws.String(etag),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteObject with If-Match failed", err)
		return
	}

	// Verify object is deleted
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 object should have been deleted", errors.New("object still exists"))
		return
	}

	successLogger(function, args, startTime)
}

func testConditionalDeleteWithIncorrectETag(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ConditionalDeleteWithIncorrectETag"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testConditionalDelete"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Put object
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Body:   bytes.NewReader([]byte("test content")),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	// Delete with If-Match using incorrect ETag
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(object),
		IfMatch: aws.String("\"wrong-etag\""),
	})
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteObject with wrong ETag should have failed", errors.New("expected precondition failure"))
		return
	}

	// Verify error is PreconditionFailed
	if !strings.Contains(err.Error(), "PreconditionFailed") && !strings.Contains(err.Error(), "412") {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 expected PreconditionFailed error but got: %v", err), err)
		return
	}

	// Verify object still exists
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 object should still exist after failed conditional delete", err)
		return
	}

	successLogger(function, args, startTime)
}

func testConditionalDeleteWithWildcardExists(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ConditionalDeleteWithWildcardExists"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testConditionalDelete"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Put object
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Body:   bytes.NewReader([]byte("test content")),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	// Delete with If-Match: "*" (wildcard - object exists)
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(object),
		IfMatch: aws.String("*"),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteObject with wildcard If-Match failed", err)
		return
	}

	// Verify object is deleted
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 object should have been deleted", errors.New("object still exists"))
		return
	}

	successLogger(function, args, startTime)
}

func testConditionalDeleteWithWildcardMissing(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ConditionalDeleteWithWildcardMissing"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "nonexistent-object"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Delete non-existent object with If-Match: "*"
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(object),
		IfMatch: aws.String("*"),
	})
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteObject with wildcard on missing object should have failed", errors.New("expected precondition failure"))
		return
	}

	// Verify error is PreconditionFailed
	if !strings.Contains(err.Error(), "PreconditionFailed") && !strings.Contains(err.Error(), "412") {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 expected PreconditionFailed error but got: %v", err), err)
		return
	}

	successLogger(function, args, startTime)
}

func testChecksumCRC32(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ChecksumCRC32"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testChecksumCRC32"
	content := []byte("test content for CRC32 checksum validation")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
		"algorithm":  "CRC32",
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Calculate CRC32 checksum
	crc32Hash := crc32.ChecksumIEEE(content)
	expectedChecksum := base64.StdEncoding.EncodeToString([]byte{
		byte(crc32Hash >> 24),
		byte(crc32Hash >> 16),
		byte(crc32Hash >> 8),
		byte(crc32Hash),
	})

	// Put object with CRC32 checksum
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(object),
		Body:              bytes.NewReader(content),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject with CRC32 failed", err)
		return
	}

	// Verify checksum is returned in response
	if putResp.ChecksumCRC32 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject response missing ChecksumCRC32", errors.New("missing checksum"))
		return
	}

	if *putResp.ChecksumCRC32 != expectedChecksum {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ChecksumCRC32 mismatch: expected %s, got %s", expectedChecksum, *putResp.ChecksumCRC32), errors.New("checksum mismatch"))
		return
	}

	// Get object and verify checksum is returned
	getResp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(object),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject failed", err)
		return
	}
	defer getResp.Body.Close()

	if getResp.ChecksumCRC32 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject response missing ChecksumCRC32", errors.New("missing checksum"))
		return
	}

	if *getResp.ChecksumCRC32 != expectedChecksum {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 GetObject ChecksumCRC32 mismatch: expected %s, got %s", expectedChecksum, *getResp.ChecksumCRC32), errors.New("checksum mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func testChecksumCRC32C(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ChecksumCRC32C"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testChecksumCRC32C"
	content := []byte("test content for CRC32C checksum validation")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
		"algorithm":  "CRC32C",
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Calculate CRC32C checksum (Castagnoli polynomial)
	crc32cTable := crc32.MakeTable(crc32.Castagnoli)
	crc32cHash := crc32.Checksum(content, crc32cTable)
	expectedChecksum := base64.StdEncoding.EncodeToString([]byte{
		byte(crc32cHash >> 24),
		byte(crc32cHash >> 16),
		byte(crc32cHash >> 8),
		byte(crc32cHash),
	})

	// Put object with CRC32C checksum
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(object),
		Body:              bytes.NewReader(content),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32c,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject with CRC32C failed", err)
		return
	}

	// Verify checksum is returned
	if putResp.ChecksumCRC32C == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject response missing ChecksumCRC32C", errors.New("missing checksum"))
		return
	}

	if *putResp.ChecksumCRC32C != expectedChecksum {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ChecksumCRC32C mismatch: expected %s, got %s", expectedChecksum, *putResp.ChecksumCRC32C), errors.New("checksum mismatch"))
		return
	}

	// Get object and verify checksum
	getResp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(object),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject failed", err)
		return
	}
	defer getResp.Body.Close()

	if getResp.ChecksumCRC32C == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject response missing ChecksumCRC32C", errors.New("missing checksum"))
		return
	}

	successLogger(function, args, startTime)
}

func testChecksumSHA1(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ChecksumSHA1"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testChecksumSHA1"
	content := []byte("test content for SHA1 checksum validation")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
		"algorithm":  "SHA1",
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Calculate SHA1 checksum
	sha1Hash := sha1.Sum(content)
	expectedChecksum := base64.StdEncoding.EncodeToString(sha1Hash[:])

	// Put object with SHA1 checksum
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(object),
		Body:              bytes.NewReader(content),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha1,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject with SHA1 failed", err)
		return
	}

	// Verify checksum is returned
	if putResp.ChecksumSHA1 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject response missing ChecksumSHA1", errors.New("missing checksum"))
		return
	}

	if *putResp.ChecksumSHA1 != expectedChecksum {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ChecksumSHA1 mismatch: expected %s, got %s", expectedChecksum, *putResp.ChecksumSHA1), errors.New("checksum mismatch"))
		return
	}

	// Get object and verify checksum
	getResp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(object),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject failed", err)
		return
	}
	defer getResp.Body.Close()

	if getResp.ChecksumSHA1 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject response missing ChecksumSHA1", errors.New("missing checksum"))
		return
	}

	successLogger(function, args, startTime)
}

func testChecksumSHA256(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ChecksumSHA256"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testChecksumSHA256"
	content := []byte("test content for SHA256 checksum validation")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
		"algorithm":  "SHA256",
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Calculate SHA256 checksum
	sha256Hash := sha256.Sum256(content)
	expectedChecksum := base64.StdEncoding.EncodeToString(sha256Hash[:])

	// Put object with SHA256 checksum
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(object),
		Body:              bytes.NewReader(content),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject with SHA256 failed", err)
		return
	}

	// Verify checksum is returned
	if putResp.ChecksumSHA256 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject response missing ChecksumSHA256", errors.New("missing checksum"))
		return
	}

	if *putResp.ChecksumSHA256 != expectedChecksum {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ChecksumSHA256 mismatch: expected %s, got %s", expectedChecksum, *putResp.ChecksumSHA256), errors.New("checksum mismatch"))
		return
	}

	// Get object and verify checksum
	getResp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(object),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject failed", err)
		return
	}
	defer getResp.Body.Close()

	if getResp.ChecksumSHA256 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject response missing ChecksumSHA256", errors.New("missing checksum"))
		return
	}

	successLogger(function, args, startTime)
}

func testChecksumInvalidValue(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "ChecksumInvalidValue"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testChecksumInvalid"
	content := []byte("test content")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Try to put object with wrong CRC32 checksum value
	wrongChecksum := base64.StdEncoding.EncodeToString([]byte{0x00, 0x00, 0x00, 0x00})
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(object),
		Body:          bytes.NewReader(content),
		ChecksumCRC32: aws.String(wrongChecksum),
	})

	// Should fail with BadRequest or similar
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject with wrong checksum should have failed", errors.New("expected checksum validation failure"))
		return
	}

	// Verify error indicates checksum mismatch
	if !strings.Contains(err.Error(), "BadDigest") && !strings.Contains(err.Error(), "InvalidRequest") && !strings.Contains(err.Error(), "400") {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 expected checksum error but got: %v", err), err)
		return
	}

	successLogger(function, args, startTime)
}

func testGetObjectAttributesBasic(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "GetObjectAttributesBasic"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testGetObjectAttributes"
	content := []byte("test content for GetObjectAttributes")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Put object
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Body:   bytes.NewReader(content),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	expectedETag := putResp.ETag

	// GetObjectAttributes requesting ETag, StorageClass, ObjectSize
	attrResp, err := s3Client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		ObjectAttributes: []types.ObjectAttributes{
			types.ObjectAttributesEtag,
			types.ObjectAttributesStorageClass,
			types.ObjectAttributesObjectSize,
		},
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes Failed", err)
		return
	}

	// Verify ETag
	if attrResp.ETag == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ETag", errors.New("missing etag"))
		return
	}
	normalizedExpected := strings.Trim(*expectedETag, "\"")
	normalizedActual := strings.Trim(*attrResp.ETag, "\"")
	if normalizedActual != normalizedExpected {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ETag mismatch: expected %s, got %s", normalizedExpected, normalizedActual), errors.New("etag mismatch"))
		return
	}

	// Verify ObjectSize
	if attrResp.ObjectSize == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ObjectSize", errors.New("missing size"))
		return
	}
	if *attrResp.ObjectSize != int64(len(content)) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ObjectSize mismatch: expected %d, got %d", len(content), *attrResp.ObjectSize), errors.New("size mismatch"))
		return
	}

	// Verify StorageClass (should be STANDARD)
	if attrResp.StorageClass == "" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing StorageClass", errors.New("missing storage class"))
		return
	}

	successLogger(function, args, startTime)
}

func testGetObjectAttributesWithChecksum(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "GetObjectAttributesWithChecksum"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testGetObjectAttributesChecksum"
	content := []byte("test content for checksum attributes")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
		"algorithm":  "CRC32C",
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Put object with CRC32C checksum
	putResp, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(object),
		Body:              bytes.NewReader(content),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32c,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	expectedChecksum := putResp.ChecksumCRC32C

	// GetObjectAttributes requesting Checksum
	attrResp, err := s3Client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		ObjectAttributes: []types.ObjectAttributes{
			types.ObjectAttributesChecksum,
			types.ObjectAttributesObjectSize,
		},
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes Failed", err)
		return
	}

	// Verify Checksum is returned
	if attrResp.Checksum == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing Checksum", errors.New("missing checksum"))
		return
	}

	// Verify ChecksumCRC32C
	if attrResp.Checksum.ChecksumCRC32C == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ChecksumCRC32C", errors.New("missing crc32c"))
		return
	}

	if *attrResp.Checksum.ChecksumCRC32C != *expectedChecksum {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ChecksumCRC32C mismatch: expected %s, got %s", *expectedChecksum, *attrResp.Checksum.ChecksumCRC32C), errors.New("checksum mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func testGetObjectAttributesMultipart(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "GetObjectAttributesMultipart"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testGetObjectAttributesMultipart"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Create multipart upload
	createResp, err := s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateMultipartUpload Failed", err)
		return
	}

	uploadID := createResp.UploadId

	// Upload 3 parts (each part must be >= 5MB except the last one)
	// Create 5MB + 1 byte parts to satisfy S3 minimum part size requirement
	minPartSize := 5*1024*1024 + 1 // 5MB + 1 byte
	var completedParts []types.CompletedPart
	for i := 1; i <= 3; i++ {
		// Create part content of minimum required size
		partContent := make([]byte, minPartSize)
		// Add some identifiable content at the beginning
		copy(partContent, []byte(fmt.Sprintf("Part %d content - ", i)))

		uploadResp, err := s3Client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(object),
			PartNumber: aws.Int32(int32(i)),
			UploadId:   uploadID,
			Body:       bytes.NewReader(partContent),
		})
		if err != nil {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 UploadPart %d Failed", i), err)
			return
		}
		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadResp.ETag,
			PartNumber: aws.Int32(int32(i)),
		})
	}

	// Complete multipart upload
	_, err = s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(object),
		UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CompleteMultipartUpload Failed", err)
		return
	}

	// GetObjectAttributes requesting ObjectParts
	attrResp, err := s3Client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		ObjectAttributes: []types.ObjectAttributes{
			types.ObjectAttributesObjectParts,
		},
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes Failed", err)
		return
	}

	// Verify ObjectParts is returned
	if attrResp.ObjectParts == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ObjectParts", errors.New("missing object parts"))
		return
	}

	if attrResp.ObjectParts.Parts == nil || len(attrResp.ObjectParts.Parts) != 3 {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Parts array mismatch: expected 3 parts, got %d", len(attrResp.ObjectParts.Parts)), errors.New("parts array mismatch"))
		return
	}

	// Verify each part has PartNumber and Size
	for i, part := range attrResp.ObjectParts.Parts {
		if part.PartNumber == nil {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Part %d missing PartNumber", i), errors.New("missing part number"))
			return
		}
		if part.Size == nil {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Part %d missing Size", i), errors.New("missing part size"))
			return
		}
	}

	successLogger(function, args, startTime)
}

func testGetObjectAttributesNonExistent(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "GetObjectAttributesNonExistent"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "nonexistent-object"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Try to get attributes for non-existent object
	_, err = s3Client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		ObjectAttributes: []types.ObjectAttributes{
			types.ObjectAttributesEtag,
		},
	})

	// Should fail with 404 NotFound
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes for non-existent object should have failed", errors.New("expected not found error"))
		return
	}

	// Verify error is NotFound
	if !strings.Contains(err.Error(), "NotFound") && !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "NoSuchKey") {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 expected NotFound error but got: %v", err), err)
		return
	}

	successLogger(function, args, startTime)
}

func testGetObjectAttributesCombined(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "GetObjectAttributesCombined"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "testGetObjectAttributesCombined"
	content := []byte("test content for combined attributes")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanupBucket(ctx, s3Client, bucket, function, args, startTime)

	// Put object with SHA256 checksum
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(object),
		Body:              bytes.NewReader(content),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	// GetObjectAttributes requesting ALL attributes
	attrResp, err := s3Client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		ObjectAttributes: []types.ObjectAttributes{
			types.ObjectAttributesEtag,
			types.ObjectAttributesChecksum,
			types.ObjectAttributesObjectSize,
			types.ObjectAttributesStorageClass,
		},
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes Failed", err)
		return
	}

	// Verify all attributes are present
	if attrResp.ETag == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ETag", errors.New("missing etag"))
		return
	}

	if attrResp.ObjectSize == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ObjectSize", errors.New("missing size"))
		return
	}

	if attrResp.StorageClass == "" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing StorageClass", errors.New("missing storage class"))
		return
	}

	if attrResp.Checksum == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing Checksum", errors.New("missing checksum"))
		return
	}

	if attrResp.Checksum.ChecksumSHA256 == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObjectAttributes missing ChecksumSHA256", errors.New("missing sha256"))
		return
	}

	successLogger(function, args, startTime)
}

func testListObjects(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testListObjects"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object1 := "testObject1"
	object2 := "testObject2"
	expiry := 1 * time.Minute
	args := map[string]interface{}{
		"bucketName":  bucket,
		"objectName1": object1,
		"objectName2": object2,
		"expiry":      expiry,
	}

	getKeys := func(objects []types.Object) []string {
		var rv []string
		for _, obj := range objects {
			rv = append(rv, *obj.Key)
		}
		return rv
	}
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object1, function, args, startTime, true)
	defer cleanup(ctx, s3Client, bucket, object2, function, args, startTime, false)

	listInput := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(1000),
		Prefix:  aws.String(""),
	}
	result, err := s3Client.ListObjectsV2(ctx, listInput)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 listobjects expected to success but got %v", err), err)
		return
	}
	if result.KeyCount != nil && *result.KeyCount != 0 {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 listobjects with prefix '' expected 0 key but got %v, %v", *result.KeyCount, getKeys(result.Contents)), errors.New("aws S3 key count mismatch"))
		return
	}
	putInput1 := &s3.PutObjectInput{
		Body:   strings.NewReader("filetoupload"),
		Bucket: aws.String(bucket),
		Key:    aws.String(object1),
	}
	_, err = s3Client.PutObject(ctx, putInput1)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to success but got %v", err), err)
		return
	}
	putInput2 := &s3.PutObjectInput{
		Body:   strings.NewReader("filetoupload"),
		Bucket: aws.String(bucket),
		Key:    aws.String(object2),
	}
	_, err = s3Client.PutObject(ctx, putInput2)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to success but got %v", err), err)
		return
	}
	result, err = s3Client.ListObjectsV2(ctx, listInput)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 listobjects expected to success but got %v", err), err)
		return
	}
	if result.KeyCount != nil && *result.KeyCount != 2 {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 listobjects with prefix '' expected 2 key but got %v, %v", *result.KeyCount, getKeys(result.Contents)), errors.New("aws S3 key count mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func testSelectObject(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testSelectObject"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object1 := "object1.csv"
	object2 := "object2.csv"
	args := map[string]interface{}{
		"bucketName":  bucket,
		"objectName1": object1,
		"objectName2": object2,
	}

	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}

	// Test comma field separator
	inputCsv1 := `year,gender,ethnicity,firstname,count,rank
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,SOPHIA,119,1
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,CHLOE,106,2
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,EMILY,93,3
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,OLIVIA,89,4
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,EMMA,75,5
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,ISABELLA,67,6
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,TIFFANY,54,7
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,ASHLEY,52,8
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,FIONA,48,9
2011,FEMALE,ASIAN AND PACIFIC ISLANDER,ANGELA,47,10
`

	outputCSV1 := `2011
2011
2011
2011
2011
2011
2011
2011
2011
2011
`

	putInput1 := &s3.PutObjectInput{
		Body:   strings.NewReader(inputCsv1),
		Bucket: aws.String(bucket),
		Key:    aws.String(object1),
	}
	_, err = s3Client.PutObject(ctx, putInput1)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object failed %v", err), err)
		return
	}

	defer cleanup(ctx, s3Client, bucket, object1, function, args, startTime, true)

	params := &s3.SelectObjectContentInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(object1),
		ExpressionType:  types.ExpressionTypeSql,
		Expression:      aws.String("SELECT s._1 FROM S3Object s"),
		RequestProgress: &types.RequestProgress{},
		InputSerialization: &types.InputSerialization{
			CompressionType: types.CompressionTypeNone,
			CSV: &types.CSVInput{
				FileHeaderInfo:  types.FileHeaderInfoIgnore,
				FieldDelimiter:  aws.String(","),
				RecordDelimiter: aws.String("\n"),
			},
		},
		OutputSerialization: &types.OutputSerialization{
			CSV: &types.CSVOutput{},
		},
	}

	resp, err := s3Client.SelectObjectContent(ctx, params)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object failed %v", err), err)
		return
	}
	defer resp.GetStream().Close()

	payload := ""
	for event := range resp.GetStream().Events() {
		switch v := event.(type) {
		case *types.SelectObjectContentEventStreamMemberRecords:
			// Records event contains the payload
			payload = string(v.Value.Payload)
		}
	}

	if err := resp.GetStream().Err(); err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object failed %v", err), err)
		return
	}

	if payload != outputCSV1 {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object output mismatch %v", payload), errors.New("aws S3 select object mismatch"))
		return
	}

	// Test unicode field separator
	inputCsv2 := `"year"╦"gender"╦"ethnicity"╦"firstname"╦"count"╦"rank"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"SOPHIA"╦"119"╦"1"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"CHLOE"╦"106"╦"2"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"EMILY"╦"93"╦"3"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"OLIVIA"╦"89"╦"4"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"EMMA"╦"75"╦"5"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"ISABELLA"╦"67"╦"6"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"TIFFANY"╦"54"╦"7"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"ASHLEY"╦"52"╦"8"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"FIONA"╦"48"╦"9"
"2011"╦"FEMALE"╦"ASIAN AND PACIFIC ISLANDER"╦"ANGELA"╦"47"╦"10"
`

	outputCSV2 := `2011
2011
2011
2011
2011
2011
2011
2011
2011
2011
`

	putInput2 := &s3.PutObjectInput{
		Body:   strings.NewReader(inputCsv2),
		Bucket: aws.String(bucket),
		Key:    aws.String(object2),
	}
	_, err = s3Client.PutObject(ctx, putInput2)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object upload failed: %v", err), err)
		return
	}

	defer cleanup(ctx, s3Client, bucket, object2, function, args, startTime, false)

	params2 := &s3.SelectObjectContentInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(object2),
		ExpressionType:  types.ExpressionTypeSql,
		Expression:      aws.String("SELECT s._1 FROM S3Object s"),
		RequestProgress: &types.RequestProgress{},
		InputSerialization: &types.InputSerialization{
			CompressionType: types.CompressionTypeNone,
			CSV: &types.CSVInput{
				FileHeaderInfo:  types.FileHeaderInfoIgnore,
				FieldDelimiter:  aws.String("╦"),
				RecordDelimiter: aws.String("\n"),
			},
		},
		OutputSerialization: &types.OutputSerialization{
			CSV: &types.CSVOutput{},
		},
	}

	resp, err = s3Client.SelectObjectContent(ctx, params2)
	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object failed for unicode separator %v", err), err)
		return
	}
	defer resp.GetStream().Close()

	for event := range resp.GetStream().Events() {
		switch v := event.(type) {
		case *types.SelectObjectContentEventStreamMemberRecords:
			// Records event contains the payload
			payload = string(v.Value.Payload)
		}
	}

	if err := resp.GetStream().Err(); err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object failed for unicode separator %v", err), err)
		return
	}

	if payload != outputCSV2 {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Select object output mismatch %v", payload), errors.New("aws S3 select object mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func testObjectTagging(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testObjectTagging"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := randString(60, rand.NewSource(time.Now().UnixNano()), "")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	taginput := "Tag1=Value1"
	tagInputSet := []types.Tag{
		{
			Key:   aws.String("Tag1"),
			Value: aws.String("Value1"),
		},
	}
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Body:    strings.NewReader("testfile"),
		Bucket:  aws.String(bucket),
		Key:     aws.String(object),
		Tagging: aws.String(taginput),
	})

	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to success but got %v", err), err)
		return
	}

	tagop, err := s3Client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUTObjectTagging expected to success but got %v", apiErr.ErrorCode()), err)
			return
		}
	}
	if !reflect.DeepEqual(tagop.TagSet, tagInputSet) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUTObject Tag input did not match with GetObjectTagging output %v", nil), nil)
		return
	}

	taginputSet1 := []types.Tag{
		{
			Key:   aws.String("Key4"),
			Value: aws.String("Value4"),
		},
	}
	_, err = s3Client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Tagging: &types.Tagging{
			TagSet: taginputSet1,
		},
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUTObjectTagging expected to success but got %v", apiErr.ErrorCode()), err)
			return
		}
	}

	tagop, err = s3Client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUTObjectTagging expected to success but got %v", apiErr.ErrorCode()), err)
			return
		}
	}
	if !reflect.DeepEqual(tagop.TagSet, taginputSet1) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUTObjectTagging input did not match with GetObjectTagging output %v", nil), nil)
		return
	}
	successLogger(function, args, startTime)
}

func testObjectTaggingErrors(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testObjectTaggingErrors"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := randString(60, rand.NewSource(time.Now().UnixNano()), "")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Body:   strings.NewReader("testfile"),
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})

	if err != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to success but got %v", err), err)
		return
	}

	// case 1 : Too many tags > 10
	input := &s3.PutObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{Key: aws.String("Key1"), Value: aws.String("Value3")},
				{Key: aws.String("Key2"), Value: aws.String("Value4")},
				{Key: aws.String("Key3"), Value: aws.String("Value3")},
				{Key: aws.String("Key4"), Value: aws.String("Value3")},
				{Key: aws.String("Key5"), Value: aws.String("Value3")},
				{Key: aws.String("Key6"), Value: aws.String("Value3")},
				{Key: aws.String("Key7"), Value: aws.String("Value3")},
				{Key: aws.String("Key8"), Value: aws.String("Value3")},
				{Key: aws.String("Key9"), Value: aws.String("Value3")},
				{Key: aws.String("Key10"), Value: aws.String("Value3")},
				{Key: aws.String("Key11"), Value: aws.String("Value3")},
			},
		},
	}

	_, err = s3Client.PutObjectTagging(ctx, input)
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PUT expected to fail but succeeded", err)
		return
	}

	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() != "BadRequest" {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to fail but got %v", err), err)
			return
		}
	}

	// case 2 : Duplicate Tag Keys
	input = &s3.PutObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{Key: aws.String("Key1"), Value: aws.String("Value3")},
				{Key: aws.String("Key1"), Value: aws.String("Value4")},
			},
		},
	}

	_, err = s3Client.PutObjectTagging(ctx, input)
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PUT expected to fail but succeeded", err)
		return
	}

	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() != "InvalidTag" {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to fail but got %v", err), err)
			return
		}
	}

	// case 3 : Too long Tag Key
	input = &s3.PutObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{
					Key:   aws.String("Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1"),
					Value: aws.String("Value3"),
				},
				{Key: aws.String("Key1"), Value: aws.String("Value4")},
			},
		},
	}

	_, err = s3Client.PutObjectTagging(ctx, input)
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PUT expected to fail but succeeded", err)
		return
	}

	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() != "InvalidTag" {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to fail but got %v", err), err)
			return
		}
	}

	// case 4 : Too long Tag value
	input = &s3.PutObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{
					Key:   aws.String("Key1"),
					Value: aws.String("Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1Key1"),
				},
				{Key: aws.String("Key1"), Value: aws.String("Value4")},
			},
		},
	}

	_, err = s3Client.PutObjectTagging(ctx, input)
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PUT expected to fail but succeeded", err)
		return
	}

	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() != "InvalidTag" {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to fail but got %v", err), err)
			return
		}
	}

	successLogger(function, args, startTime)
}

// Tests bucket re-create errors.
func testCreateBucketError(ctx context.Context, s3Client *s3.Client, origRegion string) {
	// initialize logging params
	startTime := time.Now()
	function := "testMakeBucketError"
	bucketName := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	args := map[string]interface{}{
		"bucketName": bucketName,
	}

	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint("us-west-1"),
		},
	})
	if err != nil {
		// InvalidRegion is a valid error if the endpoint doesn't support
		// different 'regions', we simply skip this test in such scenarios.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidRegion" {
			successLogger(function, args, startTime)
			return
		}
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucketName, "", function, args, startTime, true)

	_, errCreating := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if errCreating == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Should Return Error for Existing bucket", err)
		return
	}
	// Verify valid error response from server.
	var apiErr interface{ ErrorCode() string }
	if errors.As(errCreating, &apiErr) {
		if apiErr.ErrorCode() != "BucketAlreadyExists" && apiErr.ErrorCode() != "BucketAlreadyOwnedByYou" {
			failureLog(function, args, startTime, "", "Invalid error returned by server", err)
			return
		}
	}

	successLogger(function, args, startTime)
}

func testListMultipartUploads(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testListMultipartUploads"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := randString(60, rand.NewSource(time.Now().UnixNano()), "")
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}
	_, errCreating := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if errCreating != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", errCreating)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	multipartUpload, err := s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 createMultipartupload API failed", err)
		return
	}
	parts := make([]*string, 5)
	for i := 0; i < 5; i++ {
		result, errUpload := s3Client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(object),
			UploadId:   multipartUpload.UploadId,
			PartNumber: aws.Int32(int32(i + 1)),
			Body:       strings.NewReader("fileToUpload"),
		})
		if errUpload != nil {
			_, _ = s3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(bucket),
				Key:      aws.String(object),
				UploadId: multipartUpload.UploadId,
			})
			failureLog(function, args, startTime, "", "AWS SDK Go V2 uploadPart API failed for", errUpload)
			return
		}
		parts[i] = result.ETag
	}

	listParts, errParts := s3Client.ListParts(ctx, &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(object),
		UploadId: multipartUpload.UploadId,
	})
	if errParts != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 ListPartsInput API failed for", err)
		return
	}

	if len(parts) != len(listParts.Parts) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ListParts.Parts len mismatch want: %d got: %d", len(parts), len(listParts.Parts)), err)
		return
	}

	completedParts := make([]types.CompletedPart, len(parts))
	for i, part := range listParts.Parts {
		tag := parts[i]
		if *tag != *part.ETag {
			failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 ListParts.Parts output mismatch want: %#v got: %#v", tag, part.ETag), err)
			return
		}
		completedParts[i] = types.CompletedPart{
			ETag:       part.ETag,
			PartNumber: part.PartNumber,
		}
	}

	_, err = s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
		UploadId: multipartUpload.UploadId,
	})
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CompleteMultipartUpload is expected to fail but succeeded", errors.New("expected nil"))
		return
	}

	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) && apiErr.ErrorCode() != "EntityTooSmall" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CompleteMultipartUpload is expected to fail with EntityTooSmall", err)
		return
	}

	// Error cases

	// MaxParts < 0
	lpInput := &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(object),
		UploadId: multipartUpload.UploadId,
		MaxParts: aws.Int32(-1),
	}
	_, err = s3Client.ListParts(ctx, lpInput)
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 ListPartsInput API (MaxParts < 0) failed for", err)
		return
	}

	// PartNumberMarker < 0
	lpInput.PartNumberMarker = aws.String("-1")
	_, err = s3Client.ListParts(ctx, lpInput)
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 ListPartsInput API (PartNumberMarker < 0) failed for", err)
		return
	}

	successLogger(function, args, startTime)
}

func testSSECopyObject(ctx context.Context, s3Client *s3.Client) {
	// initialize logging params
	startTime := time.Now()
	function := "testSSECopyObjectSourceEncrypted"
	bucketName := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := randString(60, rand.NewSource(time.Now().UnixNano()), "")
	args := map[string]interface{}{
		"bucketName": bucketName,
		"objectName": object,
	}
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucketName, object+"-enc", function, args, startTime, true)
	defer cleanup(ctx, s3Client, bucketName, object+"-noenc", function, args, startTime, false)
	wrongSuccess := errors.New("succeeded instead of failing. ")

	// create encrypted object
	sseCustomerKey := "32byteslongsecretkeymustbegiven2"
	_, errPutEnc := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Body:                 strings.NewReader("fileToUpload"),
		Bucket:               aws.String(bucketName),
		Key:                  aws.String(object + "-enc"),
		SSECustomerAlgorithm: aws.String("AES256"),
		SSECustomerKey:       aws.String(sseCustomerKey),
	})
	if errPutEnc != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to succeed but got %v", errPutEnc), errPutEnc)
		return
	}

	// copy the encrypted object
	_, errCopyEnc := s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		SSECustomerAlgorithm: aws.String("AES256"),
		SSECustomerKey:       aws.String(sseCustomerKey),
		CopySource:           aws.String(bucketName + "/" + object + "-enc"),
		Bucket:               aws.String(bucketName),
		Key:                  aws.String(object + "-copy"),
	})
	if errCopyEnc == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CopyObject expected to fail, but it succeeds ", wrongSuccess)
		return
	}
	invalidSSECustomerAlgorithm := "Requests specifying Server Side Encryption with Customer provided keys must provide a valid encryption algorithm"
	if !strings.Contains(errCopyEnc.Error(), invalidSSECustomerAlgorithm) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 CopyObject expected error %v got %v", invalidSSECustomerAlgorithm, errCopyEnc), errCopyEnc)
		return
	}

	// create non-encrypted object
	_, errPut := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Body:   strings.NewReader("fileToUpload"),
		Bucket: aws.String(bucketName),
		Key:    aws.String(object + "-noenc"),
	})
	if errPut != nil {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 PUT expected to succeed but got %v", errPut), errPut)
		return
	}

	// copy the non-encrypted object
	_, errCopy := s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		CopySourceSSECustomerAlgorithm: aws.String("AES256"),
		CopySourceSSECustomerKey:       aws.String(sseCustomerKey),
		SSECustomerAlgorithm:           aws.String("AES256"),
		SSECustomerKey:                 aws.String(sseCustomerKey),
		CopySource:                     aws.String(bucketName + "/" + object + "-noenc"),
		Bucket:                         aws.String(bucketName),
		Key:                            aws.String(object + "-copy"),
	})
	if errCopy == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CopyObject expected to fail, but it succeeds ", wrongSuccess)
		return
	}
	invalidEncryptionParameters := "The encryption parameters are not applicable to this object."
	if !strings.Contains(errCopy.Error(), invalidEncryptionParameters) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 CopyObject expected error %v got %v", invalidEncryptionParameters, errCopy), errCopy)
		return
	}

	successLogger(function, args, startTime)
}

func testBasicObjectOperations(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testBasicObjectOperations"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "test-object.txt"
	content := "Hello, Obstor with AWS SDK Go v2!"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	// PUT Object
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(object),
		Body:        strings.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	// GET Object
	getResult, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject Failed", err)
		return
	}
	defer getResult.Body.Close()

	// Verify content
	body := make([]byte, len(content))
	_, err = getResult.Body.Read(body)
	if err != nil && err.Error() != "EOF" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject read body failed", err)
		return
	}
	if string(body) != content {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 GetObject content mismatch: expected '%s', got '%s'", content, string(body)), errors.New("content mismatch"))
		return
	}

	// HEAD Object
	headResult, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 HeadObject Failed", err)
		return
	}
	if *headResult.ContentType != "text/plain" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 HeadObject ContentType mismatch: expected 'text/plain', got '%s'", *headResult.ContentType), errors.New("content type mismatch"))
		return
	}
	if *headResult.ContentLength != int64(len(content)) {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 HeadObject ContentLength mismatch: expected %d, got %d", len(content), *headResult.ContentLength), errors.New("content length mismatch"))
		return
	}

	// DELETE Object
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 DeleteObject Failed", err)
		return
	}

	// Verify object is deleted
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err == nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 Object should not exist after DELETE", errors.New("object still exists"))
		return
	}

	successLogger(function, args, startTime)
}

func testGetObjectRange(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testGetObjectRange"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "range-test-object.txt"
	content := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	// PUT Object
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Body:   strings.NewReader(content),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject Failed", err)
		return
	}

	// Test 1: Get first 10 bytes (bytes=0-9)
	getResult1, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Range:  aws.String("bytes=0-9"),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject with Range Failed", err)
		return
	}
	defer getResult1.Body.Close()

	body1 := make([]byte, 10)
	_, err = getResult1.Body.Read(body1)
	if err != nil && err.Error() != "EOF" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject range read failed", err)
		return
	}
	if string(body1) != "0123456789" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 GetObject range content mismatch: expected '0123456789', got '%s'", string(body1)), errors.New("range content mismatch"))
		return
	}

	// Test 2: Get middle 10 bytes (bytes=10-19)
	getResult2, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Range:  aws.String("bytes=10-19"),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject with Range Failed", err)
		return
	}
	defer getResult2.Body.Close()

	body2 := make([]byte, 10)
	_, err = getResult2.Body.Read(body2)
	if err != nil && err.Error() != "EOF" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject range read failed", err)
		return
	}
	if string(body2) != "ABCDEFGHIJ" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 GetObject range content mismatch: expected 'ABCDEFGHIJ', got '%s'", string(body2)), errors.New("range content mismatch"))
		return
	}

	// Test 3: Get last 10 bytes (bytes=-10)
	getResult3, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
		Range:  aws.String("bytes=-10"),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject with suffix Range Failed", err)
		return
	}
	defer getResult3.Body.Close()

	body3 := make([]byte, 10)
	_, err = getResult3.Body.Read(body3)
	if err != nil && err.Error() != "EOF" {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject suffix range read failed", err)
		return
	}
	if string(body3) != "qrstuvwxyz" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 GetObject suffix range content mismatch: expected 'qrstuvwxyz', got '%s'", string(body3)), errors.New("suffix range content mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func testObjectMetadata(ctx context.Context, s3Client *s3.Client) {
	startTime := time.Now()
	function := "testObjectMetadata"
	bucket := randString(60, rand.NewSource(time.Now().UnixNano()), "aws-sdk-go-test-")
	object := "metadata-test-object.txt"
	content := "Object with custom metadata"
	args := map[string]interface{}{
		"bucketName": bucket,
		"objectName": object,
	}

	// Create bucket
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 CreateBucket Failed", err)
		return
	}
	defer cleanup(ctx, s3Client, bucket, object, function, args, startTime, true)

	// PUT Object with custom metadata
	metadata := map[string]string{
		"author":      "Obstor Test Suite",
		"environment": "testing",
		"version":     "1.0",
	}
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(object),
		Body:        strings.NewReader(content),
		ContentType: aws.String("text/plain"),
		Metadata:    metadata,
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 PutObject with metadata Failed", err)
		return
	}

	// HEAD Object to retrieve metadata
	headResult, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 HeadObject Failed", err)
		return
	}

	// Verify metadata
	if headResult.Metadata["author"] != "Obstor Test Suite" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Metadata 'author' mismatch: expected 'Obstor Test Suite', got '%s'", headResult.Metadata["author"]), errors.New("metadata mismatch"))
		return
	}
	if headResult.Metadata["environment"] != "testing" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Metadata 'environment' mismatch: expected 'testing', got '%s'", headResult.Metadata["environment"]), errors.New("metadata mismatch"))
		return
	}
	if headResult.Metadata["version"] != "1.0" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 Metadata 'version' mismatch: expected '1.0', got '%s'", headResult.Metadata["version"]), errors.New("metadata mismatch"))
		return
	}

	// GET Object and verify metadata is also returned
	getResult, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(object),
	})
	if err != nil {
		failureLog(function, args, startTime, "", "AWS SDK Go V2 GetObject Failed", err)
		return
	}
	defer getResult.Body.Close()

	// Verify metadata from GET
	if getResult.Metadata["author"] != "Obstor Test Suite" {
		failureLog(function, args, startTime, "", fmt.Sprintf("AWS SDK Go V2 GET Metadata 'author' mismatch: expected 'Obstor Test Suite', got '%s'", getResult.Metadata["author"]), errors.New("metadata mismatch"))
		return
	}

	successLogger(function, args, startTime)
}

func main() {
	ctx := context.Background()
	endpoint := os.Getenv("SERVER_ENDPOINT")
	accessKey := os.Getenv("ACCESS_KEY")
	secretKey := os.Getenv("SECRET_KEY")
	secure := os.Getenv("ENABLE_HTTPS")
	if strings.HasSuffix(endpoint, ":443") {
		endpoint = strings.ReplaceAll(endpoint, ":443", "")
	}
	if strings.HasSuffix(endpoint, ":80") {
		endpoint = strings.ReplaceAll(endpoint, ":80", "")
	}
	sdkEndpoint := "http://" + endpoint
	if secure == "1" {
		sdkEndpoint = "https://" + endpoint
	}

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               sdkEndpoint,
			HostnameImmutable: true,
			Source:            aws.EndpointSourceCustom,
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		config.WithRegion("us-east-1"),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		slog.Error(fmt.Sprintf("unable to load SDK config, %v", err))
		os.Exit(1)
	}

	// Create an S3 service client
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	// Create presign client
	presignClient := s3.NewPresignClient(s3Client)

	// Configure slog to output JSON to stdout
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// execute tests
	testBasicObjectOperations(ctx, s3Client)
	testGetObjectRange(ctx, s3Client)
	testObjectMetadata(ctx, s3Client)
	testPresignedPutInvalidHash(ctx, s3Client, presignClient)
	testConditionalDeleteWithCorrectETag(ctx, s3Client)
	testConditionalDeleteWithIncorrectETag(ctx, s3Client)
	testConditionalDeleteWithWildcardExists(ctx, s3Client)
	testConditionalDeleteWithWildcardMissing(ctx, s3Client)
	testChecksumCRC32(ctx, s3Client)
	testChecksumCRC32C(ctx, s3Client)
	testChecksumSHA1(ctx, s3Client)
	testChecksumSHA256(ctx, s3Client)
	testChecksumInvalidValue(ctx, s3Client)
	testGetObjectAttributesBasic(ctx, s3Client)
	testGetObjectAttributesWithChecksum(ctx, s3Client)
	testGetObjectAttributesMultipart(ctx, s3Client)
	testGetObjectAttributesNonExistent(ctx, s3Client)
	testGetObjectAttributesCombined(ctx, s3Client)
	testListObjects(ctx, s3Client)
	testSelectObject(ctx, s3Client)
	testCreateBucketError(ctx, s3Client, "us-east-1")
	testListMultipartUploads(ctx, s3Client)
	if secure == "1" {
		testSSECopyObject(ctx, s3Client)
	}
	if isObjectTaggingImplemented(ctx, s3Client) {
		testObjectTagging(ctx, s3Client)
		testObjectTaggingErrors(ctx, s3Client)
	}
}
