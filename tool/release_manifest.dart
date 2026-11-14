import 'dart:convert';
import 'dart:io';

import 'package:args/args.dart';
import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as p;

/// Writes a checksum manifest for every release asset.
///
/// Unlike `update.json`, this is not consumed by the application. It is an
/// inventory for publishing the release files to S3 and for users who want to
/// verify a manual download.
void main(List<String> arguments) {
  final parser = ArgParser()
    ..addOption('dist', help: 'Directory with release assets.')
    ..addOption('version', help: 'Released version, for example 0.6.1.')
    ..addOption('base-url', help: 'S3 URL prefix for the release assets.')
    ..addOption('out', defaultsTo: 'manifest.json', help: 'File to write.')
    ..addFlag('help', abbr: 'h', negatable: false);

  final ArgResults args;
  try {
    args = parser.parse(arguments);
  } on FormatException catch (error) {
    stderr.writeln('${error.message}\n\n${parser.usage}');
    exit(64);
  }
  if (args.flag('help')) {
    stdout.writeln(parser.usage);
    return;
  }

  final distPath = args.option('dist');
  final version = args.option('version');
  final baseUrl = args.option('base-url');
  if (distPath == null || version == null || baseUrl == null) {
    stderr.writeln('--dist, --version and --base-url are required\n');
    stderr.writeln(parser.usage);
    exit(64);
  }
  final directory = Directory(distPath);
  if (!directory.existsSync()) {
    stderr.writeln('no such directory: $distPath');
    exit(66);
  }

  final out = File(args.option('out') ?? 'manifest.json');
  final manifest = createReleaseManifest(
    directory: directory,
    version: version,
    baseUrl: baseUrl,
    excludedFileName: p.basename(out.path),
  );
  out.writeAsStringSync(
    '${const JsonEncoder.withIndent('  ').convert(manifest)}\n',
  );
  stdout.writeln('Wrote ${out.path}');
}

Map<String, Object> createReleaseManifest({
  required Directory directory,
  required String version,
  required String baseUrl,
  String excludedFileName = 'manifest.json',
}) {
  final normalizedBaseUrl = baseUrl.replaceAll(RegExp(r'/+$'), '');
  final assets = directory
      .listSync()
      .whereType<File>()
      .where((file) => p.basename(file.path) != excludedFileName)
      .map((file) {
        final name = p.basename(file.path);
        return <String, String>{
          'name': name,
          'url': '$normalizedBaseUrl/$name',
          'sha256': sha256.convert(file.readAsBytesSync()).toString(),
        };
      })
      .toList()
    ..sort((a, b) => a['name']!.compareTo(b['name']!));
  return {'version': version, 'assets': assets};
}
