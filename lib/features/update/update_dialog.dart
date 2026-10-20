import 'dart:async';

import 'package:fl_clash/common/common.dart';
import 'package:fl_clash/ui/ui.dart';
import 'package:material_ui/material_ui.dart';

/// Downloads [update], verifies it and starts the installer, showing progress
/// in a dialog that stays open until the work finishes.
///
/// Returns true once the installer was started; the caller quits on desktop so
/// the installer can replace the running files.
Future<bool> showAppUpdateDownloadDialog(
  BuildContext context,
  AppUpdate update,
) async {
  final result = await dialogs.showCommonDialog<bool>(
    context: context,
    dismissible: false,
    child: _UpdateDownloadDialog(update: update),
  );
  return result ?? false;
}

class _UpdateDownloadDialog extends StatefulWidget {
  const _UpdateDownloadDialog({required this.update});

  final AppUpdate update;

  @override
  State<_UpdateDownloadDialog> createState() => _UpdateDownloadDialogState();
}

class _UpdateDownloadDialogState extends State<_UpdateDownloadDialog> {
  double _progress = 0;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_run());
  }

  Future<void> _run() async {
    final result = await AppUpdateService.downloadAndInstall(
      widget.update,
      onProgress: (received, total) {
        if (!mounted) return;
        setState(() {
          _progress = total > 0 ? (received / total).clamp(0.0, 1.0) : 0;
        });
      },
    );
    if (!mounted) return;
    final appLocalizations = currentAppLocalizations;
    switch (result) {
      case UpdateInstallResult.started:
        Navigator.of(context).pop(true);
      case UpdateInstallResult.downloadFailed:
        setState(() => _error = appLocalizations.updateDownloadFailed);
      case UpdateInstallResult.installFailed:
        setState(() => _error = appLocalizations.updateInstallFailed);
    }
  }

  @override
  Widget build(BuildContext context) {
    final appLocalizations = currentAppLocalizations;
    final error = _error;
    return CommonDialog(
      title: '${appLocalizations.download} ${widget.update.displayVersion}',
      actions: error == null
          ? null
          : [
              TextButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: Text(appLocalizations.close),
              ),
            ],
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (error == null) ...[
            ClipPath(
              clipper: ShapeBorderClipper(
                shape: AppShape.all(AppCorner.xs),
                textDirection: Directionality.of(context),
              ),
              child: LinearProgressIndicator(
                value: _progress == 0 ? null : _progress,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              '${(_progress * 100).round()}%',
              textAlign: TextAlign.center,
              style: AppTextStyle.footnote.of(context),
            ),
          ] else
            Text(
              error,
              textAlign: TextAlign.center,
              style: AppTextStyle.subheadline.of(context),
            ),
        ],
      ),
    );
  }
}
