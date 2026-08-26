import 'dart:async';
import 'dart:io';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flclashx/clash/clash.dart';
import 'package:flclashx/common/common.dart';
import 'package:flclashx/l10n/l10n.dart';
import 'package:flclashx/manager/hotkey_manager.dart';
import 'package:flclashx/manager/manager.dart';
import 'package:flclashx/plugins/app.dart';
import 'package:flclashx/providers/providers.dart';
import 'package:flclashx/state.dart';
import 'package:flutter/material.dart';
import 'package:flutter_displaymode/flutter_displaymode.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'controller.dart';
import 'pages/pages.dart';
import 'services/app_update.dart';

class Application extends ConsumerStatefulWidget {
  const Application({
    super.key,
  });

  @override
  ConsumerState<Application> createState() => ApplicationState();
}

class ApplicationState extends ConsumerState<Application> {
  Timer? _autoUpdateProfilesTaskTimer;

  final _pageTransitionsTheme = const PageTransitionsTheme(
    builders: <TargetPlatform, PageTransitionsBuilder>{
      TargetPlatform.android: CommonPageTransitionsBuilder(),
      TargetPlatform.windows: CommonPageTransitionsBuilder(),
      TargetPlatform.linux: CommonPageTransitionsBuilder(),
      TargetPlatform.macOS: CommonPageTransitionsBuilder(),
    },
  );

  ColorScheme _getAppColorScheme({
    required Brightness brightness,
    int? primaryColor,
  }) =>
      ref.read(genColorSchemeProvider(brightness));

  @override
  void initState() {
    super.initState();

    if (Platform.isWindows) {
      windows?.enableDarkModeForApp();
    }

    if (Platform.isAndroid) {
      // Pin the highest refresh rate (per session) so the Flutter engine samples
      // 120 Hz at surface creation. A hand-rolled preferredDisplayModeId left LTPO
      // Pixel panels showing 120 Hz while the engine rendered at 60 (visible jank);
      // flutter_displaymode handles the OEM quirks. Best-effort, never fatal.
      unawaited(FlutterDisplayMode.setHighRefreshRate().catchError((_) {}));
    }

    globalState.startGroupsUpdateTask();
    _autoUpdateProfilesTask();
    globalState.appController = AppController(context, ref);
    WidgetsBinding.instance.addPostFrameCallback((timeStamp) async {
      final currentContext = globalState.navigatorKey.currentContext;
      if (currentContext != null) {
        globalState.appController = AppController(currentContext, ref);
      }
      await globalState.appController.init();
      globalState.appController.initLink();
      app?.initShortcuts();
      _checkForAppUpdate();
    });
  }

  Future<void> _checkForAppUpdate() async {
    final update = await AppUpdateService.check();
    if (!mounted || update == null) return;
    final isRussian = Localizations.localeOf(context).languageCode == 'ru';
    final accepted = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(isRussian ? 'Доступно обновление' : 'Update available'),
        content: Text(isRussian
            ? 'Доступна новая версия ${update.version}. Скачать и установить её?'
            : 'Version ${update.version} is available. Download and install it?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false),
              child: Text(isRussian ? 'Позже' : 'Later')),
          FilledButton(onPressed: () => Navigator.pop(dialogContext, true),
              child: Text(isRussian ? 'Обновить' : 'Update')),
        ],
      ),
    ) ?? false;
    if (!accepted || !mounted) return;
    final installed = await AppUpdateService.downloadAndInstall(update);
    if (!mounted || installed) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
      content: Text(isRussian ? 'Не удалось запустить установку обновления'
          : 'Could not start update installation'),
    ));
  }

  void _autoUpdateProfilesTask() {
    _autoUpdateProfilesTaskTimer = Timer(const Duration(minutes: 20), () async {
      await globalState.appController.autoUpdateProfiles();
      // dispose() may have landed during the await; don't arm a fresh
      // post-dispose timer that would keep firing.
      if (!mounted) return;
      _autoUpdateProfilesTask();
    });
  }

  Widget _buildPlatformState(Widget child) {
    if (system.isDesktop) {
      return WindowManager(
        child: TrayManager(
          child: HotKeyManager(
            child: ProxyManager(
              child: child,
            ),
          ),
        ),
      );
    }
    return AndroidManager(
      child: TileManager(
        child: child,
      ),
    );
  }

  Widget _buildState(Widget child) => AppStateManager(
        child: ClashManager(
          child: ConnectivityManager(
            onConnectivityChanged: (results) async {
              if (!results.contains(ConnectivityResult.vpn)) {
                clashCore.closeConnections();
              }
              globalState.appController.updateLocalIp();
              globalState.appController.addCheckIpNumDebounce();
            },
            child: child,
          ),
        ),
      );

  Widget _buildPlatformApp(Widget child) {
    if (system.isDesktop) {
      return WindowHeaderContainer(
        child: child,
      );
    }
    return VpnManager(
      child: child,
    );
  }

  Widget _buildApp(Widget child) => MessageManager(
        child: ThemeManager(
          child: child,
        ),
      );

  @override
  Widget build(BuildContext context) => _buildPlatformState(
        _buildState(
          Consumer(
            builder: (_, ref, child) {
              final locale =
                  ref.watch(appSettingProvider.select((state) => state.locale));
              final themeProps = ref.watch(themeSettingProvider);
              return MaterialApp(
                debugShowCheckedModeBanner: false,
                navigatorKey: globalState.navigatorKey,
                checkerboardRasterCacheImages: false,
                checkerboardOffscreenLayers: false,
                showPerformanceOverlay: false,
                localizationsDelegates: const [
                  AppLocalizations.delegate,
                  GlobalMaterialLocalizations.delegate,
                  GlobalCupertinoLocalizations.delegate,
                  GlobalWidgetsLocalizations.delegate
                ],
                builder: (_, child) {
                  final Widget app = AppEnvManager(
                    child: _buildPlatformApp(
                      _buildApp(child!),
                    ),
                  );

                  if (Platform.isMacOS) {
                    return FittedBox(
                      fit: BoxFit.contain,
                      alignment: Alignment.topCenter,
                      child: SizedBox(
                        width: 500,
                        height: 800,
                        child: app,
                      ),
                    );
                  }

                  return app;
                },
                scrollBehavior: BaseScrollBehavior(),
                title: appName,
                locale: utils.getLocaleForString(locale),
                supportedLocales: AppLocalizations.delegate.supportedLocales,
                themeMode: themeProps.themeMode,
                theme: ThemeData(
                  useMaterial3: true,
                  pageTransitionsTheme: _pageTransitionsTheme,
                  colorScheme: _getAppColorScheme(
                    brightness: Brightness.light,
                    primaryColor: themeProps.primaryColor,
                  ),
                  // Reduce animation duration for snappier feel
                  visualDensity: VisualDensity.adaptivePlatformDensity,
                ),
                darkTheme: ThemeData(
                  useMaterial3: true,
                  pageTransitionsTheme: _pageTransitionsTheme,
                  colorScheme: _getAppColorScheme(
                    brightness: Brightness.dark,
                    primaryColor: themeProps.primaryColor,
                  ).toPureBlack(themeProps.pureBlack),
                  // Reduce animation duration for snappier feel
                  visualDensity: VisualDensity.adaptivePlatformDensity,
                ),
                home: child,
              );
            },
            child: const HomePage(),
          ),
        ),
      );

  @override
  void dispose() {
    linkManager.destroy();
    globalState.stopGroupsUpdateTask();
    _autoUpdateProfilesTaskTimer?.cancel();
    if (Platform.isAndroid) {
      // Activity teardown (recreation, "don't keep activities", OEM kill) must
      // not shut the core down: the FGS/tunnel outlives the UI by design, and
      // handleExit()/destroy() here killed the executor under a live VPN.
      // savePreferences() is debounced/saved-on-pause, so fire-and-forget it to
      // honor the synchronous State.dispose() contract.
      unawaited(globalState.appController.savePreferences());
      super.dispose();
      return;
    }
    // Desktop teardown ends in system.exit(); run it detached so dispose() stays
    // synchronous (no await before super.dispose()).
    unawaited(_desktopTeardown());
    super.dispose();
  }

  Future<void> _desktopTeardown() async {
    await clashCore.destroy();
    await globalState.appController.savePreferences();
    await globalState.appController.handleExit();
  }
}
