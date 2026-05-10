# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo-25.png" /> /pkg/history

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

## Editor Support

This doc file contains tips on how to handle the bulk of boilerplate needed for **asyncmachine-go**.

## Agent Skills

Using LLMs to generate boilerplate is often faster than using [am-gen](/tools/cmd/am-gen/README.md) or using live
templates. Dirs with `SKILL.md` files are auto-generated from documentation using the command below. Each skill being a
single file can easily be copy-pasted and guarantees the highest compatibility.

```bash
# in repo root
./scripts/dep-taskfile.sh
task gen-docs-skills
cp ./docs/editors/skills/* $DEST
```

## Token Usage

Skills are divided based on token usage, each fully contained within a single `SKILL.md` file.

- `/am-mini` ~1.3k tokens - compacted version
- `/am` ~2.9k tokens - basic boilerplate
- `/am-cook` ~11k tokens - also includes [/docs/cookbook.md](/docs/cookbook.md)
- `/am-full` ~18k tokens - also includes [/docs/manual.md](/docs/manual.md)

## Live Templates

[Live templates](https://www.jetbrains.com/help/idea/using-live-templates.html#live_templates_configure) expand
semantically inside **Goland IDE** for fast tab-completion.

### Handler

- `am`

```go
var _ = $SS$.$STATE$
func ($N$ *$HANDLERS$) $STATE$$TYPE$(e *am.Event) $RET$ {
   $END$
}
```

| Name       | Expression                           | Default value | Skip if defined |
|:-----------|:-------------------------------------|:--------------|:----------------|
| `SS`       | `completeSmart()`                    | ss            |                 |
| `STATE`    | `completeSmart()`                    | `"Foo"`       |                 |
| `TYPE`     | `enum("State","Enter","Exit","End")` | `"State"`     |                 |
| `RET`      | `enum("","bool")`                    |               |                 |
| `HANDLERS` | `completeSmart()`                    |               |                 |
| `N`        | `goSuggestVariableName()`            | `"h"`         |                 |

### Handler With Boilerplate

- `am2`

```go
var _ = $SS$.$STATE$
func ($N$ *$HANDLERS$) $STATE$$TYPE$(e *am.Event) $RET$ {
    ctx := $N$.Mach.NewStateCtx($STATE$)
    mach := $N$.Mach

    mach.Fork(ctx, e, func() {
        $END$
    })
}
```

| Name       | Expression                           | Default value | Skip if defined |
|:-----------|:-------------------------------------|:--------------|:----------------|
| `SS`       | `completeSmart()`                    | ss            |                 |
| `STATE`    | `completeSmart()`                    | `"Foo"`       |                 |
| `TYPE`     | `enum("State","Enter","Exit","End")` | `"State"`     |                 |
| `RET`      | `enum("","bool")`                    |               |                 |
| `HANDLERS` | `completeSmart()`                    |               |                 |
| `N`        | `goSuggestVariableName()`            | `"h"`         |                 |

### Error Setter

- `amerr`

```go
var Err$NAME$ = errors.New("$NAME2$ error")

// AddErr$NAME$ adds [Err$NAME$].
func AddErr$NAME$(
    event *am.Event, mach *am.Machine, err error, args ...am.A,
) am.Result {
    if err == nil {
        return am.Executed
    }
    err = fmt.Errorf("%w: %w", Err$NAME$, err)

    return mach.EvAddErrState(event, ss.Err$NAME$, err, am.OptArgs(args))
}
```

| Name    | Expression           | Default value | Skip if defined |
|:--------|:---------------------|:--------------|:----------------|
| `NAME`  | `capitalize(String)` |               |                 |
| `NAME2` | `camelCase(String)`  | `$NAME$`      |                 |

### Context Expiration Check

- `ctxexp`

```go
if ctx.Err() != nil {
    return // expired
}$END$
```
