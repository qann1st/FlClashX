#!/usr/bin/env bash
set -euo pipefail

# Copies the working Elysium-Core checkout into core/third_party/elysium-core,
# which is the copy every build (desktop hooks and Android's gomobile module)
# compiles. Run it after changing the fork, then commit the result.
#
# Usage: tool/sync_core.sh [path-to-fork]   (default: ../Prizrak-Core)

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
src="${1:-"$root/../Prizrak-Core"}"
dest="$root/core/third_party/elysium-core"

[[ -f "$src/go.mod" ]] || {
  echo "error: $src has no go.mod; pass the fork's path" >&2
  exit 1
}

if ! grep -q '^module github.com/metacubex/mihomo$' "$src/go.mod"; then
  echo "error: $src does not keep the mihomo module path" >&2
  exit 1
fi

mkdir -p "$dest"
# .git and the fork's downloaded binaries never belong in this repository.
rsync -a --delete --exclude='.git' --exclude='bin' "$src"/ "$dest"/

echo "synced $src -> $dest"
echo
echo "Core version in build_config.yaml: $(sed -n 's/^core_version: *//p' "$root/build_config.yaml")"
echo "Windows core version: $(sed -n 's/^windows_core_version: *//p' "$root/build_config.yaml")"
echo "Bump them if the fork's version changed, then commit and push."
