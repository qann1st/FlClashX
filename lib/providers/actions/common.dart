part of '../action.dart';

@Riverpod(keepAlive: true)
class CommonAction extends _$CommonAction {
  CoreController get _core => ref.read(coreHandlerProvider);
  bool _isUpdatingTraffic = false;

  @override
  void build() {}

  void toggleRunning() {
    final running = !ref.read(isStartProvider);
    unawaited(
      globalState.safeRun(
        () => ref
            .read(setupActionProvider.notifier)
            .setRunning(
              running,
              initialize: running && !ref.read(initProvider),
            ),
      ),
    );
  }

  void updateSpeedStatistics() {
    ref
        .read(appSettingProvider.notifier)
        .update((state) => state.copyWith(showTrayTitle: !state.showTrayTitle));
  }

  void updateMode() {
    final modes = ref.read(availableModesProvider);
    ref.read(patchClashConfigProvider.notifier).update((state) {
      final index = modes.indexWhere((item) => item == state.mode);
      final nextIndex = index + 1 > modes.length - 1 ? 0 : index + 1;
      return state.copyWith(mode: modes[nextIndex]);
    });
  }

  Future<void> updateTraffic() async {
    if (_isUpdatingTraffic) {
      return;
    }
    _isUpdatingTraffic = true;
    try {
      final onlyStatisticsProxy = ref.read(
        appSettingProvider.select((state) => state.onlyStatisticsProxy),
      );
      final [traffic, totalTraffic] = await Future.wait([
        _readTraffic(() => _core.getTraffic(onlyStatisticsProxy)),
        _readTraffic(() => _core.getTotalTraffic(onlyStatisticsProxy)),
      ]);
      if (traffic != null) {
        ref.read(trafficsProvider.notifier).addTraffic(traffic);
      }
      if (totalTraffic != null) {
        ref.read(totalTrafficProvider.notifier).value = totalTraffic;
      }
    } finally {
      _isUpdatingTraffic = false;
    }
  }

  Future<Traffic?> _readTraffic(Future<Traffic> Function() request) async {
    try {
      return await request();
    } catch (error) {
      commonPrint.log(
        'updateTraffic error: $error',
        logLevel: coreFailureLogLevel(error),
      );
      return null;
    }
  }

  Future<bool> autoCheckUpdate() async {
    if (!ref.read(appSettingProvider).autoCheckUpdate) return false;
    final AppUpdate? update;
    try {
      update = await AppUpdateService.check();
    } catch (error) {
      commonPrint.log(
        'checkForUpdate failed: ${compactError(error)}',
        logLevel: LogLevel.warning,
      );
      return false;
    }
    await checkUpdateResultHandle(update: update);
    return update != null;
  }

  TextSpan _updateSpan(BuildContext context, AppUpdate update) {
    final notes = update.notes?.trim() ?? '';
    return TextSpan(
      text: '${update.displayVersion} \n',
      style: AppTextStyle.title2.of(context),
      children: [
        const TextSpan(text: '\n'),
        for (final line in notes.split('\n'))
          if (line.trim().isNotEmpty)
            TextSpan(
              text: '${line.trim().replaceFirst(RegExp(r'^[-*] '), '')} \n',
              style: AppTextStyle.subheadline.of(context),
            ),
      ],
    );
  }

  Future<void> checkUpdateResultHandle({
    AppUpdate? update,
    bool isUser = false,
  }) async {
    if (update == null) {
      if (isUser) {
        unawaited(
          dialogs.showMessage(
            title: currentAppLocalizations.checkUpdate,
            message: TextSpan(text: currentAppLocalizations.checkUpdateError),
          ),
        );
      }
      return;
    }
    final context = globalState.navigatorKey.currentContext!;
    final res = await dialogs.showMessage(
      title: currentAppLocalizations.discoverNewVersion,
      message: _updateSpan(context, update),
      confirmText: AppUpdateService.canSelfInstall && update.hasDownload
          ? currentAppLocalizations.update
          : currentAppLocalizations.goDownload,
      cancelText: isUser ? null : currentAppLocalizations.noLongerRemind,
    );
    if (res != true) {
      if (!isUser && res == false) {
        ref
            .read(appSettingProvider.notifier)
            .update((state) => state.copyWith(autoCheckUpdate: false));
      }
      return;
    }
    if (AppUpdateService.canSelfInstall && update.hasDownload) {
      final downloadContext = globalState.navigatorKey.currentContext;
      if (downloadContext != null && downloadContext.mounted) {
        final installed = await showAppUpdateDownloadDialog(
          downloadContext,
          update,
        );
        if (installed && !Platform.isAndroid) {
          await ref.read(systemActionProvider.notifier).handleExit(false);
        }
        if (installed) return;
      }
    }
    final page = update.pageUrl;
    if (page != null && page.isNotEmpty) {
      unawaited(launchUrl(Uri.parse(page)));
    }
  }
}
