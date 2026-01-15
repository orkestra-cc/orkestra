import 'environment.dart';

/// Global application configuration.
///
/// This singleton provides access to the current environment configuration.
/// Initialize it at app startup using [initialize] with the desired
/// environment configuration.
///
/// Example:
/// ```dart
/// void main() {
///   AppConfig.initialize(EnvironmentConfig.development);
///   runApp(const MyApp());
/// }
/// ```
class AppConfig {
  static late EnvironmentConfig _config;
  static bool _initialized = false;

  /// Initialize the application configuration.
  ///
  /// This must be called before accessing any configuration values.
  /// Typically called in main() before runApp().
  static void initialize(EnvironmentConfig config) {
    _config = config;
    _initialized = true;
  }

  /// Get the current environment configuration.
  ///
  /// Throws [StateError] if [initialize] has not been called.
  static EnvironmentConfig get config {
    _ensureInitialized();
    return _config;
  }

  /// Get the API base URL.
  static String get apiBaseUrl {
    _ensureInitialized();
    return _config.apiBaseUrl;
  }

  /// Get the WebSocket base URL.
  static String get wsBaseUrl {
    _ensureInitialized();
    return _config.wsBaseUrl;
  }

  /// Whether debug mode is enabled.
  static bool get debug {
    _ensureInitialized();
    return _config.debug;
  }

  /// Get the application name.
  static String get appName {
    _ensureInitialized();
    return _config.appName;
  }

  /// Whether running in development environment.
  static bool get isDevelopment {
    _ensureInitialized();
    return _config.isDevelopment;
  }

  /// Whether running in staging environment.
  static bool get isStaging {
    _ensureInitialized();
    return _config.isStaging;
  }

  /// Whether running in production environment.
  static bool get isProduction {
    _ensureInitialized();
    return _config.isProduction;
  }

  /// Whether running in a production-like environment (staging or production).
  static bool get isProductionLike {
    _ensureInitialized();
    return _config.isProductionLike;
  }

  /// Get the current environment.
  static Environment get environment {
    _ensureInitialized();
    return _config.environment;
  }

  static void _ensureInitialized() {
    if (!_initialized) {
      throw StateError(
        'AppConfig has not been initialized. '
        'Call AppConfig.initialize() before accessing configuration values.',
      );
    }
  }
}
