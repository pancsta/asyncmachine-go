package sitemap

type Entry struct {
	Url  string
	Path     string
	SkipMenu bool
}

var MainMenu = []Entry{
	{Url: "", Path: "README.md"},
	{Url: "examples", Path: "examples/README.md"},
	{Url: "manual", Path: "docs/manual.md"},
	{"", "", false},
	{Url: "machine", Path: "pkg/machine/README.md"},
	{Url: "states", Path: "pkg/states/README.md"},
	{Url: "helpers", Path: "pkg/helpers/README.md"},
	{Url: "telemetry", Path: "pkg/telemetry/README.md"},
	{Url: "history", Path: "pkg/history/README.md"},
	{"", "", false},
	{Url: "rpc", Path: "pkg/rpc/README.md"},
	{Url: "integrations", Path: "pkg/integrations/README.md"},
	{Url: "node", Path: "pkg/node/README.md"},
	{Url: "pubsub", Path: "pkg/pubsub/README.md"},
	{"", "", false},
	{Url: "am-gen", Path: "tools/cmd/am-gen/README.md"},
	{Url: "am-dbg", Path: "tools/cmd/am-dbg/README.md"},
	{Url: "arpc", Path: "tools/cmd/arpc/README.md"},
	{Url: "am-vis", Path: "tools/cmd/am-vis/README.md"},
	{Url: "am-relay", Path: "tools/cmd/am-relay/README.md"},

	// rest (non-menu)
	{Url: "faq", Path: "FAQ.md", SkipMenu: true},
	{Url: "changelog", Path: "CHANGELOG.md", SkipMenu: true},
	{Url: "env-configs", Path: "docs/env-configs.md", SkipMenu: true},
	{Url: "tree-state-source",
		Path: "examples/tree_state_source/README.md", SkipMenu: true},
	{Url: "benchmark-grpc",
		Path: "examples/benchmark_grpc/README.md", SkipMenu: true},
	{Url: "benchmark-libp2p-pubsub",
		Path: "examples/benchmark_libp2p_pubsub/README.md", SkipMenu: true},
	{Url: "tools-debugger",
		Path: "tools/debugger/README.md", SkipMenu: true},
	{Url: "benchmark-state-source",
		Path: "examples/benchmark_state_source/README.md", SkipMenu: true},
	{Url: "arpc-howto", Path: "pkg/rpc/HOWTO.md", SkipMenu: true},
	{Url: "cookbook", Path: "docs/cookbook.md", SkipMenu: true},
	{Url: "diagrams", Path: "docs/diagrams.md", SkipMenu: true},
	{Url: "roadmap", Path: "ROADMAP.md", SkipMenu: true},
	{Url: "breaking-changes", Path: "BREAKING.md", SkipMenu: true},
}
