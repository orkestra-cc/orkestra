# Module: Mobile - Flutter Cross-Platform Application

_Path: `/mobile`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

<!-- Navigation -->

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)

<!-- /Navigation -->

## Module Purpose

The mobile module provides a **Flutter-based cross-platform application** for iOS and Android, enabling operators and field personnel to access Orkestra features on mobile devices.

- **Primary Role**: Mobile interface for operators and field personnel
- **System Integration**: Consumes backend APIs and WebSocket events
- **Architecture**: Flutter application with environment-based configuration

## Dependencies

### Imports

- **[`/backend/`](../backend/CLAUDE.md)** - REST APIs, WebSocket events, authentication

### Importers

- **End Users**: Mobile app users on iOS and Android devices

## Environment Configuration

The mobile app supports three environments with separate entry points:

| Environment | Entry Point | API URL |
|-------------|-------------|---------|
| Development | `lib/main_development.dart` | `http://localhost:3000` |
| Staging | `lib/main_staging.dart` | `https://staging-api.orkestra.cc` |
| Production | `lib/main_production.dart` | `https://api.orkestra.cc` |

### Running Different Environments

```bash
# Development
flutter run -t lib/main_development.dart

# Staging
flutter run -t lib/main_staging.dart

# Production
flutter run -t lib/main_production.dart

# Build APK for production
flutter build apk -t lib/main_production.dart

# Build iOS for production
flutter build ios -t lib/main_production.dart
```

### Environment Configuration Files

- **`lib/config/environment.dart`** - Environment enum and EnvironmentConfig class
- **`lib/config/app_config.dart`** - Global configuration singleton

## Project Structure

```
mobile/
├── lib/
│   ├── main.dart                    # Main app widget
│   ├── main_development.dart        # Development entry point
│   ├── main_staging.dart            # Staging entry point
│   ├── main_production.dart         # Production entry point
│   └── config/
│       ├── environment.dart         # Environment configuration
│       └── app_config.dart          # Global app config singleton
├── test/
│   └── widget_smoke_test.dart       # Boots OrkestraApp + asserts home renders
├── android/                         # Android platform files (scaffolded 2026-05-14 — applicationId `cc.orkestra.orkestra_mobile`)
├── pubspec.yaml                     # Flutter dependencies
└── analysis_options.yaml            # Dart analyzer configuration
```

## Technology Stack

- **Framework**: Flutter 3.35+
- **Language**: Dart 3.0+
- **State Management**: Provider + Riverpod
- **Networking**: Dio, WebSocket Channel
- **Storage**: Shared Preferences, Flutter Secure Storage
- **Navigation**: Go Router
- **Code Generation**: Freezed, JSON Serializable

## Quick Start

### Prerequisites

- Flutter 3.35+ installed
- Android Studio or Xcode for platform-specific development

### Development Setup

```bash
# Navigate to mobile directory
cd mobile

# Get dependencies
flutter pub get

# Run in development mode
flutter run -t lib/main_development.dart

# Run code generation (if using freezed)
flutter packages pub run build_runner build
```

## Module-Specific Guidelines

- **Environment Awareness**: Always use `AppConfig` for environment-specific values
- **API Integration**: All API calls must use `AppConfig.apiBaseUrl`
- **Security**: Use `isProductionLike` for production-equivalent security in staging
- **State Management**: Use Provider for simple state, Riverpod for complex features
- **Testing**: Write widget tests and integration tests for all features

## Common Commands

```bash
# Analyze code
flutter analyze

# Run tests
flutter test

# Build release APK
flutter build apk -t lib/main_production.dart --release

# Clean and rebuild
flutter clean && flutter pub get
```

## Platform scaffolds

Only **Android** is currently scaffolded under `mobile/android/` (added 2026-05-14 to unblock `Mobile CI > Build Android` on pushes to `main`). iOS, web, macOS, Linux, and Windows have no platform scaffold yet — `flutter build ios` / `flutter run -d <other>` will fail until those are added with `flutter create --platforms=<name> .` and the resulting directories committed.

Two notes on the Android scaffold:
- `gradle-wrapper.jar`, `gradlew`, and `gradlew.bat` are committed (Flutter's default `.gitignore` would exclude them, but `flutter pub get` does not regenerate them and CI fails without them). The gitignore in `mobile/android/.gitignore` is patched accordingly with a comment explaining why.
- Release builds sign with the debug keystore. Fine for CI artifacts; a real signing config is needed before any APK ships to users.

---

### Related Guides

- [Project Overview](../CLAUDE.md) - System architecture and design principles
- [Backend APIs](../backend/CLAUDE.md) - API specifications and authentication
- [Docker Development](../docker/CLAUDE.md) - Development environment setup
