import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as p;

import '../../tool/release_manifest.dart' as manifest;

void main() {
  test('lists release files with S3 URLs and SHA-256 hashes', () {
    final directory = Directory.systemTemp.createTempSync('release-manifest-');
    addTearDown(() => directory.deleteSync(recursive: true));
    File(
      p.join(directory.path, 'FlClashX-linux-amd64.pkg'),
    ).writeAsStringSync('amd64');
    File(p.join(directory.path, 'update.json')).writeAsStringSync('{}');
    File(p.join(directory.path, 'manifest.json')).writeAsStringSync('stale');

    final result = manifest.createReleaseManifest(
      directory: directory,
      version: '0.6.1',
      baseUrl: 'https://elysium.s3.cloud.ru/releases/0.6.1/',
    );
    final assets = result['assets']! as List<Map<String, String>>;

    expect(result['version'], '0.6.1');
    expect(assets.map((asset) => asset['name']), [
      'FlClashX-linux-amd64.pkg',
      'update.json',
    ]);
    expect(assets.first, {
      'name': 'FlClashX-linux-amd64.pkg',
      'url': 'https://elysium.s3.cloud.ru/releases/0.6.1/FlClashX-linux-amd64.pkg',
      'sha256': sha256.convert('amd64'.codeUnits).toString(),
    });
  });
}
