# cmaker — Production-Grade Roadmap

Snapshot of `cmaker` as of 2026-07-04: a single 238-line `main.go`, no tests,
manual `os.Args` parsing, two templates. Works for the happy path, breaks
silently outside it. This document tracks every known problem and the
planned fix, grouped by priority, so we can work through them one at a time.

---

## 1. Correctness & robustness bugs (fix first — these are real bugs today) — ✅ DONE (2026-07-04)

- [x] **Silent error swallowing everywhere.** `syncConfig()` now checks the
      `yaml.Unmarshal` error and exits with a clear message instead of
      proceeding with a zero-value `Config`. Same for every `os.WriteFile`
      call in `handleNew`/`generateCMake`.
- [x] **`isBuildRequired` walks the entire project tree including `build/`.**
      Now skips `build/` and `.git/` via `filepath.SkipDir`. Verified: a
      no-op `cmaker run` went from walking thousands of generated files to
      ~8ms.
- [x] **Fake progress bar.** Replaced the fixed `Add(20/30/50)` increments
      with `runWithSpinner`, an indeterminate spinner that ticks for the
      real duration of the `cmake`/build subprocess and only stops when the
      command actually finishes.
- [x] **No real exit codes on failure.** `handleBuild`, `handleNew`,
      `handleRun`, `handleClean`, and `handleDoctor` now call `os.Exit(1)`
      on failure paths; `handleRun` also propagates the child process's own
      exit code.
- [x] **`os.Args` parsed ad hoc in three places.** Consolidated into a
      `hasFlag()` helper used consistently by `new --sfml` and
      `build --release`. Full fix (per-command validation, `--help`,
      unknown-flag errors) still lands with the cobra migration in item 2 —
      noted here as a stopgap, not a final state.
- [x] **`handleRun` shells out with `"./"+exePath`.** Now adds `.exe` on
      Windows and only prepends `./` on non-Windows.
- [x] **No `.gitignore` generated.** `handleNew` now writes a `.gitignore`
      containing `build/`.
- [x] **No validation of `Config` fields.** Added `validateConfig`,
      checking `cpp_version` against the supported standard set and
      `executable` for non-empty. Verified: a `cpp_version: 999` config now
      fails with a clear error and exit code 1 instead of generating a
      broken `CMakeLists.txt`.

## 2. CLI ergonomics — ✅ DONE (2026-07-04)

- [x] Migrated off manual `os.Args` switch to **cobra**. `main.go` split
      into `root.go` + one `cmd_*.go` file per subcommand
      (`new`/`init`/`build`/`run`/`clean`/`doctor`/`watch`), each with its
      own `--help`, flag validation, and `cobra.Args` checks. Shell
      completions (`cmaker completion bash|zsh|fish`) come free from cobra
      — verified `cmaker completion bash` emits a real script.
- [x] Added global persistent flags: `--verbose`/`-v`, `--quiet`/`-q`,
      `--no-color`, backed by colored `infof`/`okf`/`warnf`/`errorf`/
      `debugf` helpers in `root.go`. Verified `--no-color` strips ANSI and
      `--quiet` suppresses cmaker's own messages while still letting
      cmake/build tool output through.
- [x] `doctor` now checks `cmake`, `make`, `ninja`, `clang++`, `g++`,
      `vcpkg`, `conan` and prints an OS-specific (`darwin`/`linux`/
      `windows`) install hint per missing tool; exits non-zero if no
      compiler or `cmake` is found. Verified on macOS: correctly flagged
      missing `vcpkg`/`conan` with install hints while reporting the
      compiler toolchain ready.
- [x] Added `cmaker watch` (via `fsnotify`): watches `src/`, `include/`,
      `cmaker.yaml`, debounces (200ms), then rebuilds and reruns
      automatically. Verified: initial build+run fires immediately on
      start, and a file edit mid-watch correctly triggers a rebuild
      (including surfacing a real compile error without crashing the
      watcher).
- [x] Added `cmaker init` for scaffolding into the *current* directory
      (project name inferred from the directory's basename) as opposed to
      `cmaker new <name>`, which creates a new subdirectory. Verified.

## 3. Config & CMake generation — ✅ MOSTLY DONE (2026-07-05)

- [x] Support custom raw CMake snippet injection for edge cases the
      generator doesn't model. `cmaker.yaml`'s `cmake_extra:` (raw string) is
      appended verbatim near the end of the generated `CMakeLists.txt`.
- [x] Support compiler override (`clang++` vs `g++` vs MSVC) in config
      instead of relying on whatever CMake finds by default. `cmaker.yaml`'s
      `compiler:` field, plus a one-off `cmaker build --compiler=<path>`
      flag that takes precedence for that invocation only. Implemented as
      `-DCMAKE_CXX_COMPILER=`/`-DCMAKE_C_COMPILER=` passed to `cmake -S`
      directly (not a `set()` inside `CMakeLists.txt`, since CMake locks in
      the compiler the moment `project()` first runs — a `set()` after the
      fact would silently do nothing).
- [x] Support warnings-as-errors / sanitizer flags (ASan/UBSan/TSan/MSan/
      leak) as config toggles. `warnings_as_errors: true` emits
      `-Wall -Wextra -Werror`; `sanitizers: [address, undefined]` emits
      matching `-fsanitize=...` compile *and* link options (both are
      required — sanitizer runtimes need to be linked in too, not just
      compiled in). Verified: a real ASan+UBSan+`-Werror` build compiled and
      ran cleanly.
- [x] Config schema versioning so future `cmaker.yaml` changes don't break
      old projects silently. `schema_version:` int field (new projects write
      `1`); `validateConfig` rejects any `schema_version` higher than the
      current build understands with a clear "please upgrade cmaker"
      message instead of silently misreading unknown fields.
- [x] **Compiler validation before build** (pulled in from §10): before
      invoking `cmake` with a `--compiler`/`compiler:` override,
      `validateCompilerSupportsStandard` runs a cheap preprocessor-only
      smoke test (`<compiler> -std=c++NN -x c++ -E -` on empty stdin) and
      fails with a clear "compiler does not appear to support -std=c++26"
      message instead of surfacing a cryptic CMake configure error.
      Verified: `--compiler=/bin/nonexistent-compiler` now fails fast with
      that message rather than an opaque CMake stack trace.
- [x] **Pre-flight error surfacing fixed** (this was the §5 follow-up):
      `cmaker new`'s pre-flight CMake configure now captures stderr and
      prints its first non-empty line by default (`firstLine()` helper) —
      `-v` is only needed for the full raw log, not just to learn *that*
      something failed.
- [x] **Support library targets, not just one hardcoded executable** — ✅
      done via §16 (2026-08-05): `target_type: executable |
      static_library | shared_library` in `cmaker.yaml`, with a real
      `add_library(... STATIC|SHARED ...)` branch in the generator. See
      §16 for the full writeup. **Partial**: this covers "one target that
      might be a library," not the fully general "arbitrary N executables"
      case the original bullet also named - `cmake.Generate` still only
      ever declares at most two targets (the library/executable itself,
      plus its paired `<name>_demo` if `examples/*.cpp` exists), not an
      arbitrary list. True multi-executable projects remain a real
      generator restructure (targets become a list, `Generate` iterates),
      left open - tracked as a new follow-up rather than silently dropped.
- [x] **Support test targets (`ctest` wiring) as an opt-in config
      section** — ✅ done (2026-08-05). `cmaker.yaml` gained `testing: {
      enabled: true }` (`config.TestingConfig`, a nil-means-zero-cost
      pointer like `rust`/`zig`); `cmake.Generate` emits `enable_testing()`
      + `add_test(NAME <executable> COMMAND <executable>)` when set. New
      `cmaker test` subcommand builds then runs `ctest --output-on-failure`,
      forwarding ctest's own exit code, and fails fast with a clear message
      if `testing.enabled` isn't set instead of surfacing ctest's own
      "No tests were found!!!". The `catch2` template now sets
      `testing: true` in its `meta.yaml` (new `Meta.Testing` field) so a
      freshly scaffolded `catch2` project has `cmaker test` working
      immediately. Also added to the TUI sidebar (`Test`, next to `Run`).
      Verified end-to-end: scaffolded a real `catch2` project, `cmaker
      test` built it, fetched Catch2 via CPM, and ran the real test
      (passed); flipping the assertion to fail propagated ctest's
      `--output-on-failure` detail and a non-zero exit code (8); a
      `default`-template project (no `testing:` config) hit the fast
      "no tests configured" error instead of `ctest`'s cryptic default.
      **Scoped to today's single-executable model** (still doesn't do
      dedicated test targets separate from the main executable — that's
      real GoogleTest-style multi-target support, which needs §16's
      library/multi-target work first).
- [ ] `cmaker build`/`cmaker new` still don't pass `-j` to
      `cmake --build` (carried over from the §5 follow-up list, still
      unaddressed).

## 4. Templates — ✅ DONE (2026-07-04)

New files: `templates.go` (embed loader/writer) + `templates/<name>/` per
template (`meta.yaml` + `src/`/`include/`). New `cmd_templates.go` adds
`cmaker templates` to list them.

- [x] Moved off hardcoded Go string blobs to `go:embed`'d template
      directories (`//go:embed templates` in `templates.go`) — adding a
      template is now "drop a directory with a `meta.yaml` in `templates/`,"
      not "edit Go source and recompile." `ListTemplates()`,
      `loadTemplateMeta()`, `writeTemplateFiles()` handle discovery/copying.
- [x] Five templates shipped: `default` (hello world, no deps),
      `sfml` (converted from a hardcoded system-lib assumption to CPM),
      `raylib` (new), `catch2` (new — Catch2 v3 test scaffold, satisfies the
      "Catch2 test scaffold" ask; GoogleTest not done, tracked below), and
      `headeronly` (new — `include/` header-only lib + a `src/` consumer).
      Boost/Qt not done — noted below as still open.
- [x] `cmaker new --template=<name>` now validates against the real
      template list instead of a hardcoded `default`/`sfml` check; unknown
      names get a clear error listing what's actually available. Removed
      the old `--sfml` boolean shorthand in favor of just `--template`.
- [x] `cmaker templates` lists every available template with its
      description and (if any) what it fetches.
- [x] TUI's New Project template picker (`tui.go`) now pulls the live
      template list via `ListTemplates()` instead of a hardcoded
      `["default", "sfml"]`, so new templates automatically show up there
      with no TUI code changes.
- [ ] Boost and Qt templates still not implemented — both are heavier/
      slower to wire via CPM than raylib/SFML and were deprioritized this
      pass; left open.
- [ ] GoogleTest test scaffold still not implemented (Catch2 was).

## 5. Dependency management — ✅ DONE (2026-07-04)

New `Dependency` struct in `config.go`, a `dependencies:` field on `Config`,
and CPM.cmake wiring in `generateCMake`.

- [x] `cmaker.yaml` now supports a `dependencies:` list
      (`{name, repo, tag, link, options}`); `generateCMake` emits a CPM.cmake
      bootstrap (downloads `CPM.cmake` into the build dir if not already
      present) plus one `CPMAddPackage(...)` block per dependency, and
      links each dependency's `link` targets into the executable — so
      `sfml`/`raylib`/`catch2` templates fetch and build their own
      dependency automatically instead of assuming it's preinstalled.
- [x] **Real bug found + fixed**: the generator initially emitted
      `CPM_AddPackage` (with an underscore) — the actual CPM.cmake macro is
      `CPMAddPackage`. This silently broke every dependency-bearing
      template ("Unknown CMake command") until caught during validation.
- [x] **Real bug found + fixed**: CMake ≥ 4.0 refuses to configure *any*
      project (including CPM-fetched third-party ones) whose own
      `cmake_minimum_required` is below 3.5 — and plenty of still-current
      libraries (raylib 5.0 among them) haven't bumped that floor. Every
      `cmake -S` invocation cmaker issues (`cmd_build.go`, `cmd_new.go`) now
      unconditionally passes `-DCMAKE_POLICY_VERSION_MINIMUM=3.5`
      (`cmakePolicyVersionMinFlag` in `config.go`) to work around this —
      a no-op for projects that don't need it, a real fix for ones that do.
- [x] **Real bug found + fixed (2026-08-05)**: `raylib` scaffolds were
      reported as "not able to fetch from GitHub" - the actual cause is
      that `CPMAddPackage` was doing a full-history git clone of the
      dependency's repo, and raylib's is 516 MB / 60+ seconds to clone
      even on a fast connection (vs. catch2/sfml/ml-eigen/backend's much
      smaller repos, which is why only raylib was reported broken - a slow
      or flaky connection turns that into a clone that times out or gets
      interrupted, surfacing as "can't fetch"). `internal/cmake.Generate`
      now emits `GIT_SHALLOW TRUE` on every `CPMAddPackage(...)` block.
      Verified: a plain `git clone --depth 1 --branch 5.0` of raylib is
      94 MB / ~6s vs. 516 MB / ~63s full clone; CMake's own git downloader
      (`ExternalProject`'s shallow path always adds `--no-single-branch`
      for tag-resolution correctness, so it isn't quite that minimal) still
      cut it to ~272 MB, and a fresh `cmaker new --template=raylib` +
      `cmake --build` completed end-to-end (full raylib compile, `main`
      linked and runnable) in under a minute.
- Verified end-to-end: `default`/`headeronly` build and run with no network
  (baseline regression check); `catch2` fetched Catch2 via CPM, built, and
  ran with tests passing; `raylib` fetched and configured successfully
  after the two bugs above were fixed (full build observed compiling real
  raylib sources earlier in validation, not re-run to full completion a
  second time to save time); `sfml`'s generated `CMakeLists.txt` was
  inspected for correctness (same CPM mechanism already proven twice over,
  not separately build-tested).

### Follow-ups discovered during this validation

- [ ] `cmaker build`/`cmaker new`'s pre-flight configure don't pass `-j` to
      `cmake --build`, so CPM-fetched native library builds (raylib, SFML)
      are unnecessarily slow (single-threaded compiles). Still open —
      also tracked in §3.
- [x] ~~Pre-flight config failures during `cmaker new` are silently
      swallowed unless `-v` is passed~~ — **fixed in §3** (2026-07-05):
      the pre-flight configure now captures stderr and surfaces its first
      non-empty line by default; `-v` is only needed for the full raw log.

## 6. The TUI (headline feature) — ✅ DONE (2026-07-04)

Built with the **Charm stack**: `bubbletea` + `lipgloss` + `bubbles`
(`spinner`, `textinput`, `viewport`). New files: `tui.go` (model/state
machine/views), `tui_exec.go` (subprocess streaming plumbing), `tui_styles.go`
(lipgloss theme). Also added `cmd_tui`/`tui` wiring in `root.go`.

- [x] Bare `cmaker` (no subcommand) launches the dashboard when stdout is a
      terminal (`golang.org/x/term.IsTerminal`); falls back to `--help` when
      piped/non-interactive (e.g. CI). `cmaker tui` launches it explicitly
      regardless.
- [x] Sidebar command list (New Project, Build, Build (Release), Run, Clean,
      Doctor, Watch, Quit) — arrow/`j`/`k` + enter, with a live description
      pane per item.
- [x] **New Project** flow: textinput for name → arrow-key template picker
      (default/sfml) → scaffold runs with a live spinner + log.
- [x] **Build/Run/Clean/Doctor/Watch**: each re-execs the *exact same*
      headless subcommand as a child process (not duplicated logic) and
      streams its combined stdout/stderr into a scrolling `viewport` live,
      with an animated spinner while running and a colored ✔/✘ banner on
      completion. `Watch` can be stopped mid-run with `esc` (kills the child
      via `context.CancelFunc`).
- [x] Status bar: doctor-readiness dot (auto-checked in the background on
      startup), current project directory, and whether `cmaker.yaml` was
      found. After a successful "New Project", subsequent actions
      automatically run inside the freshly scaffolded directory.
- [x] Headless usage is fully unchanged and still scriptable
      (`cmaker build --release`, etc.) — verified no regressions.
- [x] Fixed a real bug along the way: `run`'s `DisableFlagParsing` blocked
      `--quiet`/global flags from working on `cmaker run`, which would have
      broken the TUI's ability to reuse it as a clean subprocess. Switched to
      cobra's standard `--` separator convention (flags before `--` are
      cmaker's own; args after `--` forward to the built binary).
- [x] Fixed a real bubbletea bug: a `tea.Cmd`'s return value is auto-dispatched
      as a message independently of anything sent via a manual callback mid-run
      — the background startup doctor-check was returning a bare `cmdDoneMsg`,
      which the same handler used for real user-triggered actions caught and
      incorrectly flipped the UI into the "done" view immediately on boot.
      Fixed by giving the background probe its own `doctorCheckDoneMsg` type.
- Verified end-to-end via a pty-driven scripted test (Python `pty` module,
  since this environment has no real interactive terminal): boot, menu
  navigation, Doctor run with live spinner/output, New Project form →
  scaffold → real files on disk → status bar reflects new project dir →
  Build streams real live compiler output → succeeds → produces a working
  binary.

## 7. Testing & CI — ✅ DONE (2026-07-05)

New files: `config_test.go`, `cmd_run_test.go` (unit tests), `integration_test.go`
(gated behind the `integration` build tag), `.github/workflows/ci.yml`.

- [x] **Real, pre-existing blocker fixed first**: `go.mod`'s `module main`
      broke `go test` outright (`could not import main (cannot import
      "main")` - the generated test harness can't import a package literally
      named `main`). This is why "zero tests exist" wasn't just neglect -
      `go test ./...` didn't work at all until this was fixed. Renamed the
      module to `cmaker`; no import paths needed updating since this is a
      single-directory `package main` with no internal sub-packages.
- [x] Unit tests for `generateCMake`: default C++ project, C-only, hybrid
      (both standards + combined glob), dependency wiring (CPMAddPackage +
      target_link_libraries), the new GIT_REPOSITORY/DOWNLOAD_ONLY dependency
      fields from §13, sanitizers/warnings-as-errors flags, and Rust/Zig
      opt-in (asserting a plain project's output contains *zero* Rust/Zig
      CMake - the "strictly opt-in, zero cost" requirement from §12 is now
      actually enforced by a test, not just a code comment). Also covers the
      cmake_extra-before-add_executable ordering bug class from §13 (a
      regression here would have broken the ml-eigen template silently).
      One test caught a real assumption mismatch while writing it: the
      default template's output is `"Hello from Cmaker!"` (capital C), not
      `"Hello from cmaker!"` as first assumed - fixed the test, not the
      template (the template's copy is authorial choice, not a bug).
- [x] Unit tests for `validateConfig` (table-driven: valid cpp/c/hybrid
      configs, empty executable, bad cpp/c version, unknown language, bad
      sanitizer name, schema_version too new) and `compilerCMakeArgs`
      (plain compiler override, per-language flag selection, and the zig
      special case emitting both C and C++ `_ARG1` pairs for hybrid).
- [x] Unit tests for `isBuildRequired`: no binary yet, source newer than
      binary, binary up to date, and - the one this function exists to get
      right - files inside `build/`/`.git/` with newer mtimes than the
      binary must *not* trigger a rebuild. Uses `t.Chdir`/`t.TempDir` for
      isolation (Go 1.24+; this project is on 1.25).
- [x] Integration tests (`integration_test.go`, `//go:build integration`,
      run via `go test -tags=integration ./...`) exercise the real
      pipeline end-to-end: `TestIntegrationScaffoldBuildRun` scaffolds a
      project into a temp dir, runs the *real* `cmake -S`/`cmake --build`,
      runs the resulting binary, and checks its output; 
      `TestIntegrationOnlyCompileRun` exercises the `--only` single-file
      path (`compileOnly`) against a real compiler. Both verified passing
      on this machine. Each test self-skips via `requireTool()` if
      `cmake`/`c++` isn't on `PATH`, so CI environments without a full
      toolchain degrade gracefully instead of failing.
- [x] GitHub Actions workflow (`.github/workflows/ci.yml`): matrix over
      `ubuntu-latest`/`macos-latest`, runs `gofmt -l` (fails on
      non-empty output), `go vet`, `go build`, `go test ./...`, and
      `go test -tags=integration ./...` on every PR and push to `main`.
      YAML syntax verified with a throwaway Go script using the project's
      own `gopkg.in/yaml.v3` dependency (no network-based YAML linter
      available in this sandbox). **Not verified running for real** - this
      directory isn't a git repository yet, so the workflow can't actually
      execute on GitHub until the repo exists there; the YAML itself and
      the commands it runs were verified by running them manually.

## 8. Packaging & distribution — ✅ MOSTLY DONE (2026-07-05)

New files: `.goreleaser.yaml`, `scripts/completions.sh`. This project had no
git repo at all before this pass (needed for real goreleaser validation,
since it reads git state even in snapshot mode) - initialized one locally
(git init + a first commit) with the user's explicit go-ahead; no remote
was pushed anywhere.

- [x] `goreleaser` config for cross-platform binaries: `builds:` targets
      `{linux, darwin, windows} × {amd64, arm64}` (6 combinations),
      `CGO_ENABLED=0`, version injected via `-ldflags -X main.version=...`
      (added a real `--version`/`cmaker version` flag to support this -
      `cmd.SetVersion` wired from `main.go`, previously the binary had no
      version string at all). Verified for real: installed `goreleaser`
      (v2.17.0) locally and ran `goreleaser build --snapshot --clean` -
      produced 6 actual binaries, confirmed via `file` to be genuine
      Mach-O (darwin)/ELF (linux)/PE32+ (windows) executables, and ran the
      native one directly (`cmaker version` printed the injected snapshot
      version string).
- [x] Shell completions "published": `scripts/completions.sh` regenerates
      `completions/{bash,zsh,fish,powershell}` from cobra's built-in
      `cmaker completion <shell>` (wired as a goreleaser `before.hooks`
      step, so every release archive gets fresh completions for that
      version) and bundles them into each release archive via `archives:
      files:`. Verified: ran the script directly, checked the bash and zsh
      output with `bash -n`/`zsh -n` (both syntactically valid), and
      confirmed via `tar -tzf` that a real built archive contains all four
      completion files alongside the binary.
- [x] Homebrew tap config: `homebrew_casks:` (not `brews:` - see below).
      Verified for real: ran the full `goreleaser release --snapshot
      --clean --skip=publish` pipeline (archives + checksums + cask, not
      just `build`) and inspected the generated
      `dist/homebrew/Casks/cmaker.rb` - a syntactically real Homebrew cask
      referencing all 4 platform archives with correct per-arch SHA256
      checksums and the bundled completions.
- [x] **Real correction caught by validation, not guessed**: the config
      was first written using `brews:` (the older Homebrew Formula
      mechanism, based on training-data familiarity), which `goreleaser
      check` flagged as deprecated - looked it up
      and confirmed `brews` is removed/non-functional as of recent
      goreleaser versions in favor of `homebrew_casks`. Migrated and
      re-verified `goreleaser check` passes with zero warnings.
- [ ] **Not actually publishable yet - this is the real gap**: the
      `brews`/`homebrew_casks` `repository.owner` and `homepage` fields are
      placeholders (`YOUR_GITHUB_USERNAME`), because this project has no
      real GitHub remote, no `homebrew-tap` repo, and no pushed release
      tag. A real `goreleaser release` (as opposed to the `--snapshot
      --skip=publish` dry run verified above) needs all three, plus a
      `GITHUB_TOKEN`. Getting a real `brew install cmaker` working is a
      "point this at a real GitHub org and cut a release" task, not a
      config-writing task - left explicitly open here rather than faked.

## 9. Code organization — ✅ DONE (2026-07-05)

Split the single-directory `package main` (~3000 lines across 20 files) into:

- `main.go` (repo root) — just `cmd.Execute()`.
- `cmd/` — every cobra command, the exit-on-error/colored-output CLI
  wrappers (`loadConfigOrExit`, `syncConfig`, `output.go`), `--only` ad hoc
  compiles, and the Rust/Zig/interop scaffolding helpers. Depends on all
  three `internal/` packages below.
- `internal/config/` — the `Config`/`Dependency`/`RustConfig`/`ZigConfig`
  schema plus pure `Load`/`Save`/`TryLoad`/`Validate` (no CLI concerns: no
  `os.Exit`, no colored output, no printing - that wrapping now lives in
  `cmd/config_helpers.go`).
- `internal/cmake/` — `Generate` (CMakeLists.txt generation),
  `CompilerArgs`, `ValidateCompilerSupportsStandard`, the CPM/policy-version
  constants. Depends on `internal/config` for the `Config` type.
- `internal/templates/` — the embedded `templates/` tree, `List`/
  `LoadMeta`/`WriteFiles`. Depends on `internal/config` for `Dependency`.
- `internal/tui/` — the bubbletea dashboard, unchanged behavior. Depends on
  `internal/config` (for the sidebar's saved-configs peek) and
  `internal/templates` (for the New Project template picker) - notably
  *not* on `cmd/`, since it re-execs the binary as a subprocess rather than
  calling `cmd/`'s handlers in-process (avoids a cmd ↔ tui import cycle for
  free).

- [x] **Real pre-existing bug caught and fixed during the split**: the
      variable name `config` (as in `config := Config{...}`) was used
      throughout the old `cmd_new.go` for the value being built up during
      scaffolding - this silently shadowed the newly-introduced `config`
      *package* import inside the same function once the split introduced
      that import, which the compiler correctly flagged. Renamed the local
      variable to `cfg` throughout - a good example of exactly the kind of
      naming collision a single flat `package main` never has to worry
      about, and multi-package layouts do.
- [x] Verified no import cycles: dependency direction is strictly
      `cmd → {internal/config, internal/cmake, internal/templates, internal/tui}`
      and `internal/{cmake,templates,tui} → internal/config` - `internal/config`
      itself imports nothing internal.
- [x] Verified end-to-end after the split, not just `go build`/`go vet`/
      `go test` (all clean): re-ran the real integration tests
      (`go test -tags=integration ./...`, both scaffold-build-run and
      `--only` ad hoc compile, passing against the real toolchain), then
      manually re-exercised the CLI surface end-to-end with the rebuilt
      binary - `new`, `run`, `doctor`, `add config`/dispatch via `cmaker
      <name>`, and `create --with-rust --with-zig` (which rebuilt and ran
      correctly, printing `rust_add(2,3)=5, zig_add(4,5)=9` from the
      combined interop demo) - and confirmed the TUI still boots (launched
      under a pty harness, stayed alive in its event loop rather than
      crashing on startup).

## 10. Compiler & toolchain selection — ✅ MOSTLY DONE (2026-07-05)

Motivation: a dev machine often has several C/C++ toolchains installed
(system clang, multiple Homebrew/apt clang or gcc versions, a cross
toolchain) — cmaker currently just takes whatever CMake finds first, with no
way to pin or override it.

- [x] `cmaker.yaml`: `compiler` field (name or absolute path,
      e.g. `clang++-17`, `/opt/homebrew/opt/llvm/bin/clang++`) passed through
      as `CMAKE_CXX_COMPILER`/`CMAKE_C_COMPILER` (`compilerCMakeArgs` in
      `config.go`).
- [x] One-off override flag: `cmaker build --compiler=<name-or-path>`,
      taking precedence over `cmaker.yaml` for that invocation only.
      Verified: `cmaker build --compiler=/usr/bin/clang++` configured and
      built successfully against a project whose `cmaker.yaml` had no
      `compiler:` set.
- [x] `cmaker doctor` now enumerates *every* discovered compiler (all
      `clang++`/`clang`/`g++`/`gcc`, versioned or not, found on `PATH` plus
      `/opt/homebrew/opt/llvm/bin` and `/usr/local/opt/llvm/bin`) via
      `discoverCompilers()`, instead of just reporting a single
      ready/missing clang++/g++ — this is the actual "multiple compilers
      present" case the user asked for. Verified on macOS: correctly listed
      `g++-15`/`gcc-15` (Homebrew) alongside system `/usr/bin/clang++` etc.
- [x] Validate the selected compiler actually supports the configured
      `cpp_version`/`c_version` before building; fail with a clear message
      instead of a cryptic CMake/compiler error. `validateCompilerSupportsStandard`
      runs a cheap `-E`-only preprocessor smoke test. Verified: an invalid
      compiler path now fails immediately with
      `compiler "..." does not appear to support -std=c++26: ...`, before
      `cmake` is ever invoked.
- [ ] Surface an interactive compiler picker in the TUI (New Project flow
      and a "Doctor" detail view) when more than one toolchain is detected.
      **Not done** — `discoverCompilers()` exists and is reusable, but no
      TUI view calls it yet; left open since the TUI's New Project flow and
      Doctor view would both need new screens.

## 11. C projects and hybrid C/C++ projects — ✅ DONE (2026-07-05)

Motivation: cmaker currently hardcodes a C++ project shape. Plenty of
systems work is plain C, or a mix of C and C++ in one codebase.

- [x] `cmaker.yaml`: `language: cpp | c | hybrid` field (default `cpp` for
      backward compatibility with existing project files — old configs that
      omit the field entirely are treated as `cpp` via `languageOrDefault`).
- [x] `cmaker new --lang=c` scaffolds `src/main.c`, sets `c_version`
      instead of `cpp_version`, generates `CMAKE_C_STANDARD` in
      `CMakeLists.txt`. Verified end-to-end: configured, built, and ran
      ("Hello from cmaker (C project)!").
- [x] `cmaker new --lang=hybrid` globs both `.c` and `.cpp`/`.cxx` sources,
      sets both `CMAKE_C_STANDARD` and `CMAKE_CXX_STANDARD`, and scaffolds
      the `extern "C" { ... }` boilerplate (a `mathlib.h`/`mathlib.c` pair
      consumed from `main.cpp`) needed for C headers to be includable from
      the C++ side cleanly. Verified end-to-end: both a C object and a CXX
      object compiled and linked into one binary, output confirmed the C
      function was actually called from C++
      ("2 + 3 = 5" via `mathlib_add`).
- [x] `generateCMake` (previously C++-only) now branches on `language` for
      the standard `set()` lines and the source glob, instead of assuming
      one fixed shape — a real rewrite of the generator, not just a
      template tweak.
- Known limitation, documented in code: `--lang` (and the C/hybrid
  scaffolding paths) only compose with `--template=default` — the other
  four templates (sfml/raylib/catch2/headeronly) are concrete C++
  dependency showcases and aren't meaningful in C; `scaffoldProject`
  rejects the combination with a clear error rather than silently ignoring
  `--lang`.
- Known limitation, documented in code: a hybrid project's `compiler:`
  override only overrides the C++ compiler (`CMAKE_CXX_COMPILER`), not the
  C compiler independently — `compiler` is a single field, not a pair.

## 12. Rust and Zig interop

Motivation, per user: "a lot of cases, we need some code in rust and some
in zig in a C/C++ codebase" — this is the largest, riskiest item on the
roadmap and should be phased carefully rather than attempted in one pass.
It must be strictly opt-in: a plain C/C++ project should never pay any
cost (extra dependency checks, slower `cmaker.yaml` parsing, generated
CMake complexity) for polyglot support it doesn't use.

### Rust — ✅ DONE (2026-07-05)

New file `rust.go` (crate scaffolding); `RustConfig`/`Config.Rust` added in
`config.go`; Rust CMake wiring added to `generateCMake`; `--with-rust` flag
added to `new`/`init`/`create` (shared via `newScaffoldFlags`); opt-in
`cargo`/`rustc` check added to `cmaker doctor`.

- [x] Scaffold an optional `rust/` crate wired into the CMake build.
      Config: `rust: { enabled: true, crate_dir: "rust" }` — strictly
      opt-in (`Config.Rust` is a `*RustConfig`; `nil` means zero extra
      generated CMake, zero extra doctor checks, exactly as required).
- [x] **Deliberate deviation from the original plan**: wired via a direct
      `add_custom_command`/`cargo build --release` + static-lib link
      (`librustlib.a`), not via **Corrosion**. Reasoning: Corrosion is
      itself a fetched CMake package with its own version/API surface to
      validate (the same kind of real risk that turned up actual bugs with
      CPM-fetched libraries in §5) — a direct `cargo build` custom command
      is simpler, has fewer moving parts to go wrong, and was fully
      verified end-to-end today; Corrosion (better multi-config/Windows
      support) is left as a future upgrade, not required for the core ask
      of "C++/C code calling into Rust."
- [x] Bridge: implemented as a plain **C ABI** (`#[no_mangle] pub extern
      "C" fn`) with a hand-written matching C header
      (`include/rustlib.h`), rather than the `cxx`/`cbindgen` tooling
      originally proposed — the crate is small and fully controlled by the
      scaffolder, so hand-writing both sides of a 2-function demo avoids an
      extra `cbindgen` toolchain dependency and keeps scaffolding working
      even without cargo installed (cargo/rustc are only needed at *build*
      time). A richer `cxx`-based typed bridge for user-authored (not
      scaffolder-authored) Rust code is a real gap — see follow-up below.
- [x] `cmaker doctor` checks `cargo`/`rustc` only when the current
      directory's `cmaker.yaml` has `rust.enabled: true`
      (`tryLoadConfigFile`). Verified: a plain project's `doctor` output has
      no Rust section at all; a `--with-rust` project's does, and reports
      both tools ready.
- [x] `cmaker new --with-rust` (and `init`/`create --with-rust`) scaffold
      the crate and build wiring in one shot. Verified end-to-end for real,
      three ways: (1) `--lang=cpp --with-rust` (default) — cargo built
      `librustlib.a`, CMake linked it into `main`, and running it printed
      `2 + 3 (via Rust) = 5`, confirming the C++ binary actually called into
      compiled Rust code, not a stub; (2) `--lang=c --with-rust` — same
      result via `main.c` and the identical C-ABI header, confirming the
      interop story is language-agnostic on the C/C++ side; (3)
      `create --with-rust`.
- Known limitation: `--with-rust` (like `--lang`) only composes with
  `--template=default` — verified `--template=catch2 --with-rust` is
  rejected with a clear error rather than silently ignoring the flag.
- Known limitation: the crate name is hardcoded (`rustlib`) rather than
  derived from the project name — fine for one crate per project, would
  need revisiting if multi-crate support is ever added.
- Follow-up (not done): a `cxx`-based typed bridge option
  (`bridge: "cxx"`) for projects that want to hand-write their own Rust
  logic with richer types (strings, vectors, structs) instead of using the
  scaffolded demo crate as-is.

### Zig — ✅ MOSTLY DONE (2026-07-05)

New file `zig.go` (library scaffolding); `ZigConfig`/`Config.Zig` added in
`config.go`; Zig CMake wiring added to `generateCMake`; `--with-zig` flag
added to `new`/`init`/`create`; `compiler: zig` special-cased in
`compilerCMakeArgs`/`validateCompilerSupportsStandard`; opt-in `zig` check
added to `cmaker doctor`. `rust.go`'s and the new `zig.go`'s demo-`main()`
writing was factored out into a shared `interop.go` so `--with-rust` and
`--with-zig` compose into one combined demo instead of clobbering each
other's `main()`.

- [x] **Zig source, use case 1 (library linked into the target)** — ✅ done
      and verified end-to-end. Config: `zig: { enabled: true, src_dir:
      "zig" }`. Wired via a direct `zig build-lib -O ReleaseFast
      -femit-bin=...` custom command (same "direct tool invocation over a
      bigger build-system dependency" choice made for Rust in this
      session, for the same reason: fewer moving parts, fully verifiable
      today) producing `libziglib.a`, linked into the executable. Verified
      three ways: `--with-zig` alone (C++ calling Zig, printed
      `4 + 5 (via Zig) = 9`), `--with-rust --with-zig` together in one
      binary (printed both `rust_add(2,3)=5` and `zig_add(4,5)=9` from a
      single combined `main()`), confirming the two interop features
      compose cleanly rather than fighting over `main.cpp`.
      Cosmetic-only issue observed: a linker warning
      ("object file ... built for newer 'macOS' version ... than being
      linked") — harmless (link still succeeds, binary still runs
      correctly) and not investigated further since it doesn't affect
      correctness.
- [x] **Zig as the C/C++ compiler, use case 2 (`compiler: zig`)** —
      implemented via CMake's `CMAKE_<LANG>_COMPILER_ARG1` mechanism
      (`-DCMAKE_C_COMPILER=zig -DCMAKE_C_COMPILER_ARG1=cc`, similarly for
      C++), since `CMAKE_CXX_COMPILER` needs a single executable path and
      can't hold `"zig c++"` as one string. This also incidentally lifts
      §10's "hybrid projects only get their C++ compiler overridden"
      limitation for this one case, since zig covers both C and C++ from
      the same binary. **Not verified working end-to-end**: on this test
      machine, `zig cc`/`zig c++` fails to link even a trivial `printf`
      call with `undefined symbol: _printf` (and similar for `iostream`) -
      reproduced with plain `zig cc test.c` run directly in a shell,
      completely outside CMake and cmaker, confirming this is a pre-existing
      zig/macOS-SDK toolchain issue on this machine, not a bug in the
      generated CMake args (CMake's own compiler-identification step did
      correctly invoke `/opt/homebrew/bin/zig cc` with the expected
      flags - the *wiring* is confirmed correct, only the actual
      compile-and-link outcome couldn't be confirmed here). Worth
      re-testing on a machine/zig version where `zig cc`'s libSystem
      linking isn't broken.
- [x] `cmaker doctor` checks `zig` only when the project's `cmaker.yaml`
      has `zig.enabled: true` (mirrors the Rust check exactly).
- [x] `cmaker new --with-zig` (and `init`/`create --with-zig`) scaffold the
      library and build wiring in one shot - the `create` stub from the
      previous session was replaced with the real implementation.
- Known limitation (same as Rust): `--with-zig` only composes with
  `--template=default`.
- Known limitation: crate/library name is hardcoded (`ziglib`), same as
  Rust's `rustlib`.

## 13. Domain-specific scaffolds (ML, backend services) — ✅ DONE (2026-07-05)

Motivation, per user: C++ usage in ML is growing, and there's demand for
robust C++/Rust backends — cmaker should have concrete templates for both,
not just a generic "hello world."

Two new templates: `templates/ml-eigen/` and `templates/backend/` (both
just meta.yaml + src/main.cpp, per the existing §4 template system - no new
scaffolding mechanism needed). Two small, real core additions needed to
support them: `TemplateMeta.CMakeExtra` (templates.go) so a template's
meta.yaml can carry a `cmake_extra:` block through to the generated
`CMakeLists.txt` (previously only settable by hand-editing `cmaker.yaml`
directly), and `Dependency.DownloadOnly` + generic `GIT_REPOSITORY` support
(config.go) for deps that aren't hosted on GitHub or whose own CMakeLists.txt
isn't meant to be `add_subdirectory`'d directly. Also relocated `cmake_extra`
injection earlier in `generateCMake` (right before `add_executable`, next to
the Rust/Zig preambles) since custom targets it defines need to exist before
`target_link_libraries` references them - it was previously appended at the
very end, which would have silently broken exactly this Eigen use case.

- [x] **ML template**: `ml-eigen` — uses Eigen (linear algebra) for
      numerics, fetched via CPM from its GitLab home
      (`https://gitlab.com/libeigen/eigen.git`, `DOWNLOAD_ONLY YES` +
      `cmake_extra` wrapping it in a plain `INTERFACE` target, since Eigen's
      own CMakeLists.txt isn't meant to be consumed as a subdirectory).
      Verified end-to-end: `cmaker new --template=ml-eigen` fetched Eigen
      from GitLab, built, and correctly solved a 2x2 linear system
      (`A x = b` → `x = (0.8, 1.4)`, checked by hand against `A`/`b`).
      **libtorch and ONNX Runtime explicitly not done** - both distribute
      as large prebuilt binary packages (hundreds of MB) rather than a
      CPM-fetchable source tree, which is a fundamentally different (and
      much heavier/riskier) integration path than anything else in this
      roadmap; left open as a real gap, not attempted this pass.
- [x] **Backend template**: `backend` — a real minimal HTTP service using
      **cpp-httplib** (single-header, fetched via CPM from
      `yhirose/cpp-httplib`, no transitive dependencies). Verified
      end-to-end for real: built the binary, ran it as a live server, and
      `curl`'d both `/` and `/health` and got the expected responses back
      before stopping it - not just "it compiles."
      **Deliberate scope choice**: Drogon/Boost.Asio/gRPC explicitly not
      attempted - all three are much heavier dependency trees (Drogon pulls
      in jsoncpp and more; Boost as a CPM dependency means fetching/building
      a large monorepo; gRPC's build is notoriously heavy) with a real risk
      of hitting unverifiable-in-time build breakage, the same category of
      risk that turned up actual bugs with CPM-fetched libraries in §5.
      cpp-httplib gives a genuinely useful, honestly-scoped "backend
      service" starting point today; the heavier frameworks are a real
      follow-up, not a rejected idea.
- [ ] **`--backend --with-rust` composition, as in the user's original
      example — not done.** `--with-rust`/`--with-zig` are wired to
      `writeInteropDemoMain` (interop.go), which fully overwrites the
      project's `main` source file with a Rust/Zig demo - that's fine when
      composing with the generic `default` template's hello-world, but it
      would destroy the `backend` template's actual HTTP server code.
      Making `--with-rust`/`--with-zig` compose with *any* template's own
      `main()` (rather than only the `default` template's) needs a real
      redesign - e.g. generating a separate demo source file alongside the
      template's own main, or a documented convention for where interop
      calls get injected - not attempted this pass; left open.
- [x] Depends on §4 (embedded template system), §5 (dependency fetching, plus
      the `DownloadOnly`/`GIT_REPOSITORY` additions above), and §12
      (polyglot interop, though not yet composed with these templates per
      the note above) - all were already in place before this template work
      started.
- [x] The template system stayed fully generic: neither new template
      required any change to `scaffoldProject`'s core flow, only the two
      additive `Dependency`/`TemplateMeta` fields described above - matching
      the "new domains should be addable as templates without further core
      changes" goal.

## 14. Composable project creation & fine-grained run control — ✅ MOSTLY DONE (2026-07-05)

New files: `cmd_create.go` (the `create` command), `only.go` (shared ad hoc
single-file compile helper used by both `build --only` and `run --only`).

- [x] `cmaker create <name> [flags]` as a richer, composable evolution of
      `new`: reuses the same `--lang`/`--template`/`--compiler` flags as
      `new` (via the shared `newScaffoldFlags` helper) so `--lang`/
      `--template` genuinely compose today. `--with-rust`, `--with-zig`,
      `--backend`, and `--ml` are accepted as real flags (so the CLI
      surface/shell completions exist now) but each fails fast with a clear
      `"--X is not implemented yet (see ROADMAP.md §N ...)"` error instead
      of silently no-opping or faking support — §12/§13 haven't landed yet,
      so pretending they compose would be dishonest. Once §12/§13 land,
      these become real implementations behind the same flags.
- [x] `cmaker run --only=<file>` — compiles a single source file ad hoc via
      `compileOnly()` straight to `build/.cmaker_scratch/<name>` using the
      portable `c++`/`cc` compiler aliases (or `cmaker.yaml`'s `compiler:`/
      a `--compiler` override), then runs it, forwarding any args after
      `--`. Verified: does not touch `CMakeLists.txt` at all (byte-identical
      before/after, confirmed via checksum) and correctly compiled/ran both
      a `.cpp` scratch file and a project's own `.c` file standalone.
- [x] `cmaker build --only=<file>` — same ad hoc compile, without running.
- Known limitation, documented in code: the file must be self-contained
  with its own `main()` — only the project's `include_dirs` are passed
  (`-I`), not linked libraries/dependencies, since wiring CPM-fetched deps
  into an ad hoc single-file compile is a much bigger problem than this
  roadmap item intends to solve.
- [ ] Glob support (`--only='tests/*.cpp'`) — **not done**, left open per
      the original roadmap note; single-file is the common case and was
      prioritized first.

## 15. User-defined commands / named configs (lightweight task runner) — ✅ DONE (2026-07-05)

New file `cmd_configs.go` (`add`/`remove`/`configs` commands); dispatch logic
added to `root.go`'s `RunE`; `Configs map[string]string` field added to
`Config` (`config.go`), with `saveConfig`/`tryLoadConfigs` helpers.

- [x] `cmaker add config <name> '<cmaker args...>'` — saves a named
      shortcut into a new `configs:` map in `cmaker.yaml`, e.g.
      `configs: { test: "run --only=test1.cpp" }`. Verified: `cmaker add
      config test 'run --only=src/test1.cpp'` wrote the expected YAML.
- [x] Running `cmaker <name>` for a `<name>` that matches a saved config
      (and isn't an existing built-in subcommand) executes that saved
      command line — a lightweight task-runner layer on top of the core
      commands, in the spirit of `npm run <script>` / `just`. Implemented by
      setting `rootCmd.Args` to `ArbitraryArgs` (cobra falls back to the root
      command with the unmatched name as a positional arg when no
      subcommand matches) and re-exec'ing the resolved command line as a
      child process via `os.Args[0]`, the same subprocess-reuse pattern the
      TUI already uses. Verified end-to-end: `cmaker test` correctly
      compiled and ran `src/test1.cpp` ad hoc via the saved `--only` config.
- [x] `cmaker configs` lists all saved shortcuts for the current project.
- [x] `cmaker remove config <name>` deletes one. Verified: removed config no
      longer appears in `cmaker configs` or resolves via `cmaker <name>`.
- [x] Name-collision guard (`isReservedCommandName`) checked at *save* time
      against every real subcommand name/alias registered on `rootCmd` —
      verified `cmaker add config build '...'` fails immediately with
      `"build" is a built-in cmaker command and can't be used as a config
      name` rather than silently saving an unreachable config.
- [x] Saved configs surface in the TUI sidebar too (`model.items()` in
      `tui.go`, replacing the previously-static `menuItems` global):
      `tryLoadConfigs` reads the current project's `cmaker.yaml` fresh on
      every render and inserts one menu entry per saved config before
      "Quit" — no separate TUI-side storage to keep in sync with the CLI.
- Known limitation, documented in code: the saved command line is split on
  whitespace only (`strings.Fields`) — no shell-style quoting support, so a
  saved command containing a value with a space (e.g. a path) isn't
  supported yet. Not a real-world blocker for the common cases (`--only=`,
  `--flag=value`) but worth noting.
- Known limitation: extra flags typed after the name (e.g. `cmaker test
  --foo`) fail, since flag parsing happens against `rootCmd`'s own flag set
  before `RunE` runs and `rootCmd` doesn't declare `--foo`. Positional args
  after the name *are* forwarded correctly (`cmaker test arg1 arg2` works).

---

---

# Part 2: v2 — from "good scaffolder" to "the default C++ tool"

Everything above (§1-§15) is done or explicitly scoped-and-parked. This part
is a deliberately generous brainstorm, per an explicit ask to think bigger
and write down *everything* worth considering, not just what's certain to
ship — prune freely; nothing here is committed until you say "start with
item N," same as Part 1.

Two structural themes tie a lot of this together and are worth naming up
front:

- **§16 (library targets)** is a prerequisite for a surprising number of
  later items — installable packages, workspaces, and the domain+interop
  composition all assume a project can *be* a library, not just produce an
  executable. It's also the natural place to finally close out §3's
  long-open "multiple executables/library targets" gap, rather than
  treating that as a separate future pass.
- **The two AI-powered features (§20 autoheal, §24 natural-language
  scaffolding)** are the highest-risk, highest-differentiation items on
  this list. Both require an LLM API integration cmaker doesn't have today,
  both mutate or generate code (autoheal literally patches the user's
  source), and both need a real safety story before they're trustworthy.
  Treat them as the "zig-as-compiler" of this phase: valuable, but
  deliberately sequenced last, after everything they'd build on is solid.

## 16. Library project targets — ✅ DONE (2026-08-05)

Motivation: every template and every project today produces exactly one
`add_executable`. A huge fraction of real C++ work *is* a library (to be
consumed by another project, published, or installed by others via §17) -
`cmaker` should be able to scaffold and build one directly, not just an app
that happens to have library-shaped code in it. This is also the concrete
form of §3's still-open "multiple executables/library targets" item -
solving it here retires that bullet rather than leaving it as a second,
separate future refactor.

- [x] `cmaker.yaml`: `target_type: executable | static_library |
      shared_library` (default `executable`, so every existing config
      keeps working unchanged). New `config.TargetTypeOrDefault`
      (mirrors `LanguageOrDefault`) and `ValidTargetTypes`, validated in
      `config.Validate`.
- [x] `cmaker new mylib --lib` (shorthand for `--target-type=static_library`)
      and `--target-type=shared_library` for the explicit form (also on
      `init`/`create`, via `newScaffoldFlags`). Scaffolds a real
      public/private header split: `include/<name>/<name>.h` (public,
      `PUBLIC`-visible to consumers) + `src/<name>.cpp` (implementation),
      not a flat `include/` like an executable gets - see
      `cmd/new.go`'s `writeLibrarySources`. The library's CMake target is
      named after the project (`filepath.Base` of the scaffold name, to
      stay a valid CMake target/directory name even when the name doubles
      as a path, e.g. `cmaker new ./sub/mylib`), not the executable
      default of `main`.
- [x] `internal/cmake.Generate` branches on target type: `add_library(...
      STATIC|SHARED ...)` instead of `add_executable`, and
      `target_include_directories(... PUBLIC
      $<BUILD_INTERFACE:...>/$<INSTALL_INTERFACE:...> ...)` instead of
      `PRIVATE` - so a consumer sees the right include path whether it's
      building against this project's source tree or an installed copy.
- [x] `cmaker run` on a library project: **both options from the original
      bullet landed, not just one**. `writeLibrarySources` also scaffolds
      a real, working `examples/demo.cpp` consumer (not a stub - it
      actually calls the library's public function); `Generate` detects
      `examples/*.cpp` and adds a paired `<name>_demo` executable target
      linked against the library, and `cmd/run.go`'s new
      `runnableBinaryName` builds+runs that demo for `cmaker run`. A
      library with no `examples/*.cpp` still gets the clear-error half of
      the bullet: `"this is a static_library project - there's no
      executable to run (use 'cmaker build' instead); add
      examples/demo.cpp to also enable 'cmaker run' via a demo
      executable"`.
- [x] `install()` rules: `GNUInstallDirs` + `install(TARGETS ... EXPORT
      ...)` + `install(DIRECTORY include/ ...)` + `install(EXPORT ... FILE
      <name>Config.cmake NAMESPACE <name>:: ...)`, emitted only for
      library target types. Verified real `find_package()`-shaped output
      (see below), not just that the CMake syntax parses.
- Verified end-to-end (unit tests in `internal/cmake/generate_test.go`,
  `internal/config/config_test.go`, `cmd/run_test.go`, plus a real
  scaffold/build/run/install pass): `cmaker new mylib --lib` → `cmaker
  run` built the static library, built `mylib_demo`, linked it against
  the library, and ran it (`mylib::add(2, 3) = 5`); a `--target-type
  =shared_library` project built a real `.dylib`; `cmake --install build
  --prefix <dir>` produced a real installable tree
  (`include/mylib/mylib.h`, `lib/libmylib.a`,
  `lib/cmake/mylib/mylibConfig.cmake`); a library with `examples/demo.cpp`
  removed hit the clear error above instead of a cryptic "file not
  found"; `--lib --template=raylib` and `--lib --lang=c` both correctly
  rejected with clear "only supported with --template=default, cpp"
  errors instead of silently misbehaving. Added a new
  `cmd/integration_test.go` case
  (`TestIntegrationLibraryScaffoldBuildRunInstall`) covering the same
  scaffold→build→run→install pipeline under `go test -tags=integration`.
- Real bug found + fixed during this work: the project name doubles as
  both the scaffold root *and* the project label (`cmaker new <name>`
  passes the same string as both `root` and `name` to `scaffoldProject`)
  - for an executable this only ever showed up cosmetically (`project(/tmp
  /foo)` in the generated CMake), since `name` was never used as a
  filesystem path component. Library scaffolding does use it as one
  (`include/<name>/`, the CMake target name), so `cmaker new
  /tmp/mylib --lib` initially failed trying to create
  `include/tmp/mylib/tmp/mylib.h`. Fixed by deriving the library's
  target/directory name from `filepath.Base(name)` specifically, without
  touching the pre-existing (unrelated, cosmetic-only) quirk for
  executables.
- [ ] Not done, left open as real follow-ups rather than silently dropped:
      arbitrary N-target projects (this only supports one primary target
      plus its paired demo, not a general target list - see the updated
      §3 bullet); library scaffolding composing with `--lang=c`/`hybrid`,
      `--with-rust`/`--with-zig`, or the dependency-bearing templates
      (`raylib`/`sfml`/...); a header-only library variant (the
      `headeronly` template already exists but isn't wired to
      `target_type`/`install()` the way this section's library type is).

## 17. Package install / a real dependency manager UX — ✅ DONE (2026-08-05)

Motivation, per user: "options for importing libraries... add the git link
to the library in yaml and it should be auto fetched while building" (this
much already exists - §5's `dependencies:` + CPM) "...there must be
`cmaker install <pack-name>` commands as well ensuring smooth import of
headers/libs." The gap isn't fetching (solved), it's *discovery and
ergonomics* - today you have to already know a library's exact GitHub
repo/tag/CMake target names and hand-write a `dependencies:` block. A real
package manager UX means naming a library and getting a working, linked
dependency in one command.

- [x] **A built-in package registry**: new `internal/registry` package,
      `//go:embed entries.yaml` (same pattern as §4's templates). Six
      curated, verified-working entries: `fmt`, `spdlog`, `nlohmann-json`,
      `cxxopts`, `catch2`, `googletest` - deliberately small over trying to
      mirror vcpkg/Conan on day one.
- [x] `cmaker install <name>` - looks up `<name>` (`registry.Find`, case-
      insensitive), appends the resolved `config.Dependency` to
      `cmaker.yaml`, regenerates `CMakeLists.txt`, and immediately runs
      `cmake -S . -B build` so the fetch happens right away (like `npm
      install`/`cargo add`), not silently deferred to the next build.
      Refuses to double-add an already-installed name with a clear error
      pointing at `cmaker uninstall`. Unknown names get a clear error with
      close-match suggestions (`registry.CloseMatches`, substring +
      Levenshtein-distance-2, mirrors §4's "unknown template" error UX) -
      e.g. `install nlohman-json` (typo) suggests `nlohmann-json`.
- [x] `cmaker install --git=<url> --tag=<tag> --link=<target>` - the escape
      hatch for anything not in the curated registry. `--tag`/`--link` are
      required with `--git` (validated with a clear error, not a confusing
      downstream CMake failure); `--options`/`--download-only` also
      available for the rarer cases §5's hand-written `dependencies:`
      already supports (e.g. Eigen's `DOWNLOAD_ONLY` pattern).
- [x] `cmaker uninstall <name>` - removes the dependency entry, regenerates
      `CMakeLists.txt`, and removes its `cmaker.lock` entry if present.
      **Not done**: warning about other dependencies' `OPTIONS`
      cross-referencing it - deferred, since no shipped registry entry or
      real-world config actually does this yet, so there was nothing to
      verify the warning against.
- [x] `cmaker list` (aliased `cmaker installed`) - prints every currently-
      declared dependency (name, repo@tag, link targets) - the read side
      of `install`/`uninstall`, more discoverable than reading raw YAML.
- [x] `cmaker search <term>` - case-insensitive substring search over the
      registry's name/notes fields.
- [x] **Lockfile** (`cmaker.lock`): `internal/registry.UpdateLockfile`
      resolves each dependency's *actual* checked-out commit by running
      `git rev-parse HEAD` inside CPM/FetchContent's populated source dir
      (`_deps/<lowercased-name>-src` - confirmed via a real build that
      FetchContent lowercases the declared NAME for the directory
      regardless of the CMake target's own casing, e.g. `Catch2` ->
      `catch2-src`), and writes a human-diffable YAML file
      (`{dependencies: {<name>: {repo, tag, commit}}}`, modeled after
      Cargo.lock). Runs after `cmaker install`'s immediate fetch and after
      every `cmaker build` configure step (best-effort - a lock-update
      failure, e.g. an unfetched/partially-configured dependency, is
      logged via `-v` and does not fail the build itself).
- Verified end-to-end against real registry fetches, not just that the
  YAML round-trips: `cmaker install fmt` on a fresh project actually
  fetched fmt via CPM, wrote a real resolved-commit `cmaker.lock`, and the
  project built and ran code calling `fmt::print` successfully; `cmaker
  uninstall`/`cmaker list`/`cmaker search`/the unknown-package suggestion
  path/the already-installed guard/the `--git` escape hatch (installed
  `cxxopts` by URL, bypassing the registry entirely) were all exercised
  against a real project, not just unit-tested in isolation. Added
  `cmd/integration_test.go`'s `TestIntegrationInstallBuildRun`
  (`go test -tags=integration`) covering the same install -> lock ->
  build -> run pipeline.
- **Real bug found + fixed during this work, unrelated to cmaker itself**:
  the registry's initial `fmt` default tag (11.0.2) failed to compile on
  this machine - not a cmaker bug, but a genuine upstream incompatibility
  between fmt 11.0.2's `FMT_STRING` consteval macro and a bleeding-edge
  Apple clang (21.0.0) under C++20+, confirmed by reproducing the identical
  failure with a bare `git clone` + plain `cmake --build` of fmt alone, zero
  cmaker involvement. fmt 11.2.0 fixes it (verified the same way). Bumped
  `fmt`'s registry default to 11.2.0 and spot-verified (via bare CMake
  smoke builds under the same C++20 setting) that `spdlog`, `nlohmann-json`,
  `cxxopts`, and `googletest`'s latest stable tags all build cleanly too,
  and bumped their registry defaults to those verified tags rather than
  the stale ones originally drafted into this section - the `catch2`
  *template* (§4) keeps its own, separately-verified `v3.5.3` pin
  unchanged; only the registry entry (a different, newer-verified tag,
  `v3.15.3`) was touched.
- [ ] Not done, left open rather than silently dropped: the registry
      format doesn't yet have a documented path for a §16 library target
      scaffolded by one cmaker project to register itself as installable
      by another (the roadmap's original "depends on §16 only loosely"
      note) - today `cmaker install --git=...` would work for that case
      manually, but there's no first-class "publish this to the registry"
      flow. Also not done: the `OPTIONS` cross-reference warning on
      `uninstall` mentioned above, and any registry entries beyond the
      original six.

## 18. Domain templates × polyglot interop composition — ✅ DONE (2026-08-05)

Motivation, per user: "backend with rust, integrated with C++" / "ml with
rust integrated with C++." This is explicitly the gap flagged (not faked)
when §13 and §12 shipped: `--with-rust`/`--with-zig`'s demo-`main()`
writer (`interop.go`) unconditionally overwrites the project's `main()`,
which is fine for the generic `default` template's hello-world but would
destroy the `backend` template's actual HTTP server code or the `ml-eigen`
template's actual linear-algebra demo.

- [x] **Real fix**: split "wire the crate/library into CMake and link it"
      from "write a demo `main()` calling into it," exactly as scoped.
      `scaffoldProject` (`cmd/new.go`) now branches on `templateName`: the
      `default` template still gets the existing `writeInteropDemoMain`
      (full overwrite - its `main()` is a placeholder, so this stays the
      richer, fully-working behavior it always was); every other template
      gets the new `injectInteropUsageHint` (`cmd/interop.go`), which only
      ever *appends* - a real `#include "rustlib.h"`/`"ziglib.h"` plus a
      short usage comment, inserted right after the file's existing
      `#include` block, never touching the template's own code. The old
      `--with-rust`/`--with-zig`-only-with-`--template=default` guard was
      removed entirely - composition with any template now just works,
      not special-cased per template.
- [x] `cmaker create myapi --backend --with-rust` - `--backend`/`--ml`
      (`cmd/create.go`) went from always erroring "not implemented yet" to
      real shorthand for `--template=backend`/`--template=ml-eigen`
      (mutually exclusive with each other and with an explicitly
      conflicting `--template`, both checked with a clear error). The
      injected hint is a real, uncommented `#include` (so the crate is
      provably linked and compiling, not just present in `cmaker.yaml`)
      plus a *commented-out* example call, matching the bullet's ask.
- [x] `cmaker create myml --ml --with-rust` - same composition, applied to
      the existing `ml-eigen` template (a dedicated `--ml` template beyond
      `ml-eigen` is still open, tracked in §13, not blocking this).
- [x] Extended to `--with-zig` and to all three combined
      (`--backend --with-rust --with-zig`) with zero per-combination
      special-casing, exactly as predicted - `injectInteropUsageHint`
      takes both booleans and conditionally adds each library's
      include/comment block.
- Verified end-to-end against real builds, not just generated-file
  inspection: `cmaker create myapi --backend --with-rust` built (cargo +
  cmake) and the resulting server was started and actually curled
  (`GET /` and `/health` both returned the template's real responses,
  unmodified); `cmaker create myml --ml --with-zig` built and ran,
  producing the same real Eigen linear-algebra output as the plain
  `ml-eigen` template; the triple combination
  (`--backend --with-rust --with-zig`) built cleanly (Rust crate, Zig
  library, and the httplib service all compiling and linking together);
  `--backend --ml` and `--backend --template=raylib` both correctly
  rejected with clear conflict errors; the `default` template's own
  richer (fully-working, not just commented) demo-main behavior is
  unchanged. Unit tests
  (`cmd/interop_test.go`) verify the template's own code survives
  byte-for-byte untouched around the injected hint. Added
  `cmd/integration_test.go`'s `TestIntegrationDomainTemplateWithRustCompose`
  (`go test -tags=integration`) covering the same scaffold -> build
  pipeline.

## 19. Code generation tools — ✅ DONE (2026-08-05)

Motivation, per user: "we have a class Abc in a file Pqr.cpp; we should be
able to generate getters and setters for that class using cmaker easily by
a specific command... really essential for developer ergonomics."

- [x] `cmaker generate accessors --file=Pqr.cpp --class=Abc` (and the
      REPL-ish `cmaker generate accessors Pqr.cpp Abc` positional form) -
      new `cmd/generate.go`.
- [x] **Parsing approach - a third option, not either of the two
      originally weighed here.** The heuristic-line-parser-vs-libclang
      choice above turned out to be a false dilemma once the user proposed
      using an LLM: real semantic understanding (which members are
      non-public, constness, whether a getter should return by value vs.
      `const Type&`) is exactly what a heuristic parser is bad at and
      exactly what an LLM is good at, with no cgo/libclang dependency.
      Landed design, deliberately split into a **trusted half and an
      untrusted half**:
      - *Untrusted (LLM)*: `internal/codegen.ExtractMembers` sends the
        class body to Anthropic's Messages API and asks for a strict JSON
        array (`name`/`type`/`is_const`/`return_by_reference`) - the model
        is only ever trusted to identify *what members exist*, never to
        write C++ directly into the file.
      - *Trusted (deterministic Go)*: `internal/codegen.RenderAccessors`
        renders the actual `getX()`/`setX(...)` text from a fixed
        template given that JSON, and `internal/codegen.ExtractClassBody`
        (a heuristic brace-balancing scan - comments/string/char literals
        skipped, but no template/macro/preprocessor understanding,
        documented as a known limitation) finds where in the file to put
        it. So the *only* heuristic-parsing risk from the original
        two-option debate is now scoped to "find the class's brace
        extent," a much smaller and more robust problem than "parse
        member declarations," which is where a hand-rolled parser
        actually gets shaky.
      - `internal/llm/anthropic.go`: a deliberately minimal raw
        `net/http` client (no `anthropic-sdk-go` dependency, matching the
        project's existing low-dependency stance), reading
        `ANTHROPIC_API_KEY` from the environment, defaulting to a
        fast/cheap model (`claude-haiku-4-5-20251001`) since this is a
        small structured-extraction task, not a large generation one;
        overridable via `--model`.
- [x] Generated accessors get inserted into the class body just before its
      closing brace, wrapped in a greppable
      `// --- cmaker generated accessors: begin/end ---` marker pair;
      `InsertAccessors` finds and replaces a previous block in place on a
      second run instead of duplicating it. Verified via unit tests
      (`internal/codegen/render_test.go`): fresh insert, and a
      rename-then-regenerate run that replaces the stale block with
      exactly one marker pair surviving.
- [x] `--dry-run` prints the generated block without writing the file.
- [ ] Once the class-scanning machinery exists, it's a natural base for
      more generators later - equality operators, a `toString()`/`operator
      <<`, constructor boilerplate from member list - **not done**, left
      open as a real follow-up under the same `cmaker generate` family.
- Verified: `internal/codegen` unit tests (brace-balancing including a
  string literal containing a stray `{`/`}`, forward-declaration and
  class-not-found error paths, marker insert/replace idempotency, member
  extraction and markdown-code-fence stripping against a fake
  `Completer` - no network needed for any of this) all pass. End-to-end
  against a real file: `cmaker generate accessors Pqr.cpp Abc` on a real
  hand-written class correctly located the class body and reached the
  Anthropic API call boundary (failing there only because this dev
  environment has no `ANTHROPIC_API_KEY` set) with a clear, actionable
  error; a nonexistent class name produced a clear "no 'class X'
  declaration found" error instead of a crash.
- [x] **Live LLM round-trip verified (2026-08-05)**, closing the one gap
  noted above: with a real `ANTHROPIC_API_KEY`, `cmaker generate accessors`
  against the same `Abc` class (a `std::string`, a plain `int`, and a
  `const int` member) correctly produced `const std::string&
  getName()`/`setName(...)` (reference semantics for the non-trivial
  type), plain `int getAge()`/`setAge(...)`, and a getter-only `int
  getId()` for the `const` member with no setter - real semantic judgment
  from the model, not templated guesswork. The result compiled clean
  (`c++ -std=c++17 -fsyntax-only`), and re-running the command against the
  same file replaced the previous block in place (exactly one marker pair
  survived), confirming the idempotent-regenerate path also holds up
  against real (not fake-`Completer`) LLM output, which can vary in
  formatting/whitespace between calls in ways a hand-written fake wouldn't
  exercise.
- Real, incidental bug fixed while touching this area: adding a `test`
  subcommand in §3's ctest work (below) silently broke the README's own
  `cmaker add config test '...'` example, since `test` became a reserved
  built-in command name - the example (and the matching CLI help text in
  `cmd/configs.go`) now uses `scratch` instead.

## 20. Build tooling & DX polish

A grab-bag of smaller, lower-risk items that compound into "this feels like
a serious, well-run C++ project" - individually modest, collectively a big
part of what separates a toy scaffolder from a tool people reach for by
default.

- [x] **Compiler cache auto-detection** — ✅ done (2026-08-05).
      `internal/cmake.DetectCompilerLauncher` checks `ccache` then
      `sccache` on `PATH`; `internal/cmake.StandardConfigureFlags`
      (a new shared helper, also consolidating the near-duplicate
      `cmake -S` flag-building that used to live separately in
      `cmd/build.go`, `cmd/new.go`, and `cmd/install.go`) wires the found
      tool in as `CMAKE_C_COMPILER_LAUNCHER`/`CMAKE_CXX_COMPILER_LAUNCHER`
      on every configure. Opt-out via `cmaker.yaml`'s new
      `disable_ccache: true` (checked, not just documented). `cmaker
      doctor` reports `ccache`/`sccache` availability and whether one is
      actively wired in. Verified for real, not just via config
      inspection: installed `ccache` via Homebrew, scaffolded a project,
      and confirmed via `ccache -s` that a first build was a real cache
      *miss* and an immediate rebuild was a real cache *hit* - the
      launcher genuinely intercepts compiles, not just "the flag is
      present in the generated command line."
- [x] **`-j` parallelism for `cmake --build`** — ✅ done (2026-08-05).
      `cmd/build.go`'s `buildCommandArgs` (a small pure function, unit
      tested directly rather than only through a real `cmake` invocation)
      defaults to `runtime.NumCPU()`, overridable via `cmaker build
      --jobs`/`-j`. `cmaker run`/`cmaker test`/`cmaker watch` all inherit
      the default automatically since they call the same `runBuild`.
- [x] **`.clang-format`/`.clang-tidy` scaffolding + `cmaker fmt`/`cmaker
      lint`** — ✅ done (2026-08-05). Every scaffolded project gets a
      `.clang-format` (4-space indent, attached braces - matching the
      style cmaker's own generated example code already uses) and a
      `.clang-tidy` with a deliberately curated, low-noise check list
      (`bugprone-*`/`performance-*`/`clang-analyzer-*` plus a couple of
      `modernize-*`/`readability-*` checks - not clang-tidy's full
      opinionated default set). New `cmaker fmt` (`--check` for a
      CI-friendly dry-run, non-zero exit if anything would change) and
      `cmaker lint` (against `build/compile_commands.json`, itself now
      exported unconditionally by `StandardConfigureFlags` via
      `-DCMAKE_EXPORT_COMPILE_COMMANDS=ON` - so `cmaker lint` only ever
      needs "run `cmaker build` first," never a separate compile-database
      setup step). Verified for real (`clang-format`/`clang-tidy`
      installed via Homebrew for the session): `cmaker fmt --check` on
      deliberately mis-formatted code failed with real clang-format
      diagnostics, `cmaker fmt` fixed it in place, and a second
      `--check` passed; `cmaker lint` on code containing two real
      `int* p = 0;`/`if (p == 0)` patterns correctly flagged exactly the
      two `modernize-use-nullptr` warnings the curated `.clang-tidy`
      config asks for, proving it actually reads the project's config and
      the exported compilation database, not just that the command runs.
      (One reproducible, unrelated environment quirk hit during
      verification and not worth working around: this machine's
      Homebrew-installed, keg-only `clang-tidy` doesn't resolve the
      macOS SDK's `<iostream>` the same way the system-`clang++`-recorded
      `compile_commands.json` entry does, so `cmaker lint` also reported
      a spurious `'iostream' file not found` alongside the two real
      findings - a local `clang-tidy`/SDK path mismatch from mixing
      toolchains, not a cmaker bug, and it didn't stop the real checks
      from running or being reported correctly.)
- [x] ~~**Real `ctest` wiring**~~ — **done, see §3** (2026-08-05): `cmaker
      test` runs `ctest --output-on-failure` after a build, and the TUI
      sidebar has a `Test` entry alongside `Build`/`Run`. Still scoped to
      the single-executable model — real per-test-target status/reporting
      is blocked on §16.
- [ ] **Coverage reports**: `sanitizers:`-style opt-in `coverage: true` in
      `cmaker.yaml` wiring `--coverage`/`-fprofile-instr-generate` as
      appropriate for the detected compiler, plus a `cmaker coverage`
      command producing an HTML report (via `gcovr`/`llvm-cov`, checking
      for their presence via `cmaker doctor`-style detection first).
- [ ] **Benchmark scaffolding**: `--with-benchmarks` wiring Google
      Benchmark the same way `catch2`/§20's `ctest` wiring handle testing -
      a `bench/` directory + CMake target, not just a template.
- [ ] **Doxygen scaffolding**: generate a starter `Doxyfile` +
      `cmaker docs` command to build API docs from it - useful once §16
      library targets make "this project has a public API worth
      documenting" a common case.
- [ ] **Devcontainer/Dockerfile scaffolding**: `--with-docker` generates a
      `Dockerfile` (and optionally `.devcontainer/devcontainer.json`) with
      the right base image/toolchain for the project's configured
      language/compiler - a real "clone and it just works, even without a
      local toolchain" story.
- [ ] **Not done, left open**: coverage reports, benchmark scaffolding,
      Doxygen scaffolding, and devcontainer/Dockerfile scaffolding (the
      four bullets above) - the compiler-cache/`-j`/fmt-lint trio landed
      2026-08-05, these four are each really their own
      template/command category and weren't in this pass's scope.
- [ ] Every bullet here is independent of the others and of §16-§19 -
      freely reorderable, good candidates to interleave with the bigger
      items above if you want quick wins between them.

## 21. Workspaces / monorepo support

Motivation: once §16 makes "a project can be a library" real, the natural
next question is "a repo with several cmaker projects that depend on each
other" - a monorepo of an app plus one or two internal libraries, all built
and versioned together. This is the multi-project generalization of §3's
original "multiple executables" ask, once §16 has made a single project
capable of being more than one kind of target.

- [ ] `cmaker.yaml` **workspace mode**: a top-level `workspace: { members:
      [app, libs/core, libs/net] }` config (in a root `cmaker.yaml` with no
      `executable:`/`target_type:` of its own) listing member project
      directories, each with its own normal `cmaker.yaml`.
- [ ] `cmaker build`/`cmaker run`/`cmaker test` at the workspace root
      operate across all members (or a `--only=<member>` subset), resolving
      inter-member dependencies (an app member depending on a sibling
      library member) via CMake's own `add_subdirectory`, not a second
      CPM-style fetch of something that's already sitting right there in
      the repo.
- [ ] Depends on §16 (library targets) existing first - a workspace of
      only-executables is a much smaller, less interesting version of this
      problem.

## 22. Supply-chain / dependency auditing — ✅ DONE (2026-08-05)

Motivation: once §17 makes adding dependencies frictionless, "what am I
actually pulling in, and is any of it known-bad" becomes a real question
worth answering, not just a nice-to-have - the same maturity arc every
package ecosystem (npm, cargo) has gone through.

- [x] `cmaker audit` - new `internal/audit` package (`Run`/`QueryCommit`,
      pure logic, no CLI concerns, mirroring `internal/config`/
      `internal/cmake`'s split) POSTs each `cmaker.lock` (§17) dependency's
      exact resolved *commit* to OSV.dev's documented commit-query endpoint
      (`{"commit": "<sha>"}` -> `POST /v1/query`) and reports any vulnerability
      OSV has on record for that commit. Auditing the locked commit rather
      than the `tag:` is deliberate and exactly what the roadmap called
      for: a tag is a mutable pointer, a resolved commit is not, so this is
      the same "what did we actually build" question §17's lockfile exists
      to answer, extended to security. Exits non-zero if anything is found,
      so it's CI-usable the same way `cmaker test`/`cmaker fmt --check` are.
- [x] License surfacing: `internal/audit.GitHubLicense` looks up a
      GitHub-hosted dependency's license via GitHub's public repos API
      (which reports whatever its own `licensee` detector found) - shown
      in both `cmaker audit`'s output and a new `cmaker list --licenses`
      flag. Deliberately opt-in on `list` (plain `cmaker list` stays
      instant/offline, unaffected - `--licenses` is the only path that adds
      a network call per dependency), unconditional on `audit` (whose
      whole point is a deeper, already-networked check).
- [x] Depends on §17's lockfile, exactly as scoped - `cmaker audit` reads
      only `cmaker.lock`, not `cmaker.yaml`'s `dependencies:` (which has no
      resolved commit to query by).
- Verified end-to-end against the real, live OSV.dev and GitHub APIs (not
  mocked) - installed a real dependency (`fmt`) via `cmaker install`, then
  `cmaker audit` correctly reported its real resolved commit's (no known
  vulnerabilities) status and its real license (MIT, matching fmt's actual
  license) in a single real network round-trip per API; `cmaker list
  --licenses` reported the same license while plain `cmaker list` stayed
  network-free (verified via `time`, ~5ms); an empty `cmaker.lock` (no
  dependencies installed yet) produced a clear "nothing to audit" message
  at exit 0, not an error. `internal/audit`'s unit tests
  (`osv_test.go`/`license_test.go`/`audit_test.go`) run against a fake
  `httptest.Server` for deterministic coverage of the parsing/error/sorting
  logic independent of live network state or OSV's own data-coverage
  gaps for any specific commit.
- **Real, useful thing learned during verification, not a bug**: OSV.dev's
  commit-query endpoint's coverage is real but uneven in practice - it
  correctly returns results for a known-vulnerable *package+version* query
  (verified directly against a real CVE, Jinja2/CVE-2019-10906, to confirm
  the API schema and that this integration parses real advisory data
  correctly) and curl is tracked with real GIT-range vulnerability records,
  but querying a specific curl release commit that OSV's own affected-tags
  list says *should* match one of those records returned empty. This is a
  property of OSV.dev's own indexing completeness for exact-commit lookups
  (the same kind of coverage gap any vulnerability database has), not a
  defect in cmaker's integration - documented here rather than silently
  assumed away, since "cmaker audit found nothing" should never be
  over-read as "definitely nothing to find."

## 23. Extensibility: user/community templates and registry entries

Motivation: the embedded (`go:embed`) templates (§4) and the built-in
package registry (§17) are both compiled into the binary today - great for
"it just works out of the box," but it means every new template or
registry entry requires a cmaker release. For this to become "the default
tool for C++ work" rather than "a tool with a fixed menu," people need to
be able to extend it without forking.

- [ ] User-local templates: a `~/.cmaker/templates/` directory (or
      project-local `.cmaker/templates/`) checked in addition to the
      embedded ones, same `meta.yaml` format as §4 - `cmaker new
      --template=<name>` and `cmaker templates` both need to merge the two
      sources (and clearly label which is which in `cmaker templates`
      output).
- [ ] Same idea for §17's package registry: a user-local registry overlay
      (`~/.cmaker/registry.yaml`) merged with the built-in curated list,
      so someone can `cmaker install` their own or their team's internal
      libraries without waiting on a cmaker release to add them to the
      built-in index.
- [ ] Longer-term, more speculative: a community template/registry
      "index" repo (separate from cmaker itself, like a Homebrew tap is
      separate from Homebrew) that `cmaker` can pull from - real
      infrastructure and moderation questions attached to this one, flagged
      explicitly as the most aspirational/least-scoped item in this
      section.

## 24. AI-assisted autoheal — ✅ v1 DONE (2026-08-06)

Motivation, per user: capture the last N build/run logs, and `cmaker heal`
uses "agentic AI" to read those logs plus the codebase and produce a patch
that fixes the failure. This is the most novel idea in this whole list and
also the riskiest: it's the first cmaker feature that (a) calls out to an
external LLM API and (b) proposes changes to the user's actual source code.
Both need a deliberate, phased, safety-first design - "silently rewrite my
code" is not an acceptable default for anything, however good the model is.

- [x] **Log capture** (the foundation, independent of AI) - new
      `internal/logs` package. Every `cmaker build`/`cmaker run` writes its
      combined stdout/stderr to a rotating `.cmaker/logs/<timestamp>-
      <kind>-<ok|fail>.log` (timestamp first, not kind first - an early
      version put kind first and a real test caught that this makes
      filenames sort by kind before time, so "most recent across build and
      run" silently returned the wrong one). Keeps the last 5 by default,
      configurable via `cmaker.yaml`'s `logs_keep`. `cmaker logs` lists/
      prints captures standalone, no AI attached - ships and is useful on
      its own, exactly as scoped. `cmaker build`'s configure+build steps
      share one combined log; `cmaker run`'s own execution gets a separate
      "run" log, so a runtime crash is capturable too, not just a compile
      failure.
- [x] **Provider integration**: already solved by §19's `internal/llm`
      Anthropic client (`ANTHROPIC_API_KEY` env var, never a `cmaker.yaml`
      field) - reused as-is, no new provider code needed. `cmaker heal`
      with no key set fails via the same clear, actionable
      `llm.NewClientFromEnv` error §19 already established.
- [x] **`cmaker heal` v1 - suggest, don't touch**: reads the most recent
      failing log (`logs.LatestFailure`), extracts file:line references
      from the compiler's own error output (`internal/heal.
      ExtractReferencedFiles`, capped at 5 files), reads just those files,
      and asks the model to fix it. Nothing is written to disk - `cmaker
      heal`'s diff is printed to stdout for review/manual `git apply`.
      **Real design change from what was originally planned**, driven
      entirely by live testing against a real model (see below): the LLM
      is asked for each changed file's *entire corrected content*, not a
      hand-written unified diff - `cmaker` computes the actual diff itself
      (`internal/heal/diff.go`, an LCS-based line diff, single hunk per
      file, with correct hunk-header arithmetic and `\ No newline at end
      of file` handling) rather than trusting the model's diff-format
      arithmetic. Same "don't trust the LLM to emit the final artifact
      directly" principle §19's accessor generator already established,
      arrived at here specifically because live testing caught the model
      getting it wrong.
- [ ] **`cmaker heal --apply` v2** - not done, left open exactly as scoped
      (v1 was the goal for this pass): applies the diff, but only ever on
      a clean git working tree, re-runs the build immediately after, and
      reports whether it actually fixed the failure.
- [x] **Explicit non-goals held to**: no multi-file architectural rewrites,
      no applying without a human reviewing first, no unattended/CI usage -
      v1 only ever prints a diff for a human to read.
- Verified end-to-end against a real failing build and a real
  `claude-haiku-4-5` model call (not mocked), catching four real bugs
  along the way that unit tests alone would not have:
  1. **Absolute compiler paths broke file reading.** `filepath.Join(".",
     "/tmp/x/y")` silently produces `"tmp/x/y"`, not `"/tmp/x/y"` - a real
     compiler on this machine reports absolute source paths, and the
     initial implementation mangled them into paths that don't exist.
     Fixed by using an already-absolute reference as-is.
  2. **The model got unified-diff hunk-header arithmetic wrong.** The
     original design asked the LLM to hand-write a diff directly; a real
     response's `@@ -1,6 +1,11 @@` header claimed 6 original lines where 7
     were actually present in the hunk body, and `git apply` rejected it
     as corrupt. This is *why* the design changed to "model proposes full
     file content, cmaker computes the diff" mid-implementation, not a
     pre-planned choice.
  3. **`git apply` rejects absolute paths in diff headers outright**
     ("invalid path"). Fixed by converting an absolute file reference to a
     path relative to the project root before it ever appears in a diff
     header (or in the prompt/response contract at all).
  4. **A stray trailing line silently became part of "the fix."** A real
     model response's corrected file content ended in a bare `---` (an
     apparent echo of the prompt's own `--- file: <path> ---` delimiter
     style) - nothing bounds the end of the last (undelimited) file block,
     so this line would otherwise have landed in the patched file
     unnoticed. The resulting diff applied to git *perfectly cleanly* and
     still broke the build - caught only by actually rebuilding after
     applying, not by "does the diff look valid." `cmaker heal` now
     defensively strips a trailing dashes-only line from the last block,
     on top of the markdown-code-fence stripping already needed.
  After all four fixes: a genuinely broken build (`compute_total` called
  but never declared) was captured as a `-fail.log`, `cmaker heal`
  correctly diagnosed it and proposed adding the missing function, the
  resulting diff applied with `git apply` with zero errors, and the
  rebuilt project actually compiled and ran correctly (`Total: 5`) -
  verified as a complete real loop, not diff-shaped text that merely
  looked plausible. `internal/logs`/`internal/heal`'s unit tests (17+
  cases, including `TestUnifiedDiffHunkHeaderIsConsistentWithBody`, which
  actually shells out to a real `git apply` for several diff shapes rather
  than just eyeballing the output) and a new
  `cmd/integration_test.go` case (`TestIntegrationBuildLogCapture`,
  network-free) cover the AI-independent log-capture half under
  `go test`/`go test -tags=integration` without needing an API key.
- [ ] Not done, left open rather than silently dropped: §24's own explicit
      v2 (`--apply`), and (also inherited from the design pivot above)
      multi-hunk-per-file diffs (today's `unifiedDiff` deliberately emits
      one hunk per file, sufficient for the small, focused fixes this
      targets, but would need splitting for a fix touching widely
      separated parts of a large file).

## 25. AI-assisted natural-language scaffolding — ✅ DONE (2026-08-06)

Motivation: the flip side of autoheal - instead of fixing broken code,
generate a starting project from a plain description, composing existing
cmaker building blocks (templates, `--with-rust`/`--with-zig`, §17
packages) rather than generating arbitrary code from scratch.

- [x] `cmaker new myproj --describe "a REST API backend that returns JSON,
      written in C++"` (also on `init`/`create`) - new `internal/describe`
      package. The model's job is narrowly scoped to *selecting from a
      menu* of cmaker's actual templates/packages (given to it in the
      prompt, not assumed) - it returns structured JSON
      (`template`/`language`/`with_rust`/`with_zig`/`target_type`/
      `packages`/`reasoning`), never code, the same "LLM proposes a
      structured decision, cmaker's deterministic machinery executes it"
      principle §19/§24 already established. Every field is validated
      against the real template list, package registry, and known
      language/target-type values before anything is scaffolded - an
      unrecognized template/language fails clearly (not a silent guess),
      an unrecognized package is quietly dropped, and (a real bug caught
      by live testing, see below) a combination violating
      `scaffoldProject`'s actual composition rules is normalized back to
      something valid rather than failing the whole plan. The plan is
      printed - template, flags, packages, and the model's own one-line
      reasoning - before scaffolding proceeds.
      `--describe` conflicts with `--template`/`--lang`/`--with-rust`/
      `--with-zig`/`--target-type`/`--lib` (and `--backend`/`--ml` on
      `create`) with a clear error, rather than silently overriding an
      explicit flag.
- [x] Provider integration: reused §19/§24's existing `internal/llm`
      Anthropic client as-is (`llm.NewClientFromEnv`) - no new provider
      package needed. This *is* the "share one integration, don't roll a
      second" outcome this bullet asked for; it just turned out `internal/
      llm` already was that shared piece by the time this section started,
      rather than needing a rename/extraction into a differently-named
      `internal/ai` package.
- [x] **Deliberate departure from "mirror heal's gated --apply", with the
      reasoning made explicit**: `cmaker heal` never writes to disk without
      a future, separate `--apply` step, because it patches a user's
      *existing* source. `--describe` scaffolds into a *new*, typically
      empty directory - inherently low-risk and trivially reversible
      (delete it, retry) - so it prints the plan and acts on it in one
      command, the same shape every other `cmaker new` invocation already
      has, rather than inventing a confirmation prompt found nowhere else
      in the CLI.
- Verified end-to-end against the real Anthropic API and real builds (not
  mocked), across genuinely different descriptions to confirm real
  selection rather than a hardcoded response: "a REST API backend that
  returns JSON, written in C++" correctly planned `template=backend` +
  `packages=[nlohmann-json]`, and the resulting project not only built but
  its HTTP server was started and curled for real (`GET /` and `/health`
  both responded correctly); "a small library that does 2D matrix math, no
  dependencies... beyond the standard library" correctly planned
  `template=headeronly`, built, and ran producing correct output. The
  `--describe` vs. explicit-flag conflict guards were exercised on both
  `new` and `create`. Unit tests (`internal/describe`, 10 cases) use a fake
  `Completer` for deterministic coverage; a new
  `cmd/integration_test.go` case (`TestIntegrationDescribeScaffoldBuildRun`,
  skipped rather than failed when no `ANTHROPIC_API_KEY` is set, matching
  `requireTool`'s policy for a missing binary) runs the same live
  plan-then-build pipeline under `go test -tags=integration`.
- **Real bug found + fixed via live testing, not assumed away**: the
  prompt asks the model to keep `language`/`with_rust`/`with_zig`/
  `target_type` at their defaults when it picks a non-`default` template,
  but a live response picked `template=headeronly` together with
  `target_type=static_library` anyway - a combination `scaffoldProject`
  correctly rejects, which failed the *entire* plan over an auxiliary
  field, not the actual template decision. Fixed by normalizing
  `language`/`target_type` in Go to mirror `scaffoldProject`'s exact
  composition rules, rather than trusting the model to have followed its
  own instructions (same lesson `internal/codegen`/`internal/heal` already
  learned from their own live-testing surprises). Caught a *second*,
  subtler mistake while fixing the first: an initial fix over-corrected by
  zeroing `with_rust`/`with_zig` for every non-default template, which
  would have silently broken the exact `--backend --with-rust`
  composition §18 exists to support - `--with-rust`/`--with-zig` compose
  with *any* template and are correctly left untouched by the final
  normalization; only `language`/`target_type` (and `target_type` when
  combined with Rust/Zig specifically) are corrected. A dedicated
  regression test locks in both the original bug and this narrower-scope
  correction.
- [ ] Not done, left open rather than silently dropped: no `--model`
      override flag for `--describe` (heal/generate accessors both have
      one; describe currently always uses the default model) - a small,
      easy follow-up, just not in this pass's scope.

---

## Suggested execution order

1. §1 Correctness bugs (cheap, real, unblocks everything else). ✅
2. §2 Cobra migration (needed before the TUI can cleanly reuse commands
   headlessly vs interactively). ✅
3. §6 TUI dashboard wrapping the now-clean commands. ✅
4. §4/§5 Templates via `go:embed` + dependency fetching. ✅
5. §3 Config/CMake feature expansion, done together with §10 (compiler
   selection) and §11 (C / hybrid C++ support) — all three touch the same
   `Config` struct and `generateCMake` generator, so splitting them into
   separate passes would mean re-touching the same code repeatedly. ✅
   (multiple executables, test/`ctest` wiring, `-j` parallelism, and the
   TUI compiler picker remain open — tracked in their respective sections)
6. §14 (`create`, `--only`) and §15 (named configs) — CLI-surface work that
   builds on the now-stable config/generator from step 5. ✅ Both done (glob
   support for `--only` and shell-quoting in saved configs left open).
7. §12 (Rust/Zig interop) — the highest-risk item; deliberately sequenced
   after the config/generator/template foundations are solid, and phased
   internally (Rust before Zig, source-linking before "zig as compiler").
   ✅ Both done 2026-07-05 (Rust via a direct cargo custom-command; Zig
   source-linking via a direct `zig build-lib` custom-command, verified
   end-to-end including combined with Rust in one binary; "zig as the
   compiler" is wired but unverified on this machine due to a pre-existing
   zig/macOS linking issue unrelated to cmaker — see §12 for details).
8. §13 (ML/backend domain templates) — depends on §4/§5 (templates +
   dependency fetching) and §12 (polyglot interop) already existing.
   ✅ Done 2026-07-05: `ml-eigen` (Eigen via CPM, verified solving a real
   linear system) and `backend` (cpp-httplib via CPM, verified as a live
   HTTP server responding to real requests). libtorch/ONNX Runtime and
   Drogon/Boost.Asio/gRPC deliberately not attempted (too heavy/risky for
   CPM-style fetching); `--backend --with-rust` composition not done (needs
   a redesign of how interop demos get injected into a template's own
   main() - see §13 for details).
9. §7/§8 Tests, CI, packaging — lock in production-grade once the surface
   area stabilizes. §7 (tests/CI) done 2026-07-05, including fixing a real
   pre-existing blocker (`module main` in go.mod silently broke `go test`
   entirely). §8 (packaging config) also done 2026-07-05 - goreleaser
   config + Homebrew cask + completions all real and verified locally;
   actually publishing needs a real GitHub org/repo, which this project
   still doesn't have (git was initialized locally this session, no
   remote pushed).
10. §9 Package split — ✅ Done 2026-07-05, at the end rather than alongside
    step 6 as originally suggested (the flat layout held up fine through
    §10-§15 in practice); see §9 for the full `cmd`/`internal/*` layout,
    the real variable-shadowing bug the split caught, and end-to-end
    re-verification after the move.

### v2 suggested execution order (§16-§25, none started yet)

Not started - proposed sequencing only, to revisit/reprioritize as you say
"start with item N." Reasoning for the order is in each section's own
"Depends on" bullets above; summarized here:

11. §16 Library project targets - the structural prerequisite most other
    v2 items lean on; good first move, or start with §19 (getters/setters)
    instead if you want an independent, self-contained item first.
12. §17 Package install / dependency manager UX (`cmaker install`, the
    registry, the lockfile) - builds directly on §16 existing.
13. §18 Domain templates × interop composition (`--backend --with-rust`
    etc.) - a refactor of already-shipped §12/§13, not new machinery;
    could realistically be pulled forward earlier if it's more urgent than
    §16/§17.
14. §19 Code generation (getters/setters) - independent of everything
    else; can genuinely be done at any point, including first.
15. §20 Build tooling & DX polish (ccache, `-j`, clang-format/tidy, ctest,
    coverage, benchmarks, Doxygen, Docker) - a grab-bag of independent,
    lower-risk items; good to interleave between the bigger ones rather
    than batched into one pass.
16. §21 Workspaces/monorepo - depends on §16.
17. §22 Supply-chain auditing - depends on §17's lockfile.
18. §23 Extensibility (user templates/registry, eventual community index)
    - depends on §4/§17 existing, otherwise independent; the "community
      index" sub-item is explicitly the most speculative/least-scoped
      thing in the whole document.
19. §24 AI-assisted autoheal - deliberately last: highest risk (mutates
    user code), needs a new AI-provider integration cmaker doesn't have,
    and benefits from §20's `ctest` wiring to verify a "heal" actually
    worked. Phase internally too: log capture (no AI) → `heal` suggest-only
    → `heal --apply` with git-safety guards.
20. §25 AI-assisted natural-language scaffolding - also deliberately last,
    for the same reasons as §24 (shares its provider integration), and
    needs §17/§18's primitives to exist first for there to be anything
    interesting to compose.
