import 'dart:convert';
import 'dart:io';

import 'package:args/args.dart';
import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as p;

/// Writes the manifest the app checks for updates.
///
/// The app never asks GitHub for anything: `update.json` and the assets it
/// points at are served from an S3 bucket. Upload the assets first and the
/// manifest last, so a client never reads a manifest whose files are missing.
///
/// ```sh
/// dart run tool/update_manifest.dart \
///   --dist dist \
///   --version 0.5.1 \
///   --base-url https://elysium.s3.cloud.ru/releases/0.5.1 \
///   --notes notes.md \
///   --out update.json
/// ```
///
/// `--base-url` is the prefix the assets are uploaded under; file names are
/// appended to it unchanged.
void main(List<String> arguments) {
  final parser = ArgParser()
    ..addOption('dist', help: 'Directory with the built artifacts.')
    ..addOption('version', help: 'Released version, for example 0.5.1.')
    ..addOption('base-url', help: 'URL prefix the artifacts are uploaded to.')
    ..addOption('notes', help: 'File with release notes, one bullet per line.')
    ..addOption('page', help: 'Download page offered when no asset fits.')
    ..addOption('out', defaultsTo: 'update.json', help: 'Manifest to write.')
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

  final dist = args.option('dist');
  final version = args.option('version');
  final baseUrl = args.option('base-url');
  if (dist == null || version == null || baseUrl == null) {
    stderr.writeln('--dist, --version and --base-url are required\n');
    stderr.writeln(parser.usage);
    exit(64);
  }
  final directory = Directory(dist);
  if (!directory.existsSync()) {
    stderr.writeln('no such directory: $dist');
    exit(66);
  }

  final urls = <String, String>{};
  final sha256s = <String, String>{};
  for (final entry in artifactNames.entries) {
    final file = File(p.join(directory.path, entry.value));
    if (!file.existsSync()) {
      stderr.writeln('skip ${entry.key}: ${entry.value} is not in $dist');
      continue;
    }
    urls[entry.key] =
        '${baseUrl.replaceAll(RegExp(r'/+$'), '')}/${entry.value}';
    sha256s[entry.key] = sha256.convert(file.readAsBytesSync()).toString();
  }
  if (urls.isEmpty) {
    stderr.writeln('nothing to publish: no known artifact in $dist');
    exit(66);
  }

  final notesFile = args.option('notes');
  final notes = notesFile == null
      ? null
      : File(notesFile).existsSync()
      ? File(notesFile).readAsStringSync().trim()
      : null;
  if (notesFile != null && notes == null) {
    stderr.writeln('no such notes file: $notesFile');
    exit(66);
  }

  final manifest = <String, Object?>{
    'version': version,
    if (notes != null && notes.isNotEmpty) 'notes': notes,
    if (args.option('page') != null) 'page': args.option('page'),
    'urls': urls,
    'sha256s': sha256s,
  };
  final out = File(args.option('out') ?? 'update.json');
  out.writeAsStringSync(
    '${const JsonEncoder.withIndent('  ').convert(manifest)}\n',
  );

  stdout.writeln('Wrote ${out.path}\n');
  stdout.writeln('Upload these files to ${args.option('base-url')}:');
  for (final name in urls.keys.map((key) => artifactNames[key]!)) {
    stdout.writeln('  $name');
  }
  stdout.writeln('  ${p.basename(out.path)} (last)');
}

/// The names `setup.dart` gives the artifacts the app installs.
const artifactNames = <String, String>{
  'windows-amd64': 'FlClashX-windows-amd64-setup.exe',
  'windows-arm64': 'FlClashX-windows-arm64-setup.exe',
  'macos-amd64': 'FlClashX-macos-amd64.dmg',
  'macos-arm64': 'FlClashX-macos-arm64.dmg',
  'android': 'FlClashX-android-universal.apk',
};
