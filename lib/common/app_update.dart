import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:fl_clash/common/print.dart';
import 'package:fl_clash/enum/enum.dart';
import 'package:fl_clash/plugins/app.dart';
import 'package:flutter/foundation.dart' show visibleForTesting;
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

/// Where the release manifest lives. Assets are served from this bucket as
/// well: GitHub is slow or unreachable in some regions, so the app never asks
/// it for anything.
///
/// Override it at build time, for example:
/// `--dart-define=FLCLASHX_UPDATE_MANIFEST_URL=https://bucket/update.json`
const updateManifestUrl = String.fromEnvironment(
  'FLCLASHX_UPDATE_MANIFEST_URL',
  defaultValue: 'https://elysium.s3.cloud.ru/update.json',
);

/// The manifest is a JSON object published next to the assets:
///
/// ```json
/// {
///   "version": "0.5.1",
///   "notes": "- What changed\n- What else changed",
///   "page": "https://example.com/download",
///   "urls": {
///     "windows-amd64": "https://bucket/FlClashX-windows-amd64-setup.exe",
///     "windows-arm64": "https://bucket/FlClashX-windows-arm64-setup.exe",
///     "macos-arm64": "https://bucket/FlClashX-macos-arm64.dmg",
///     "macos-amd64": "https://bucket/FlClashX-macos-amd64.dmg",
///     "android": "https://bucket/FlClashX-android-universal.apk"
///   },
///   "sha256s": {"windows-amd64": "…", "macos-arm64": "…"},
///   "sha256": "…"
/// }
/// ```
///
/// `urls`/`sha256s` may also be keyed by platform alone, and the flat legacy
/// keys (`windows_url`, `apk_url`, `url`, …) are still understood, so an
/// existing manifest keeps working. A `files` list of `{name, url, sha256}`
/// entries is accepted too, matched by the names the release builder uses.
class AppUpdate {
  const AppUpdate({
    required this.version,
    required this.downloadUrl,
    this.notes,
    this.sha256,
    this.pageUrl,
  });

  final String version;
  final String? downloadUrl;
  final String? notes;

  /// Hex SHA-256 of the asset; the download is refused when it does not match.
  final String? sha256;

  /// Where to send the user when the app cannot install by itself.
  final String? pageUrl;

  bool get hasDownload => downloadUrl != null && downloadUrl!.isNotEmpty;

  String get displayVersion => version.startsWith('v') ? version : 'v$version';
}

/// The name the release builder gives an asset for [platform] and [arch].
String expectedAssetName(String platform, String arch) => switch (platform) {
  'windows' => 'FlClashX-windows-$arch-setup.exe',
  'android' => 'FlClashX-android-universal.apk',
  'macos' => 'FlClashX-macos-$arch.dmg',
  _ => 'FlClashX-$platform-$arch',
};

/// Reads a manifest and answers with the update to install, or null when the
/// manifest is malformed, older than [currentVersion] or has nothing for
/// [platform]/[arch].
AppUpdate? parseUpdateManifest(
  Map<String, dynamic> json, {
  required String currentVersion,
  required String platform,
  required String arch,
}) {
  final version = normalizeVersion(
    (json['version'] ?? json['tag'] ?? json['tag_name']) as String? ?? '',
  );
  if (version.isEmpty || compareAppVersions(version, currentVersion) <= 0) {
    return null;
  }
  final downloadUrl = _manifestDownloadUrl(json, platform, arch);
  final pageUrl = _manifestPageUrl(json);
  if ((downloadUrl == null || downloadUrl.isEmpty) &&
      (pageUrl == null || pageUrl.isEmpty)) {
    return null;
  }
  final notes = (json['notes'] ?? json['body'] ?? json['changelog']) as String?;
  return AppUpdate(
    version: version,
    downloadUrl: downloadUrl,
    notes: notes,
    sha256: _manifestSha256(json, platform, arch),
    pageUrl: pageUrl,
  );
}

String? _manifestDownloadUrl(
  Map<String, dynamic> json,
  String platform,
  String arch,
) {
  for (final key in [platform, '$platform-$arch', '$platform-$arch-url']) {
    final urls = json['urls'];
    if (urls is Map) {
      final value = urls[key];
      if (value is String && value.isNotEmpty) return value;
      if (value is Map) {
        final nested = value['url'];
        if (nested is String && nested.isNotEmpty) return nested;
      }
    }
  }
  final files = json['files'];
  if (files is List) {
    final expected = expectedAssetName(platform, arch);
    for (final item in files) {
      if (item is! Map) continue;
      if (item['name'] != expected) continue;
      final url = item['url'];
      if (url is String && url.isNotEmpty) return url;
    }
  }
  final legacy = <Object?>[
    json['${platform}_${arch}_url'],
    json['${platform}_$arch'],
    switch (platform) {
      'android' => json['apk_url'] ?? json['android_url'],
      'windows' => json['windows_url'] ?? json['exe_url'],
      'macos' => json['macos_url'] ?? json['dmg_url'],
      _ => null,
    },
    json['url'],
  ];
  for (final value in legacy) {
    if (value is String && value.isNotEmpty) return value;
  }
  return null;
}

String? _manifestSha256(
  Map<String, dynamic> json,
  String platform,
  String arch,
) {
  final sha256s = json['sha256s'] ?? json['sha256_map'];
  if (sha256s is Map) {
    for (final key in ['$platform-$arch', platform]) {
      final value = sha256s[key];
      if (value is String && value.isNotEmpty) return _normalizeSha256(value);
      if (value is Map) {
        final nested = value['sha256'];
        if (nested is String && nested.isNotEmpty) {
          return _normalizeSha256(nested);
        }
      }
    }
  }
  final files = json['files'];
  if (files is List) {
    final expected = expectedAssetName(platform, arch);
    for (final item in files) {
      if (item is! Map || item['name'] != expected) continue;
      final digest = item['sha256'] ?? item['digest'];
      if (digest is String && digest.isNotEmpty) {
        return _normalizeSha256(digest);
      }
    }
  }
  final single = json['sha256'];
  return single is String && single.isNotEmpty
      ? _normalizeSha256(single)
      : null;
}

String? _manifestPageUrl(Map<String, dynamic> json) {
  for (final key in ['page', 'page_url', 'homepage', 'release_url']) {
    final value = json[key];
    if (value is String && value.isNotEmpty) return value;
  }
  return null;
}

String _normalizeSha256(String value) {
  final normalized = value.trim().toLowerCase();
  return normalized.startsWith('sha256:')
      ? normalized.substring('sha256:'.length)
      : normalized;
}

/// `v0.5.1` -> `0.5.1`; keeps a `-pre.N` suffix.
String normalizeVersion(String value) {
  var version = value.trim();
  if (version.startsWith('v') || version.startsWith('V')) {
    version = version.substring(1);
  }
  return version.split('+').first;
}

/// Compares dotted versions, ordering a prerelease below its release:
/// `0.5.1-pre.2 < 0.5.1`.
int compareAppVersions(String version1, String version2) {
  final left = _ParsedVersion.parse(version1);
  final right = _ParsedVersion.parse(version2);
  final core = _compareLists(left.core, right.core);
  if (core != 0) return core;
  if (left.pre.isEmpty && right.pre.isEmpty) return 0;
  if (left.pre.isEmpty) return 1;
  if (right.pre.isEmpty) return -1;
  return _compareLists(left.pre, right.pre);
}

int _compareLists(List<Object> left, List<Object> right) {
  final length = left.length > right.length ? left.length : right.length;
  for (var i = 0; i < length; i++) {
    final a = i < left.length ? left[i] : 0;
    final b = i < right.length ? right[i] : 0;
    if (a is int && b is int) {
      if (a != b) return a.compareTo(b);
      continue;
    }
    final result = a.toString().compareTo(b.toString());
    if (result != 0) return result;
  }
  return 0;
}

class _ParsedVersion {
  const _ParsedVersion(this.core, this.pre);

  factory _ParsedVersion.parse(String version) {
    final clean = normalizeVersion(version);
    final separator = clean.indexOf('-');
    final coreText = separator == -1 ? clean : clean.substring(0, separator);
    final preText = separator == -1 ? '' : clean.substring(separator + 1);
    final core = coreText
        .split('.')
        .map((part) => int.tryParse(part) ?? 0)
        .toList();
    final pre = preText.isEmpty
        ? const <Object>[]
        : preText
              .split('.')
              .map<Object>((part) => int.tryParse(part) ?? part)
              .toList();
    return _ParsedVersion(core, pre);
  }

  final List<Object> core;
  final List<Object> pre;
}

/// Checks the manifest and downloads and starts the platform installer.
class AppUpdateService {
  AppUpdateService._();

  /// Platforms that can install an update without help.
  static bool get canSelfInstall =>
      selfInstallOverride ??
      (Platform.isAndroid || Platform.isWindows || Platform.isMacOS);

  /// Lets a test run the desktop flow on the host it runs on.
  @visibleForTesting
  static bool? selfInstallOverride;

  /// Replaced by tests to answer the manifest without the network.
  @visibleForTesting
  static Dio? httpClient;

  /// Replaced by tests: `PackageInfo.fromPlatform()` needs a real host.
  @visibleForTesting
  static String? currentVersionOverride;

  static String get platformName => Platform.isAndroid
      ? 'android'
      : Platform.isWindows
      ? 'windows'
      : Platform.isMacOS
      ? 'macos'
      : 'linux';

  static String get archName {
    final version = Platform.version.toLowerCase();
    return version.contains('arm64') || version.contains('aarch64')
        ? 'arm64'
        : 'amd64';
  }

  /// Null when the manifest is unreachable, the build is current, or this
  /// platform has nothing to install.
  static Future<AppUpdate?> check({String? currentVersion}) async {
    if (!canSelfInstall || updateManifestUrl.isEmpty) return null;
    final version =
        currentVersion ??
        currentVersionOverride ??
        (await PackageInfo.fromPlatform()).version;
    final dio = httpClient ?? Dio();
    final ownsClient = httpClient == null;
    try {
      final response = await dio.get<String>(
        updateManifestUrl,
        options: Options(
          responseType: ResponseType.plain,
          receiveTimeout: const Duration(seconds: 20),
          sendTimeout: const Duration(seconds: 20),
        ),
      );
      final decoded = jsonDecode(response.data ?? '');
      if (decoded is! Map<String, dynamic>) return null;
      return parseUpdateManifest(
        decoded,
        currentVersion: version,
        platform: platformName,
        arch: archName,
      );
    } catch (error) {
      commonPrint.log(
        'update manifest failed: ${compactError(error)}',
        logLevel: LogLevel.warning,
      );
      return null;
    } finally {
      if (ownsClient) dio.close();
    }
  }

  /// Downloads the asset, verifies its SHA-256 when the manifest carries one
  /// and starts the installer. The caller quits on desktop so the installer
  /// can replace the running files.
  static Future<bool> downloadAndInstall(
    AppUpdate update, {
    void Function(int received, int total)? onProgress,
  }) async {
    final url = update.downloadUrl;
    if (url == null || url.isEmpty) return false;
    if (Platform.isAndroid) return _installAndroid(update, onProgress);
    if (Platform.isWindows) return _installWindows(update, onProgress);
    if (Platform.isMacOS) return _installMacOS(update, onProgress);
    return false;
  }

  // --------------------------------------------------------------- Android

  static Future<bool> _installAndroid(
    AppUpdate update,
    void Function(int, int)? onProgress,
  ) async {
    final directory = await getApplicationSupportDirectory();
    final apk = await _download(
      update,
      p.join(directory.path, 'flclashx-update.apk'),
      onProgress: onProgress,
    );
    if (apk == null) return false;
    return await app?.installApk(apk.path) ?? false;
  }

  // --------------------------------------------------------------- Windows

  static Future<bool> _installWindows(
    AppUpdate update,
    void Function(int, int)? onProgress,
  ) async {
    final directory = await getTemporaryDirectory();
    final setup = await _download(
      update,
      p.join(directory.path, 'FlClashX-update-setup.exe'),
      onProgress: onProgress,
    );
    if (setup == null) return false;

    // The Inno Setup installer needs elevation and closes the running app, so
    // wait for this instance, install silently (one UAC prompt) and relaunch.
    // The script is detached, so it outlives the process that started it.
    final script = File(p.join(directory.path, 'FlClashX-update.ps1'));
    final appExe = Platform.resolvedExecutable;
    final escapedSetup = _psQuote(setup.path);
    final escapedApp = _psQuote(appExe);
    final escapedScript = _psQuote(script.path);
    await script.writeAsString(
      '\$appPid = $pid\n'
      'while (Get-Process -Id \$appPid -ErrorAction SilentlyContinue) {\n'
      '  Start-Sleep -Milliseconds 400\n'
      '}\n'
      'try {\n'
      '  Start-Process -FilePath $escapedSetup '
      "-ArgumentList '/SILENT','/CLOSEAPPLICATIONS','/NORESTART' "
      '-Verb RunAs -Wait -ErrorAction Stop\n'
      '} catch {}\n'
      'Start-Process -FilePath $escapedApp\n'
      'Remove-Item -LiteralPath $escapedScript -Force '
      '-ErrorAction SilentlyContinue\n',
      flush: true,
    );
    try {
      await Process.start('powershell.exe', [
        '-NoProfile',
        '-ExecutionPolicy',
        'Bypass',
        '-WindowStyle',
        'Hidden',
        '-File',
        script.path,
      ], mode: ProcessStartMode.detached);
      return true;
    } catch (error) {
      commonPrint.log(
        'update installer failed: ${compactError(error)}',
        logLevel: LogLevel.warning,
      );
      return false;
    }
  }

  /// PowerShell single-quoted literal: single quotes are doubled.
  static String _psQuote(String value) => "'${value.replaceAll("'", "''")}'";

  // ----------------------------------------------------------------- macOS

  static Future<bool> _installMacOS(
    AppUpdate update,
    void Function(int, int)? onProgress,
  ) async {
    final directory = await getTemporaryDirectory();
    final dmg = await _download(
      update,
      p.join(directory.path, 'FlClashX-update.dmg'),
      onProgress: onProgress,
    );
    if (dmg == null) return false;

    final appPath = _macAppBundlePath();
    if (appPath == null) {
      // Running from the DMG or a translocated copy: the bundle cannot be
      // replaced in place, so open the image and let the user drag it over.
      await Process.start('open', [dmg.path], mode: ProcessStartMode.detached);
      return false;
    }

    final replaceScript = File(p.join(directory.path, 'flclashx-replace.sh'));
    await replaceScript.writeAsString(
      '#!/bin/bash\n'
      'set -e\n'
      'APP=${_shQuote(appPath)}\n'
      'SRC="\$1"\n'
      'OLD="\$APP.old"\n'
      'rm -rf "\$OLD"\n'
      'mv "\$APP" "\$OLD"\n'
      'if /usr/bin/ditto "\$SRC" "\$APP"; then\n'
      '  rm -rf "\$OLD"\n'
      'else\n'
      '  rm -rf "\$APP"\n'
      '  mv "\$OLD" "\$APP"\n'
      '  exit 1\n'
      'fi\n',
      flush: true,
    );

    final script = File(p.join(directory.path, 'flclashx-update.sh'));
    await script.writeAsString(
      '#!/bin/bash\n'
      'APP=${_shQuote(appPath)}\n'
      'DMG=${_shQuote(dmg.path)}\n'
      'REPLACE=${_shQuote(replaceScript.path)}\n'
      'PID=$pid\n'
      'while kill -0 "\$PID" 2>/dev/null; do sleep 0.4; done\n'
      'MNT="\$(mktemp -d /tmp/flclashx-update.XXXXXX)"\n'
      'if ! /usr/bin/hdiutil attach -nobrowse -quiet "\$DMG" '
      '-mountpoint "\$MNT"; then\n'
      '  rmdir "\$MNT" 2>/dev/null\n'
      '  /usr/bin/open "\$APP"\n'
      '  exit 1\n'
      'fi\n'
      'SRC="\$(/usr/bin/find "\$MNT" -maxdepth 1 -name "*.app" -print -quit)"\n'
      'if [ -n "\$SRC" ]; then\n'
      '  if ! /bin/bash "\$REPLACE" "\$SRC"; then\n'
      '    /usr/bin/osascript -e "do shell script \\"/bin/bash '
      '${_shForAppleScript(replaceScript.path)} '
      '${_shForAppleScript(r'$SRC')}\\" with administrator privileges"\n'
      '  fi\n'
      'fi\n'
      '/usr/bin/hdiutil detach "\$MNT" -quiet\n'
      'rm -rf "\$MNT"\n'
      '/usr/bin/open "\$APP"\n'
      'rm -f "\$REPLACE" "\$0"\n',
      flush: true,
    );
    await Process.run('chmod', ['+x', script.path]);
    try {
      await Process.start('/bin/bash', [
        script.path,
      ], mode: ProcessStartMode.detached);
      return true;
    } catch (error) {
      commonPrint.log(
        'update installer failed: ${compactError(error)}',
        logLevel: LogLevel.warning,
      );
      return false;
    }
  }

  /// /Applications/FlClashX.app/Contents/MacOS/FlClashX -> .../FlClashX.app
  static String? _macAppBundlePath() {
    var path = Platform.resolvedExecutable;
    for (var i = 0; i < 3; i++) {
      path = p.dirname(path);
    }
    if (!path.endsWith('.app')) return null;
    // A DMG mount is read-only and a translocated bundle disappears on exit.
    if (path.startsWith('/Volumes/') || path.contains('AppTranslocation')) {
      return null;
    }
    return path;
  }

  /// Bash single-quoted literal.
  static String _shQuote(String value) => "'${value.replaceAll("'", "'\\''")}'";

  /// A shell word that stays safe inside an AppleScript double-quoted string.
  static String _shForAppleScript(String value) =>
      _shQuote(value).replaceAll(r'\', r'\\').replaceAll('"', r'\"');

  // ---------------------------------------------------------------- helpers

  static Future<File?> _download(
    AppUpdate update,
    String path, {
    void Function(int received, int total)? onProgress,
  }) async {
    final file = File(path);
    final dio = httpClient ?? Dio();
    final ownsClient = httpClient == null;
    try {
      if (await file.exists()) await file.delete();
      await dio.download(
        update.downloadUrl!,
        path,
        onReceiveProgress: onProgress,
        deleteOnError: true,
        options: Options(
          followRedirects: true,
          receiveTimeout: const Duration(minutes: 10),
        ),
      );
      if (!await file.exists()) return null;
      final expected = update.sha256;
      if (expected != null && expected.isNotEmpty) {
        final actual = (await sha256.bind(file.openRead()).first).toString();
        if (actual.toLowerCase() != expected.toLowerCase()) {
          commonPrint.log(
            'update checksum mismatch: expected $expected, got $actual',
            logLevel: LogLevel.warning,
          );
          await file.delete();
          return null;
        }
      }
      return file;
    } catch (error) {
      commonPrint.log(
        'update download failed: ${compactError(error)}',
        logLevel: LogLevel.warning,
      );
      if (await file.exists()) await file.delete();
      return null;
    } finally {
      if (ownsClient) dio.close();
    }
  }
}
