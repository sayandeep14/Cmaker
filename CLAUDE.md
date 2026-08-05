# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`cmaker` is a Go CLI (Go 1.25+, cobra) that scaffolds, configures, and builds CMake-based C/C++
projects. Its central idea: **`cmaker.yaml` is the source of truth and `CMakeLists.txt` is a
generated artifact** — every `build`/`run` regenerates it from the config, so `CMakeLists.txt` in a
scaffolded project should never be hand-edited (use `cmake_extra:` in the YAML for anything the
generator doesn't model). `README.md` is the user-facing reference for the full flag/field surface;
`ROADMAP.md` tracks known gaps and what's already done.

## Commands

```bash
go build -o cmaker .          # build (version comes from -ldflags -X main.version=...; plain builds say "dev")
go test ./...                 # unit tests — fast, no toolchain needed
go test -tags=integration ./... # integration tests: real scaffold -> cmake -> compiler -> run
go test ./internal/cmake -run TestGenerate -v   # single test
gofmt -l .                    # CI fails if this prints anything
go vet ./...
./scripts/completions.sh      # regenerate completions/ (also run by goreleaser)
goreleaser check && goreleaser build --snapshot --clean   # validate release config locally
```

CI (`.github/workflows/ci.yml`) runs gofmt-check, vet, build, unit tests, then integration tests on
ubuntu + macos. Integration tests self-skip via `requireTool()` when `cmake`/a compiler is absent,
so they're safe to run anywhere.

## Architecture

**Layering rule:** `internal/*` packages are pure — no `os.Exit`, no colored output. All CLI-exit and
presentation behavior lives in `cmd/`. `config.Load` returns errors; `cmd/config_helpers.go` wraps it
in `loadConfigOrExit()`/`syncConfig()` which exit the process. Keep new logic on the right side of
this line, or the TUI (which imports `internal/*`) breaks.

- `cmd/` — one file per cobra subcommand plus shared helpers. `output.go` holds the
  `infof`/`okf`/`warnf`/`errorf`/`debugf` helpers gated on the global `--quiet`/`--verbose`/`--no-color`
  flags; use these rather than `fmt.Println`. `config_helpers.go`'s `syncConfig()` is the standard
  entry for any command that needs a loaded config *and* a fresh `CMakeLists.txt`.
- `internal/config` — the `cmaker.yaml` schema + `Validate`. `Load` (strict, validating) vs `TryLoad`
  (best-effort peek, swallows errors — used by the TUI sidebar and doctor's opt-in checks).
- `internal/cmake` — `Generate()` builds the whole `CMakeLists.txt` as a string from a `config.Config`,
  plus shared cmake-invocation helpers (`PolicyVersionMinFlag`, compiler-override args, compiler
  smoke-testing). Dependencies are emitted as `CPMAddPackage` calls after a CPM.cmake bootstrap block;
  `repo:` containing `://` switches `GITHUB_REPOSITORY` → `GIT_REPOSITORY`.
- `internal/templates` — templates are `go:embed`ed. **Adding a template is dropping a directory into
  `internal/templates/templates/<name>/` with a `meta.yaml` plus source files** — no Go changes needed.
  `meta.yaml` carries description, cpp_version, dependencies, link_libraries, cmake_extra.
- `internal/tui` — bubbletea dashboard, launched by bare `cmaker` when stdout is a TTY (falls back to
  `--help` when piped).

### Two things that look odd but are deliberate

1. **The TUI re-execs this binary as a subprocess** to run build/run/clean/doctor/watch/new rather
   than calling the handlers in-process (`internal/tui/exec.go`). Those handlers call `os.Exit` on
   failure — fine for a one-shot CLI, fatal inside the TUI. The subprocess contains the blast radius
   and lets output stream into the log viewport. Don't "optimize" this into direct calls.
2. **`cmaker <unknown-name>` is not an error** — `rootCmd` uses `cobra.ArbitraryArgs` and falls
   through to `runNamedConfig`, which looks the name up in `cmaker.yaml`'s `configs:` map and
   re-execs the resolved command line (same subprocess pattern). This is why `cmaker add config`
   refuses to shadow a built-in command name.

### Config-driven behaviors worth knowing before touching build/run

- `runner:` (e.g. `crun`) short-circuits the whole CMake path: `run --only` and even a plain
  `cmaker run` just exec `<runner> <file>`. It takes priority over `compiler:`.
- `--only=<file>` compiles ad hoc into `build/.cmaker_scratch/` without touching `CMakeLists.txt`
  or the main target; no dependencies get linked.
- `rust:`/`zig:` are `*RustConfig`/`*ZigConfig` pointers precisely so a nil means *zero* cost —
  no generated CMake, no doctor toolchain check. Preserve that opt-in property.
- `PolicyVersionMinFlag` is passed to every cmaker-issued `cmake -S`; CMake ≥4.0 otherwise rejects
  dependencies whose own `cmake_minimum_required` is below 3.5 (raylib 5.0, etc.).
- `schema_version` exists so a future breaking config change can reject old binaries; bump
  `CurrentSchemaVersion` if you change the config shape incompatibly.
- Known composition limit: `--lang`, `--with-rust`, `--with-zig` only work with `--template=default`.
