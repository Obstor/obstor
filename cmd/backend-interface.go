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
	"github.com/obstor/obstor/pkg/auth"
)

const (
	NASBackend  = "nas"
	S3Backend   = "s3"
	SFTPBackend = "sftp"
)

// Backend represents a storage backend.
type Backend interface {
	// Name returns the unique name of the backend.
	Name() string

	// NewBackendLayer returns a new ObjectLayer.
	NewBackendLayer(creds auth.Credentials) (ObjectLayer, error)

	// Returns true if backend is ready for production.
	Production() bool
}
