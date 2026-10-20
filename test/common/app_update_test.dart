import 'package:fl_clash/common/app_update.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('compareAppVersions', () {
    test('orders dotted versions numerically', () {
      expect(compareAppVersions('0.5.1', '0.5.0'), greaterThan(0));
      expect(compareAppVersions('0.10.0', '0.9.9'), greaterThan(0));
      expect(compareAppVersions('1.0', '1.0.0'), 0);
      expect(compareAppVersions('v0.5.0', '0.5.0'), 0);
      expect(compareAppVersions('0.4.14', '0.5.0'), lessThan(0));
    });

    test('orders a prerelease below its release', () {
      expect(compareAppVersions('0.5.1-pre.2', '0.5.1'), lessThan(0));
      expect(compareAppVersions('0.5.1-pre.2', '0.5.1-pre.1'), greaterThan(0));
      expect(compareAppVersions('0.5.1-pre.10', '0.5.1-pre.9'), greaterThan(0));
      expect(compareAppVersions('0.5.1', '0.5.1-pre.9'), greaterThan(0));
    });

    test('ignores the build number', () {
      expect(compareAppVersions('0.5.0+2026102001', '0.5.0'), 0);
    });
  });

  group('normalizeVersion', () {
    test('drops the v prefix and the build number', () {
      expect(normalizeVersion(' v0.5.1+20261020 '), '0.5.1');
      expect(normalizeVersion('0.5.1-pre.1'), '0.5.1-pre.1');
    });
  });

  group('parseUpdateManifest', () {
    AppUpdate? parse(
      Map<String, dynamic> json, {
      String currentVersion = '0.5.0',
      String platform = 'windows',
      String arch = 'amd64',
    }) => parseUpdateManifest(
      json,
      currentVersion: currentVersion,
      platform: platform,
      arch: arch,
    );

    test('reads the asset of this platform and architecture', () {
      final update = parse({
        'version': '0.5.1',
        'notes': '- Faster startup\n- Fixed tray',
        'urls': {
          'windows-amd64': 'https://bucket/windows-amd64.exe',
          'windows-arm64': 'https://bucket/windows-arm64.exe',
        },
        'sha256s': {'windows-amd64': 'ABC123'},
      });

      expect(update, isNotNull);
      expect(update!.version, '0.5.1');
      expect(update.displayVersion, 'v0.5.1');
      expect(update.downloadUrl, 'https://bucket/windows-amd64.exe');
      expect(update.sha256, 'abc123');
      expect(update.hasDownload, isTrue);
      expect(update.notes, contains('Faster startup'));
    });

    test('falls back to the platform key and to legacy flat keys', () {
      expect(
        parse({
          'version': '0.5.1',
          'urls': {'windows': 'https://bucket/windows.exe'},
        })?.downloadUrl,
        'https://bucket/windows.exe',
      );
      expect(
        parse({
          'version': '0.5.1',
          'windows_url': 'https://bucket/legacy.exe',
        })?.downloadUrl,
        'https://bucket/legacy.exe',
      );
      expect(
        parse({
          'version': '0.5.1',
          'url': 'https://bucket/any.exe',
        })?.downloadUrl,
        'https://bucket/any.exe',
      );
    });

    test('matches a files list by the name the release builder uses', () {
      final update = parse({
        'version': '0.5.1',
        'files': [
          {
            'name': 'FlClashX-windows-arm64-setup.exe',
            'url': 'https://bucket/arm.exe',
            'sha256': 'sha256:DDEEFF',
          },
          {
            'name': 'FlClashX-windows-amd64-setup.exe',
            'url': 'https://bucket/amd.exe',
            'sha256': 'sha256:112233',
          },
        ],
      }, arch: 'arm64');

      expect(update?.downloadUrl, 'https://bucket/arm.exe');
      expect(update?.sha256, 'ddeeff');
    });

    test('selects the macOS image and the Android package', () {
      expect(
        parse(
          {
            'version': '0.5.1',
            'urls': {'macos-arm64': 'https://bucket/app.dmg'},
          },
          platform: 'macos',
          arch: 'arm64',
        )?.downloadUrl,
        'https://bucket/app.dmg',
      );
      expect(
        parse(
          {
            'version': '0.5.1',
            'urls': {'android': 'https://bucket/app.apk'},
          },
          platform: 'android',
          arch: 'arm64',
        )?.downloadUrl,
        'https://bucket/app.apk',
      );
    });

    test('ignores the same and older versions', () {
      expect(parse({'version': '0.5.0', 'url': 'https://x'}), isNull);
      expect(parse({'version': '0.4.9', 'url': 'https://x'}), isNull);
      expect(parse({'version': '0.5.0', 'url': 'https://x'}), isNull);
    });

    test('accepts a newer prerelease only when it is newer', () {
      expect(
        parse({
          'version': '0.5.1-pre.1',
          'url': 'https://x',
        }, currentVersion: '0.5.0')?.version,
        '0.5.1-pre.1',
      );
      expect(
        parse({
          'version': '0.5.1-pre.1',
          'url': 'https://x',
        }, currentVersion: '0.5.1'),
        isNull,
      );
    });

    test('keeps a download page when the platform has no asset', () {
      final update = parse({
        'version': '0.5.1',
        'page': 'https://example.com/download',
      });

      expect(update, isNotNull);
      expect(update!.hasDownload, isFalse);
      expect(update.pageUrl, 'https://example.com/download');
    });

    test('rejects a manifest without a version or a place to download', () {
      expect(parse({'url': 'https://x'}), isNull);
      expect(parse({'version': '0.5.1'}), isNull);
      expect(parse({'version': '', 'url': 'https://x'}), isNull);
    });
  });

  group('expectedAssetName', () {
    test('matches the release builder', () {
      expect(
        expectedAssetName('windows', 'amd64'),
        'FlClashX-windows-amd64-setup.exe',
      );
      expect(expectedAssetName('macos', 'arm64'), 'FlClashX-macos-arm64.dmg');
      expect(
        expectedAssetName('android', 'arm64'),
        'FlClashX-android-universal.apk',
      );
    });
  });
}
