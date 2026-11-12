# AGENTS.md — what this fork owns and what must not break

FlClashX is a fork of `pluralplay/FlClashX` (which in turn forks
`chen08209/FlClash`), a Flutter client for mihomo on Android, Windows, Linux
and macOS. Releases are cut from tags (`v*`), built by `.github/workflows/build.yaml`,
and published as GitHub releases. This file is for the next agent or maintainer:
it lists the parts that are **not** plain upstream and the rules that keep them
working across upstream merges.

Everything below has been broken at least once by a careless merge.

---

## 1. The core is split by platform — never make it one core

| Platform | Core | Selected by |
| --- | --- | --- |
| Windows | Elysium-Core fork, vendored in `core/third_party/elysium-core` | `core/go.windows.mod` |
| Android, Linux, macOS | original `legiz-ru/Prizrak-Core` release | `core/go.mod` |

How it works:

- `core/go.mod` replaces `github.com/metacubex/mihomo` with
  `github.com/legiz-ru/Prizrak-Core vX.Y.ZrN`.
- `core/go.windows.mod` (and its own `core/go.windows.sum`) replaces the same
  module with `./third_party/elysium-core`.
- `plugins/setup/setup_hooks/lib/src/go_builder.dart` passes
  `-modfile=go.windows.mod` when the target is `GOOS=windows`; every other
  target builds `core/go.mod`.
- Android's `android/core` Gradle module runs `gomobile bind` against `core`,
  so it builds `core/go.mod` (the original core) with `with_gvisor,cmfa`.
- Versions live in `build_config.yaml`:
  - `core_version` — original Prizrak-Core version, used everywhere but Windows;
  - `windows_core_version` — the fork's version, stamped into the Windows core.
  `setup.dart` (`_readCoreVersion`) and the Go builder pick the right one, and
  that value becomes the app's `CORE_VERSION` define, the About badge and the UA.

Keeping them in sync:

```bash
tool/sync_core.sh                 # copies ../Prizrak-Core into core/third_party/elysium-core
tool/sync_core.sh /path/to/fork   # or an explicit checkout
```

The script refuses a checkout that does not keep the mihomo module path and
excludes `.git` and `bin`. After syncing, commit the vendored tree.

**Do not** point `core/go.mod` at the vendored fork (that makes Windows-only
zapret code part of every build) and **do not** delete `core/go.windows.mod`.

## 2. The Elysium-Core fork differs in specific ways

The vendored tree carries its own rules in
`core/third_party/elysium-core/AGENTS.md`; read it before touching the core.
In short, compared with `Prizrak-Core v1.19.34-r1` it adds:

- `transport/zapret/` — packet-level TCP/UDP desync (fake, split, multisplit)
  with a WinDivert backend on Windows amd64 (`CGO_ENABLED=0`, no cgo);
- `transport/finalmask/fake/` — UDP decoy masks (`discord-udp`, `quic`, `stun`,
  `dtls`, …) registered in `transport/finalmask/option`;
- `adapter/outbound/freedom.go` — `finalmask` and `zapret` options; zapret wins
  over `fragment`/`finalmask` on Windows amd64, and silently falls back
  elsewhere (including Windows arm64, which has no WinDivert driver).

Those differences are Windows-only in this app. If a build for another platform
starts compiling `transport/zapret`, the `-modfile` split is broken.

## 3. Auto-update goes through the S3 manifest, never GitHub

- `lib/common/app_update.dart` reads `FLCLASHX_UPDATE_MANIFEST_URL`
  (`https://elysium.s3.cloud.ru/update.json` by default). Do not add a
  GitHub-releases update check or replace this source.
- Windows installs through Inno Setup silently, macOS mounts the DMG and
  replaces the bundle with `ditto` (admin prompt when needed), Android hands
  the APK to the system installer (`AppPlugin.installApk`). Keep all three.
- The `upload` job in `build.yaml` generates `update.json` with
  `tool/update_manifest.dart` (base URL `.../releases/<version>`) and ships it
  as a release asset; macOS DMGs get their entries automatically.
- Publishing is still manual: upload the assets to
  `https://elysium.s3.cloud.ru/releases/<version>/` and `update.json` last
  (files first, manifest last). The app caches nothing else.

## 4. User-Agent and About screen

- `lib/common/package.dart` builds `FlClashX/v<app> core/<core>/v<core> Platform/<os>`.
  The core name is `Elysium-Core` on Windows and `Prizrak-Core` everywhere else
  (`lib/common/constant.dart`, `coreDisplayName`).
- `lib/views/about.dart` links the core to `legiz-ru/Prizrak-Core`.
- Examples in `README.md`, `README_EN.md` and `docs/panel-headers.md` follow the
  same rule; keep them truthful when versions change.

## 5. Windows packaging

- `windows/windivert/` holds the official WinDivert 2.2.2 x64 pair and its
  license. `setup.dart` (`_packageWindows`) copies them into the Flutter bundle
  before the zip and the Inno script run, so both artifacts carry them beside
  `FlClashX.exe`.
- The installer (`windows/packaging/exe/inno_setup.iss`) calls `StopWinDivert`
  in `PrepareToInstall` **and** in the uninstall path: the driver keeps
  `WinDivert64.sys` locked and the next update fails otherwise. Codes 1060/1072
  count as success.
- arm64 bundles intentionally carry no WinDivert files.

## 6. Linux and macOS release details

- `setup.dart` (`_packageAppImage`) moves the bundled libraries to `usr/lib`
  and creates `usr/bin/lib -> ../lib`. The Flutter engine opens
  `<exe dir>/lib/libapp.so`; deleting that symlink brings back
  `Failed to create AOT data`.
- macOS release jobs are enabled but **unsigned** (ad-hoc): the first launch
  needs right-click → Open. `macos/Runner/Configs/Release.xcconfig` pins ARCHS
  to the host, which is what the host-only Go core expects. The release
  template carries the macOS download row and the ad-hoc note.

## 7. AUR and the Arch package

- `aur/flclashx-elysium-git/PKGBUILD` repacks the released `.deb` into
  `/usr/lib/flclashx-elysium` with a wrapper in `PATH`; `publish.yaml`
  (workflow_dispatch) creates or updates the AUR package `flclashx-elysium-git`.
- The `upload` job builds the same package for stable tags and attaches
  `flclashx-elysium-git-<version>-1-x86_64.pkg.tar.zst`; the release template
  links it as “ArchPackage”. Pre-releases skip it (a `-pre.N` pkgver cannot
  hold a hyphen).

## 8. Smart groups are optional, their leftovers are cleaned

- The app reads and round-trips `type: smart` groups and their options
  (`uselightgbm`, `collectdata`, `sample-rate`, `prefer-asn`,
  `policy-priority`), but nothing runs unless a config contains such a group.
- `cleanStaleCoreArtifacts` (`lib/common/app_data.dart`) removes `Model.bin`,
  `model.bin` and `smart_weight_data.csv` from the core home once per
  app + core version. Do not remove that cleanup: stale LightGBM files kept an
  idle phone writing to disk.
- Android builds **without** `with_powersave`; do not add it back without the
  matching host plumbing.

## 9. Upstream merge playbook (pluralplay/FlClashX)

The app's upstream is `pluralplay/FlClashX`; active development lives on
`upstream/0.5.0-dev`, and that branch is **rewritten**, so a plain
`git merge upstream/0.5.0-dev` drags in an unrelated history.

What worked (and must be repeated):

1. Pick the tag the fork last adopted (e.g. `v0.5.0-pre.2`).
2. Compute the upstream delta and apply it three-way, excluding the files this
   fork owns:

   ```bash
   git diff --binary <last-adopted-tag>..upstream/0.5.0-dev > /tmp/upstream.patch
   # drop the paths listed below, then:
   git apply --3way --index --whitespace=nowarn /tmp/upstream.patch
   ```

3. Merge the excluded files by hand with `git merge-file -p --diff3 ours base theirs`
   and resolve. The last merge had conflicts only in `AppPlugin.kt`,
   `lib/common/package.dart`, `lib/plugins/app.dart`,
   `lib/providers/actions/common.dart` and `pubspec.yaml`; every one was
   “keep both”.
4. Run the checks below, commit as `feat(upstream): adopt <tag>`.

Fork-owned files (do not take upstream's copy without merging our parts):

- `core/go.mod`, `core/go.sum`, `core/go.windows.mod`, `core/go.windows.sum`,
  `core/third_party/elysium-core/**`
- `build_config.yaml`, `setup.dart` (core/packaging logic), `tool/**`,
  `aur/**`, `windows/windivert/**`
- `lib/common/app_update.dart`, `lib/plugins/app.dart`,
  `lib/providers/actions/common.dart` and the Android install/consent code
  (`AppPlugin.kt`) — the S3 updater and APK installer live here
- `lib/common/constant.dart` (core name), `lib/common/package.dart` (UA),
  `lib/views/about.dart` (core link)
- `.github/workflows/**`, `.github/*_template.md`, `README*.md`,
  `docs/panel-headers.md`

## 10. Checks before every release

```bash
PATH=.../flutter/bin:$PATH flutter analyze
PATH=.../flutter/bin:$PATH dart run tool/test.dart

cd core
go build -tags with_gvisor ./cmd/core
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -modfile=go.windows.mod -tags with_gvisor ./cmd/core
go test ./...
```

`dart run tool/test.dart` matters extra: the model tests read the *original*
core's Go source through `go list`, so a core bump that adds config keys or
proxy types fails here and the app models must be updated with it.

Release:

```bash
tool/release.sh stable --push      # bumps, regenerates the changelog, tags, pushes
```

The tag starts `build.yaml`; afterwards upload the release assets to the S3
bucket (files first, `update.json` last) and, when needed, run the `publish`
workflow for the AUR package (`tag=<tag>`, `telegram=false`).
