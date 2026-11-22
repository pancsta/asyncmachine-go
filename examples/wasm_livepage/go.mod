module github.com/pancsta/asyncmachine-go/examples/wasm_livepage

go 1.26.5

replace github.com/pancsta/asyncmachine-go => ../../

// https://github.com/open-telemetry/opentelemetry-go/pull/8120
replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48

require (
	github.com/chromedp/chromedp v0.21.0
	github.com/joho/godotenv v1.5.1
	github.com/pancsta/asyncmachine-go v0.19.6
	github.com/pancsta/go-app v0.0.0-20261030132146-e4194936713d
	github.com/pancsta/livepage v0.1.3
	honnef.co/go/js/dom/v2 v2.0.0-20250304181735-b5e52f05e89d
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/K-Phoen/grabana v0.22.2 // indirect
	github.com/K-Phoen/sdk v0.12.4 // indirect
	github.com/alitto/pond/v2 v2.7.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chromedp/cdproto v0.157.8 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/dave/jennifer v1.7.1 // indirect
	github.com/failsafe-go/failsafe-go v0.6.8 // indirect
	github.com/go-json-experiment/json v0.0.0-20260213210345-44df1a37e875 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gosimple/slug v1.13.1 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/ic2hrmk/promtail v0.0.5 // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/orsinium-labs/enum v1.4.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/tmc/go-iroh v0.1.0 // indirect
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
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260330182312-d5a96adf58d8 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
