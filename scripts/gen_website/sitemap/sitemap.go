package sitemap

type Entry struct {
	Url      string
	Path     string
	SkipMenu bool
	Bold     bool
}

var MainMenu = []Entry{
	{Url: "", Path: "README.md"},
	{Url: "machine", Path: "pkg/machine/README.md", Bold: true},
	{"", "", false, false},
	{Url: "examples", Path: "examples/README.md"},
	{Url: "manual", Path: "docs/manual.md"},
	{Url: "cookbook", Path: "docs/cookbook.md", SkipMenu: false},
	{"", "", false, false},
	{Url: "states", Path: "pkg/states/README.md"},
	{Url: "helpers", Path: "pkg/helpers/README.md"},
	{Url: "telemetry", Path: "pkg/telemetry/README.md"},
	{Url: "history", Path: "pkg/history/README.md"},
	{"", "", false, false},
	{Url: "rpc", Path: "pkg/rpc/README.md"},
	{Url: "integrations", Path: "pkg/integrations/README.md"},
	{Url: "node", Path: "pkg/node/README.md"},
	{Url: "pubsub", Path: "pkg/pubsub/README.md"},
	{"", "", false, false},
	{Url: "am-dbg", Path: "tools/cmd/am-dbg/README.md"},
	{Url: "arpc", Path: "tools/cmd/arpc/README.md"},
	{Url: "am-vis", Path: "tools/cmd/am-vis/README.md"},
	{Url: "am-gen", Path: "tools/cmd/am-gen/README.md"},
	{Url: "am-relay", Path: "tools/cmd/am-relay/README.md"},

	// rest (non-menu)
	{Url: "faq", Path: "FAQ.md", SkipMenu: true},
	{Url: "changelog", Path: "CHANGELOG.md", SkipMenu: true},
	{Url: "env-configs", Path: "docs/env-configs.md", SkipMenu: true},

	// examples

	{Url: "tree-state-source",
		Path: "examples/tree_state_source", SkipMenu: true},
	{Url: "benchmark-grpc",
		Path: "examples/benchmark_grpc", SkipMenu: true},
	{Url: "benchmark-libp2p-pubsub",
		Path: "examples/benchmark_libp2p_pubsub", SkipMenu: true},
	{Url: "tools-debugger",
		Path: "tools/debugger", SkipMenu: true},
	{Url: "benchmark-state-source",
		Path: "examples/benchmark_state_source", SkipMenu: true},
	{Url: "wasm",
		Path: "examples/wasm", SkipMenu: true},
	{Url: "wasm-workflow",
		Path: "examples/wasm_workflow", SkipMenu: true},

	// docs

	{Url: "arpc-howto", Path: "pkg/rpc/HOWTO.md", SkipMenu: true},
	{Url: "docs-wasm", Path: "docs/wasm.md", SkipMenu: true},
	{Url: "docs-schema", Path: "docs/schema.md", SkipMenu: true},
	{Url: "diagrams", Path: "docs/diagrams.md", SkipMenu: true},
	{Url: "roadmap", Path: "ROADMAP.md", SkipMenu: true},
	{Url: "breaking-changes", Path: "BREAKING.md", SkipMenu: true},
}
