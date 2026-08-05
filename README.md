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
- [Dependencies (fetched automatically via CPM)](#dependencies-fetched-automatically-via-cpm)
- [Compiler selection](#compiler-selection)
- [Sanitizers and warnings-as-errors](#sanitizers-and-warnings-as-errors)
- [Rust and Zig interop](#rust-and-zig-interop)
- [Ad hoc single-file compiles (`--only`)](#ad-hoc-single-file-compiles---only)
- [Named configs (your own shortcuts)](#named-configs-your-own-shortcuts)
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
go build -o cmaker .
sudo mv cmaker /usr/local/bin/   # or anywhere on your PATH
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
sanitizers: [address, undefined]
warnings_as_errors: true
cmake_extra: |               # raw CMake, appended for anything the generator doesn't model
  message(STATUS "hello from cmake_extra")
configs:
  test: "run --only=tests/scratch.cpp"
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

## Rust and Zig interop

Add a Rust crate or a Zig library to any default-template C/C++/hybrid
project in one shot:

```bash
cmaker new myapp --with-rust             # adds rust/ + a demo calling into it
cmaker new myapp --with-zig              # adds zig/ + a demo calling into it
cmaker new myapp --with-rust --with-zig  # both, in one combined main()
```

Each scaffolds a small crate/library exposing a plain C-ABI function
(`rust_add`/`zig_add`), a matching hand-written C header, and rewrites your
project's `main()` to actually call into it — not a stub, a real working
example you build on. Under the hood: `cargo build --release` / `zig
build-lib` run as CMake custom commands, and the resulting static library
links straight into your executable.

`cmaker doctor` only checks for `cargo`/`rustc`/`zig` when your project's
`cmaker.yaml` actually declares `rust.enabled`/`zig.enabled` — a plain
project never sees (or pays for) a toolchain check it doesn't need.

**Known limits today:** `--with-rust`/`--with-zig` only compose with
`--template=default`; the Rust/Zig crate/library name is fixed
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
cmaker add config test 'run --only=tests/scratch.cpp'
cmaker test                 # runs the saved command
cmaker configs              # lists everything you've saved
cmaker remove config test   # deletes it
```

Saved shortcuts live in `cmaker.yaml`'s `configs:` map, so they travel with
the project. `cmaker add config` refuses to overwrite the name of a real
built-in command (`build`, `run`, `doctor`, ...) so a saved shortcut can
never become silently unreachable. They also show up automatically in the
TUI sidebar — no separate setup needed there.

---

## The interactive dashboard (TUI)

Run bare `cmaker` inside a terminal (with no subcommand) and you get a
full-screen dashboard instead of a help page — arrow keys + Enter to
navigate, live streaming output for build/run/watch, a New Project wizard
(name → template picker), and your saved named configs listed right
alongside the built-in commands.

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
- `--with-rust` — add a Rust crate (only with `--template=default`)
- `--with-zig` — add a Zig library (only with `--template=default`)
- `--with string` — always run this project via a custom compile-and-run tool (e.g. `crun`), saved as `runner:` in `cmaker.yaml` (see [above](#using-a-custom-compile-and-run-tool-eg-crun))
</details>

<details>
<summary><code>cmaker init [flags]</code></summary>

Same flags as `new`, but scaffolds into the current directory instead of a
new subdirectory. Project name is inferred from the directory's basename.
</details>

<details>
<summary><code>cmaker create <name> [flags]</code></summary>

Same flags as `new`, plus:

- `--backend` — *not implemented yet* (see `ROADMAP.md` §13)
- `--ml` — *not implemented yet* (see `ROADMAP.md` §13)

These two fail with a clear error rather than silently no-opping, so the
CLI surface exists ahead of the feature landing.
</details>

<details>
<summary><code>cmaker build [flags]</code></summary>

- `--release` — build with `CMAKE_BUILD_TYPE=Release` (`-O3`)
- `--compiler string` — override the compiler for this build only
- `--only string` — compile a single source file ad hoc (see [above](#ad-hoc-single-file-compiles---only))
</details>

<details>
<summary><code>cmaker run [-- args...] [flags]</code></summary>

Builds only if a tracked source file is newer than the existing binary,
then runs it. Args after `--` are forwarded to your program.

- `--only string` — compile and run a single source file ad hoc
- `--compiler string` — compiler to use for `--only`
- `--runner string` — custom compile-and-run tool to invoke instead (e.g. `crun`), overriding `cmaker.yaml`'s `runner` — applies to `--only` and to a whole-project `run` (see [above](#using-a-custom-compile-and-run-tool-eg-crun))
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

Checks `cmake`/`make`/`ninja`/`clang++`/`g++`/`vcpkg`/`conan`, lists every
detected compiler toolchain, and — only if your project's `cmaker.yaml`
declares it needs them — checks `cargo`/`rustc`/`zig` too.
</details>

<details>
<summary><code>cmaker templates</code></summary>

Lists every available template with its description and what it fetches.
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
<summary><code>cmaker tui</code></summary>

Launches the interactive dashboard explicitly (same as bare `cmaker` in a
terminal).
</details>

---

## Project layout cmaker creates

```
myapp/
├── cmaker.yaml          # the one file you hand-edit
├── CMakeLists.txt       # generated - don't hand-edit, cmaker regenerates it
├── .gitignore           # ignores build/
├── src/                 # your .cpp/.c sources
├── include/              # your headers
├── build/                # cmake's build directory (safe to delete: cmaker clean)
├── rust/                 # only if --with-rust
└── zig/                  # only if --with-zig
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
