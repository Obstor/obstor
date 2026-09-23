# Obstor Server Config Guide

## Configuration Directory

Configuration is stored in the backend under `.obstor.sys/config/`; there is no local config directory.

```bash
obstor server /data
```

Obstor also encrypts all the config, IAM and policies content with admin credentials.

### Certificate Directory

TLS certificates by default are stored under ``${HOME}/.obstor/certs`` directory. You need to place certificates here to enable `HTTPS` based access. Read more about [How to secure access to Obstor server with TLS](/docs/tls).

Following is the directory structure for Obstor server with TLS certificates.

```bash
$ tree ~/.obstor
/home/user1/.obstor
└─ certs
   ├─ CAs
   ├─ private.key
   └─ public.crt
```

You can provide a custom certs directory using `--certs-dir` command line option.

#### Credentials
On Obstor admin credentials or root credentials are only allowed to be changed using ENVs namely `OBSTOR_ROOT_USER` and `OBSTOR_ROOT_PASSWORD`. Using the combination of these two values Obstor encrypts the config stored at the backend.

```bash
export OBSTOR_ROOT_USER=obstor
export OBSTOR_ROOT_PASSWORD=obstor13
obstor server /data
```

##### Rotating encryption with new credentials

Additionally if you wish to change the admin credentials, then Obstor will automatically detect this and re-encrypt with new credentials as shown below. For one time only special ENVs as shown below needs to be set for rotating the encryption config.

> Old ENVs are never remembered in memory and are destroyed right after they are used to migrate your existing content with new credentials. You are safe to remove them after the server as successfully started, by restarting the services once again.

```bash
export OBSTOR_ROOT_USER=newobstor
export OBSTOR_ROOT_PASSWORD=newobstor123
export OBSTOR_ROOT_USER_OLD=obstor
export OBSTOR_ROOT_PASSWORD_OLD=obstor123
obstor server /data
```

Once the migration is complete, server will automatically unset the `OBSTOR_ROOT_USER_OLD` and `OBSTOR_ROOT_PASSWORD_OLD` with in the process namespace.

> **NOTE: Make sure to remove `OBSTOR_ROOT_USER_OLD` and `OBSTOR_ROOT_PASSWORD_OLD` in scripts or service files before next service restarts of the server to avoid double encryption of your existing contents.**

#### Region
```bash
KEY:
region  label the location of the server

ARGS:
name     (string)    name of the location of the server e.g. "us-west-rack2"
comment  (sentence)  optionally add a comment to this setting
```

or environment variables
```bash
KEY:
region  label the location of the server

ARGS:
OBSTOR_REGION_NAME     (string)    name of the location of the server e.g. "us-west-rack2"
OBSTOR_REGION_COMMENT  (sentence)  optionally add a comment to this setting
```

Example:

```bash
export OBSTOR_REGION_NAME="my_region"
obstor server /data
```

### Storage Class
By default, parity for objects with standard storage class is set to `N/2`, and parity for objects with reduced redundancy storage class objects is set to `2`. Read more about storage class support in Obstor server [here](/docs/erasure/storage-class).

```bash
KEY:
storage_class  define object level redundancy

ARGS:
standard  (string)    set the parity count for default standard storage class e.g. "EC:4"
rrs       (string)    set the parity count for reduced redundancy storage class e.g. "EC:2"
comment   (sentence)  optionally add a comment to this setting
```

or environment variables
```bash
KEY:
storage_class  define object level redundancy

ARGS:
OBSTOR_STORAGE_CLASS_STANDARD  (string)    set the parity count for default standard storage class e.g. "EC:4"
OBSTOR_STORAGE_CLASS_RRS       (string)    set the parity count for reduced redundancy storage class e.g. "EC:2"
OBSTOR_STORAGE_CLASS_COMMENT   (sentence)  optionally add a comment to this setting
```

### Cache
Obstor provides caching storage tier for primarily backend deployments, allowing you to cache content for faster reads, cost savings on repeated downloads from the cloud.

```bash
KEY:
cache  add caching storage tier

ARGS:
drives*  (csv)       comma separated mountpoints e.g. "/optane1,/optane2"
expiry   (number)    cache expiry duration in days e.g. "90"
quota    (number)    limit cache drive usage in percentage e.g. "90"
exclude  (csv)       comma separated wildcard exclusion patterns e.g. "bucket/*.tmp,*.exe"
after    (number)    minimum number of access before caching an object
comment  (sentence)  optionally add a comment to this setting
```

or environment variables
```bash
KEY:
cache  add caching storage tier

ARGS:
OBSTOR_CACHE_DRIVES*  (csv)       comma separated mountpoints e.g. "/optane1,/optane2"
OBSTOR_CACHE_EXPIRY   (number)    cache expiry duration in days e.g. "90"
OBSTOR_CACHE_QUOTA    (number)    limit cache drive usage in percentage e.g. "90"
OBSTOR_CACHE_EXCLUDE  (csv)       comma separated wildcard exclusion patterns e.g. "bucket/*.tmp,*.exe"
OBSTOR_CACHE_AFTER    (number)    minimum number of access before caching an object
OBSTOR_CACHE_COMMENT  (sentence)  optionally add a comment to this setting
```

#### Etcd
Obstor supports storing encrypted IAM assets and bucket DNS records on etcd.

> NOTE: if *path_prefix* is set then Obstor will not federate your buckets, namespaced IAM assets are assumed as isolated tenants, only buckets are considered globally unique but performing a lookup with a *bucket* which belongs to a different tenant will fail unlike federated setups where Obstor would port-forward and route the request to relevant cluster accordingly. This is a special feature, federated deployments should not need to set *path_prefix*.

```bash
KEY:
etcd  federate multiple clusters for IAM and Bucket DNS

ARGS:
endpoints*       (csv)       comma separated list of etcd endpoints e.g. "http://localhost:2379"
path_prefix      (path)      namespace prefix to isolate tenants e.g. "customer1/"
coredns_path     (path)      shared bucket DNS records, default is "/skydns"
client_cert      (path)      client cert for mTLS authentication
client_cert_key  (path)      client cert key for mTLS authentication
comment          (sentence)  optionally add a comment to this setting
```

or environment variables
```bash
KEY:
etcd  federate multiple clusters for IAM and Bucket DNS

ARGS:
OBSTOR_ETCD_ENDPOINTS*       (csv)       comma separated list of etcd endpoints e.g. "http://localhost:2379"
OBSTOR_ETCD_PATH_PREFIX      (path)      namespace prefix to isolate tenants e.g. "customer1/"
OBSTOR_ETCD_COREDNS_PATH     (path)      shared bucket DNS records, default is "/skydns"
OBSTOR_ETCD_CLIENT_CERT      (path)      client cert for mTLS authentication
OBSTOR_ETCD_CLIENT_CERT_KEY  (path)      client cert key for mTLS authentication
OBSTOR_ETCD_COMMENT          (sentence)  optionally add a comment to this setting
```

### API
By default, there is no limitation on the number of concurrent requests that a server/cluster processes at the same time. However, it is possible to impose such limitation using the API subsystem. Read more about throttling limitation in Obstor server [here](/docs/throttle).

```bash
KEY:
api  manage global HTTP API call specific features, such as throttling, authentication types, etc.

ARGS:
requests_max               (number)    set the maximum number of concurrent requests, e.g. "1600"
requests_deadline          (duration)  set the deadline for API requests waiting to be processed e.g. "1m"
cors_allow_origin          (csv)       set comma separated list of origins allowed for CORS requests e.g. "https://example1.com,https://example2.com"
remote_transport_deadline  (duration)  set the deadline for API requests on remote transports while proxying between federated instances e.g. "2h"
```

or environment variables

```bash
OBSTOR_API_REQUESTS_MAX               (number)    set the maximum number of concurrent requests, e.g. "1600"
OBSTOR_API_REQUESTS_DEADLINE          (duration)  set the deadline for API requests waiting to be processed e.g. "1m"
OBSTOR_API_CORS_ALLOW_ORIGIN          (csv)       set comma separated list of origins allowed for CORS requests e.g. "https://example1.com,https://example2.com"
OBSTOR_API_REMOTE_TRANSPORT_DEADLINE  (duration)  set the deadline for API requests on remote transports while proxying between federated instances e.g. "2h"
```

#### Notifications
Notification targets supported by Obstor are in the following list. To configure individual targets please refer to more detailed documentation [here](/docs/bucket/notifications)

```bash
notify_webhook        publish bucket notifications to webhook endpoints
notify_amqp           publish bucket notifications to AMQP endpoints
notify_kafka          publish bucket notifications to Kafka endpoints
notify_mqtt           publish bucket notifications to MQTT endpoints
notify_nats           publish bucket notifications to NATS endpoints
notify_nsq            publish bucket notifications to NSQ endpoints
notify_mysql          publish bucket notifications to MySQL databases
notify_postgres       publish bucket notifications to Postgres databases
notify_elasticsearch  publish bucket notifications to Elasticsearch endpoints
notify_redis          publish bucket notifications to Redis datastores
```

### Accessing configuration
All configuration is set through `OBSTOR_*` environment variables. Each subsystem maps to a set of environment variables, named by uppercasing the subsystem and its keys and prefixing them with `OBSTOR_`. Set the relevant variables in your environment (or service file) before starting the server.

#### Example: etcd subsystem

The `etcd` subsystem maps to the following environment variables:

```bash
KEY:
etcd  federate multiple clusters for IAM and Bucket DNS

ARGS:
OBSTOR_ETCD_ENDPOINTS*       (csv)       comma separated list of etcd endpoints e.g. "http://localhost:2379"
OBSTOR_ETCD_PATH_PREFIX      (path)      namespace prefix to isolate tenants e.g. "customer1/"
OBSTOR_ETCD_COREDNS_PATH     (path)      shared bucket DNS records, default is "/skydns"
OBSTOR_ETCD_CLIENT_CERT      (path)      client cert for mTLS authentication
OBSTOR_ETCD_CLIENT_CERT_KEY  (path)      client cert key for mTLS authentication
OBSTOR_ETCD_COMMENT          (sentence)  optionally add a comment to this setting
```

This pattern is consistent across all subsystems. The environment variable forms for each key is documented with examples throughout this guide.

## Dynamic systems without restarting server

The following sub-systems are dynamic i.e., configuration parameters for each sub-systems can be changed while the server is running without any restarts.

```bash
api                   manage global HTTP API call specific features, such as throttling, authentication types, etc.
heal                  manage object healing frequency and bitrot verification checks
scanner               manage namespace scanning for usage calculation, lifecycle, healing and more
```

> NOTE: if you set any of the following sub-system configuration using ENVs, dynamic behavior is not supported.

### Usage scanner

Data usage scanner is enabled by default. The following configuration settings allow for more staggered delay in terms of usage calculation. The scanner adapts to the system speed and completely pauses when the system is under load. It is possible to adjust the speed of the scanner and thereby the latency of updates being reflected. The delays between each operation of the scanner are controlled by `OBSTOR_SCANNER_DELAY`. By default the value is `10.0`. This means the scanner will sleep *10x* the time each operation takes.

In most setups this will keep the scanner slow enough to not impact overall system performance. Setting the delay to a *lower* value will make the scanner faster and setting it to 0 will make the scanner run at full speed (not recommended in production). Setting it to a higher value will make the scanner slower, consuming less resources with the trade off of not collecting metrics for operations like healing and disk usage as fast.

The scanner subsystem maps to the following environment variables:

```bash
KEY:
scanner  manage namespace scanning for usage calculation, lifecycle, healing and more

ARGS:
OBSTOR_SCANNER_DELAY     (float)     scanner delay multiplier, defaults to '10.0'
OBSTOR_SCANNER_MAX_WAIT  (duration)  maximum wait time between operations, defaults to '15s'
```

Example: Following setting will decrease the scanner speed by a factor of 3, reducing the system resource use, but increasing the latency of updates being reflected.

```bash
export OBSTOR_SCANNER_DELAY=30.0
```

The scanner settings are applied when the server is started with these environment variables set.

> NOTE: Data usage scanner is not supported under Backend deployments.

### Healing

Healing is enabled by default. The following configuration settings allow for more staggered delay in terms of healing. The healing system by default adapts to the system speed and pauses up to '1sec' per object when the system has `max_io` number of concurrent requests. It is possible to adjust the `max_sleep` and `max_io` values thereby increasing the healing speed. The delays between each operation of the healer can be adjusted by `OBSTOR_HEAL_MAX_SLEEP` and the maximum concurrent requests allowed before we start slowing things down can be configured with `OBSTOR_HEAL_MAX_IO`. By default the wait delay is `1sec` beyond 10 concurrent operations. This means the healer will sleep *1 second* at max for each heal operation if there are more than *10* concurrent client requests.

In most setups this is sufficient to heal the content after drive replacements. Setting `max_sleep` to a *lower* value and setting `max_io` to a *higher* value would make heal go faster.

The heal subsystem maps to the following environment variables:

```bash
KEY:
heal  manage object healing frequency and bitrot verification checks

ARGS:
OBSTOR_HEAL_BITROTSCAN  (on|off)    perform bitrot scan on disks when checking objects during scanner
OBSTOR_HEAL_MAX_SLEEP   (duration)  maximum sleep duration between objects to slow down heal operation. eg. 2s
OBSTOR_HEAL_MAX_IO      (int)       maximum IO requests allowed between objects to slow down heal operation. eg. 3
```

Example: The following settings will increase the heal operation speed by allowing healing operation to run without delay up to `100` concurrent requests, and the maximum delay between each heal operation is set to `300ms`.

```bash
export OBSTOR_HEAL_MAX_SLEEP=300ms
export OBSTOR_HEAL_MAX_IO=100
```

The healer settings are applied when the server is started with these environment variables set.

> NOTE: Healing is not supported under Backend deployments.


## Environment only settings (not in config)

### Browser

Enable or disable access to web UI. By default it is set to `on`. You may override this field with `OBSTOR_BROWSER` environment variable.

Example:

```bash
export OBSTOR_BROWSER=off
obstor server /data
```

### Domain

By default, Obstor supports path-style requests that are of the format http://example.com/bucket/object. `OBSTOR_DOMAIN` environment variable is used to enable virtual-host-style requests. If the request `Host` header matches with `(.+).example.com` then the matched pattern `$1` is used as bucket and the path is used as object. More information on path-style and virtual-host-style [here](http://docs.aws.amazon.com/AmazonS3/latest/dev/RESTAPI.html)
Example:

```bash
export OBSTOR_DOMAIN=example.com
obstor server /data
```

For advanced use cases `OBSTOR_DOMAIN` environment variable supports multiple-domains with comma separated values.
```bash
export OBSTOR_DOMAIN=sub1.example.com,sub2.example.com
obstor server /data
```

## Explore Further
* Obstor Quickstart Guide
* [Configure Obstor Server with TLS](/docs/tls)
