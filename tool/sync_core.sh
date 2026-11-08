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

# Local patches on top of the fork live here, not in the fork's checkout, so
# rsync drops them and they are applied again on every sync.
shopt -s nullglob
for patch_file in "$root"/core/patches/*.patch; do
  if ! patch -p1 --batch -d "$dest" < "$patch_file" >/dev/null; then
    echo "error: $patch_file did not apply to the synced copy" >&2
    exit 1
  fi
  echo "applied $(basename "$patch_file")"
done

echo "synced $src -> $dest"
echo
echo "Core version in build_config.yaml: $(sed -n 's/^core_version: *//p' "$root/build_config.yaml")"
echo "Bump it if the fork's version changed, then commit and push."
