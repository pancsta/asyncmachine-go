module github.com/pancsta/asyncmachine-go

go 1.25.0

// FORK REPLACES

// replace libp2p's prom with no-op to prevent a goroutine leak in load tests
//replace github.com/prometheus/client_golang => github.com/pancsta/prom_client_golang v0.0.0-20250629100015-513d4c0bdd5e

// https://github.com/open-telemetry/opentelemetry-go/pull/8120
//replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48

// DEV REPLACES

//replace github.com/pancsta/cview => ../cview

//replace github.com/cenkalti/rpc2 => ../rpc2

//replace go.opentelemetry.io => ../opentelemetry-go

//replace github.com/AlexanderGrooff/mermaid-ascii => ../mermaid-ascii

//replace github.com/gdamore/tcell/v2 => ../tcell
//replace github.com/pancsta/tcell-v2 => ../tcell-v2

require (
	github.com/AlexanderGrooff/mermaid-ascii v0.0.0-20260221123917-b5d02c35decf
	github.com/K-Phoen/grabana v0.22.2
	github.com/PuerkitoBio/goquery v1.10.0
	github.com/alexflint/go-arg v1.6.0
	github.com/andybalholm/brotli v1.2.0
	github.com/apache/arrow-go/v18 v18.4.1
	github.com/cenkalti/rpc2 v1.0.4
	github.com/charmbracelet/ssh v0.0.0-20250826160808-ebfa259c7309
	github.com/coder/websocket v1.8.12
	github.com/dominikbraun/graph v0.23.0
	github.com/failsafe-go/failsafe-go v0.6.8
	github.com/fsnotify/fsnotify v1.7.1-0.20240403050945-7086bea086b7
	github.com/hibiken/asynq v0.24.1
	github.com/ic2hrmk/promtail v0.0.5
	github.com/ipfs/go-log/v2 v2.6.0
	github.com/joho/godotenv v1.5.1
	github.com/libp2p/go-libp2p v0.47.0
	github.com/libp2p/go-libp2p-pubsub v0.13.0
	github.com/lithammer/dedent v1.1.0
	github.com/mark3labs/mcp-go v0.48.0
	github.com/multiformats/go-multiaddr v0.16.0
	github.com/multiformats/go-multiaddr-fmt v0.1.0
	github.com/nats-io/nats-server/v2 v2.12.3
	github.com/nats-io/nats.go v1.48.0
	github.com/ncruces/go-sqlite3 v0.30.4
	github.com/ncruces/go-sqlite3/gormlite v0.30.2
	github.com/orsinium-labs/enum v1.4.0
	github.com/pancsta/cview v1.5.22-0.20260308120046-97d519652c66
	github.com/pancsta/tcell-v2 v0.0.1-fork1
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/polarsignals/frostdb v0.0.0-20250728125857-906ebbaef267
	github.com/prometheus/client_golang v1.22.0
	github.com/reeflective/console v0.1.25
	github.com/reeflective/readline v1.1.3
	github.com/sethvargo/go-envconfig v1.1.0
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/soheilhy/cmux v0.1.5
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.6
	github.com/stretchr/testify v1.11.1
	github.com/teivah/onecontext v1.3.0
	github.com/testcontainers/testcontainers-go v0.40.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.40.0
	github.com/vmihailenco/msgpack/v5 v5.4.1
	github.com/zyedidia/clipper v0.1.1
	go.etcd.io/bbolt v1.3.6
	go.opentelemetry.io/otel v1.42.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.42.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.42.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.19.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.18.0
	go.opentelemetry.io/otel/log v0.18.0
	go.opentelemetry.io/otel/sdk v1.42.0
	go.opentelemetry.io/otel/sdk/log v0.18.0
	go.opentelemetry.io/otel/trace v1.42.0
	golang.org/x/exp v0.0.0-20250606033433-dcc06ee1d476
	golang.org/x/sync v0.20.0
	golang.org/x/text v0.35.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
	gorm.io/datatypes v1.2.7
	gorm.io/driver/postgres v1.6.0
	gorm.io/gorm v1.31.1
	honnef.co/go/js/dom/v2 v2.0.0-20250304181735-b5e52f05e89d
	oss.terrastruct.com/d2 v0.7.1
	oss.terrastruct.com/util-go v0.0.0-20250213174338-243d8661088a
)

require (
	dario.cat/mergo v1.0.2 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/K-Phoen/sdk v0.12.4 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/RoaringBitmap/roaring v1.9.4 // indirect
	github.com/alecthomas/chroma/v2 v2.14.0 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/andybalholm/cascadia v1.3.2 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/antithesishq/antithesis-sdk-go v0.5.0-default-no-op // indirect
	github.com/apache/thrift v0.22.0 // indirect
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/benbjohnson/immutable v0.4.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.13.0 // indirect
	github.com/carapace-sh/carapace v1.7.1 // indirect
	github.com/carapace-sh/carapace-shlex v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/x/conpty v0.1.0 // indirect
	github.com/charmbracelet/x/errors v0.0.0-20240508181413-e8d8b6e2de86 // indirect
	github.com/charmbracelet/x/termios v0.1.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/coreos/etcd v3.3.27+incompatible // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20220810130054-c7d1c02cb6cf // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/creack/pty v1.1.21 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/davidlazar/go-crypto v0.0.0-20200604182044-b73af7476f6c // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/dgryski/go-metro v0.0.0-20250106013310-edb8663e5e33 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/docker/docker v28.5.1+incompatible // indirect
	github.com/docker/go-connections v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dop251/goja v0.0.0-20240927123429-241b342198c2 // indirect
	github.com/dunglas/httpsfv v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.8.4 // indirect
	github.com/efficientgo/core v1.0.0-rc.2 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.7 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/pprof v0.0.0-20250607225305-033d6d78b36a // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/gosimple/slug v1.13.1 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hamba/avro/v2 v2.29.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/ipfs/go-cid v0.5.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.6.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jbenet/go-temp-err-catcher v0.1.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/klauspost/asmfmt v1.3.2 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/koron/go-ssdp v0.0.6 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-flow-metrics v0.2.0 // indirect
	github.com/libp2p/go-libp2p-asn-util v0.4.1 // indirect
	github.com/libp2p/go-msgio v0.3.0 // indirect
	github.com/libp2p/go-netroute v0.3.0 // indirect
	github.com/libp2p/go-reuseport v0.4.0 // indirect
	github.com/libp2p/go-yamux/v5 v5.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/marten-seemann/tcp v0.0.0-20210406111302-dfbc87cc63fd // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mazznoer/csscolorparser v0.1.5 // indirect
	github.com/miekg/dns v1.1.66 // indirect
	github.com/mikioh/tcpinfo v0.0.0-20190314235526-30a79bb1804b // indirect
	github.com/mikioh/tcpopt v0.0.0-20190314235656-172688c1accc // indirect
	github.com/minio/asm2plan9s v0.0.0-20200509001527-cdd76441f9d8 // indirect
	github.com/minio/c2goasm v0.0.0-20190812172519-36a3d3bbc4f3 // indirect
	github.com/minio/highwayhash v1.0.4-0.20251030100505-070ab1a87a76 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.1.0 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multiaddr-dns v0.4.1 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.1 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-multistream v0.6.1 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.0 // indirect
	github.com/nats-io/nkeys v0.4.12 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/oklog/ulid/v2 v2.1.0 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/parquet-go/parquet-go v0.24.0 // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v2 v2.2.12 // indirect
	github.com/pion/dtls/v3 v3.0.6 // indirect
	github.com/pion/ice/v4 v4.0.10 // indirect
	github.com/pion/interceptor v0.1.40 // indirect
	github.com/pion/logging v0.2.3 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.15 // indirect
	github.com/pion/rtp v1.8.19 // indirect
	github.com/pion/sctp v1.8.39 // indirect
	github.com/pion/sdp/v3 v3.0.13 // indirect
	github.com/pion/srtp/v3 v3.0.6 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v2 v2.2.10 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.2 // indirect
	github.com/pion/webrtc/v4 v4.1.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/polarsignals/iceberg-go v0.0.0-20240502213135-2ee70b71e76b // indirect
	github.com/polarsignals/wal v0.0.0-20240619104840-9da940027f9c // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.59.0 // indirect
	github.com/quic-go/webtransport-go v0.10.0 // indirect
	github.com/redis/go-redis/v9 v9.0.3 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/shirou/gopsutil/v4 v4.25.6 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/thanos-io/objstore v0.0.0-20240818203309-0363dadfdfb1 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.7.4 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.49.0 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/dig v1.19.0 // indirect
	go.uber.org/fx v1.24.0 // indirect
	go.uber.org/mock v0.5.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/image v0.20.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/telemetry v0.0.0-20260209163413-e7419c687ee4 // indirect
	golang.org/x/term v0.41.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260330182312-d5a96adf58d8 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/driver/mysql v1.5.6 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
	mvdan.cc/sh/v3 v3.7.0 // indirect
)
