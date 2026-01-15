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
  static const EnvironmentConfig development = EnvironmentConfig(
    environment: Environment.development,
    apiBaseUrl: 'http://localhost:3000',
    wsBaseUrl: 'ws://localhost:3000/ws',
    debug: true,
    appName: 'Orkestra Dev',
  );

  /// Staging environment configuration.
  static const EnvironmentConfig staging = EnvironmentConfig(
    environment: Environment.staging,
    apiBaseUrl: 'https://staging-api.orkestra.cc',
    wsBaseUrl: 'wss://staging-api.orkestra.cc/ws',
    debug: false,
    appName: 'Orkestra Staging',
  );

  /// Production environment configuration.
  static const EnvironmentConfig production = EnvironmentConfig(
    environment: Environment.production,
    apiBaseUrl: 'https://api.orkestra.cc',
    wsBaseUrl: 'wss://api.orkestra.cc/ws',
    debug: false,
    appName: 'Orkestra',
  );

  @override
  String toString() {
    return 'EnvironmentConfig(environment: $environment, apiBaseUrl: $apiBaseUrl, debug: $debug)';
  }
}
