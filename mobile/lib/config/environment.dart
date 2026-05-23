/// Environment types for the application.
enum Environment {
  development,
  staging,
  production,
}

/// Environment-specific configuration.
///
/// This class provides environment-aware configuration for the mobile app.
/// Use the static constants [development], [staging], and [production]
/// to access pre-configured environment settings.
class EnvironmentConfig {
  final Environment environment;
  final String apiBaseUrl;
  final String wsBaseUrl;
  final bool debug;
  final String appName;

  const EnvironmentConfig({
    required this.environment,
    required this.apiBaseUrl,
    required this.wsBaseUrl,
    required this.debug,
    required this.appName,
  });

  /// Whether currently running in development environment.
  bool get isDevelopment => environment == Environment.development;

  /// Whether currently running in staging environment.
  bool get isStaging => environment == Environment.staging;

  /// Whether currently running in production environment.
  bool get isProduction => environment == Environment.production;

  /// Whether running in a production-like environment (staging or production).
  /// Use this for security and behavior that should match production.
  bool get isProductionLike => isProduction || isStaging;

  /// Development environment configuration.
  ///
  /// Defaults to `http://localhost:3000` (works for iOS simulator + most
  /// desktops). Android emulator + physical-device users must override:
  ///   flutter run -t lib/main_development.dart \
  ///     --dart-define=ORKESTRA_API_URL=http://10.0.2.2:3000 \
  ///     --dart-define=ORKESTRA_WS_URL=ws://10.0.2.2:3000/ws
  /// or use --dart-define-from-file=dart_define/dev.json (see
  /// mobile/dart_define/example.json for the template).
  static const EnvironmentConfig development = EnvironmentConfig(
    environment: Environment.development,
    apiBaseUrl: String.fromEnvironment(
      'ORKESTRA_API_URL',
      defaultValue: 'http://localhost:3000',
    ),
    wsBaseUrl: String.fromEnvironment(
      'ORKESTRA_WS_URL',
      defaultValue: 'ws://localhost:3000/ws',
    ),
    debug: true,
    appName: 'Orkestra Dev',
  );

  /// Staging environment configuration.
  ///
  /// The hardcoded defaults below point at the canonical Orkestra
  /// staging environment. Forkers running their own staging must
  /// override via --dart-define-from-file=dart_define/staging.json.
  static const EnvironmentConfig staging = EnvironmentConfig(
    environment: Environment.staging,
    apiBaseUrl: String.fromEnvironment(
      'ORKESTRA_API_URL',
      defaultValue: 'https://staging-api.orkestra.cc',
    ),
    wsBaseUrl: String.fromEnvironment(
      'ORKESTRA_WS_URL',
      defaultValue: 'wss://staging-api.orkestra.cc/ws',
    ),
    debug: false,
    appName: 'Orkestra Staging',
  );

  /// Production environment configuration.
  ///
  /// The hardcoded defaults below point at the canonical Orkestra
  /// production environment. Forkers shipping to their own users must
  /// override via --dart-define-from-file=dart_define/production.json.
  static const EnvironmentConfig production = EnvironmentConfig(
    environment: Environment.production,
    apiBaseUrl: String.fromEnvironment(
      'ORKESTRA_API_URL',
      defaultValue: 'https://api.orkestra.cc',
    ),
    wsBaseUrl: String.fromEnvironment(
      'ORKESTRA_WS_URL',
      defaultValue: 'wss://api.orkestra.cc/ws',
    ),
    debug: false,
    appName: 'Orkestra',
  );

  @override
  String toString() {
    return 'EnvironmentConfig(environment: $environment, apiBaseUrl: $apiBaseUrl, debug: $debug)';
  }
}
