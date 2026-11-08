import 'dart:io';

import 'package:fl_clash/common/common.dart';
import 'package:fl_clash/database/database.dart';
import 'package:fl_clash/enum/enum.dart';
import 'package:path/path.dart';

Future<void> clearAppData() async {
  await preferences.clearPreferences();
  try {
    await database.close();
  } catch (error) {
    // Drift rethrows a failed open from close; that file still has to go.
    commonPrint.log(
      'close database failed: $error',
      logLevel: LogLevel.warning,
    );
  }
  await File(await appPath.databasePath).safeDelete(recursive: true);
  await Directory(await appPath.profilesPath).safeDelete(recursive: true);
}

/// What a smart group and LightGBM keep in the core home. The files outlive
/// the group that needed them, and a stale model or a training file that keeps
/// growing has an idle phone writing to disk, so a new release removes them.
const _staleCoreArtifacts = ['Model.bin', 'model.bin', 'smart_weight_data.csv'];

/// Removes those files once per [release], comparing against the release that
/// last cleaned them, so an update starts without the previous one's leftovers.
Future<void> cleanStaleCoreArtifacts(String release) async {
  try {
    if (await preferences.getCleanedCoreArtifactsRelease() == release) {
      return;
    }
    final home = Directory(await appPath.homeDirPath);
    if (await home.exists()) {
      for (final name in _staleCoreArtifacts) {
        final file = File(join(home.path, name));
        if (await file.exists()) {
          await file.delete();
          commonPrint.log('removed stale core artifact $name');
        }
      }
    }
    await preferences.setCleanedCoreArtifactsRelease(release);
  } catch (error) {
    commonPrint.log(
      'clean stale core artifacts failed: ${compactError(error)}',
      logLevel: LogLevel.warning,
    );
  }
}

/// [clearAppData] plus the rest of the app home, bar the lock this process holds.
Future<void> resetAppData() async {
  await clearAppData();
  await fileLogger.dispose();
  final home = Directory(await appPath.homeDirPath);
  final lockPath = await appPath.lockFilePath;
  if (!await home.exists()) {
    return;
  }
  await for (final entity in home.list(followLinks: false)) {
    if (entity.path == lockPath) {
      continue;
    }
    try {
      await entity.delete(recursive: true);
    } on FileSystemException catch (error) {
      commonPrint.log(
        'reset left ${basename(entity.path)}: ${error.message}',
        logLevel: LogLevel.warning,
      );
    }
  }
}
