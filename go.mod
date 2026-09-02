module github.com/obstor/obstor

go 1.26.0

require (
	cloud.google.com/go/storage v1.62.3
	github.com/Azure/azure-pipeline-go v0.2.3
	github.com/Azure/azure-storage-blob-go v0.15.0
	github.com/IBM/sarama v1.50.2
	github.com/alecthomas/participle v0.7.1
	github.com/apache/thrift v0.23.0
	github.com/bits-and-blooms/bloom/v3 v3.7.1
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/colinmarc/hdfs/v2 v2.4.0
	github.com/coredns/coredns v1.14.4
	github.com/dchest/siphash v1.2.3
	github.com/dustin/go-humanize v1.0.1
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/elastic/go-elasticsearch/v8 v8.19.6
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-ldap/ldap/v3 v3.4.13
	github.com/go-sql-driver/mysql v1.10.0
	github.com/golang-jwt/jwt/v4 v4.5.2
	github.com/google/uuid v1.6.0
	github.com/gorilla/handlers v1.5.2
	github.com/gorilla/mux v1.8.1
	github.com/klauspost/compress v1.18.6
	github.com/klauspost/cpuid/v2 v2.3.0
	github.com/klauspost/pgzip v1.2.6
	github.com/klauspost/reedsolomon v1.14.0
	github.com/lib/pq v1.12.3
	github.com/miekg/dns v1.1.72
	github.com/montanaflynn/stats v0.9.0
	github.com/nats-io/nats-server/v2 v2.12.6
	github.com/nats-io/nats.go v1.52.0
	github.com/nats-io/stan.go v0.10.4
	github.com/nsqio/go-nsq v1.1.0
	github.com/obstor/highwayhash v1.0.4
	github.com/obstor/obstor-go/v7 v7.1.0
	github.com/obstor/selfupdate v0.6.0
	github.com/obstor/sha256-simd v1.0.1
	github.com/obstor/sio v0.5.1
	github.com/pierrec/lz4 v2.6.1+incompatible
	github.com/pkg/sftp v1.13.10
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/procfs v0.20.1
	github.com/rabbitmq/amqp091-go v1.11.0
	github.com/redis/go-redis/v9 v9.20.1
	github.com/rs/cors v1.11.1
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/tinylib/msgp v1.6.4
	github.com/urfave/cli v1.22.17
	github.com/xdg-go/scram v1.2.0
	go.etcd.io/etcd/api/v3 v3.6.12
	go.etcd.io/etcd/client/v3 v3.6.12
	go.uber.org/zap v1.28.0
	golang.org/x/crypto v0.53.0
	golang.org/x/net v0.56.0
	golang.org/x/sys v0.46.0
	golang.org/x/term v0.44.0
	golang.org/x/time v0.15.0
	google.golang.org/api v0.284.0
	gopkg.in/yaml.v2 v2.4.0
)

require (
	aead.dev/minisign v0.3.0 // indirect
	cel.dev/expr v0.25.2 // indirect
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.11.0 // indirect
	cloud.google.com/go/monitoring v1.29.0 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.33.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.57.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.57.0 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.6.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.5 // indirect
	github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/elastic/elastic-transport-go/v8 v8.11.0 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.37.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/frankban/quicktest v1.14.6 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8-0.20250403174932-29230038a667 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.16 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/goidentity/v6 v6.0.1 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/klauspost/crc32 v1.3.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20260330125221-c963978e514e // indirect
	github.com/mattn/go-ieproxy v0.0.12 // indirect
	github.com/minio/highwayhash v1.0.4-0.20251030100505-070ab1a87a76 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.1 // indirect
	github.com/nats-io/nats-streaming-server v0.24.6 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/obstor/crc64nvme v1.1.1 // indirect
	github.com/obstor/md5-simd v1.1.2 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/common v0.68.1 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shoenig/go-m1cpu v0.2.1 // indirect
	github.com/shoenig/test v1.12.1 // indirect
	github.com/spiffe/go-spiffe/v2 v2.7.0 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.12 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.44.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.69.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/tools v0.46.0 // indirect
	google.golang.org/genproto v0.0.0-20260610212136-7ab31c22f7ad // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260610212136-7ab31c22f7ad // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260610212136-7ab31c22f7ad // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/ini.v1 v1.67.3 // indirect
)
