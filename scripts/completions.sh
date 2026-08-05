#!/bin/sh
# Regenerates shell completion scripts (cobra provides `cmaker completion
# <shell>` for free). Run by goreleaser's `before.hooks` so release
# archives always ship completions for the version being released, and
# runnable standalone (`./scripts/completions.sh`) to refresh them locally.
set -eu

cd "$(dirname "$0")/.."

rm -rf completions
mkdir -p completions

for sh in bash zsh fish powershell; do
  go run . completion "$sh" > "completions/cmaker.$sh"
done
