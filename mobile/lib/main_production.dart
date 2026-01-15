import 'config/environment.dart';
import 'config/app_config.dart';
import 'main.dart' as app;

/// Production environment entry point.
///
/// Run with: flutter run -t lib/main_production.dart
/// Build with: flutter build apk -t lib/main_production.dart
void main() {
  AppConfig.initialize(EnvironmentConfig.production);
  app.main();
}
