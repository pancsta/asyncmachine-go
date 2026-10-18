module github.com/pancsta/asyncmachine-go/examples/wasm_workflow

go 1.26.5

// https://github.com/open-telemetry/opentelemetry-go/pull/8120
replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48

replace github.com/pancsta/asyncmachine-go => ../../

tool github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg

tool github.com/pancsta/asyncmachine-go/tools/cmd/am-vis

tool github.com/pancsta/asyncmachine-go/tools/cmd/arpc

require (
	github.com/gookit/goutil v0.8.0
	github.com/joho/godotenv v1.5.1
	github.com/pancsta/asyncmachine-go v0.18.4
)

require (
	charm.land/ssh v0.4.3 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/AlexanderGrooff/mermaid-ascii v0.0.0-20260221123917-b5d02c35decf // indirect
	github.com/PuerkitoBio/goquery v1.12.0 // indirect
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/alexflint/go-arg v1.6.0 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/alitto/pond/v2 v2.7.1 // indirect
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/carapace-sh/carapace v1.7.1 // indirect
	github.com/carapace-sh/carapace-shlex v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/d2lang/dagro v0.2.0 // indirect
	github.com/d2lang/elk-go v0.2.0 // indirect
	github.com/d2lang/mathjax-go v0.1.0 // indirect
	github.com/d2lang/rough-go v0.2.0 // indirect
	github.com/d2lang/util-go v0.2.0 // indirect
	github.com/deckarep/golang-set/v2 v2.9.0 // indirect
	github.com/dlclark/regexp2/v2 v2.6.0 // indirect
	github.com/dominikbraun/graph v0.23.0 // indirect
	github.com/dsoprea/go-exif/v3 v3.0.1 // indirect
	github.com/dsoprea/go-logging v0.0.0-20200710184922-b02d349568dd // indirect
	github.com/dsoprea/go-png-image-structure/v2 v2.0.0-20210512210324-29b889a6093d // indirect
	github.com/dsoprea/go-utility/v2 v2.0.0-20221003172846-a3e1774ef349 // indirect
	github.com/failsafe-go/failsafe-go v0.6.8 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/gdamore/tcell/v2 v2.13.9 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-jose/go-jose/v3 v3.0.5 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/golang/geo v0.0.0-20260713102120-857a528af641 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/ic2hrmk/promtail v0.0.5 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mark3labs/mcp-go v0.48.0 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mazznoer/csscolorparser v0.1.8 // indirect
	github.com/mxschmitt/playwright-go v0.6100.0 // indirect
	github.com/orsinium-labs/enum v1.4.0 // indirect
	github.com/pancsta/cview v1.5.23 // indirect
	github.com/pancsta/d2 v0.8.2-0.20260829154946-02b64fd28fde // indirect
	github.com/pires/go-proxyproto v0.12.0 // indirect
	github.com/reeflective/console v0.1.25 // indirect
	github.com/reeflective/readline v1.1.3 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/teivah/onecontext v1.3.0 // indirect
	github.com/tmc/go-iroh v0.1.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.8.5 // indirect
	github.com/zyedidia/clipper v0.1.1 // indirect
	go.mongodb.org/mongo-driver v1.17.9 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.42.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.42.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.19.0 // indirect
	go.opentelemetry.io/otel/log v0.18.0 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	go.opentelemetry.io/otel/sdk v1.42.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.18.0 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/exp v0.0.0-20260727155853-b88d891fe743 // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mvdan.cc/sh/v3 v3.7.0 // indirect
)
