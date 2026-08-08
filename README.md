# cmaker

A CLI (and a full-screen dashboard, if you'd rather point-and-click) that
scaffolds, configures, and builds CMake-based C/C++ projects — with
first-class support for pulling in dependencies, mixing in Rust or Zig,
and running as a lightweight task runner for your own project-specific
shortcuts.

`cmaker` keeps `CMakeLists.txt` in sync with a single, human-readable
`cmaker.yaml` file. You edit YAML; `cmaker` regenerates the CMake for you.

```
$ cmaker new hello-world
$ cd hello-world
$ cmaker run
-- Running build/main:
Hello from Cmaker!
```

---

## Table of contents

- [Install](#install)
- [Quickstart](#quickstart)
- [Core commands](#core-commands)
- [The `cmaker.yaml` config file](#the-cmakeryaml-config-file)
- [Templates](#templates)
- [Languages: C, C++, and hybrid projects](#languages-c-c-and-hybrid-projects)
- [Library project targets](#library-project-targets)
- [Dependencies (fetched automatically via CPM)](#dependencies-fetched-automatically-via-cpm)
- [Package install (a real dependency manager UX)](#package-install-a-real-dependency-manager-ux)
  - [Supply-chain auditing (`cmaker audit`)](#supply-chain-auditing-cmaker-audit)
- [Extensibility: your own templates and registry entries](#extensibility-your-own-templates-and-registry-entries)
- [Compiler selection](#compiler-selection)
- [Build speed: parallelism and compiler caching](#build-speed-parallelism-and-compiler-caching)
- [Formatting and linting (`cmaker fmt` / `cmaker lint`)](#formatting-and-linting-cmaker-fmt--cmaker-lint)
- [Coverage, benchmarks, docs, and Docker](#coverage-benchmarks-docs-and-docker)
- [Sanitizers and warnings-as-errors](#sanitizers-and-warnings-as-errors)
- [Testing (`ctest`)](#testing-ctest)
- [Workspaces (monorepo support)](#workspaces-monorepo-support)
- [Rust and Zig interop](#rust-and-zig-interop)
- [Ad hoc single-file compiles (`--only`)](#ad-hoc-single-file-compiles---only)
- [Named configs (your own shortcuts)](#named-configs-your-own-shortcuts)
- [Code generation (`generate accessors`)](#code-generation-generate-accessors)
- [Build/run logs and AI-assisted healing (`cmaker logs` / `cmaker heal`)](#buildrun-logs-and-ai-assisted-healing-cmaker-logs--cmaker-heal)
- [Natural-language scaffolding (`--describe`)](#natural-language-scaffolding---describe)
- [The interactive dashboard (TUI)](#the-interactive-dashboard-tui)
- [Shell completions](#shell-completions)
- [Global flags](#global-flags)
- [Command reference](#command-reference)
- [Project layout cmaker creates](#project-layout-cmaker-creates)
- [Contributing / roadmap](#contributing--roadmap)

---

## Install

**From source** (until prebuilt releases exist — see `ROADMAP.md` §8):

```bash
git clone https://github.com/<you>/cmaker.git
cd cmaker
make install   # builds ./cmaker and installs it to /usr/local/bin (sudo)
```

`make install` is also the command to re-run after pulling or making local
changes — it rebuilds from the current working tree and overwrites whatever
`cmaker` is currently on `PATH`, so the installed binary never silently
drifts out of sync with the source (the version it reports is derived from
`git describe`, so `cmaker --version` tells you exactly what's installed).

Prefer not to install system-wide, or don't have `sudo`? `make build`
just builds `./cmaker` in the repo without touching `PATH`:

```bash
go build -o cmaker .
# or: make build
```

**Requirements to build cmaker itself:** Go 1.25+.

**Requirements to use cmaker on a project:** `cmake` and a C/C++ compiler
(clang or gcc) at minimum. Run `cmaker doctor` any time to check what's
installed and get install hints for what's missing.

---

## Quickstart

```bash
# Scaffold a new C++ project into ./hello-world
cmaker new hello-world
cd hello-world

# Build + run it
cmaker run

# Or just launch the dashboard from inside any project (or an empty dir)
cmaker
```

That's the whole loop for the simplest case. Everything below is what's
available once you need more than "hello world."

---

## Core commands

| Command | What it does |
|---|---|
| `cmaker new <name>` | Scaffold a new project into `./<name>` |
| `cmaker init` | Scaffold into the *current* directory (name inferred from the directory) |
| `cmaker create <name> [flags]` | Composable scaffolding — same as `new` but built for stacking flags like `--with-rust --with-zig` |
| `cmaker build` | Configure + build (`cmake -S`/`cmake --build`) |
| `cmaker run` | Build if needed, then run the executable, streaming its output |
| `cmaker test` | Build, then run the project's `ctest` suite (needs `testing.enabled` in `cmaker.yaml`) |
| `cmaker clean` | Wipe and recreate `build/` |
| `cmaker watch` | Rebuild + rerun automatically on file changes (Ctrl+C to stop) |
| `cmaker doctor` | Check your toolchain (cmake, compilers, Rust/Zig if your project needs them) and print install hints |
| `cmaker templates` | List every available project template |
| `cmaker tui` | Launch the interactive dashboard explicitly |

Run any command with `--help` for its full flag list, e.g. `cmaker new --help`.

---

## The `cmaker.yaml` config file

Every scaffolded project gets a `cmaker.yaml` at its root. This is the one
file you hand-edit; `cmaker build`/`cmaker run` regenerate `CMakeLists.txt`
from it automatically every time, so you never touch CMake syntax directly
for anything this file models.

A fully-loaded example, showing every field:

```yaml
project_name: myapp
schema_version: 1
language: cpp              # cpp | c | hybrid (omit for cpp, the default)
target_type: executable    # executable | static_library | shared_library (omit for executable, the default)
cpp_version: 20
c_version: 17               # only used when language is c or hybrid
executable: main
include_dirs:
  - include
libraries: []                # system libraries to link, e.g. [pthread, m]
dependencies:
  - name: raylib
    repo: raysan5/raylib      # "owner/repo" GitHub shorthand, or a full git URL
    tag: "5.0"
    link: [raylib]
    options: ["BUILD_EXAMPLES OFF"]
compiler: clang++-17         # optional override; omit to let CMake pick
runner: crun                 # optional: custom compile-and-run tool for 'run --only' (see below)
disable_ccache: false        # opt out of automatic ccache/sccache wiring (on by default when found)
logs_keep: 5                 # how many .cmaker/logs/ build/run logs to retain (default 5)
coverage: false               # opt-in --coverage instrumentation, consumed by 'cmaker coverage'
sanitizers: [address, undefined]
warnings_as_errors: true
cmake_extra: |               # raw CMake, appended for anything the generator doesn't model
  message(STATUS "hello from cmake_extra")
configs:
  scratch: "run --only=tests/scratch.cpp"
testing:
  enabled: true                # wires enable_testing()/add_test() for 'cmaker test' (ctest)
rust:
  enabled: true
  crate_dir: rust
zig:
  enabled: true
  src_dir: zig
```

Every field except `project_name` and `executable` is optional — a fresh
`cmaker new` writes only what's relevant to the template/language/flags you
picked. Nothing here costs you anything at build time unless you actually
set it (a plain project with no `rust:`/`zig:`/`sanitizers:` generates
exactly the same lean `CMakeLists.txt` it always did).

---

## Templates

`cmaker new --template=<name>` picks which starter project you get. Run
`cmaker templates` to see what's available in your installed version — as
of this writing:

| Template | What it gives you |
|---|---|
| `default` | Minimal hello-world C++ executable, no dependencies |
| `sfml` | SFML 2D game/graphics window, fetched via CPM |
| `raylib` | raylib window + game loop, fetched via CPM |
| `catch2` | Catch2 v3 test scaffold, fetched via CPM |
| `headeronly` | A header-only library skeleton (`include/` + a demo consumer in `src/`) |
| `ml-eigen` | Eigen (linear algebra) numerics starter, fetched via CPM |
| `backend` | A minimal real HTTP service using cpp-httplib, fetched via CPM |

```bash
cmaker new mygame --template=raylib
```

`--lang`, `--with-rust`, and `--with-zig` (see below) currently only compose
with `--template=default` — the other templates are concrete C++ dependency
showcases and aren't meaningful in C, or don't yet have a defined way to
merge their own `main()` with an interop demo's.

---

## Languages: C, C++, and hybrid projects

By default you get a C++ project. Pass `--lang` to `new`/`init`/`create` to
change that:

```bash
cmaker new myclib --lang=c        # plain C project (src/main.c, C17 by default)
cmaker new mymixed --lang=hybrid  # both C and C++ sources in one target
```

A `hybrid` project scaffolds a real, working example of the pattern you'd
actually use: a C library (`mathlib.h`/`mathlib.c`) wrapped in
`extern "C" { ... }`, called from a C++ `main()` — not just two empty files
sitting next to each other.

---

## Library project targets

By default `cmaker new` scaffolds an executable. Pass `--lib` (shorthand for
`--target-type=static_library`) or `--target-type=shared_library` to
scaffold a library instead:

```bash
cmaker new mylib --lib                       # static library
cmaker new mylib --target-type=shared_library # shared library
```

Unlike an executable's flat `include/`, a library gets the public/private
header split real library code actually uses:

```
mylib/
├── include/mylib/mylib.h   # public header, exported at 'target_include_directories(... PUBLIC ...)'
├── src/mylib.cpp           # implementation
└── examples/demo.cpp       # a real consumer, linked against the library target
```

`examples/demo.cpp` isn't a stub - it's a working example that calls into
the library, compiled as its own `<name>_demo` executable and linked
against the library target. That's what makes `cmaker run` still work on a
library project: it builds and runs the demo, so "does my library actually
work" stays a one-command answer. A library with no `examples/*.cpp` file
makes `cmaker run` fail with a clear error pointing at `cmaker build`
instead, rather than trying to run something that doesn't exist.

`cmake --install build --prefix <dir>` also works out of the box: cmaker
generates real `install()` rules (`GNUInstallDirs`, headers, and an
exported `<name>Config.cmake`), so a cmaker-built library is genuinely
consumable from another CMake project via `find_package(<name>)`, not just
buildable in place.

**Known limits today:** library scaffolding only composes with the default
C++ template (not `--lang=c`/`hybrid`, not `--with-rust`/`--with-zig`, not
the dependency-bearing templates like `raylib`/`sfml`) - see `ROADMAP.md`
§16 for what's tracked as follow-up.

---

## Dependencies (fetched automatically via CPM)

Templates like `sfml`/`raylib`/`catch2`/`ml-eigen`/`backend` declare a
`dependencies:` list in `cmaker.yaml` (see the example above). Under the
hood, `cmaker` bootstraps [CPM.cmake](https://github.com/cpm-cmake/CPM.cmake)
and emits a `CPMAddPackage(...)` per dependency — so the library is fetched
and built automatically the first time you configure, no `vcpkg`/`conan`/
system package manager required.

You can add your own dependency to any project by hand-editing
`cmaker.yaml`'s `dependencies:` list; `cmaker build`/`cmaker run` will pick
it up on the next run. `repo:` accepts either the GitHub `"owner/repo"`
shorthand or a full git URL (needed for repos not on GitHub, e.g. Eigen's
GitLab home) — `cmaker` picks the right CPM keyword automatically.

---

## Package install (a real dependency manager UX)

Hand-editing `dependencies:` works, but you have to already know a
library's exact repo/tag/CMake target names. `cmaker install` closes that
gap — name a library, get a working, linked dependency in one command:

```bash
cmaker search json           # discover what's available
cmaker install nlohmann-json # adds it to cmaker.yaml and fetches it immediately
cmaker list                  # see what's currently installed
cmaker uninstall nlohmann-json
```

`cmaker install <name>` looks `<name>` up in cmaker's built-in registry (a
small, curated list of well-behaved CPM-friendly libraries — currently
`fmt`, `spdlog`, `nlohmann-json`, `cxxopts`, `catch2`, `googletest`),
appends the resolved dependency to `cmaker.yaml`, and reconfigures right
away so the fetch happens immediately (like `npm install`/`cargo add`), not
silently deferred to the next build. An unknown name gets a clear error
with close-match suggestions instead of a dead end.

For anything not in the registry, `--git` is the escape hatch — any
git-hosted library with a real `CMakeLists.txt`:

```bash
cmaker install mylib --git=https://github.com/me/mylib --tag=v1.0.0 --link=mylib::mylib
```

### The lockfile (`cmaker.lock`)

A `tag:` in `cmaker.yaml` says what you *asked* for; a mutable tag (or a
branch used as one) can silently point somewhere different later. Every
`cmaker install` and `cmaker build` refreshes `cmaker.lock` with the exact
commit CPM actually resolved for each dependency — modeled after
`Cargo.lock`/`package-lock.json`: human-diffable, meant to be checked into
git, regenerated rather than hand-edited.

### Supply-chain auditing (`cmaker audit`)

`cmaker.lock`'s exact resolved commits make a real question askable:
"what am I actually pulling in, and is any of it known-bad?"

```bash
cmaker audit
```

Queries [OSV.dev](https://osv.dev) for known vulnerabilities affecting each
dependency's exact locked *commit* (not just its tag — a tag can move, a
commit can't), and looks up each GitHub-hosted dependency's declared
license. Exits non-zero if any known vulnerability is found, so it's
CI-usable the same way `cmaker test`/`cmaker fmt --check` are:

```
$ cmaker audit
-- Auditing 1 locked dependencies against OSV.dev...
fmt (fmtlib/fmt@40626af8) - license: MIT
  no known vulnerabilities
-- No known vulnerabilities found.
```

License lookups are also available from `cmaker list --licenses` (a
network call per dependency, so it's opt-in — plain `cmaker list` stays
instant and offline).

---

## Extensibility: your own templates and registry entries

The built-in templates and package registry are deliberately small and
curated. For your own or your team's internal libraries and starter
projects, you don't need to wait on a cmaker release or fork the repo —
drop files in one of two well-known locations and cmaker picks them up
automatically:

| Location | Scope | Applies to |
|---|---|---|
| `~/.cmaker/templates/<name>/` | Every project on your machine | `cmaker new --template=<name>` |
| `.cmaker/templates/<name>/` (relative to cwd) | Just this project/repo | `cmaker new --template=<name>` |
| `~/.cmaker/registry.yaml` | Every project on your machine | `cmaker install <name>` / `cmaker search` |

**Custom templates** use the exact same shape as a built-in template: a
`meta.yaml` plus whatever source files you want copied into the scaffolded
project.

```yaml
# ~/.cmaker/templates/my-http-template/meta.yaml
name: my-http-template
description: Our team's internal HTTP service starter
cpp_version: 20
# dependencies:, link_libraries:, cmake_extra: all work the same as a
# built-in template's meta.yaml
```

```
~/.cmaker/templates/my-http-template/
├── meta.yaml
└── src/
    └── main.cpp
```

```bash
cmaker templates                              # lists it alongside the built-ins, labeled [user (~/.cmaker/templates)]
cmaker new myservice --template=my-http-template
```

**Custom registry entries** use the same shape as an entry in cmaker's
built-in `entries.yaml`:

```yaml
# ~/.cmaker/registry.yaml
- name: my-internal-lib
  repo: myorg/my-internal-lib
  default_tag: v1.0.0
  link: [my-internal-lib::my-internal-lib]
  notes: our team's internal library
```

```bash
cmaker search internal   # finds it, labeled [user (~/.cmaker/registry.yaml)]
cmaker install my-internal-lib
```

**Precedence** (most to least specific): project-local template
(`.cmaker/templates/`) > user-local template (`~/.cmaker/templates/`) >
built-in. A name that collides with a built-in — including `default` —
overrides it entirely rather than erroring, which is also how you'd pin a
different version of a library the built-in registry already knows about
(e.g. a `fmt` entry in `~/.cmaker/registry.yaml` overrides the built-in
`fmt` entry's tag). A missing or malformed `~/.cmaker/registry.yaml`, or a
`meta.yaml` that fails to parse, is silently skipped rather than treated as
an error — it's an optional personal/team overlay, not a required file.

`cmaker templates` and `cmaker search` label every non-built-in result
with where it came from, so it's always clear whether you're looking at
cmaker's own curated list or your own overlay.

---

## Compiler selection

If a machine has more than one C/C++ toolchain installed, `cmaker doctor`
lists every `clang`/`clang++`/`gcc`/`g++` (versioned or not) it can find on
`PATH` plus common LLVM install locations:

```bash
cmaker doctor
# ...
# Detected toolchains (use with 'cmaker build --compiler=<path>'):
#   -- /opt/homebrew/bin/g++-15
#   -- /usr/bin/clang++
```

Pick one either permanently (`compiler:` in `cmaker.yaml`, saved via
`cmaker new --compiler=...`) or for a single build:

```bash
cmaker build --compiler=clang++-17
```

`cmaker` validates the compiler actually supports your configured
`cpp_version`/`c_version` *before* invoking CMake, so a bad `--compiler`
fails with a clear one-line message instead of a cryptic CMake error.

**Zig as your C/C++ compiler**: pass `--compiler=zig` — `cmaker` wires this
up correctly via CMake's `CMAKE_<LANG>_COMPILER_ARG1` mechanism (`zig` isn't
invoked as `zig cc`/`zig c++` any other way CMake understands), and it's the
one compiler override that works for *both* C and C++ in a hybrid project
at once.

The [TUI](#the-interactive-dashboard-tui)'s New Project wizard surfaces the
same detection interactively: when more than one toolchain is found, it
adds a compiler-picker step after the template picker instead of making
you already know a path to type.

---

## Build speed: parallelism and compiler caching

`cmaker build` always passes `-j <NumCPU>` to `cmake --build` — no more
accidental single-threaded builds. Override it per-invocation with
`--jobs`/`-j`:

```bash
cmaker build --jobs=4
```

If `ccache` or `sccache` is on `PATH`, `cmaker` automatically wires it in as
`CMAKE_C_COMPILER_LAUNCHER`/`CMAKE_CXX_COMPILER_LAUNCHER` on every
configure — a real, free speed-up on rebuilds (especially for the
CPM-fetched-dependency templates, which otherwise recompile their
dependency from source every clean build). `cmaker doctor` reports whether
one was found and is active. Opt out per-project if you don't want it:

```yaml
disable_ccache: true
```

Every configure also gets `-DCMAKE_EXPORT_COMPILE_COMMANDS=ON` for free —
`build/compile_commands.json` is what makes `cmaker lint` (and any
IDE/clangd pointed at the project) work.

---

## Formatting and linting (`cmaker fmt` / `cmaker lint`)

Every scaffolded project gets a `.clang-format` and a `.clang-tidy` (a
deliberately curated, low-noise check list — `bugprone-*`, `performance-*`,
`clang-analyzer-*`, plus a couple of high-value `modernize-*`/`readability-*`
checks) so there's one blessed way to format/lint a cmaker project instead
of everyone hand-rolling their own invocation:

```bash
cmaker fmt          # clang-format -i, project-wide
cmaker fmt --check  # dry-run: non-zero exit if anything would change (CI-friendly)
cmaker lint         # clang-tidy, using build/compile_commands.json
```

`cmaker lint` needs `build/compile_commands.json` to exist — run `cmaker
build` at least once first.

---

## Coverage, benchmarks, docs, and Docker

Four more independent, opt-in pieces of build tooling.

### Coverage (`cmaker coverage`)

```yaml
coverage: true
```

Add this to `cmaker.yaml`, then:

```bash
cmaker coverage
```

Builds with `--coverage` (gcov-compatible, works identically whether the
project builds with gcc or clang), runs `ctest` if `testing.enabled` (or
the main executable/library demo otherwise) to generate coverage data, and
uses [gcovr](https://gcovr.com) to produce an HTML report at
`build/coverage/index.html`. Requires `gcovr` (`cmaker doctor` checks for
it).

### Benchmarks (`--with-benchmarks` / `cmaker bench`)

```bash
cmaker new myproj --with-benchmarks
cmaker bench
```

Scaffolds a real, working `bench/bench_main.cpp` (a
[Google Benchmark](https://github.com/google/benchmark) example, not a
stub) and wires it into the build as a `<name>_bench` executable —
`cmaker bench` builds it (Release, since Debug numbers aren't meaningful)
and runs it.

### API docs (`--with-docs` / `cmaker docs`)

```bash
cmaker new myproj --with-docs
cmaker docs
```

Scaffolds a minimal `Doxyfile` and builds `docs/html/index.html` from it
via [Doxygen](https://www.doxygen.nl). `cmaker docs` also works without
`--with-docs` having been used first — it writes a default `Doxyfile` on
the fly if none exists.

### Docker (`--with-docker`)

```bash
cmaker new myproj --with-docker
docker build -t myproj .
```

Scaffolds a `Dockerfile` (+ `.dockerignore`, + a matching
`.devcontainer/devcontainer.json`) that builds the project with plain
`cmake`/a compiler inside the container — a real "clone and it just
works, even without a local toolchain" story, verified end-to-end with a
real `docker build` + `docker run`. cmaker itself doesn't need to be
installed in the container, since `CMakeLists.txt` is already generated
and checked in.

---

## Sanitizers and warnings-as-errors

```yaml
sanitizers: [address, undefined]   # or: thread, memory, leak
warnings_as_errors: true
```

Add either (or both) to `cmaker.yaml` and the next build compiles with
`-fsanitize=...`/`-Wall -Wextra -Werror` automatically — no manual
`CMakeLists.txt` editing.

---

## Testing (`ctest`)

Opt in with `testing: { enabled: true }` in `cmaker.yaml` — the `catch2`
template does this for you automatically:

```yaml
testing:
  enabled: true
```

This wires `enable_testing()` and `add_test(NAME <executable> COMMAND
<executable>)` into the generated `CMakeLists.txt`, registering your main
executable itself as the test run — the model the `catch2` template
already uses (its `main()` *is* the Catch2 test runner). Then:

```bash
cmaker test              # build, then run ctest --output-on-failure
cmaker test --release    # same, but a Release build first
```

`cmaker test` fails fast with a clear message if `testing.enabled` isn't
set, instead of surfacing `ctest`'s "No tests were found!!!" error. Exit
code and `--output-on-failure` output are forwarded straight from `ctest`,
so it composes with CI the same way `cmaker build`/`cmaker run` do. This
covers the single-executable case cmaker supports today — dedicated test
targets separate from the main executable (multi-target projects) are
tracked in `ROADMAP.md` §16.

Known gap: `testing: { enabled: true }` assumes the target itself is
runnable, so it only works for `target_type: executable` (the default).
Setting it on a `static_library`/`shared_library` member generates an
`add_test()` that ctest can't actually run (a library isn't an
executable) — use an executable elsewhere in the project (or workspace
member, see below) as the test binary instead, the same pattern the
`catch2` template already uses.

---

## Workspaces (monorepo support)

A workspace root `cmaker.yaml` groups several ordinary cmaker projects —
typically an app plus one or more internal libraries (§16's
`target_type: static_library`/`shared_library`) — into one repo, built and
versioned together:

```yaml
# cmaker.yaml (workspace root - no 'executable:'/'target_type:' of its own)
project_name: myworkspace
workspace:
  members:
    - libs/core   # a static_library target
    - app         # an executable that links against 'core'
```

Each member directory (`libs/core/`, `app/`) is a completely normal
cmaker project with its own `cmaker.yaml` — scaffold each with a plain
`cmaker new libs/core --template=default` (then set `target_type:
static_library`) and `cmaker new app`, same as any standalone project. A
member that depends on a sibling member just names that sibling's target
in its own `libraries:`:

```yaml
# app/cmaker.yaml
project_name: app
executable: myapp
libraries:
  - core   # resolved via CMake's own add_subdirectory, not a second CPM fetch
```

**Member order matters**: CMake requires a target to already exist by the
time `target_link_libraries` references it, so a library member must be
listed in `workspace.members` before any member that links against it.

From the workspace root:

```bash
cmaker build                  # regenerates every member's CMakeLists.txt + the
                               # root's, then configures and builds the whole
                               # tree as a single ./build
cmaker build --member=app     # build (or rebuild) just one member's target
cmaker run --member=app       # build (if needed) and run one member's executable
                               # ('run' has no default member - it's required)
cmaker test                   # ctest across every member with testing.enabled
cmaker test --member=app      # ctest scoped to just one member's build subdir
```

A built executable lands at `build/<member>/<executable>` (CMake mirrors
the source tree under `build/`), e.g. `build/app/myapp` above. Compiler
and ccache/sccache settings (`compiler:`, `disable_ccache:`) apply
workspace-wide from the root `cmaker.yaml` only — CMake locks in one
compiler per configure, so individual members can't each override it in
workspace mode. `cmaker clean` works unchanged (it just removes the whole
`build/` directory).

---

## Rust and Zig interop

Add a Rust crate or a Zig library to *any* template in one shot — not just
the generic `default` one:

```bash
cmaker new myapp --with-rust             # adds rust/ + a demo calling into it
cmaker new myapp --with-zig              # adds zig/ + a demo calling into it
cmaker new myapp --with-rust --with-zig  # both, in one combined main()
cmaker create myapi --backend --with-rust  # cpp-httplib server + a linked Rust crate
```

Each scaffolds a small crate/library exposing a plain C-ABI function
(`rust_add`/`zig_add`) and a matching hand-written C header. Under the
hood: `cargo build --release` / `zig build-lib` run as CMake custom
commands, and the resulting static library links straight into your
executable.

**How the demo shows up depends on the template.** The `default` template's
`main()` is just a placeholder, so `--with-rust`/`--with-zig` rewrite it
into a real, working example that calls into the crate — not a stub. Any
other template (`--backend`, `--ml`, `raylib`, ...) has its *own* real
code (an HTTP server, a linear-algebra demo, ...) that never gets
overwritten: the crate is scaffolded and linked exactly the same way, but
you get a real `#include` for it plus a short comment showing how to call
it, appended right after the template's existing includes — e.g.
`cmaker create myapi --backend --with-rust` leaves the cpp-httplib service
fully intact and adds:

```cpp
#include <httplib.h>
#include <iostream>
#include "rustlib.h"

// --- cmaker: linked native crate(s) available, see below ---
// Rust (rust/src/lib.rs) is linked into this target - call it like:
//   int sum = rust_add(2, 3);
```

`cmaker doctor` only checks for `cargo`/`rustc`/`zig` when your project's
`cmaker.yaml` actually declares `rust.enabled`/`zig.enabled` — a plain
project never sees (or pays for) a toolchain check it doesn't need.

**Known limits today:** the Rust/Zig crate/library name is fixed
(`rustlib`/`ziglib`); and a hybrid project's `--compiler` override only
covers the C++ side unless you use `--compiler=zig` (see above). See
`ROADMAP.md` §12 for what's still open (a typed `cxx`-bridge option, "zig as
compiler" verification on non-macOS setups).

---

## Ad hoc single-file compiles (`--only`)

For a scratch file or an isolated experiment you don't want wired into your
main executable target:

```bash
cmaker run --only=scratch/idea.cpp
cmaker build --only=scratch/idea.cpp   # compile without running
```

This compiles the single file straight to `build/.cmaker_scratch/<name>`
using your project's include dirs and configured standard/compiler, without
touching `CMakeLists.txt` or your main build at all. The file needs its own
`main()` and must be self-contained — no linked dependencies get pulled in
for an ad hoc compile.

### Using a custom compile-and-run tool (e.g. `crun`)

Some tools don't behave like a plain compiler — they compile *and* run a
file in one step (`crun main.cpp`), so there's no separate binary path for
cmaker to invoke afterwards. For those, set `runner:` instead of `compiler:`,
either in `cmaker.yaml` or via `--runner` for a single invocation:

```bash
cmaker run --only=main.cpp --runner=crun
```

```yaml
runner: crun   # cmaker.yaml — applies to every 'run --only' from here on
```

When a runner is set, `cmaker run --only` skips its own compile step
entirely and just executes `<runner> <file> [args...]` directly, streaming
output straight through. `runner` takes priority over `compiler` when both
are set. Note this only applies to `run --only`, not `build --only` — a
runner has no separate "compile only" mode to call into.

`runner:` also applies to a plain `cmaker run` (no `--only`) for the whole
project: if set, it skips the CMake build entirely and runs `<runner>
src/main.cpp` (or `src/main.c`) directly instead. The easiest way to get
this is to scaffold it up front:

```bash
cmaker create demo --with=crun
cd demo
cmaker run          # always invokes `crun src/main.cpp`, no CMake build step
```

`--with` just sets `runner:` in the generated `cmaker.yaml` — you can add or
remove it from any existing project by editing that field directly.

---

## Named configs (your own shortcuts)

Save any `cmaker` invocation as a shortcut, then run it by name — a small
task runner in the spirit of `npm run <script>` or `just`:

```bash
cmaker add config scratch 'run --only=tests/scratch.cpp'
cmaker scratch               # runs the saved command
cmaker configs                # lists everything you've saved
cmaker remove config scratch  # deletes it
```

Saved shortcuts live in `cmaker.yaml`'s `configs:` map, so they travel with
the project. `cmaker add config` refuses to overwrite the name of a real
built-in command (`build`, `run`, `doctor`, ...) so a saved shortcut can
never become silently unreachable. They also show up automatically in the
TUI sidebar — no separate setup needed there.

---

## Code generation (`generate accessors`)

Generate getter/setter accessors for a class's private members, using an
LLM to do the one part a hand-rolled C++ parser is genuinely bad at
(understanding types, constness, and which members are non-public):

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cmaker generate accessors Pqr.cpp Abc
# or:
cmaker generate accessors --file=include/Pqr.hpp --class=Abc --dry-run
```

The LLM is only ever trusted to *identify* members (name, type, constness,
whether the getter should return by reference) — it never writes C++ code
directly into your file. `cmaker` renders the actual getter/setter text
itself from a fixed template, so the generated code is always consistent
and reviewable, not whatever the model happened to write that run.

Output is inserted into the class body just before its closing brace,
wrapped in a greppable marker comment:

```cpp
    // --- cmaker generated accessors: begin ---
    public:
    int getAge() const { return age_; }
    void setAge(int value) { age_ = value; }
    // --- cmaker generated accessors: end ---
```

Re-running the command on the same class finds and replaces this block in
place instead of duplicating it — safe to run again after adding or
renaming members.

- `--model string` — override the Anthropic model (default: a fast/cheap
  model, since this is a small structured-extraction task, not a large
  generation one)
- `--dry-run` — print what would be generated without writing the file
- Requires the `ANTHROPIC_API_KEY` environment variable; cmaker talks
  directly to the Anthropic Messages API (no separate provider config).
- Const members get a getter only, since there's nothing sensible to
  mutate. Non-trivial types (`std::string`, containers, other class types)
  get a `const Type&` getter/setter parameter instead of copying by value.

---

## Build/run logs and AI-assisted healing (`cmaker logs` / `cmaker heal`)

Every `cmaker build`/`cmaker run` captures its combined output under
`.cmaker/logs/` (the last 5 by default — `logs_keep:` in `cmaker.yaml` to
change that), independently of any AI feature — useful on its own:

```bash
cmaker logs        # list recent attempts, newest first
cmaker logs 1       # print the most recent one in full
```

When a build (or a run) actually fails, `cmaker heal` reads the most recent
failing log, the file(s) the compiler's error output pointed at, and asks
an LLM (Anthropic; requires `ANTHROPIC_API_KEY`) to suggest a fix:

```bash
cmaker heal
```

This is deliberately **suggest, don't touch**: nothing is ever written to
disk. The model is only ever trusted to propose corrected file content —
`cmaker` computes the actual diff itself (a real, deterministic line-based
diff, not text trusted verbatim from the model) and prints it for you to
review and apply by hand (or via `git apply`). Two things this design
guards against, both caught by testing against a real model rather than
assumed away:

- **The model can get unified-diff arithmetic wrong.** An early version
  asked the LLM to hand-write the diff directly; a live test produced a
  hunk header whose line count didn't match its own body, which `git
  apply` rejected as corrupt. Asking for full corrected file content
  instead (a task models are actually good at) and diffing it in Go
  sidesteps that entirely — the diff's structure is never in question.
- **A stray trailing line can silently become part of "the fix."** Nothing
  bounds the end of the last file block in a response, so if a model
  echoes an extra line out of habit, it would otherwise land in the
  patched file unnoticed. `cmaker heal` defends against this and against
  markdown code fences the model adds despite being told not to.

```bash
cmaker heal --kind=build   # only consider build failures, not run
cmaker heal --model=...    # override the Anthropic model used
```

**`cmaker heal --apply`** writes the suggested fix to disk, but with real
guardrails, not a blind auto-apply:

```bash
cmaker heal --apply
```

- Refuses outright unless the git working tree is clean (`git status
  --porcelain` is empty) — an LLM-proposed patch never lands on top of
  already-dirty state, where a partial apply or a later revert would
  become ambiguous about what came from you vs. the patch.
- If this exact failure hasn't been diagnosed yet, it runs the same
  diagnosis as plain `cmaker heal`, prints the diff, and **asks you to
  confirm** (`Apply this diff? [y/N]`) before touching anything — nothing
  is applied without an explicit `y`.
- If you already ran `cmaker heal` (or a prior `--apply` you declined) for
  this exact failing log, it reuses that already-reviewed diagnosis
  directly — no second LLM call, no re-asking for confirmation, since you
  already saw the diff once.
- After applying, it immediately rebuilds and reports whether the fix
  actually worked — "the diff applied cleanly" and "the build now
  succeeds" are different claims, and only the second one matters. If the
  rebuild still fails, the diff is left applied (so you can inspect it
  with `git diff`) rather than silently reverted.

The diagnosis is cached per failing log (`.cmaker/heal/`, gitignored) so
the "already reviewed" reuse above works across separate `cmaker heal`
invocations; it's cleared the moment a diff is actually applied, so a
second `--apply` for the same log path always re-diagnoses rather than
risking a stale reapply.

---

## Natural-language scaffolding (`--describe`)

The flip side of `cmaker heal`: instead of fixing broken code, describe a
project in plain English and let an LLM pick which of cmaker's *existing*
building blocks fit it — template, `--with-rust`/`--with-zig`, and
[§17 packages](#package-install-a-real-dependency-manager-ux) to install:

```bash
cmaker new myapi --describe "a REST API backend that returns JSON, written in C++"
```

```
-- Asking claude-haiku-4-5-20251001 to plan a project for: "a REST API backend that returns JSON, written in C++"
-- Plan: template=backend language=cpp with_rust=false with_zig=false target_type=executable
-- Packages: nlohmann-json
-- Reasoning: The backend template provides cpp-httplib for HTTP request handling, and
   nlohmann-json is the standard for JSON serialization/deserialization in C++...
-- Initializing myapi (Template: backend, Language: cpp)...
-- Installing planned package "nlohmann-json"...
```

The model's job is narrowly **selecting from a menu**, never writing code —
the same principle `generate accessors` and `cmaker heal` already use. This
keeps the blast radius of a bad decision small: worst case, it picks a
slightly wrong template or package (delete the directory and try again),
never that it generates hand-rolled, unreviewed application logic. Every
field it returns is validated against cmaker's real template list, package
registry, and known language/target-type values before anything is
scaffolded — an unrecognized template/language fails clearly rather than
guessing, an unrecognized package is quietly dropped, and a combination
that violates `cmaker.yaml`'s actual composition rules (e.g. a library
`target_type` picked alongside a non-`default` template) is normalized
back to something valid rather than failing the whole plan over one field.

`--describe` conflicts with `--template`/`--lang`/`--with-rust`/
`--with-zig`/`--target-type`/`--lib`/`--backend`/`--ml` — it picks all of
those for you, so combining it with an explicit one is a clear error
rather than a silent override. Works on `cmaker new`, `cmaker init`, and
`cmaker create`. Requires `ANTHROPIC_API_KEY`.

Unlike `cmaker heal`, there's no separate `--apply` step: the plan is
printed and then acted on in one command, matching how every other
`cmaker new` invocation already behaves — scaffolding into a fresh
directory is inherently low-risk and trivially reversible, unlike patching
a user's existing source.

---

## The interactive dashboard (TUI)

Run bare `cmaker` inside a terminal (with no subcommand) and you get a
full-screen dashboard instead of a help page — arrow keys + Enter to
navigate, live streaming output for build/run/watch, a New Project wizard
(name → template picker → compiler picker, when there's one to make), and
your saved named configs listed right alongside the built-in commands.

The compiler picker step only shows up when more than one toolchain is
actually detected on your machine (the same detection `cmaker doctor`
reports) — with 0 or 1 found, there's nothing to choose between, so the
wizard goes straight from template to creating the project, exactly like
before this step existed. Picking anything other than "(default)" passes
`--compiler=<path>` through to `cmaker new`, same as running it from the
CLI yourself.

```bash
cmaker            # launches the dashboard if stdout is a terminal
cmaker tui        # launches it explicitly, always
```

Piped/non-interactive usage (CI, scripts) automatically falls back to the
regular `--help` output instead — the dashboard never gets in the way of
scripted usage.

---

## Shell completions

Completions are generated on demand by cobra — no separate install step:

```bash
cmaker completion bash   > /usr/local/etc/bash_completion.d/cmaker
cmaker completion zsh    > "${fpath[1]}/_cmaker"
cmaker completion fish   > ~/.config/fish/completions/cmaker.fish
```

(Homebrew installs will wire these up automatically once a real tap
release exists — see `PUBLISHING.md`.)

---

## Global flags

Available on every command:

| Flag | Effect |
|---|---|
| `-v`, `--verbose` | Print extra diagnostic output (e.g. the parsed config, full CMake logs on scaffold failures) |
| `-q`, `--quiet` | Suppress cmaker's own progress messages (cmake/compiler output still comes through) |
| `--no-color` | Disable ANSI colors |
| `--version` | Print the cmaker version |
| `-h`, `--help` | Show help for the current command |

---

## Command reference

<details>
<summary><code>cmaker new [name] [flags]</code></summary>

Scaffold a new project into `./<name>` (defaults to `MyProject` if omitted).

- `--template string` — project template (default `"default"`)
- `--lang string` — `cpp`, `c`, or `hybrid` (default `"cpp"`; only with `--template=default`)
- `--compiler string` — compiler to save into `cmaker.yaml`
- `--with-rust` — add a Rust crate, linked into whichever template you pick (see [above](#rust-and-zig-interop))
- `--with-zig` — add a Zig library, linked into whichever template you pick (see [above](#rust-and-zig-interop))
- `--with string` — always run this project via a custom compile-and-run tool (e.g. `crun`), saved as `runner:` in `cmaker.yaml` (see [above](#using-a-custom-compile-and-run-tool-eg-crun))
- `--lib` — shorthand for `--target-type=static_library` (see [above](#library-project-targets); only with `--template=default`, cpp)
- `--target-type string` — `executable`, `static_library`, or `shared_library` (default `"executable"`; only with `--template=default`, cpp)
- `--describe string` — describe the project in plain English and let an LLM pick the rest for you (see [above](#natural-language-scaffolding---describe); conflicts with the other scaffold flags)
- `--with-benchmarks` — add a `bench/` directory wired to Google Benchmark (see [above](#coverage-benchmarks-docs-and-docker))
- `--with-docs` — scaffold a `Doxyfile` (see [above](#coverage-benchmarks-docs-and-docker))
- `--with-docker` — scaffold a `Dockerfile` + devcontainer (see [above](#coverage-benchmarks-docs-and-docker))
</details>

<details>
<summary><code>cmaker init [flags]</code></summary>

Same flags as `new`, but scaffolds into the current directory instead of a
new subdirectory. Project name is inferred from the directory's basename.
</details>

<details>
<summary><code>cmaker create <name> [flags]</code></summary>

Same flags as `new`, plus:

- `--backend` — shorthand for `--template=backend` (a cpp-httplib HTTP service)
- `--ml` — shorthand for `--template=ml-eigen` (an Eigen linear-algebra starter)

Both compose with `--with-rust`/`--with-zig` (see
[above](#rust-and-zig-interop)) and with each other's mutual exclusivity
enforced — `--backend --ml` and `--backend --template=raylib` both fail
with a clear error instead of silently picking one.
</details>

<details>
<summary><code>cmaker build [flags]</code></summary>

- `--release` — build with `CMAKE_BUILD_TYPE=Release` (`-O3`)
- `--compiler string` — override the compiler for this build only
- `--only string` — compile a single source file ad hoc (see [above](#ad-hoc-single-file-compiles---only))
- `--jobs int` / `-j` — parallel build jobs (default: number of CPUs, see [above](#build-speed-parallelism-and-compiler-caching))
- `--member string` — workspace root only: build just this member instead of the whole workspace (see [above](#workspaces-monorepo-support))
</details>

<details>
<summary><code>cmaker run [-- args...] [flags]</code></summary>

Builds only if a tracked source file is newer than the existing binary,
then runs it. Args after `--` are forwarded to your program. On a library
project (see [above](#library-project-targets)), builds and runs its
`examples/demo.cpp` demo instead, or fails with a clear error if there
isn't one.

- `--only string` — compile and run a single source file ad hoc
- `--compiler string` — compiler to use for `--only`
- `--runner string` — custom compile-and-run tool to invoke instead (e.g. `crun`), overriding `cmaker.yaml`'s `runner` — applies to `--only` and to a whole-project `run` (see [above](#using-a-custom-compile-and-run-tool-eg-crun))
- `--member string` — workspace root only: which member to run - required in workspace mode, there's no default (see [above](#workspaces-monorepo-support))
</details>

<details>
<summary><code>cmaker test [flags]</code></summary>

Builds (if needed), then runs `ctest --output-on-failure`. Requires
`testing.enabled: true` in `cmaker.yaml` (see [above](#testing-ctest));
fails fast with a clear error otherwise instead of `ctest`'s own
"No tests were found!!!". In workspace mode, requires at least one
member to have `testing.enabled: true`.

- `--member string` — workspace root only: scope ctest to just this member's build subdirectory (see [above](#workspaces-monorepo-support))

- `--release` — build with `CMAKE_BUILD_TYPE=Release` (`-O3`) before testing
</details>

<details>
<summary><code>cmaker clean</code></summary>

Removes and recreates `build/`.
</details>

<details>
<summary><code>cmaker watch</code></summary>

Rebuilds and reruns automatically whenever files in `src/`, `include/`, or
`cmaker.yaml` change (debounced 200ms). Ctrl+C to stop.
</details>

<details>
<summary><code>cmaker doctor</code></summary>

Checks `cmake`/`make`/`ninja`/`clang++`/`g++`/`vcpkg`/`conan`/`ccache`/
`sccache`/`clang-format`/`clang-tidy`/`gcovr`/`doxygen`, lists every
detected compiler toolchain and whether a compiler cache is actively
wired in, and — only if your project's `cmaker.yaml` declares it needs
them — checks `cargo`/`rustc`/`zig` too.
</details>

<details>
<summary><code>cmaker fmt [--check]</code></summary>

Formats every tracked source file with `clang-format` (see
[above](#formatting-and-linting-cmaker-fmt--cmaker-lint)).

- `--check` — dry-run: don't write anything, exit non-zero if formatting would change (CI-friendly)
</details>

<details>
<summary><code>cmaker lint</code></summary>

Lints every tracked source file with `clang-tidy`, using
`build/compile_commands.json` as its compilation database. Run `cmaker
build` first if it doesn't exist yet.
</details>

<details>
<summary><code>cmaker coverage</code></summary>

Builds with `--coverage`, runs the project (or `ctest` if configured), and
produces an HTML coverage report via `gcovr` (see
[above](#coverage-benchmarks-docs-and-docker)). Requires `coverage: true`
in `cmaker.yaml`.
</details>

<details>
<summary><code>cmaker bench</code></summary>

Builds (Release) and runs `bench/*.cpp` via Google Benchmark (see
[above](#coverage-benchmarks-docs-and-docker)). Requires
`--with-benchmarks` to have scaffolded a `bench/` directory first.
</details>

<details>
<summary><code>cmaker docs</code></summary>

Builds API documentation with Doxygen (see
[above](#coverage-benchmarks-docs-and-docker)). Writes a default
`Doxyfile` on the fly if none exists yet.
</details>

<details>
<summary><code>cmaker templates</code></summary>

Lists every available template with its description and what it fetches —
built-in plus any user-local (`~/.cmaker/templates/`) or project-local
(`.cmaker/templates/`) templates, labeled by source (see
[Extensibility](#extensibility-your-own-templates-and-registry-entries)).
</details>

<details>
<summary><code>cmaker add config <name> '<cmaker args...>'</code></summary>

Saves a named shortcut. Fails if `<name>` collides with a real subcommand.
</details>

<details>
<summary><code>cmaker remove config <name></code></summary>

Deletes a saved named shortcut.
</details>

<details>
<summary><code>cmaker configs</code></summary>

Lists every saved named shortcut for the current project.
</details>

<details>
<summary><code>cmaker install &lt;name&gt; [flags]</code></summary>

Adds a dependency (see [above](#package-install-a-real-dependency-manager-ux))
and fetches it immediately.

- `--git string` — install a git-hosted library not in the built-in registry, by URL (requires `--tag`)
- `--tag string` — git tag/branch to fetch (required with `--git`)
- `--link strings` — CMake target(s) to link, e.g. `--link=fmt::fmt` (required with `--git`; comma-separate for multiple)
- `--options strings` — extra `CPMAddPackage` `OPTIONS` lines (only with `--git`)
- `--download-only` — fetch source but don't `add_subdirectory` it (only with `--git`)
</details>

<details>
<summary><code>cmaker uninstall &lt;name&gt;</code></summary>

Removes a dependency from `cmaker.yaml` (and its `cmaker.lock` entry, if any).
</details>

<details>
<summary><code>cmaker list</code> (alias <code>cmaker installed</code>)</summary>

Lists every dependency currently declared in `cmaker.yaml`.

- `--licenses` — also look up each GitHub-hosted dependency's declared license (a network call per dependency)
</details>

<details>
<summary><code>cmaker search &lt;term&gt;</code></summary>

Searches the package registry by name or description — built-in plus any
user-local overlay (`~/.cmaker/registry.yaml`), labeled by source (see
[Extensibility](#extensibility-your-own-templates-and-registry-entries)).
</details>

<details>
<summary><code>cmaker audit</code></summary>

Checks `cmaker.lock`'s locked dependencies for known vulnerabilities (via
OSV.dev, by exact commit) and surfaces licenses (see
[above](#supply-chain-auditing-cmaker-audit)). Requires `cmaker.lock` to
exist. Exits non-zero if any known vulnerability is found.
</details>

<details>
<summary><code>cmaker generate accessors [file] [class] [flags]</code></summary>

Generates getter/setter accessors for a class's non-public members (see
[above](#code-generation-generate-accessors)). Requires `ANTHROPIC_API_KEY`.

- `--file string` / `-f` — source/header file containing the class (or first positional arg)
- `--class string` / `-c` — class or struct name (or second positional arg)
- `--model string` — override the Anthropic model used
- `--dry-run` — print the generated accessors without writing the file
</details>

<details>
<summary><code>cmaker logs [n]</code></summary>

Lists recent `.cmaker/logs/` build/run captures (newest first), or prints
the full content of the nth one (see
[above](#buildrun-logs-and-ai-assisted-healing-cmaker-logs--cmaker-heal)).
</details>

<details>
<summary><code>cmaker heal [flags]</code></summary>

Suggests a fix for the most recent build/run failure (see
[above](#buildrun-logs-and-ai-assisted-healing-cmaker-logs--cmaker-heal)).
Requires `ANTHROPIC_API_KEY`. Nothing is written to disk unless `--apply`
is given.

- `--kind string` — only consider `build` or `run` failures (default: either, most recent wins)
- `--model string` — override the Anthropic model used
- `--apply` — apply the suggested fix (after confirmation, unless reusing an already-reviewed diagnosis) and rebuild to verify it; requires a clean git working tree
</details>

<details>
<summary><code>cmaker tui</code></summary>

Launches the interactive dashboard explicitly (same as bare `cmaker` in a
terminal).
</details>

---

## Project layout cmaker creates

```
myapp/
├── cmaker.yaml          # the one file you hand-edit
├── cmaker.lock           # generated once you 'cmaker install' anything - commit this
├── CMakeLists.txt       # generated - don't hand-edit, cmaker regenerates it
├── .gitignore           # ignores build/
├── .clang-format         # used by 'cmaker fmt'
├── .clang-tidy           # used by 'cmaker lint'
├── src/                 # your .cpp/.c sources
├── include/              # your headers
├── build/                # cmake's build directory (safe to delete: cmaker clean)
├── .cmaker/logs/          # build/run log captures, gitignored (see 'cmaker logs'/'cmaker heal')
├── .cmaker/heal/          # cached 'cmaker heal' diagnosis, gitignored (see 'cmaker heal --apply')
├── rust/                 # only if --with-rust
├── zig/                  # only if --with-zig
├── bench/                # only if --with-benchmarks (see 'cmaker bench')
├── Doxyfile              # only if --with-docs (see 'cmaker docs')
└── Dockerfile            # only if --with-docker (+ .dockerignore, .devcontainer/)
```

---

## Contributing / roadmap

This project is developed against a living roadmap in `ROADMAP.md` —
every implemented feature is documented there along with what's verified,
what's a deliberate scope cut, and what's explicitly still open (e.g. a
typed Rust `cxx` bridge, ML backend templates beyond Eigen, `--backend`/
`--ml` domain scaffolds, real published Homebrew/goreleaser releases — see
`PUBLISHING.md` for that last one). Check there before assuming something
is or isn't supported.
