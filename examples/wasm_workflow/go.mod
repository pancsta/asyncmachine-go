module github.com/pancsta/asyncmachine-go/examples/wasm_workflow

go 1.26

// https://github.com/open-telemetry/opentelemetry-go/pull/8120
replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48

replace github.com/pancsta/asyncmachine-go => ../../

tool github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg

tool github.com/pancsta/asyncmachine-go/tools/cmd/am-vis

tool github.com/pancsta/asyncmachine-go/tools/cmd/arpc

require (
	github.com/gookit/goutil v0.7.4
	github.com/joho/godotenv v1.5.1
	github.com/pancsta/asyncmachine-go v0.18.4
)

require (
	github.com/AlexanderGrooff/mermaid-ascii v0.0.0-20260221123917-b5d02c35decf // indirect
	github.com/PuerkitoBio/goquery v1.10.0 // indirect
	github.com/alecthomas/chroma/v2 v2.14.0 // indirect
	github.com/alexflint/go-arg v1.6.0 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/andybalholm/cascadia v1.3.2 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/carapace-sh/carapace v1.7.1 // indirect
	github.com/carapace-sh/carapace-shlex v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/ssh v0.0.0-20250826160808-ebfa259c7309 // indirect
	github.com/charmbracelet/x/conpty v0.1.0 // indirect
	github.com/charmbracelet/x/errors v0.0.0-20240508181413-e8d8b6e2de86 // indirect
	github.com/charmbracelet/x/termios v0.1.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/coder/websocket v1.8.12 // indirect
	github.com/creack/pty v1.1.21 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dominikbraun/graph v0.23.0 // indirect
	github.com/dop251/goja v0.0.0-20240927123429-241b342198c2 // indirect
	github.com/failsafe-go/failsafe-go v0.6.8 // indirect
	github.com/fsnotify/fsnotify v1.7.1-0.20240403050945-7086bea086b7 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/google/pprof v0.0.0-20250607225305-033d6d78b36a // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/ic2hrmk/promtail v0.0.5 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mazznoer/csscolorparser v0.1.5 // indirect
	github.com/orsinium-labs/enum v1.4.0 // indirect
	github.com/pancsta/cview v1.5.22-0.20260308120046-97d519652c66 // indirect
	github.com/pancsta/tcell-v2 v0.0.1-fork1 // indirect
	github.com/reeflective/console v0.1.25 // indirect
	github.com/reeflective/readline v1.1.3 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/teivah/onecontext v1.3.0 // indirect
	github.com/yuin/goldmark v1.7.4 // indirect
	github.com/zyedidia/clipper v0.1.1 // indirect
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
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/exp v0.0.0-20250606033433-dcc06ee1d476 // indirect
	golang.org/x/image v0.20.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/term v0.41.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mvdan.cc/sh/v3 v3.7.0 // indirect
	oss.terrastruct.com/d2 v0.7.1 // indirect
	oss.terrastruct.com/util-go v0.0.0-20250213174338-243d8661088a // indirect
)
