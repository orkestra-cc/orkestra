import 'config/environment.dart';
import 'config/app_config.dart';
import 'main.dart' as app;

/// Staging environment entry point.
///
/// Run with: flutter run -t lib/main_staging.dart
void main() {
  AppConfig.initialize(EnvironmentConfig.staging);
  app.main();
}
