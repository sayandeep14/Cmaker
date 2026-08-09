#!/usr/bin/env bash
#
# cmaker installer for macOS and Linux (64-bit only for now - Windows has
# its own scripts/install.ps1). Builds cmaker from source via the local Go
# toolchain (no published prebuilt release exists yet), installs it to a
# per-user directory (no sudo required for cmaker's own binary - system
# prerequisite installs below are a different matter, see step 7), wires
# that directory onto PATH, checks for cmake/a C/C++ compiler+gdb/cargo+
# rustc/zig and offers to install whichever are missing (each with its own
# confirmation prompt - nothing runs without you saying yes), and
# optionally collects + validates an ANTHROPIC_API_KEY for cmaker's
# AI-assisted features (heal, describe/improvise, explain, generate
# accessors) - all of which work fine without one, just without those
# specific commands.
#
# Two ways to run this:
#   curl -fsSL https://raw.githubusercontent.com/sayandeep14/Cmaker/main/scripts/install.sh | bash
#     - no clone needed, fetches the source itself.
#   ./scripts/install.sh   (from an existing clone)
#     - builds from whatever's in that working tree, including uncommitted
#       local changes - the fast local-iteration path for repo developers.
#
# Override the install directory with: CMAKER_INSTALL_DIR=/some/path ./scripts/install.sh
set -euo pipefail

INSTALL_DIR="${CMAKER_INSTALL_DIR:-$HOME/.cmaker/bin}"
CONFIG_DIR="$HOME/.cmaker"
ENV_FILE="$CONFIG_DIR/env"
REPO_URL="https://github.com/sayandeep14/Cmaker.git"
REPO_TARBALL_URL="https://github.com/sayandeep14/Cmaker/archive/refs/heads/main.tar.gz"

info() { printf '\033[36m-- %s\033[0m\n' "$1"; }
ok()   { printf '\033[32m-- %s\033[0m\n' "$1"; }
warn() { printf '\033[33m-- %s\033[0m\n' "$1"; }
fail() { printf '\033[31m-- %s\033[0m\n' "$1" >&2; exit 1; }

# ---------- 1. platform check ----------

os="$(uname -s)"
case "$os" in
    Darwin|Linux) ;;
    *)
        fail "This installer currently only supports macOS and Linux (Windows: see scripts/install.ps1). For now, see README.md's 'Install' section to build from source manually."
        ;;
esac

arch="$(uname -m)"
case "$arch" in
    x86_64|amd64|arm64|aarch64) ;;
    *)
        fail "This installer only supports 64-bit systems (found architecture: $arch) - 32-bit is not implemented."
        ;;
esac
info "Detected $os ($arch)"

# ---------- 2. prerequisite: Go (needed to build cmaker itself) ----------

if ! command -v go >/dev/null 2>&1; then
    fail "Go is required to build cmaker (no prebuilt release exists yet - see ROADMAP.md §8). Install Go 1.25+ from https://go.dev/dl/ and re-run this script."
fi

go_version="$(go env GOVERSION | sed 's/^go//')"
go_major="$(echo "$go_version" | cut -d. -f1)"
go_minor="$(echo "$go_version" | cut -d. -f2)"
if [ "$go_major" -lt 1 ] || { [ "$go_major" -eq 1 ] && [ "$go_minor" -lt 25 ]; }; then
    fail "cmaker needs Go 1.25+ to build (found $go_version). Update Go from https://go.dev/dl/ and re-run this script."
fi
ok "Go $go_version found"

# ---------- 3. get the source ----------
#
# BASH_SOURCE[0] only resolves to a real file when this script is actually
# run from disk (./scripts/install.sh from a clone); when piped straight
# from GitHub via `curl ... | bash`, it's empty/unreliable - that's exactly
# the signal used below to tell "local checkout" and "standalone" apart, so
# a repo developer's uncommitted local changes are still picked up in the
# former case, while the latter case fetches fresh source with no clone
# required.

cleanup_src=""
cleanup() {
    if [ -n "$cleanup_src" ] && [ -d "$cleanup_src" ]; then
        rm -rf "$cleanup_src"
    fi
}
trap cleanup EXIT

repo_root=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
    candidate="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    if [ -f "$candidate/go.mod" ] && head -1 "$candidate/go.mod" | grep -q '^module cmaker$'; then
        repo_root="$candidate"
    fi
fi

if [ -n "$repo_root" ]; then
    info "Building from local checkout ($repo_root)..."
else
    info "Downloading cmaker source from GitHub..."
    # A bare "-t cmaker-src" template (no trailing X's) only works with
    # BSD mktemp (macOS) - GNU mktemp (Linux) requires the X's explicitly,
    # found the hard way testing this in a real Ubuntu container.
    tmp_src="$(mktemp -d "${TMPDIR:-/tmp}/cmaker-src.XXXXXX")"
    cleanup_src="$tmp_src"
    if command -v git >/dev/null 2>&1 && git clone --depth 1 "$REPO_URL" "$tmp_src/cmaker" >/dev/null 2>&1; then
        repo_root="$tmp_src/cmaker"
    elif command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        curl -fsSL "$REPO_TARBALL_URL" -o "$tmp_src/cmaker.tar.gz"
        tar -xzf "$tmp_src/cmaker.tar.gz" -C "$tmp_src"
        repo_root="$(find "$tmp_src" -maxdepth 1 -type d -name 'Cmaker-*' | head -1)"
    fi
    if [ -z "$repo_root" ] || [ ! -f "$repo_root/go.mod" ]; then
        fail "Couldn't fetch cmaker's source (need 'git', or 'curl'+'tar', on PATH)."
    fi
    ok "Source downloaded"
fi

cd "$repo_root"

# ---------- 4. build ----------
#
# github_client_id mirrors Makefile's/.goreleaser.yaml's own
# GITHUB_CLIENT_ID - cmaker packs' OAuth App client ID for 'cmaker
# login's device flow, a public identifier by GitHub's own design
# (device flow needs no client secret), baked into every build the same
# way main.version is, so 'cmaker login' works out of the box here too,
# not just via `make build`/a goreleaser release.

version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
github_client_id="Ov23liv0mQqsgNEcXkWZ"
info "Building cmaker ($version)..."
tmp_binary="$(mktemp "${TMPDIR:-/tmp}/cmaker-build.XXXXXX")"
go build -ldflags "-s -w -X main.version=$version -X cmaker/internal/packclient.GitHubClientID=$github_client_id" -o "$tmp_binary" .
ok "Build succeeded"

# ---------- 5. install ----------

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp_binary" "$INSTALL_DIR/cmaker"
rm -f "$tmp_binary"
ok "Installed to $INSTALL_DIR/cmaker"

# ---------- 6. PATH setup ----------
#
# A dedicated per-user directory (not /usr/local/bin) so this never needs
# sudo - but that means it isn't on PATH by default, so it has to be added
# to whichever shell rc file the user's actual shell reads. Idempotent:
# re-running this script won't add a duplicate PATH line.

add_to_rc() {
    rc_file="$1"
    if [ -f "$rc_file" ] && grep -qF "$INSTALL_DIR" "$rc_file" 2>/dev/null; then
        info "$rc_file already references $INSTALL_DIR - leaving it as-is"
        return
    fi
    {
        echo ""
        echo "# Added by the cmaker installer"
        echo "export PATH=\"$INSTALL_DIR:\$PATH\""
    } >> "$rc_file"
    ok "Added $INSTALL_DIR to PATH in $rc_file"
}

shell_name="$(basename "${SHELL:-}")"
case "$shell_name" in
    zsh)
        add_to_rc "$HOME/.zshrc"
        ;;
    bash)
        if [ "$os" = "Darwin" ]; then
            # macOS Terminal.app launches login shells, which source
            # .bash_profile (not .bashrc) - unlike most Linux terminal
            # emulators, which launch non-login interactive shells that
            # source .bashrc instead.
            add_to_rc "$HOME/.bash_profile"
        else
            add_to_rc "$HOME/.bashrc"
        fi
        ;;
    fish)
        fish_config="$HOME/.config/fish/config.fish"
        mkdir -p "$(dirname "$fish_config")"
        if [ -f "$fish_config" ] && grep -qF "$INSTALL_DIR" "$fish_config" 2>/dev/null; then
            info "$fish_config already references $INSTALL_DIR - leaving it as-is"
        else
            {
                echo ""
                echo "# Added by the cmaker installer"
                echo "set -gx PATH \"$INSTALL_DIR\" \$PATH"
            } >> "$fish_config"
            ok "Added $INSTALL_DIR to PATH in $fish_config"
        fi
        ;;
    *)
        warn "Unrecognized shell ('$shell_name') - add $INSTALL_DIR to your PATH manually."
        ;;
esac

# So the verification step below (and this same session) can already see
# the freshly installed binary without waiting for a new shell.
export PATH="$INSTALL_DIR:$PATH"

# ---------- 7. prerequisites: cmake, a C/C++ compiler, cargo/rustc, zig ----------
#
# cmake and a compiler are needed by every cmaker project; cargo/rustc and
# zig only by projects using --with-rust/--with-zig - but per an explicit
# choice, all four are checked and offered here unconditionally rather
# than only cmake/compiler, since the alternative (a later, confusing
# 'cmaker build' failure deep inside Rust/Zig interop) is worse than one
# extra prompt most people will just say no to. Each install offer asks
# for confirmation individually - nothing here runs without you saying
# yes to that specific tool. This is deliberately separate from 'cmaker
# doctor', which intentionally does NOT check cargo/rustc/zig unless a
# project's cmaker.yaml already opts in (see CLAUDE.md - "a nil means
# zero cost, no doctor toolchain check" - preserved as-is; this
# unconditional check is install-time only, lives here, not in doctor.go).

homebrew_checked=false
homebrew_available=false

confirm() {
    read -r -p "$1 [y/N] " reply
    case "$reply" in
        [yY]|[yY][eE][sS]) return 0 ;;
        *) return 1 ;;
    esac
}

# ensure_homebrew lazily checks for brew on first need and, if missing,
# offers to install it via Homebrew's own official install command - only
# asked once per run (cached in homebrew_checked) even if multiple tools
# below need it.
ensure_homebrew() {
    if [ "$homebrew_checked" = true ]; then
        return
    fi
    homebrew_checked=true
    if command -v brew >/dev/null 2>&1; then
        homebrew_available=true
        return
    fi
    echo ""
    warn "Homebrew not found - it's needed to auto-install cmake/zig."
    if confirm "Install Homebrew now?"; then
        info "Installing Homebrew (https://brew.sh)..."
        if /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"; then
            # Homebrew installs to /opt/homebrew (Apple Silicon) or
            # /usr/local (Intel) - neither is guaranteed to already be on
            # PATH in this script's own session.
            if [ -x /opt/homebrew/bin/brew ]; then
                eval "$(/opt/homebrew/bin/brew shellenv)"
            elif [ -x /usr/local/bin/brew ]; then
                eval "$(/usr/local/bin/brew shellenv)"
            fi
        fi
        if command -v brew >/dev/null 2>&1; then
            ok "Homebrew installed"
            homebrew_available=true
        else
            warn "Homebrew install didn't complete as expected - skipping auto-install for anything that needs it."
        fi
    else
        info "Skipping - tools that need Homebrew will be left for you to install manually."
    fi
}

# maybe_install_brew_formula checks for check_cmd and, if missing, offers
# to install it via 'brew install formula' - falls back to a manual hint
# if Homebrew itself isn't available (declined above, or failed to
# install).
maybe_install_brew_formula() {
    display_name="$1"; check_cmd="$2"; formula="$3"
    if command -v "$check_cmd" >/dev/null 2>&1; then
        ok "$display_name found"
        return
    fi
    ensure_homebrew
    if [ "$homebrew_available" != true ]; then
        warn "$display_name not found (install manually: brew install $formula)"
        return
    fi
    if confirm "$display_name not found - install via 'brew install $formula' now?"; then
        brew install "$formula"
        ok "$display_name installed"
    else
        warn "Skipped - install later with: brew install $formula"
    fi
}

# maybe_install_compiler is separate from maybe_install_brew_formula since
# macOS's standard C/C++ compiler comes from Xcode Command Line Tools, not
# Homebrew. xcode-select --install launches an async GUI dialog this
# script can't block on - so it's kicked off and the user is told to
# re-run this script afterward (safe: every step here is idempotent).
maybe_install_compiler() {
    if command -v clang++ >/dev/null 2>&1 || command -v g++ >/dev/null 2>&1; then
        ok "C/C++ compiler found"
        return
    fi
    warn "No C/C++ compiler found (need clang++ or g++)."
    if confirm "Install Xcode Command Line Tools now (provides clang++)?"; then
        info "Launching the Xcode Command Line Tools installer - finish the dialog that pops up, then re-run this script."
        xcode-select --install
    else
        warn "Skipped - install later with: xcode-select --install"
    fi
}

# maybe_install_rust is separate from maybe_install_brew_formula/
# maybe_install_apt_package since the standard way to install Rust on any
# platform is rustup (https://rustup.rs), not a system package manager - it
# keeps cargo/rustc updatable via 'rustup update' the way most Rust
# projects expect. Identical on macOS and Linux, so it isn't split like the
# other three checks below are.
maybe_install_rust() {
    if command -v cargo >/dev/null 2>&1 && command -v rustc >/dev/null 2>&1; then
        ok "cargo/rustc found"
        return
    fi
    warn "cargo/rustc not found (needed only for 'cmaker new --with-rust')."
    if confirm "Install Rust via rustup now?"; then
        info "Installing Rust via rustup (https://rustup.rs)..."
        if curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y; then
            export PATH="$HOME/.cargo/bin:$PATH"
        fi
        if command -v cargo >/dev/null 2>&1; then
            ok "Rust installed"
        else
            warn "Rust install didn't complete as expected - install manually from https://rustup.rs"
        fi
    else
        warn "Skipped - install later from https://rustup.rs"
    fi
}

# ---- Linux (apt-based distros only - matches cmd/install.go's own
# installSystemPackage support for opencv/boost/gtkmm §27; Fedora/Arch/etc.
# aren't attempted, same scope cut as the rest of this codebase, just a
# manual hint instead) ----

apt_sudo=""
apt_update_done=false

# ensure_apt_updated runs 'apt-get update' at most once per script run
# (cached in apt_update_done), right before the first package that actually
# needs it - not unconditionally up front, since a run where every
# prerequisite is already present shouldn't touch apt at all.
ensure_apt_updated() {
    if [ "$apt_update_done" = true ]; then
        return
    fi
    apt_update_done=true
    info "Updating apt package lists..."
    $apt_sudo apt-get update -y
}

# maybe_install_apt_package is maybe_install_brew_formula's Linux
# equivalent - checks for check_cmd and, if missing, offers to install it
# via 'apt-get install -y package'. Unlike Homebrew (which refuses to run
# as root by design), apt-get needs root - $apt_sudo is "" when this script
# is already running as root (common in containers/CI) and "sudo"
# otherwise, set once below before this function is first called.
maybe_install_apt_package() {
    display_name="$1"; check_cmd="$2"; package="$3"
    if command -v "$check_cmd" >/dev/null 2>&1; then
        ok "$display_name found"
        return
    fi
    if ! command -v apt-get >/dev/null 2>&1; then
        warn "$display_name not found (this installer only automates apt-based distros - install $package via your distro's package manager)"
        return
    fi
    if confirm "$display_name not found - install via 'apt-get install -y $package' now?"; then
        ensure_apt_updated
        # DEBIAN_FRONTEND=noninteractive via env, not a bare env-var
        # prefix - sudo resets the environment by default, so a plain
        # "VAR=val sudo ..." prefix would only apply to sudo itself, not
        # the apt-get it execs as root; wrapping in "env" makes it survive
        # that hop. Needed because some packages pull in a dependency
        # (tzdata, in testing) that otherwise prompts interactively via
        # debconf and can eat stdin meant for this script's own prompts -
        # a no-op on an already-configured desktop system, only matters on
        # a fresh minimal install.
        $apt_sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y "$package"
        ok "$display_name installed"
    else
        warn "Skipped - install later with: sudo apt-get install -y $package"
    fi
}

# maybe_install_compiler_linux is maybe_install_compiler's Linux equivalent
# - build-essential (apt) provides gcc/g++/make in one package, the
# standard way to get a C/C++ toolchain on Debian/Ubuntu (no async-installer
# complication like Xcode Command Line Tools has).
maybe_install_compiler_linux() {
    if command -v g++ >/dev/null 2>&1 || command -v clang++ >/dev/null 2>&1; then
        ok "C/C++ compiler found"
        return
    fi
    warn "No C/C++ compiler found (need g++ or clang++)."
    if ! command -v apt-get >/dev/null 2>&1; then
        warn "This installer only automates apt-based distros - install build-essential (or clang) via your distro's package manager."
        return
    fi
    if confirm "Install build-essential (gcc/g++/make) via apt now?"; then
        ensure_apt_updated
        $apt_sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y build-essential
        ok "Compiler installed"
    else
        warn "Skipped - install later with: sudo apt-get install -y build-essential"
    fi
}

echo ""
info "Checking prerequisites..."
if [ "$os" = "Darwin" ]; then
    maybe_install_brew_formula "cmake" "cmake" "cmake"
    maybe_install_compiler
    maybe_install_rust
    maybe_install_brew_formula "gdb" "gdb" "gdb"
    maybe_install_brew_formula "zig" "zig" "zig"
else
    if [ "$(id -u)" -eq 0 ]; then apt_sudo=""; else apt_sudo="sudo"; fi
    maybe_install_apt_package "cmake" "cmake" "cmake"
    maybe_install_compiler_linux
    maybe_install_rust
    maybe_install_apt_package "gdb" "gdb" "gdb"
    if command -v zig >/dev/null 2>&1; then
        ok "zig found"
    else
        warn "zig not found (needed only for 'cmaker new --with-zig') - no automated install available for Linux; see https://ziglang.org/download/"
    fi
fi

# ---------- 8. Anthropic API key ----------
#
# Stored in a dedicated cmaker config file (~/.cmaker/env, 0600) rather
# than exported in a shell dotfile - works no matter which shell runs
# cmaker later, and keeps the raw key out of plaintext dotfiles that are
# often synced/backed up/git-tracked. internal/llm.NewClientFromEnv reads
# this as a fallback whenever ANTHROPIC_API_KEY itself isn't set.

echo ""
key_exists=false
if [ -f "$ENV_FILE" ] && grep -q '^ANTHROPIC_API_KEY=' "$ENV_FILE" 2>/dev/null; then
    key_exists=true
fi

if [ "$key_exists" = true ]; then
    info "An Anthropic API key is already saved at $ENV_FILE."
    read -r -s -p "Press Enter to keep it, or paste a new key to replace it: " api_key
    echo ""
else
    info "cmaker's AI-assisted features (heal, describe/improvise, explain, generate accessors) need an Anthropic API key."
    read -r -s -p "Anthropic API key (get one at https://console.anthropic.com/, or press Enter to skip): " api_key
    echo ""
fi

check_ai=""
if [ -n "${api_key:-}" ]; then
    mkdir -p "$CONFIG_DIR"
    chmod 700 "$CONFIG_DIR"
    (umask 177 && printf 'ANTHROPIC_API_KEY=%s\n' "$api_key" > "$ENV_FILE")
    ok "API key saved to $ENV_FILE"
    check_ai="--ai"
elif [ "$key_exists" = true ]; then
    ok "Keeping existing API key."
    check_ai="--ai"
else
    warn "No API key provided - AI-assisted features will be unavailable until you set one (re-run this script, or set ANTHROPIC_API_KEY yourself)."
fi

# ---------- 9. verify ----------
#
# Reuses 'cmaker doctor' (rather than re-checking cmake/compiler/API-key
# status in bash) so there's exactly one place that knows what "ready"
# means for each tool - this script's job is just to get cmaker installed
# and hand off to it.

echo ""
info "Verifying installation..."
"$INSTALL_DIR/cmaker" doctor $check_ai || true

echo ""
ok "cmaker is installed."
info "Open a new terminal (or run 'source ~/.zshrc' / equivalent) so PATH changes take effect, then run 'cmaker' to get started."
