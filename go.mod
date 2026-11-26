module github.com/pancsta/asyncmachine-go

go 1.26.9

// FORK REPLACES

// https://github.com/open-telemetry/opentelemetry-go/pull/8120
//replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48

// DEV REPLACES

//replace github.com/pancsta/gochickpeas => ../gochickpeas

//replace github.com/cenkalti/rpc2 => ../rpc2

//replace go.opentelemetry.io => ../opentelemetry-go

//replace github.com/AlexanderGrooff/mermaid-ascii => ../mermaid-ascii

//replace github.com/gdamore/tcell/v2 => ../tcell
//replace github.com/gdamore/tcell/v2 => ../tcell-v2

//replace github.com/tmc/go-iroh => ../go-iroh

//replace github.com/pancsta/d2 => ../d2

//tool github.com/traefik/yaegi/cmd/yaegi

require (
	charm.land/ssh v0.4.3
	github.com/AlexanderGrooff/mermaid-ascii v0.0.0-20261117200642-e7a2d2f81cdb
	github.com/K-Phoen/grabana v0.22.2
	github.com/PuerkitoBio/goquery v1.14.0
	github.com/alexflint/go-arg v1.6.1
	github.com/alitto/pond/v2 v2.7.3
	github.com/andybalholm/brotli v1.3.0
	github.com/apache/arrow-go/v18 v18.9.0
	github.com/cenkalti/rpc2 v1.0.8
	github.com/chromedp/chromedp v0.24.0
	github.com/coder/websocket v1.8.15
	github.com/creack/pty v1.1.24
	github.com/d2lang/util-go v0.2.0
	github.com/dave/jennifer v1.7.1
	github.com/dgraph-io/badger/v4 v4.9.6
	github.com/failsafe-go/failsafe-go v0.9.8
	github.com/freeeve/gochickpeas v0.27.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/gdamore/tcell/v2 v2.13.10
	github.com/gobwas/ws v1.5.0
	github.com/goforj/httpx v1.1.0
	github.com/gookit/goutil v0.9.0
	github.com/hibiken/asynq v0.26.0
	github.com/ic2hrmk/promtail v0.0.5
	github.com/joho/godotenv v1.5.1
	github.com/lithammer/dedent v1.1.0
	github.com/mark3labs/mcp-go v1.6.0
	github.com/mlange-42/ark v0.8.3
	github.com/nats-io/nats-server/v2 v2.15.3
	github.com/nats-io/nats.go v1.56.0
	github.com/ncruces/go-sqlite3 v0.38.0
	github.com/ncruces/go-sqlite3/gormlite v0.34.0
	github.com/oleiade/reflections v1.1.0
	github.com/orsinium-labs/enum v1.5.0
	github.com/pancsta/cview v1.6.0
	github.com/pancsta/d2 v0.8.2-go1.26
	github.com/pancsta/go-app v0.0.0-20261030132146-e4194936713d
	github.com/pancsta/gochickpeas v0.0.0-20261104105447-4a20cacfdf7d
	github.com/pancsta/livepage v0.1.3
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/polarsignals/frostdb v0.0.0-20261108180411-9087d8f56226
	github.com/prometheus/client_golang v1.25.0
	github.com/reeflective/console v0.5.0
	github.com/reeflective/readline v1.3.0
	github.com/sethvargo/go-envconfig v1.4.3
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/soheilhy/cmux v0.1.5
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.1.0
	github.com/stretchr/testify v1.12.1
	github.com/teivah/onecontext v1.3.0
	github.com/testcontainers/testcontainers-go v0.45.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.45.0
	github.com/tmc/go-iroh v0.3.0
	github.com/traefik/yaegi v0.16.3
	github.com/vmihailenco/msgpack/v5 v5.4.1
	github.com/zyedidia/clipper v0.1.1
	go.etcd.io/bbolt v1.5.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.23.0
	go.opentelemetry.io/otel/log v1.47.0
	go.opentelemetry.io/otel/sdk v1.47.0
	go.opentelemetry.io/otel/sdk/log v1.47.0
	go.opentelemetry.io/otel/trace v1.47.0
	golang.org/x/exp v0.0.0-20261120182113-8801fbdfac81
	golang.org/x/sync v0.25.0
	golang.org/x/term v0.48.0
	golang.org/x/text v0.44.0
	golang.org/x/tools v0.53.0
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/datatypes v1.2.7
	gorm.io/driver/postgres v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	dario.cat/mergo v1.0.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20261006220739-8c912ac31dd4 // indirect
	github.com/K-Phoen/sdk v0.13.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/RoaringBitmap/roaring v1.9.4 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.30.0 // indirect
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/alecthomas/repr v0.5.4 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/andybalholm/cascadia v1.3.5 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/antithesishq/antithesis-sdk-go v0.9.0-default-no-op // indirect
	github.com/apache/thrift v0.25.0 // indirect
	github.com/benbjohnson/immutable v0.4.3 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.30.0 // indirect
	github.com/carapace-sh/carapace v1.18.2 // indirect
	github.com/carapace-sh/carapace-shlex/v2 v2.1.4 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/chromedp/cdproto v0.160.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/coreos/etcd v3.3.27+incompatible // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20240122114842-bbd7aa9bf6fb // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/d2lang/dagro v0.2.1 // indirect
	github.com/d2lang/elk-go v0.2.0 // indirect
	github.com/d2lang/mathjax-go v0.1.1-0.20261002175158-94d80186bfd7 // indirect
	github.com/d2lang/rough-go v0.2.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgraph-io/ristretto/v2 v2.4.2 // indirect
	github.com/dgryski/go-metro v0.0.0-20261008173524-5059e0b1da03 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dlclark/regexp2/v2 v2.13.1 // indirect
	github.com/docker/go-connections v0.8.2 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/efficientgo/core v1.0.0-rc.3 // indirect
	github.com/ericpauley/go-quantize v0.0.0-20200331213906-ae555eb2afa4 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/freeeve/libcodex v0.33.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/go-json-experiment/json v0.0.0-20260213210345-44df1a37e875 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.1 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sql-driver/mysql v1.10.2 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/goccy/go-json v0.12.0 // indirect
	github.com/goforj/godump v1.8.0 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gosimple/slug v1.15.0 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.34.0 // indirect
	github.com/hamba/avro/v2 v2.31.0 // indirect
	github.com/hinshun/vt10x v0.0.0-20220301184237-5011da428d02 // indirect
	github.com/icholy/digest v1.2.0 // indirect
	// req v3.61+ and quic-go <= v0.64: req http3 vs quic-go v0.65
	github.com/imroc/req/v3 v3.61.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.6.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/klauspost/compress v1.20.5 // indirect
	github.com/klauspost/cpuid/v2 v2.4.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20261101121723-be84dd9509a0 // indirect
	github.com/magiconair/properties v1.18.12 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/mazznoer/csscolorparser v0.2.0 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.3.3 // indirect
	github.com/moby/moby/api v1.56.1 // indirect
	github.com/moby/moby/client v0.6.2 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.7.0 // indirect
	github.com/moby/sys/user v0.4.1 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.3 // indirect
	github.com/nats-io/nkeys v0.4.17 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-sqlite3-wasm/v6 v6.7.35401 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/oklog/ulid/v2 v2.1.2 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pandatix/go-abnf v0.5.5 // indirect
	github.com/parquet-go/parquet-go v0.24.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/pires/go-proxyproto v0.16.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/polarsignals/iceberg-go v0.0.0-20240502213135-2ee70b71e76b // indirect
	github.com/polarsignals/wal v0.0.0-20240619104840-9da940027f9c // indirect
	github.com/power-devops/perfstat v0.0.0-20260916203055-22a1a467d9f0 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.74.0 // indirect
	github.com/prometheus/procfs v0.23.0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	// quic-go v0.64 is the last version compatible with req http3
	github.com/quic-go/quic-go v0.64.0 // indirect
	github.com/redis/go-redis/v9 v9.23.0 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/sergi/go-diff v1.4.5 // indirect
	github.com/shirou/gopsutil/v4 v4.26.6 // indirect
	github.com/shoenig/go-m1cpu v0.2.4 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/stoewer/go-strcase v1.4.0 // indirect
	github.com/thanos-io/objstore v0.0.0-20240818203309-0363dadfdfb1 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.8.7 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.2 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.1 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261118221215-c8830f628b3f // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261118221215-c8830f628b3f // indirect
	gorm.io/driver/mysql v1.6.0 // indirect
	lukechampine.com/blake3 v1.5.1 // indirect
	mvdan.cc/sh/v3 v3.14.1 // indirect
)
