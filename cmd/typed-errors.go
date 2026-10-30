/*
 * MinIO Cloud Storage, (C) 2015, 2016 MinIO, Inc.
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
	"errors"
)

// errInvalidArgument means that input argument is invalid.
var errInvalidArgument = errors.New("invalid arguments specified")

// errMethodNotAllowed means that method is not allowed.
var errMethodNotAllowed = errors.New("method not allowed")

// errSignatureMismatch means signature did not match.
var errSignatureMismatch = errors.New("signature does not match")

// Used when we deal with data larger than expected
var errSizeUnexpected = errors.New("data size larger than expected")

// Used when we deal with data with unknown size
var errSizeUnspecified = errors.New("data size is unspecified")

// When upload object size is greater than 5G in a single PUT/POST operation.
var errDataTooLarge = errors.New("object size larger than allowed limit")

// When upload object size is less than what was expected.
var errDataTooSmall = errors.New("object size smaller than expected")

// errServerNotInitialized - server not initialized.
var errServerNotInitialized = errors.New("server not initialized, please try again")

// errFleetStoreDisabled - server runs without --web-address, so no metrics
var errFleetStoreDisabled = errors.New("traffic metrics need the fleet store, start the server with --web-address")

// errRPCAPIVersionUnsupported - unsupported rpc API version.
var errRPCAPIVersionUnsupported = errors.New("unsupported rpc API version")

// errServerTimeMismatch - server times are too far apart.
var errServerTimeMismatch = errors.New("server times are too far apart")

// errInvalidBucketName - bucket name is reserved for Obstor, usually
// returned for 'obstor', '.obstor.sys', buckets with capital letters.
var errInvalidBucketName = errors.New("the specified bucket is not valid")

// errInvalidRange - returned when given range value is not valid.
var errInvalidRange = errors.New("invalid range")

// ErrInvalidRangeValue - Used by external packages
var ErrInvalidRangeValue = errInvalidRange

// errInvalidRangeSource - returned when given range value exceeds
// the source object size.
var errInvalidRangeSource = errors.New("range specified exceeds source object size")

// error returned by disks which are to be initialized are waiting for the
// first server to initialize them in distributed set to initialize them.
var errNotFirstDisk = errors.New("not first disk")

// error returned by first disk waiting to initialize other servers.
var errFirstDiskWait = errors.New("waiting on other disks")

// error returned when a bucket already exists
var errBucketAlreadyExists = errors.New("your previous request to create the named bucket succeeded and you already own it")

// error returned for a negative actual size.
var errInvalidDecompressedSize = errors.New("invalid Decompressed Size")

// error returned in IAM subsystem when user doesn't exist.
var errNoSuchUser = errors.New("specified user does not exist")

// error returned when service account is not found
var errNoSuchServiceAccount = errors.New("specified service account does not exist")

// error returned in IAM subsystem when groups doesn't exist.
var errNoSuchGroup = errors.New("specified group does not exist")

// error returned in IAM subsystem when a non-empty group needs to be
// deleted.
var errGroupNotEmpty = errors.New("specified group is not empty - cannot remove it")

// error returned in IAM subsystem when policy doesn't exist.
var errNoSuchPolicy = errors.New("specified canned policy does not exist")

// error returned in IAM subsystem when an external users systems is configured.
var errIAMActionNotAllowed = errors.New("specified IAM action is not allowed with LDAP configuration")

// error returned in IAM subsystem when IAM sub-system is still being initialized.
var errIAMNotInitialized = errors.New("iam sub-system is being initialized, please try again")

// error returned when access is denied.
var errAccessDenied = errors.New("do not have enough permissions to access this resource")

// error returned when object is locked.
var errLockedObject = errors.New("object is WORM protected and cannot be overwritten or deleted")

// error returned when upload id not found
var errUploadIDNotFound = errors.New("specified Upload ID is not found")
