/*
 * MinIO Cloud Storage, (C) 2016-2019 MinIO, Inc.
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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/obstor/obstor/cmd/config"
	"github.com/obstor/obstor/cmd/config/cache"
	"github.com/obstor/obstor/cmd/config/compress"
	xldap "github.com/obstor/obstor/cmd/config/identity/ldap"
	"github.com/obstor/obstor/cmd/config/identity/openid"
	"github.com/obstor/obstor/cmd/config/notify"
	"github.com/obstor/obstor/cmd/config/policy/opa"
	"github.com/obstor/obstor/cmd/config/storageclass"
	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/event/target"
	"github.com/obstor/obstor/pkg/kms"
	"github.com/obstor/obstor/pkg/madmin"
	xnet "github.com/obstor/obstor/pkg/net"
	"github.com/obstor/obstor/pkg/quick"
)

// DO NOT EDIT following message template, please open a github issue to discuss instead.
var configMigrateMSGTemplate = "Configuration file %s migrated from version '%s' to '%s' successfully."

// Load config from backend
func Load(configFile string, data interface{}) (quick.Config, error) {
	return quick.LoadConfig(configFile, globalEtcdClient, data)
}

// Migrates ${HOME}/.obstor/config.json to '<export_path>/.obstor.sys/config/config.json'
// if etcd is configured then migrates /config/config.json to '<export_path>/.obstor.sys/config/config.json'
func migrateConfigToObstorSys(objAPI ObjectLayer) (err error) {
	// Construct path to config.json for the given bucket.
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	defer func() {
		if err == nil {
			if globalEtcdClient != nil {
				deleteKeyEtcd(GlobalContext, globalEtcdClient, configFile)
			} else {
				// Rename config.json to config.json.deprecated only upon
				// success of this function.
				_ = os.Rename(getConfigFile(), getConfigFile()+".deprecated")
			}
		}
	}()

	// Verify if backend already has the file (after holding lock)
	if err = checkConfig(GlobalContext, objAPI, configFile); err != errConfigNotFound {
		return err
	} // if errConfigNotFound proceed to migrate..

	var configFiles = []string{
		getConfigFile(),
		getConfigFile() + ".deprecated",
		configFile,
	}
	var config = &serverConfigV27{}
	for _, cfgFile := range configFiles {
		if _, err = Load(cfgFile, config); err != nil {
			if !osIsNotExist(err) && !osIsPermission(err) {
				return err
			}
			continue
		}
		break
	}
	if osIsPermission(err) {
		logger.Info("Older config found but not readable %s, proceeding to initialize new config anyways", err)
	}
	if osIsNotExist(err) || osIsPermission(err) {
		// Initialize the server config, if no config exists.
		return newSrvConfig(objAPI)
	}
	return saveServerConfig(GlobalContext, objAPI, config)
}

// Migrates '.obstor.sys/config.json' to v33.
func migrateObstorSysConfig(objAPI ObjectLayer) error {
	// Construct path to config.json for the given bucket.
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	// Check if the config version is latest, if not migrate.
	ok, _, err := checkConfigVersion(objAPI, configFile, "33")
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	if err := migrateV27ToV28ObstorSys(objAPI); err != nil {
		return err
	}
	if err := migrateV28ToV29ObstorSys(objAPI); err != nil {
		return err
	}
	if err := migrateV29ToV30ObstorSys(objAPI); err != nil {
		return err
	}
	if err := migrateV30ToV31ObstorSys(objAPI); err != nil {
		return err
	}
	if err := migrateV31ToV32ObstorSys(objAPI); err != nil {
		return err
	}
	return migrateV32ToV33ObstorSys(objAPI)
}

func checkConfigVersion(objAPI ObjectLayer, configFile string, version string) (bool, []byte, error) {
	data, err := readConfig(GlobalContext, objAPI, configFile)
	if err != nil {
		return false, nil, err
	}

	if !utf8.Valid(data) {
		if GlobalKMS != nil {
			data, err = config.DecryptBytes(GlobalKMS, data, kms.Context{
				obstorMetaBucket: path.Join(obstorMetaBucket, configFile),
			})
			if err != nil {
				data, err = madmin.DecryptData(globalActiveCred.String(), bytes.NewReader(data))
				if err != nil {
					if err == madmin.ErrMaliciousData {
						return false, nil, config.ErrInvalidCredentialsBackendEncrypted(nil)
					}
					return false, nil, err
				}
			}
		} else {
			data, err = madmin.DecryptData(globalActiveCred.String(), bytes.NewReader(data))
			if err != nil {
				if err == madmin.ErrMaliciousData {
					return false, nil, config.ErrInvalidCredentialsBackendEncrypted(nil)
				}
				return false, nil, err
			}
		}
	}

	var versionConfig struct {
		Version string `json:"version"`
	}

	vcfg := &versionConfig
	if err = json.Unmarshal(data, vcfg); err != nil {
		return false, nil, err
	}
	return vcfg.Version == version, data, nil
}

func migrateV27ToV28ObstorSys(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)
	ok, data, err := checkConfigVersion(objAPI, configFile, "27")
	if err == errConfigNotFound {
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to load config file. %w", err)
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV28{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	cfg.Version = "28"
	if err = saveServerConfig(GlobalContext, objAPI, cfg); err != nil {
		return fmt.Errorf("failed to migrate config from ‘27’ to ‘28’. %w", err)
	}

	logger.Info(configMigrateMSGTemplate, configFile, "27", "28")
	return nil
}

func migrateV28ToV29ObstorSys(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	ok, data, err := checkConfigVersion(objAPI, configFile, "28")
	if err == errConfigNotFound {
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to load config file. %w", err)
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV29{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	cfg.Version = "29"
	if err = saveServerConfig(GlobalContext, objAPI, cfg); err != nil {
		return fmt.Errorf("failed to migrate config from ‘28’ to ‘29’. %w", err)
	}

	logger.Info(configMigrateMSGTemplate, configFile, "28", "29")
	return nil
}

func migrateV29ToV30ObstorSys(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	ok, data, err := checkConfigVersion(objAPI, configFile, "29")
	if err == errConfigNotFound {
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to load config file. %w", err)
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV30{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	cfg.Version = "30"
	// Init compression config.For future migration, Compression config needs to be copied over from previous version.
	cfg.Compression.Enabled = false
	cfg.Compression.Extensions = strings.Split(compress.DefaultExtensions, config.ValueSeparator)
	cfg.Compression.MimeTypes = strings.Split(compress.DefaultMimeTypes, config.ValueSeparator)

	if err = saveServerConfig(GlobalContext, objAPI, cfg); err != nil {
		return fmt.Errorf("failed to migrate config from ‘29’ to ‘30’. %w", err)
	}

	logger.Info(configMigrateMSGTemplate, configFile, "29", "30")
	return nil
}

func migrateV30ToV31ObstorSys(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	ok, data, err := checkConfigVersion(objAPI, configFile, "30")
	if err == errConfigNotFound {
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to load config file. %w", err)
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV31{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	cfg.Version = "31"
	cfg.OpenID = openid.Config{}
	cfg.OpenID.JWKS.URL = &xnet.URL{}

	cfg.Policy.OPA = opa.Args{
		URL:       &xnet.URL{},
		AuthToken: "",
	}

	if err = saveServerConfig(GlobalContext, objAPI, cfg); err != nil {
		return fmt.Errorf("failed to migrate config from ‘30’ to ‘31’. %w", err)
	}

	logger.Info(configMigrateMSGTemplate, configFile, "30", "31")
	return nil
}

func migrateV31ToV32ObstorSys(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	ok, data, err := checkConfigVersion(objAPI, configFile, "31")
	if err == errConfigNotFound {
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to load config file. %w", err)
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV32{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	cfg.Version = "32"
	cfg.Notify.NSQ = make(map[string]target.NSQArgs)
	cfg.Notify.NSQ["1"] = target.NSQArgs{}

	if err = saveServerConfig(GlobalContext, objAPI, cfg); err != nil {
		return fmt.Errorf("failed to migrate config from ‘31’ to ‘32’. %w", err)
	}

	logger.Info(configMigrateMSGTemplate, configFile, "31", "32")
	return nil
}

func migrateV32ToV33ObstorSys(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	ok, data, err := checkConfigVersion(objAPI, configFile, "32")
	if err == errConfigNotFound {
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to load config file. %w", err)
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV33{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	cfg.Version = "33"

	if err = saveServerConfig(GlobalContext, objAPI, cfg); err != nil {
		return fmt.Errorf("failed to migrate config from '32' to '33' . %w", err)
	}

	logger.Info(configMigrateMSGTemplate, configFile, "32", "33")
	return nil
}

func migrateObstorSysConfigToKV(objAPI ObjectLayer) error {
	configFile := path.Join(obstorConfigPrefix, obstorConfigFile)

	// Check if the config version is latest, if not migrate.
	ok, data, err := checkConfigVersion(objAPI, configFile, "33")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	cfg := &serverConfigV33{}
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}

	newCfg := newServerConfig()

	config.SetCredentials(newCfg, cfg.Credential)
	config.SetRegion(newCfg, cfg.Region)

	storageclass.SetStorageClass(newCfg, cfg.StorageClass)

	for k, loggerArgs := range cfg.Logger.HTTP {
		logger.SetLoggerHTTP(newCfg, k, loggerArgs)
	}
	for k, auditArgs := range cfg.Logger.Audit {
		logger.SetLoggerHTTPAudit(newCfg, k, auditArgs)
	}

	xldap.SetIdentityLDAP(newCfg, cfg.LDAPServerConfig)
	openid.SetIdentityOpenID(newCfg, cfg.OpenID)
	opa.SetPolicyOPAConfig(newCfg, cfg.Policy.OPA)
	cache.SetCacheConfig(newCfg, cfg.Cache)
	compress.SetCompressionConfig(newCfg, cfg.Compression)

	for k, args := range cfg.Notify.AMQP {
		_ = notify.SetNotifyAMQP(newCfg, k, args)
	}
	for k, args := range cfg.Notify.Elasticsearch {
		_ = notify.SetNotifyES(newCfg, k, args)
	}
	for k, args := range cfg.Notify.Kafka {
		_ = notify.SetNotifyKafka(newCfg, k, args)
	}
	for k, args := range cfg.Notify.MQTT {
		_ = notify.SetNotifyMQTT(newCfg, k, args)
	}
	for k, args := range cfg.Notify.MySQL {
		_ = notify.SetNotifyMySQL(newCfg, k, args)
	}
	for k, args := range cfg.Notify.NATS {
		_ = notify.SetNotifyNATS(newCfg, k, args)
	}
	for k, args := range cfg.Notify.NSQ {
		_ = notify.SetNotifyNSQ(newCfg, k, args)
	}
	for k, args := range cfg.Notify.PostgreSQL {
		_ = notify.SetNotifyPostgres(newCfg, k, args)
	}
	for k, args := range cfg.Notify.Redis {
		_ = notify.SetNotifyRedis(newCfg, k, args)
	}
	for k, args := range cfg.Notify.Webhook {
		_ = notify.SetNotifyWebhook(newCfg, k, args)
	}

	if err = saveServerConfig(GlobalContext, objAPI, newCfg); err != nil {
		return err
	}

	logger.Info("Configuration file %s migrated from version '%s' to new KV format successfully.",
		configFile, "33")
	return nil
}
