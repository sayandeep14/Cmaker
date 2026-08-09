#!/usr/bin/env pwsh
#
# cmaker installer for Windows (64-bit only for now - see ROADMAP.md). Builds
# cmaker from source via the local Go toolchain (no published prebuilt
# release exists yet - see ROADMAP.md §8), installs it to a per-user
# directory (no Administrator privileges required), wires that directory
# onto your User PATH, checks for cmake/a C/C++ compiler+gdb (via MSYS2,
# which provides a real GCC-compatible MinGW-w64/UCRT toolchain - NOT MSVC,
# so cmaker's existing GCC/Clang-style flag generation keeps working
# unchanged)/cargo+rustc/zig and offers to install whichever are missing
# (each with its own confirmation prompt - nothing runs without you saying
# yes), and optionally collects + validates an ANTHROPIC_API_KEY for
# cmaker's AI-assisted features (heal, describe/improvise, explain, generate
# accessors) - all of which work fine without one, just without those
# specific commands.
#
# Two ways to run this:
#   irm https://raw.githubusercontent.com/sayandeep14/Cmaker/main/scripts/install.ps1 | iex
#     - no clone needed, fetches the source itself.
#   .\scripts\install.ps1   (from an existing clone)
#     - builds from whatever's in that working tree, including uncommitted
#       local changes - the fast local-iteration path for repo developers.
#
# Override the install directory: $env:CMAKER_INSTALL_DIR = "C:\some\path"; .\scripts\install.ps1

$ErrorActionPreference = "Stop"

$InstallDir = if ($env:CMAKER_INSTALL_DIR) { $env:CMAKER_INSTALL_DIR } else { Join-Path $env:USERPROFILE ".cmaker\bin" }
$ConfigDir = Join-Path $env:USERPROFILE ".cmaker"
$EnvFile = Join-Path $ConfigDir "env"
$RepoUrl = "https://github.com/sayandeep14/Cmaker.git"
$RepoZipUrl = "https://github.com/sayandeep14/Cmaker/archive/refs/heads/main.zip"

function Info($msg) { Write-Host "-- $msg" -ForegroundColor Cyan }
function Ok($msg) { Write-Host "-- $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "-- $msg" -ForegroundColor Yellow }
function Fail($msg) { Write-Host "-- $msg" -ForegroundColor Red; exit 1 }

function Confirm($prompt) {
    $reply = Read-Host "$prompt [y/N]"
    return $reply -match '^(y|yes)$'
}

# ---------- 1. platform check ----------

if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64" -and $env:PROCESSOR_ARCHITEW6432 -ne "AMD64") {
    Fail "This installer currently only supports 64-bit Windows (x64). 32-bit and ARM64 support are not implemented."
}
if (-not [Environment]::Is64BitOperatingSystem) {
    Fail "This installer requires 64-bit Windows."
}
Info "Detected 64-bit Windows"

# ---------- 2. prerequisite: Go (needed to build cmaker itself) ----------

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail "Go is required to build cmaker (no prebuilt release exists yet - see ROADMAP.md section 8). Install Go 1.25+ from https://go.dev/dl/ and re-run this script."
}

$goVersionRaw = (go env GOVERSION) -replace '^go', ''
$goVersionParts = $goVersionRaw.Split('.')
$goMajor = [int]$goVersionParts[0]
$goMinor = [int]$goVersionParts[1]
if ($goMajor -lt 1 -or ($goMajor -eq 1 -and $goMinor -lt 25)) {
    Fail "cmaker needs Go 1.25+ to build (found $goVersionRaw). Update Go from https://go.dev/dl/ and re-run this script."
}
Ok "Go $goVersionRaw found"

# ---------- 3. get the source ----------
#
# $PSScriptRoot only resolves to a real directory when this script is
# actually run from disk (.\scripts\install.ps1 from a clone); when piped
# straight from GitHub via `irm ... | iex`, it's empty - that's exactly the
# signal used below to tell "local checkout" and "standalone" apart, so a
# repo developer's uncommitted local changes are still picked up in the
# former case, while the latter case fetches fresh source with no clone
# required.

$tempSrcDir = $null
try {
    $repoRoot = $null
    if ($PSScriptRoot) {
        $candidate = Split-Path -Parent $PSScriptRoot
        $goModPath = Join-Path $candidate "go.mod"
        if ((Test-Path $goModPath) -and ((Get-Content $goModPath -First 1) -eq "module cmaker")) {
            $repoRoot = $candidate
        }
    }

    if ($repoRoot) {
        Info "Building from local checkout ($repoRoot)..."
    } else {
        Info "Downloading cmaker source from GitHub..."
        $tempSrcDir = Join-Path ([System.IO.Path]::GetTempPath()) ("cmaker-src-" + [System.Guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $tempSrcDir | Out-Null

        $gotSource = $false
        if (Get-Command git -ErrorAction SilentlyContinue) {
            $cloneDir = Join-Path $tempSrcDir "cmaker"
            & git clone --depth 1 $RepoUrl $cloneDir *> $null
            if ($LASTEXITCODE -eq 0) {
                $repoRoot = $cloneDir
                $gotSource = $true
            }
        }
        if (-not $gotSource) {
            $zipPath = Join-Path $tempSrcDir "cmaker.zip"
            Invoke-WebRequest -Uri $RepoZipUrl -OutFile $zipPath
            Expand-Archive -Path $zipPath -DestinationPath $tempSrcDir
            $extracted = Get-ChildItem -Path $tempSrcDir -Directory | Where-Object { $_.Name -like "Cmaker-*" } | Select-Object -First 1
            if ($extracted) {
                $repoRoot = $extracted.FullName
                $gotSource = $true
            }
        }
        if (-not $gotSource -or -not (Test-Path (Join-Path $repoRoot "go.mod"))) {
            Fail "Couldn't fetch cmaker's source (need 'git', or network access for a source zip)."
        }
        Ok "Source downloaded"
    }

    Set-Location $repoRoot

    # ---------- 4. build ----------
    #
    # $githubClientId mirrors Makefile's/.goreleaser.yaml's own
    # GITHUB_CLIENT_ID - cmaker packs' OAuth App client ID for 'cmaker
    # login's device flow, a public identifier by GitHub's own design
    # (device flow needs no client secret), baked into every build the
    # same way main.version is, so 'cmaker login' works out of the box
    # here too, not just via `make build`/a goreleaser release.

    $version = "dev"
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $described = & git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $described) { $version = $described }
    }
    $githubClientId = "Ov23liv0mQqsgNEcXkWZ"
    Info "Building cmaker ($version)..."
    $tempBinary = Join-Path ([System.IO.Path]::GetTempPath()) ("cmaker-build-" + [System.Guid]::NewGuid().ToString("N") + ".exe")
    & go build -ldflags "-s -w -X main.version=$version -X cmaker/internal/packclient.GitHubClientID=$githubClientId" -o $tempBinary .
    if ($LASTEXITCODE -ne 0) { Fail "Build failed." }
    Ok "Build succeeded"

    # ---------- 5. install ----------

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -Path $tempBinary -Destination (Join-Path $InstallDir "cmaker.exe") -Force
    Remove-Item $tempBinary -ErrorAction SilentlyContinue
    Ok "Installed to $InstallDir\cmaker.exe"

    # ---------- 6. PATH setup ----------
    #
    # A dedicated per-user directory (not Program Files) so this never needs
    # Administrator rights - persisted to the User (not Machine) PATH
    # environment variable via the registry, which needs no elevation
    # either. Idempotent: re-running this script won't add a duplicate
    # entry.

    function Add-ToUserPath($dir) {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $pathEntries = @()
        if ($userPath) { $pathEntries = $userPath -split ';' | Where-Object { $_ -ne "" } }
        if ($pathEntries -notcontains $dir) {
            $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
            [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
            Ok "Added $dir to your User PATH (open a new terminal for it to take effect there)"
        } else {
            Info "User PATH already references $dir - leaving it as-is"
        }
        # So the rest of this script's own session can already see what was
        # just added without waiting for a new terminal.
        $env:Path = "$dir;$env:Path"
    }

    Add-ToUserPath $InstallDir

    # ---------- 7. prerequisites: cmake, a C/C++ compiler+gdb (MSYS2), cargo/rustc, zig ----------
    #
    # cmake and a compiler are needed by every cmaker project; cargo/rustc
    # and zig only by projects using --with-rust/--with-zig - but per an
    # explicit choice, all four are checked and offered here unconditionally
    # rather than only cmake/compiler, since the alternative (a later,
    # confusing 'cmaker build' failure deep inside Rust/Zig interop) is
    # worse than one extra prompt most people will just say no to. Each
    # install offer asks for confirmation individually - nothing here runs
    # without you saying yes to that specific tool.
    #
    # winget (not Chocolatey) is used for cmake/rustup/zig - it ships built
    # into Windows 10/11 already, so this doesn't need to bootstrap a second
    # package manager. The C/C++ compiler + gdb specifically come from MSYS2
    # instead (a real GCC-compatible MinGW-w64/UCRT toolchain - matches what
    # 'cmaker doctor's own Windows hints already assumed): opencv/boost/
    # gtkmm system-package installs (see cmd/install.go) also go through
    # MSYS2's pacman rather than winget/choco, since those need to be built
    # against the SAME compiler ABI cmaker itself is using - a Chocolatey
    # boost built for MSVC would not link against MinGW-compiled code.

    function Test-Winget {
        if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
            Warn "winget not found - it ships with Windows 10 (1709+)/11 via the 'App Installer' app. Install/update it from https://aka.ms/getwinget, then re-run this script for cmake/rustup/zig auto-install."
            return $false
        }
        return $true
    }

    function Install-WithWinget($displayName, $wingetId) {
        if (-not (Confirm "$displayName not found - install via 'winget install $wingetId' now?")) {
            Warn "Skipped - install later with: winget install -e --id $wingetId"
            return
        }
        & winget install -e --id $wingetId --silent --accept-source-agreements --accept-package-agreements
        if ($LASTEXITCODE -eq 0) { Ok "$displayName installed" } else { Warn "$displayName install didn't complete as expected - install manually with: winget install -e --id $wingetId" }
    }

    # cmake
    if (Get-Command cmake -ErrorAction SilentlyContinue) {
        Ok "cmake found"
    } elseif (Test-Winget) {
        Install-WithWinget "cmake" "Kitware.CMake"
    }

    # C/C++ compiler + gdb, via MSYS2's pacman - Find-Pacman resolves it
    # either from PATH or the default MSYS2 install location
    # (C:\msys64\usr\bin\pacman.exe - "the default" folder the official
    # MSYS2 installer, and winget's MSYS2.MSYS2 package, both use).
    function Find-Pacman {
        $cmd = Get-Command pacman -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
        $default = "C:\msys64\usr\bin\pacman.exe"
        if (Test-Path $default) { return $default }
        return $null
    }

    $haveCompiler = (Get-Command gcc -ErrorAction SilentlyContinue) -or (Get-Command "g++" -ErrorAction SilentlyContinue) -or (Get-Command clang++ -ErrorAction SilentlyContinue)
    if ($haveCompiler) {
        Ok "C/C++ compiler found"
    } else {
        Warn "No C/C++ compiler found (need gcc/g++, via MSYS2 - MSVC's cl.exe is not supported by cmaker's flag generation)."
        $pacmanPath = Find-Pacman
        if (-not $pacmanPath) {
            if (Test-Winget -and (Confirm "MSYS2 not found - install it via winget now (provides gcc/g++/gdb via pacman)?")) {
                & winget install -e --id MSYS2.MSYS2 --silent --accept-source-agreements --accept-package-agreements
                $pacmanPath = Find-Pacman
                if (-not $pacmanPath) { Warn "MSYS2 install didn't complete as expected - install manually from https://www.msys2.org/ and re-run this script." }
            } else {
                Warn "Skipped - install MSYS2 manually from https://www.msys2.org/ and re-run this script."
            }
        }
        if ($pacmanPath -and (Confirm "Install the gcc/g++/gdb/make toolchain via MSYS2's pacman now?")) {
            Info "Updating MSYS2's package database..."
            & $pacmanPath -Syu --noconfirm *> $null
            if ($LASTEXITCODE -ne 0) {
                # First-run keyring initialization occasionally needs an
                # explicit nudge on a freshly installed MSYS2 - see
                # ROADMAP.md's Windows-support entry for why this couldn't
                # be verified live.
                $pacmanKeyPath = Join-Path (Split-Path -Parent $pacmanPath) "pacman-key.exe"
                & $pacmanKeyPath --init *> $null
                & $pacmanKeyPath --populate msys2 *> $null
                & $pacmanPath -Syu --noconfirm *> $null
            }
            Info "Installing mingw-w64-ucrt-x86_64-toolchain (gcc/g++/gdb/make)..."
            & $pacmanPath -S --noconfirm mingw-w64-ucrt-x86_64-toolchain
            if ($LASTEXITCODE -eq 0) {
                # pacmanPath is .../<msys2 root>/usr/bin/pacman.exe - three
                # levels up (bin, usr, then the msys2 root itself) to reach
                # the root ucrt64/bin lives under.
                $msys2Root = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $pacmanPath))
                $ucrt64Bin = Join-Path $msys2Root "ucrt64\bin"
                if (Test-Path $ucrt64Bin) {
                    Add-ToUserPath $ucrt64Bin
                }
                Ok "gcc/g++/gdb/make installed"
            } else {
                Warn "MSYS2 toolchain install didn't complete as expected - open the MSYS2 terminal and run: pacman -S mingw-w64-ucrt-x86_64-toolchain"
            }
        } elseif ($pacmanPath) {
            Warn "Skipped - install later by running: pacman -S mingw-w64-ucrt-x86_64-toolchain"
        }
    }

    # cargo/rustc
    if ((Get-Command cargo -ErrorAction SilentlyContinue) -and (Get-Command rustc -ErrorAction SilentlyContinue)) {
        Ok "cargo/rustc found"
    } else {
        Warn "cargo/rustc not found (needed only for 'cmaker new --with-rust')."
        if (Test-Winget) { Install-WithWinget "Rust (rustup)" "Rustlang.Rustup" }
    }

    # zig
    if (Get-Command zig -ErrorAction SilentlyContinue) {
        Ok "zig found"
    } else {
        Warn "zig not found (needed only for 'cmaker new --with-zig')."
        if (Test-Winget) { Install-WithWinget "zig" "zig.zig" }
    }

    # ---------- 8. Anthropic API key ----------
    #
    # Stored in a dedicated cmaker config file (%USERPROFILE%\.cmaker\env)
    # rather than a PowerShell profile script - works no matter which shell
    # runs cmaker later, and keeps the raw key out of a profile script
    # that's often synced/backed up. internal/llm.NewClientFromEnv (plain
    # Go stdlib, already cross-platform - os.UserHomeDir()/filepath.Join
    # resolve correctly on Windows with no OS-specific code needed) reads
    # this as a fallback whenever ANTHROPIC_API_KEY itself isn't set.

    Write-Host ""
    $keyExists = (Test-Path $EnvFile) -and (Select-String -Path $EnvFile -Pattern '^ANTHROPIC_API_KEY=' -Quiet)

    if ($keyExists) {
        Info "An Anthropic API key is already saved at $EnvFile."
        $apiKey = Read-Host "Press Enter to keep it, or paste a new key to replace it" -AsSecureString
    } else {
        Info "cmaker's AI-assisted features (heal, describe/improvise, explain, generate accessors) need an Anthropic API key."
        $apiKey = Read-Host "Anthropic API key (get one at https://console.anthropic.com/, or press Enter to skip)" -AsSecureString
    }
    # PtrToStringBSTR, specifically - not PtrToStringAuto, which silently
    # truncated to a single character in testing (SecureStringToBSTR
    # produces a length-prefixed BSTR, not a null-terminated string in the
    # platform's "auto" charset, which is what PtrToStringAuto assumes).
    # ZeroFreeBSTR afterward clears the decrypted copy from unmanaged
    # memory rather than leaving the API key sitting in the process's
    # address space for the rest of the script's run.
    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($apiKey)
    try {
        $apiKeyPlain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
    }

    $checkAi = $false
    if ($apiKeyPlain) {
        New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
        Set-Content -Path $EnvFile -Value "ANTHROPIC_API_KEY=$apiKeyPlain"
        Ok "API key saved to $EnvFile"
        $checkAi = $true
    } elseif ($keyExists) {
        Ok "Keeping existing API key."
        $checkAi = $true
    } else {
        Warn "No API key provided - AI-assisted features will be unavailable until you set one (re-run this script, or set ANTHROPIC_API_KEY yourself)."
    }

    # ---------- 9. verify ----------
    #
    # Reuses 'cmaker doctor' (rather than re-checking cmake/compiler/API-key
    # status in this script) so there's exactly one place that knows what
    # "ready" means for each tool - this script's job is just to get cmaker
    # installed and hand off to it.

    Write-Host ""
    Info "Verifying installation..."
    if ($checkAi) {
        & "$InstallDir\cmaker.exe" doctor --ai
    } else {
        & "$InstallDir\cmaker.exe" doctor
    }

    Write-Host ""
    Ok "cmaker is installed."
    Info "Open a new terminal so the updated PATH takes effect, then run 'cmaker' to get started."
} finally {
    if ($tempSrcDir -and (Test-Path $tempSrcDir)) {
        Remove-Item -Recurse -Force $tempSrcDir -ErrorAction SilentlyContinue
    }
}
