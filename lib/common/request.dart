import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:dio/io.dart';
import 'package:fl_clash/common/common.dart';
import 'package:fl_clash/enum/enum.dart';
import 'package:fl_clash/state.dart';
import 'package:flutter/foundation.dart' show compute, visibleForTesting;

class Request {
  late final Dio dio;
  late final Dio _clashDio;

  ProviderReader? _read;

  void attach(ProviderReader read) {
    _read = read;
  }

  Request() {
    dio = Dio(BaseOptions(headers: {'User-Agent': browserUa}));
    _clashDio = Dio();
    _clashDio.httpClientAdapter = _proxiedAdapter();
  }

  IOHttpClientAdapter _proxiedAdapter() => IOHttpClientAdapter(
    createHttpClient: () {
      final client = HttpClient();
      client.findProxy = (Uri uri) {
        client.userAgent = globalState.ua;
        final read = _read;
        if (read == null) {
          return 'DIRECT';
        }
        return FlClashHttpOverrides.findProxyForReader(read, uri);
      };
      return client;
    },
  );

  /// The adapter keeps the first HttpClient it makes, under whatever
  /// HttpOverrides held then; a test that changes them starts a new one.
  @visibleForTesting
  void resetClient() => _clashDio.httpClientAdapter = _proxiedAdapter();

  Future<Response<T>> _download<T>(
    String url,
    ResponseType responseType, {
    Duration timeout = downloadTimeoutDuration,
    Map<String, String> headers = const {},
  }) {
    final target = downloadTarget(url);
    final cancelToken = CancelToken();
    var oversized = false;
    return _clashDio
        .getUri<T>(
          target.uri,
          options: Options(
            responseType: responseType,
            headers: {...headers, ...target.headers},
            // Nothing asks for a range, so a 206 is a cut-off copy.
            validateStatus: (status) =>
                status != null &&
                status >= 200 &&
                status < 300 &&
                status != HttpStatus.partialContent,
          ),
          cancelToken: cancelToken,
          onReceiveProgress: (received, total) {
            if (received > maxDownloadBytes || total > maxDownloadBytes) {
              oversized = true;
              cancelToken.cancel();
            }
          },
        )
        .catchError(
          (Object _) =>
              throw MessageException(currentAppLocalizations.downloadTooLarge),
          test: (_) => oversized,
        )
        .timeout(
          timeout,
          onTimeout: () {
            cancelToken.cancel();
            throw DioException.receiveTimeout(
              timeout: timeout,
              requestOptions: RequestOptions(path: url),
            );
          },
        );
  }

  Future<Response<Uint8List>> getFileResponseForUrl(
    String url, {
    Duration timeout = downloadTimeoutDuration,
    Map<String, String> headers = const {},
  }) async {
    try {
      return await _download<Uint8List>(
        url,
        ResponseType.bytes,
        timeout: timeout,
        headers: headers,
      );
    } catch (e) {
      commonPrint.log(
        'getFileResponseForUrl error ${compactError(e)}',
        logLevel: LogLevel.warning,
      );
      rethrow;
    }
  }

  Future<Response<String>> getTextResponseForUrl(String url) async {
    try {
      return await _download<String>(url, ResponseType.plain);
    } catch (e) {
      commonPrint.log(
        'getTextResponseForUrl error ${compactError(e)}',
        logLevel: LogLevel.warning,
      );
      rethrow;
    }
  }
}

final request = Request();

/// How a host let a download down: [soft] when it may be back in a moment,
/// [hard] when the host itself is gone or no longer serves the resource.
enum HostFailure { soft, hard }

const _goneStatuses = {HttpStatus.notFound, HttpStatus.gone};

/// Errno of a name that does not resolve or a port nobody listens on, on
/// macOS and Linux (glibc), then Windows.
const _hardSocketErrors = {8, -2, -3, -5, 61, 111, 11001, 11004, 10061};

HostFailure hostFailureOf(Object error) {
  final cause = error is DioException ? error.error ?? error : error;
  final hard = switch (cause) {
    MessageException() => true,
    HandshakeException() || CertificateException() => true,
    SocketException(:final osError, :final message) =>
      _hardSocketErrors.contains(osError?.errorCode) ||
          message.contains('Failed host lookup') ||
          message.contains('Connection refused'),
    DioException(type: DioExceptionType.badCertificate) => true,
    DioException(type: DioExceptionType.badResponse, :final response) =>
      _goneStatuses.contains(response?.statusCode),
    _ => false,
  };
  return hard ? HostFailure.hard : HostFailure.soft;
}

/// Some subscription servers send a gzip or zlib body without saying so in
/// Content-Encoding, which is the only signal dart:io decompresses on.
Future<Uint8List> decompressDownload(Uint8List bytes) async {
  if (_unlabeledCodecOf(bytes) == null) {
    return bytes;
  }
  final inflated = await compute(decompressUnlabeled, bytes);
  if (inflated == null) {
    throw MessageException(currentAppLocalizations.downloadTooLarge);
  }
  return inflated;
}

@visibleForTesting
Uint8List? decompressUnlabeled(
  Uint8List bytes, {
  int limit = maxDownloadBytes,
}) {
  final codec = _unlabeledCodecOf(bytes);
  if (codec == null) {
    return bytes;
  }
  final output = _CappedSink(limit);
  try {
    codec.decoder.startChunkedConversion(output)
      ..add(bytes)
      ..close();
  } on _OutputTooLarge {
    return null;
  } on FormatException {
    return bytes;
  }
  return output.bytes.takeBytes();
}

Codec<List<int>, List<int>>? _unlabeledCodecOf(Uint8List bytes) =>
    switch (bytes) {
      [0x1f, 0x8b, ...] => gzip,
      [0x78, final flags, ...] when (0x78 << 8 | flags) % 31 == 0 => zlib,
      _ => null,
    };

final class _OutputTooLarge implements Exception {
  const _OutputTooLarge();
}

final class _CappedSink implements Sink<List<int>> {
  _CappedSink(this.limit);

  final int limit;
  final bytes = BytesBuilder(copy: false);

  @override
  void add(List<int> chunk) {
    if (bytes.length + chunk.length > limit) {
      throw const _OutputTooLarge();
    }
    bytes.add(chunk);
  }

  @override
  void close() {}
}

/// dart:io turns [Uri.userInfo] into Basic credentials still percent-encoded.
@visibleForTesting
({Uri uri, Map<String, Object> headers}) downloadTarget(String url) {
  final uri = url.webUri ?? Uri.parse(url);
  if (uri.userInfo.isEmpty) {
    return (uri: uri, headers: const {});
  }
  final credentials = uri.userInfo.split(':').map(_decodeUserInfo).join(':');
  return (
    uri: uri.replace(userInfo: ''),
    headers: {
      HttpHeaders.authorizationHeader:
          'Basic ${base64.encode(utf8.encode(credentials))}',
    },
  );
}

String _decodeUserInfo(String value) {
  try {
    return Uri.decodeComponent(value);
  } on ArgumentError {
    return value;
  }
}

String? getFileNameForDisposition(String? disposition) {
  if (disposition == null) return null;
  final parseValue = HeaderValue.parse(disposition);
  final parameters = parseValue.parameters;
  final fileNamePointKey = parameters.keys.firstWhere(
    (key) => key == 'filename*',
    orElse: () => '',
  );
  if (fileNamePointKey.isNotEmpty) {
    final res = parameters[fileNamePointKey]?.split("''") ?? [];
    if (res.length >= 2) {
      return Uri.decodeComponent(res[1]);
    }
  }
  final fileNameKey = parameters.keys.firstWhere(
    (key) => key == 'filename',
    orElse: () => '',
  );
  if (fileNameKey.isEmpty) return null;
  return parameters[fileNameKey];
}
