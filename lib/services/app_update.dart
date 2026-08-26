import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flclashx/plugins/app.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';

/// S3 manifest URL is supplied at build time, for example:
/// --dart-define=FLCLASHX_UPDATE_MANIFEST_URL=https://bucket.s3.amazonaws.com/update.json
const _manifestUrl = String.fromEnvironment('FLCLASHX_UPDATE_MANIFEST_URL');

class AppUpdate {
  const AppUpdate({
    required this.version,
    required this.apkUrl,
    this.sha256,
    this.notes,
  });

  final String version;
  final String apkUrl;
  final String? sha256;
  final String? notes;

  factory AppUpdate.fromJson(Map<String, dynamic> json) => AppUpdate(
        version: json['version'] as String? ?? '',
        apkUrl: json['apk_url'] as String? ?? json['url'] as String? ?? '',
        sha256: (json['sha256'] as String?)?.toLowerCase(),
        notes: json['notes'] as String?,
      );
}

class AppUpdateService {
  static Future<AppUpdate?> check() async {
    if (!Platform.isAndroid || _manifestUrl.isEmpty) return null;
    try {
      final response = await Dio().get<String>(
        _manifestUrl,
        options: Options(responseType: ResponseType.plain),
      );
      final update = AppUpdate.fromJson(
        jsonDecode(response.data ?? '') as Map<String, dynamic>,
      );
      final current = (await PackageInfo.fromPlatform()).version;
      if (update.apkUrl.isEmpty || update.version.isEmpty ||
          !_isNewer(update.version, current)) return null;
      return update;
    } catch (_) {
      return null;
    }
  }

  static Future<bool> downloadAndInstall(AppUpdate update) async {
    final directory = await getApplicationSupportDirectory();
    final file = File('${directory.path}/flclashx-update.apk');
    try {
      await Dio().download(update.apkUrl, file.path,
          options: Options(headers: {'Accept': 'application/vnd.android.package-archive'}));
      if (update.sha256 != null) {
        final digest = sha256.convert(await file.readAsBytes()).toString();
        if (digest != update.sha256) {
          await file.delete();
          return false;
        }
      }
      return await app?.installApk(file.path) ?? false;
    } catch (_) {
      if (await file.exists()) await file.delete();
      return false;
    }
  }

  static bool _isNewer(String candidate, String current) {
    List<int> parts(String value) => value.split('+').first
        .split('.')
        .map((part) => int.tryParse(part.replaceAll(RegExp(r'[^0-9].*'), '')) ?? 0)
        .toList();
    final a = parts(candidate), b = parts(current);
    for (var i = 0; i < (a.length > b.length ? a.length : b.length); i++) {
      final av = i < a.length ? a[i] : 0;
      final bv = i < b.length ? b[i] : 0;
      if (av != bv) return av > bv;
    }
    return false;
  }
}
