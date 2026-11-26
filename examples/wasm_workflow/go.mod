module github.com/pancsta/asyncmachine-go/examples/wasm_workflow

go 1.26.9

// https://github.com/open-telemetry/opentelemetry-go/pull/8120
replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48

replace github.com/pancsta/asyncmachine-go => ../../

tool github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg

tool github.com/pancsta/asyncmachine-go/tools/cmd/am-vis

tool github.com/pancsta/asyncmachine-go/tools/cmd/arpc

require (
	github.com/gookit/goutil v0.9.0
	github.com/joho/godotenv v1.5.1
	github.com/pancsta/asyncmachine-go v0.19.6
)

require (
	github.com/bits-and-blooms/bitset v1.30.0 // indirect
	github.com/carapace-sh/carapace-shlex/v2 v2.1.4 // indirect
	github.com/go-sql-driver/mysql v1.10.2 // indirect
	github.com/goforj/godump v1.8.0 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/icholy/digest v1.2.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.20.5 // indirect
	github.com/mlange-42/ark v0.8.3 // indirect
	github.com/ncruces/go-sqlite3 v0.38.0 // indirect
	github.com/ncruces/go-sqlite3-wasm/v6 v6.7.35401 // indirect
	github.com/ncruces/go-sqlite3/gormlite v0.34.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.64.0 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/sync v0.25.0 // indirect
	gorm.io/datatypes v1.2.7 // indirect
	gorm.io/driver/mysql v1.6.0 // indirect
	gorm.io/gorm v1.31.2 // indirect
)

require (
	charm.land/ssh v0.4.3 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/AlexanderGrooff/mermaid-ascii v0.0.0-20261117200642-e7a2d2f81cdb // indirect
	github.com/PuerkitoBio/goquery v1.14.0 // indirect
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/alexflint/go-arg v1.6.1 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/alitto/pond/v2 v2.7.3 // indirect
	github.com/andybalholm/brotli v1.3.0 // indirect
	github.com/andybalholm/cascadia v1.3.5 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/carapace-sh/carapace v1.18.2 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.8 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/d2lang/dagro v0.2.1 // indirect
	github.com/d2lang/elk-go v0.2.0 // indirect
	github.com/d2lang/mathjax-go v0.1.1-0.20261002175158-94d80186bfd7 // indirect
	github.com/d2lang/rough-go v0.2.0 // indirect
	github.com/d2lang/util-go v0.2.0 // indirect
	github.com/dlclark/regexp2/v2 v2.13.1 // indirect
	github.com/ericpauley/go-quantize v0.0.0-20200331213906-ae555eb2afa4 // indirect
	github.com/failsafe-go/failsafe-go v0.9.8 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/gdamore/tcell/v2 v2.13.10 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/ws v1.5.0 // indirect
	github.com/goforj/httpx v1.1.0
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.34.0 // indirect
	github.com/hinshun/vt10x v0.0.0-20220301184237-5011da428d02 // indirect
	github.com/ic2hrmk/promtail v0.0.5 // indirect
	github.com/imroc/req/v3 v3.61.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mark3labs/mcp-go v1.6.0 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/mazznoer/csscolorparser v0.2.0 // indirect
	github.com/orsinium-labs/enum v1.5.0 // indirect
	github.com/pancsta/cview v1.6.0 // indirect
	github.com/pancsta/d2 v0.8.2-go1.26 // indirect
	github.com/pancsta/go-app v0.0.0-20261030132146-e4194936713d // indirect
	github.com/pancsta/livepage v0.1.3 // indirect
	github.com/pires/go-proxyproto v0.16.0 // indirect
	github.com/reeflective/console v0.5.0 // indirect
	github.com/reeflective/readline v1.3.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sergi/go-diff v1.4.5 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.1.0 // indirect
	github.com/teivah/onecontext v1.3.0 // indirect
	github.com/tmc/go-iroh v0.3.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.8.7 // indirect
	github.com/zyedidia/clipper v0.1.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.2 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk/log v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.1 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/exp v0.0.0-20261120182113-8801fbdfac81 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/term v0.48.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261118221215-c8830f628b3f // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261118221215-c8830f628b3f // indirect
	google.golang.org/grpc v1.85.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mvdan.cc/sh/v3 v3.14.1 // indirect
)
