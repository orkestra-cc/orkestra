import 'config/environment.dart';
import 'config/app_config.dart';
import 'main.dart' as app;

/// Development environment entry point.
///
/// Run with: flutter run -t lib/main_development.dart
void main() {
  AppConfig.initialize(EnvironmentConfig.development);
  app.main();
}
