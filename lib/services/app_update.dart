import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flclashx/common/constant.dart';
import 'package:flclashx/common/windows.dart';
import 'package:flclashx/plugins/app.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

/// Optional override manifest, supplied at build time, for example:
/// --dart-define=FLCLASHX_UPDATE_MANIFEST_URL=https://bucket.s3.amazonaws.com/update.json
///
/// When it is missing (or has no asset for the current platform) GitHub
/// releases are used, which is what CI publishes.
const _manifestUrl = String.fromEnvironment('FLCLASHX_UPDATE_MANIFEST_URL');

/// Exact release tag baked in by CI (e.g. "v0.4.13" or "v0.4.13-pre.2").
/// Empty on local builds, where the pubspec version is used instead.
const _appVersionDefine = String.fromEnvironment('APP_VERSION');
const _isPreBuild = String.fromEnvironment('APP_ENV') != 'stable';

class AppUpdate {
  const AppUpdate({
    required this.version,
    required this.downloadUrl,
    this.notes,
    this.sha256,
  });

  final String version;
  final String downloadUrl;
  final String? notes;

  /// Hex SHA-256 of the asset, when the source provides one.
  final String? sha256;
}

class AppUpdateService {
  /// Platforms that can download and start an installer themselves.
  static bool get supported =>
      Platform.isAndroid || Platform.isWindows || Platform.isMacOS;

  static Future<AppUpdate?> check() async {
    if (!supported) return null;
    final current = await _currentVersion();
    final fromManifest = await _checkManifest(current);
    if (fromManifest != null) return fromManifest;
    return _checkGitHub(current);
  }

  static Future<AppUpdate?> _checkManifest(String current) async {
    if (_manifestUrl.isEmpty) return null;
    try {
      final response = await Dio().get<String>(
        _manifestUrl,
        options: Options(
          responseType: ResponseType.plain,
          receiveTimeout: const Duration(seconds: 15),
          sendTimeout: const Duration(seconds: 15),
        ),
      );
      final decoded = jsonDecode(response.data ?? '');
      if (decoded is! Map<String, dynamic>) return null;
      final version = _normalizeVersion(decoded['version'] as String? ?? '');
      if (version.isEmpty || _compareVersions(version, current) <= 0) {
        return null;
      }
      final url = _manifestUrlForPlatform(decoded);
      if (url == null || url.isEmpty) return null;
      return AppUpdate(
        version: version,
        downloadUrl: url,
        notes: decoded['notes'] as String?,
        sha256: _normalizeSha256(decoded['sha256'] as String?),
      );
    } catch (_) {
      return null;
    }
  }

  static Future<AppUpdate?> _checkGitHub(String current) async {
    try {
      final response = await Dio().get<List<dynamic>>(
        'https://api.github.com/repos/$repository/releases',
        queryParameters: {'per_page': 30},
        options: Options(
          responseType: ResponseType.json,
          headers: {'Accept': 'application/vnd.github+json'},
        ),
      );
      if (response.statusCode != 200) return null;
      for (final item in response.data ?? const []) {
        if (item is! Map<String, dynamic>) continue;
        if (item['draft'] == true) continue;
        // Pre-release builds may move forward between pre-releases; stable
        // builds must never be offered a pre-release.
        if (item['prerelease'] == true && !_isPreBuild) continue;
        final tag = item['tag_name'] as String? ?? '';
        if (!tag.startsWith('v') && !tag.startsWith('V')) continue;
        final version = _normalizeVersion(tag);
        if (version.isEmpty || _compareVersions(version, current) <= 0) {
          continue;
        }
        final asset = _pickAsset(item['assets']);
        if (asset == null) continue;
        final digest = asset['digest'] as String?;
        return AppUpdate(
          version: version,
          downloadUrl: asset['browser_download_url'] as String? ?? '',
          notes: item['body'] as String?,
          sha256: digest != null && digest.startsWith('sha256:')
              ? digest.substring(7)
              : null,
        );
      }
    } catch (_) {
      // Network/rate-limit errors are not fatal: the manual check falls back
      // to the release page.
    }
    return null;
  }

  /// Downloads the new build and starts the platform installer/updater.
  ///
  /// Returns true once the detached updater has been launched; the caller is
  /// expected to quit the app on desktop so the installer can replace files.
  static Future<bool> downloadAndInstall(
    AppUpdate update, {
    void Function(int received, int total)? onProgress,
  }) async {
    if (Platform.isAndroid) return _installAndroid(update, onProgress);
    if (Platform.isWindows) return _installWindows(update, onProgress);
    if (Platform.isMacOS) return _installMacOS(update, onProgress);
    return false;
  }

  // ---------------------------------------------------------------- Android

  static Future<bool> _installAndroid(
    AppUpdate update,
    void Function(int received, int total)? onProgress,
  ) async {
    final directory = await getApplicationSupportDirectory();
    final file = await _download(
      update,
      p.join(directory.path, 'flclashx-update.apk'),
      onProgress: onProgress,
    );
    if (file == null) return false;
    return await app?.installApk(file.path) ?? false;
  }

  // ---------------------------------------------------------------- Windows

  static Future<bool> _installWindows(
    AppUpdate update,
    void Function(int received, int total)? onProgress,
  ) async {
    final directory = await getTemporaryDirectory();
    final setup = await _download(
      update,
      p.join(directory.path, 'FlClashX-update-setup.exe'),
      onProgress: onProgress,
    );
    if (setup == null) return false;

    // The Inno Setup installer requires elevation and kills running app
    // processes, so wait for this instance to exit, install silently (one UAC
    // prompt) and relaunch. The script is detached and survives our exit.
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
      await Process.start(
        'powershell.exe',
        [
          '-NoProfile',
          '-ExecutionPolicy',
          'Bypass',
          '-WindowStyle',
          'Hidden',
          '-File',
          script.path,
        ],
        mode: ProcessStartMode.detached,
      );
    } catch (_) {
      // Fall back to the plain ShellExecute path: the installer still kills the
      // running app and can be completed from its wizard.
      return windows?.runas(
            setup.path,
            '/SILENT /CLOSEAPPLICATIONS /NORESTART',
          ) ??
          false;
    }
    return true;
  }

  /// PowerShell single-quoted literal (single quotes are doubled).
  static String _psQuote(String value) => "'${value.replaceAll("'", "''")}'";

  // ------------------------------------------------------------------ macOS

  static Future<bool> _installMacOS(
    AppUpdate update,
    void Function(int received, int total)? onProgress,
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
      // Running from the DMG (or an App Translocation path): the bundle can't
      // be replaced in place. Open the new disk image so the user can install.
      await Process.start(
        'open',
        [dmg.path],
        mode: ProcessStartMode.detached,
      );
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
    final mount = '\$(mktemp -d /tmp/flclashx-update.XXXXXX)';
    await script.writeAsString(
      '#!/bin/bash\n'
      'APP=${_shQuote(appPath)}\n'
      'DMG=${_shQuote(dmg.path)}\n'
      'REPLACE=${_shQuote(replaceScript.path)}\n'
      'PID=$pid\n'
      'while kill -0 "\$PID" 2>/dev/null; do sleep 0.4; done\n'
      'MNT="$mount"\n'
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
      await Process.start(
        '/bin/bash',
        [script.path],
        mode: ProcessStartMode.detached,
      );
      return true;
    } catch (_) {
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
    // A DMG mount is read-only; translocated bundles disappear on exit.
    if (path.startsWith('/Volumes/') || path.contains('AppTranslocation')) {
      return null;
    }
    return path;
  }

  /// Bash single-quoted literal.
  static String _shQuote(String value) =>
      "'${value.replaceAll("'", "'\\''")}'";

  /// A shell word that is safe inside an AppleScript double-quoted string.
  /// [value] may reference a shell variable (e.g. `$SRC`).
  static String _shForAppleScript(String value) =>
      _shQuote(value).replaceAll(r'\', r'\\').replaceAll('"', r'\"');

  // --------------------------------------------------------------- helpers

  static Future<File?> _download(
    AppUpdate update,
    String path, {
    void Function(int received, int total)? onProgress,
  }) async {
    final file = File(path);
    try {
      if (await file.exists()) await file.delete();
      await Dio().download(
        update.downloadUrl,
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
          await file.delete();
          return null;
        }
      }
      return file;
    } catch (_) {
      if (await file.exists()) await file.delete();
      return null;
    }
  }

  static String? _manifestUrlForPlatform(Map<String, dynamic> json) {
    final platform = Platform.isAndroid
        ? 'android'
        : Platform.isWindows
            ? 'windows'
            : 'macos';
    final urls = json['urls'];
    if (urls is Map) {
      final archSpecific = urls['$platform-${_arch}'];
      if (archSpecific is String && archSpecific.isNotEmpty) {
        return archSpecific;
      }
      final generic = urls[platform];
      if (generic is String && generic.isNotEmpty) return generic;
    }
    final archSpecific = json['${platform}_${_arch}_url'] ??
        json['${platform}_${_arch}'];
    if (archSpecific is String && archSpecific.isNotEmpty) return archSpecific;
    return switch (platform) {
      'android' =>
        json['apk_url'] as String? ?? json['android_url'] as String? ??
            json['url'] as String?,
      'windows' =>
        json['windows_url'] as String? ?? json['exe_url'] as String?,
      _ => json['macos_url'] as String? ?? json['dmg_url'] as String?,
    };
  }

  static Map<String, dynamic>? _pickAsset(dynamic rawAssets) {
    if (rawAssets is! List) return null;
    final arch = _arch;
    final expected = Platform.isWindows
        ? 'FlClashX-windows-$arch-setup.exe'
        : Platform.isAndroid
            ? 'FlClashX-android-universal.apk'
            : 'FlClashX-macos-$arch.dmg';
    for (final item in rawAssets) {
      if (item is Map<String, dynamic> && item['name'] == expected) {
        return item;
      }
    }
    return null;
  }

  static String get _arch {
    final version = Platform.version.toLowerCase();
    return version.contains('arm64') || version.contains('aarch64')
        ? 'arm64'
        : 'amd64';
  }

  static Future<String> _currentVersion() async {
    if (_appVersionDefine.isNotEmpty) {
      return _normalizeVersion(_appVersionDefine);
    }
    try {
      return (await PackageInfo.fromPlatform()).version;
    } catch (_) {
      return '0.0.0';
    }
  }

  static String _normalizeVersion(String value) {
    var version = value.trim();
    if (version.startsWith('v') || version.startsWith('V')) {
      version = version.substring(1);
    }
    return version;
  }

  static String? _normalizeSha256(String? value) {
    if (value == null || value.isEmpty) return null;
    return value.startsWith('sha256:') ? value.substring(7) : value;
  }

  /// Semver-ish comparison that tolerates release tags. A pre-release
  /// ("1.2.3-pre.1") sorts before its final "1.2.3".
  static int _compareVersions(String a, String b) {
    final left = _parseVersion(a);
    final right = _parseVersion(b);
    for (var i = 0; i < 3; i++) {
      final result = left.numbers[i].compareTo(right.numbers[i]);
      if (result != 0) return result;
    }
    if (left.pre == right.pre) return 0;
    if (left.pre == null) return 1;
    if (right.pre == null) return -1;
    return _comparePre(left.pre!, right.pre!);
  }

  static int _comparePre(String a, String b) {
    final left = a.split('.');
    final right = b.split('.');
    for (var i = 0; i < left.length && i < right.length; i++) {
      final leftNumber = int.tryParse(left[i]);
      final rightNumber = int.tryParse(right[i]);
      final result = leftNumber != null && rightNumber != null
          ? leftNumber.compareTo(rightNumber)
          : left[i].compareTo(right[i]);
      if (result != 0) return result;
    }
    return left.length.compareTo(right.length);
  }

  static _ParsedVersion _parseVersion(String value) {
    var version = _normalizeVersion(value).split('+').first;
    String? pre;
    final dash = version.indexOf('-');
    if (dash >= 0) {
      pre = version.substring(dash + 1);
      version = version.substring(0, dash);
    }
    final parts = version.split('.');
    final numbers = List<int>.generate(
      3,
      (index) => index < parts.length ? int.tryParse(parts[index]) ?? 0 : 0,
    );
    return _ParsedVersion(numbers, pre);
  }
}

class _ParsedVersion {
  const _ParsedVersion(this.numbers, this.pre);

  final List<int> numbers;
  final String? pre;
}
