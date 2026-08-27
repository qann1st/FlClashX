import 'dart:convert';
import 'dart:io';

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
    this.notes,
  });

  final String version;
  final String apkUrl;
  final String? notes;

  factory AppUpdate.fromJson(Map<String, dynamic> json) => AppUpdate(
        version: json['version'] as String? ?? '',
        apkUrl: json['apk_url'] as String? ?? json['url'] as String? ?? '',
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
      if (update.apkUrl.isEmpty ||
          update.version.isEmpty ||
          update.version == current) {
        return null;
      }
      return update;
    } catch (_) {
      return null;
    }
  }

  static Future<bool> downloadAndInstall(
    AppUpdate update, {
    void Function(int received, int total)? onProgress,
  }) async {
    final directory = await getApplicationSupportDirectory();
    final file = File('${directory.path}/flclashx-update.apk');
    try {
      await Dio().download(
        update.apkUrl,
        file.path,
        onReceiveProgress: onProgress,
        options: Options(
          headers: {'Accept': 'application/vnd.android.package-archive'},
        ),
      );
      return await app?.installApk(file.path) ?? false;
    } catch (_) {
      if (await file.exists()) await file.delete();
      return false;
    }
  }
}
