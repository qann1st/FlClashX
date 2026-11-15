#!/usr/bin/env bash
set -euo pipefail

# Copies the qann1st/Prizrak-Core release checkout into
# core/third_party/elysium-core, which is the copy every Windows amd64 build
# compiles. Without an explicit source checkout it fetches the canonical
# release branch, so an update cannot accidentally come from another branch.
#
# Usage: tool/sync_core.sh [path-to-release-checkout]

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
fork_repo='https://github.com/qann1st/Prizrak-Core.git'
fork_branch='release'
src="${1:-}"
dest="$root/core/third_party/elysium-core"

if [[ -z "$src" ]]; then
  temp_dir="$(mktemp -d)"
  trap 'rm -rf "$temp_dir"' EXIT
  src="$temp_dir/Prizrak-Core"
  git clone --depth=1 --branch "$fork_branch" "$fork_repo" "$src"
fi

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
