# goworkmod

Applies a `go.work` to the `go.mod` from the same dir as local `replace`
directives, and reverts them. All edits go through the `go` CLI
(`go work edit`, `go mod edit`, `go mod tidy`).

Useful when a module must build **without** the workspace (CI, `GOWORK=off`,
publishing, `go mod tidy`), but dev builds should use the local checkouts.

## Usage

```bash
cd scripts

# walk up from cwd
GOWORK=off go run ./goworkmod apply
GOWORK=off go run ./goworkmod revert

# or point at a module explicitly (dir or go.work file)
GOWORK=off go run ./goworkmod apply  -work ../pkg/pubsub -tidy
GOWORK=off go run ./goworkmod revert -work ../pkg/pubsub
GOWORK=off go run ./goworkmod apply  -work ../examples/wasm -dry-run
```

`GOWORK=off` is needed for `go run` itself, because `/scripts` is its own
module and is not part of the root workspace. The tool always runs the `go`
CLI with a clean `GOWORK`, so it does not get confused by one either.

## Which files

- Workspace: `-work PATH`, else `$GOWORK`, else the closest `go.work` walking up
  from the current dir.
- Module: **only** `go.mod` in the same dir as that `go.work`. Nested modules
  which have no own `go.work` (eg `/scripts`) are intentionally not supported.

## `apply`

1. `go work edit -json` for `use` dirs and `replace` entries.
2. Every `use` dir (except the workspace dir itself) gets
   `replace <its module path> => <rel path from go.mod>`.
3. Every `replace` from `go.work` is copied verbatim (path and
   `path => path version` forms).
4. Applied with a single `go mod edit -replace=…`, then `go mod edit -fmt`.
5. `-tidy` adds `GOWORK=off go mod tidy` afterwards, so `go.sum` matches the
   replaced modules (same trick as the `tidy-pubsub` task).

Flags: `-work`, `-dry-run`, `-tidy`, `-strict` (fail on `use` dirs without a
`go.mod`), `-v`.

## `revert` (stateless)

Re-derives the keys from the current `go.work` and drops a replace only when:

- it is a **local path** (no version) resolving to the same dir as the
  corresponding `use` dir, or
- it matches a `replace` from `go.work` exactly.

Everything else is left alone and reported:

```
  note: 1 local replace(s) left untouched: example.com/other => ../other
```

Consequences of being stateless (no sidecar file, no markers in `go.mod`):

- `apply` which overwrites a **different** existing replace prints a warning,
  because `revert` can't bring the old value back.
- If you edit or remove a `use` entry between `apply` and `revert`, the stale
  replace is not dropped (`go.work` no longer describes it). It shows up in the
  `note:` line above, plus as a normal `go.mod` diff.

## Example

```bash
$ GOWORK=off go run ./goworkmod apply -work ../pkg/pubsub -dry-run
apply /home/…/pkg/pubsub/go.mod
  work: /home/…/pkg/pubsub/go.work
  + github.com/pancsta/asyncmachine-go => ../..
apply: 1 change(s), dry-run, nothing written
```
