/*
 * MinIO Cloud Storage, (C) 2016, 2017, 2018 MinIO, Inc.
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
	"github.com/obstor/obstor/cmd/config"
	"github.com/obstor/obstor/cmd/config/cache"
	"github.com/obstor/obstor/cmd/config/compress"
	xldap "github.com/obstor/obstor/cmd/config/identity/ldap"
	"github.com/obstor/obstor/cmd/config/identity/openid"
	"github.com/obstor/obstor/cmd/config/notify"
	"github.com/obstor/obstor/cmd/config/policy/opa"
	"github.com/obstor/obstor/cmd/config/storageclass"
	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/auth"
	"github.com/obstor/obstor/pkg/event/target"
	"github.com/obstor/obstor/pkg/quick"
)

type notifierV3 struct {
	AMQP          map[string]target.AMQPArgs          `json:"amqp"`
	Elasticsearch map[string]target.ElasticsearchArgs `json:"elasticsearch"`
	Kafka         map[string]target.KafkaArgs         `json:"kafka"`
	MQTT          map[string]target.MQTTArgs          `json:"mqtt"`
	MySQL         map[string]target.MySQLArgs         `json:"mysql"`
	NATS          map[string]target.NATSArgs          `json:"nats"`
	PostgreSQL    map[string]target.PostgreSQLArgs    `json:"postgresql"`
	Redis         map[string]target.RedisArgs         `json:"redis"`
	Webhook       map[string]target.WebhookArgs       `json:"webhook"`
}

// serverConfigV27 is just like version '26', stores additionally
// the logger field
type serverConfigV27 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Browser    config.BoolFlag  `json:"browser"`
	Worm       config.BoolFlag  `json:"worm"`
	Domain     string           `json:"domain"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`
}

// serverConfigV28 is just like version '27', additionally
// storing KMS config
type serverConfigV28 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`
}

// serverConfigV29 is just like version '28'.
type serverConfigV29 serverConfigV28

// serverConfigV30 is just like version '29', stores additionally
// extensions and mimetypes fields for compression.
type serverConfigV30 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`
}

// serverConfigV31 is just like version '30', with OPA and OpenID configuration.
type serverConfigV31 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notifierV3 `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`

	// OpenID configuration
	OpenID openid.Config `json:"openid"`

	// External policy enforcements.
	Policy struct {
		// OPA configuration.
		OPA opa.Args `json:"opa"`

		// Add new external policy enforcements here.
	} `json:"policy"`
}

// serverConfigV32 is just like version '31' with added nsq notifer.
type serverConfigV32 struct {
	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notify.Config `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`

	// OpenID configuration
	OpenID openid.Config `json:"openid"`

	// External policy enforcements.
	Policy struct {
		// OPA configuration.
		OPA opa.Args `json:"opa"`

		// Add new external policy enforcements here.
	} `json:"policy"`
}

// serverConfigV33 is just like version '32', removes clientID from NATS and MQTT, and adds queueDir, queueLimit in all notification targets.
type serverConfigV33 struct {
	quick.Config `json:"-"` // ignore interfaces

	Version string `json:"version"`

	// S3 API configuration.
	Credential auth.Credentials `json:"credential"`
	Region     string           `json:"region"`
	Worm       config.BoolFlag  `json:"worm"`

	// Storage class configuration
	StorageClass storageclass.Config `json:"storageclass"`

	// Cache configuration
	Cache cache.Config `json:"cache"`

	// Notification queue configuration.
	Notify notify.Config `json:"notify"`

	// Logger configuration
	Logger logger.Config `json:"logger"`

	// Compression configuration
	Compression compress.Config `json:"compress"`

	// OpenID configuration
	OpenID openid.Config `json:"openid"`

	// External policy enforcements.
	Policy struct {
		// OPA configuration.
		OPA opa.Args `json:"opa"`

		// Add new external policy enforcements here.
	} `json:"policy"`

	LDAPServerConfig xldap.Config `json:"ldapserverconfig"`
}
