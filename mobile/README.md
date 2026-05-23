# Orkestra Mobile

Flutter cross-platform client for [Orkestra](https://github.com/orkestra-cc/orkestra). Early-stage — the app currently boots, picks an environment, and renders the active backend URL. Real product surfaces are in active development.

> AI assistants: see [`CLAUDE.md`](CLAUDE.md) for module-specific conventions.

## Status

| Platform | Scaffolded | Builds in CI |
| --- | --- | --- |
| Android | ✅ | ✅ `flutter build apk` |
| iOS | ❌ | ❌ (run `flutter create --platforms=ios .` to add) |
| Web | ❌ | ❌ |
| macOS / Linux / Windows desktop | ❌ | ❌ |

Release builds sign with the debug keystore — fine for CI artifacts, **not** acceptable for any APK that ships to users. Wire a real signing config before publishing.

## Forking — point the app at your own backend

If you've forked Orkestra and your backend runs at a different URL than the canonical orkestra.cc instance, override the API + WebSocket URLs via `--dart-define-from-file`.

1. **Install Flutter 3.35+** — [official guide](https://docs.flutter.dev/get-started/install). `mise install` from the repo root also provisions the pinned version (see `.mise.toml`).

2. **Bootstrap the backend on your fork.** Follow [I just forked Orkestra](https://docs.orkestra.cc/getting-started/forking) — `make init && docker compose up -d` gets you a backend on `localhost:3000`.

3. **Pick the right host from the mobile device's perspective:**

   | Where the app runs | API hostname to use |
   | --- | --- |
   | iOS simulator | `http://localhost:3000` (default — no override needed) |
   | Android emulator | `http://10.0.2.2:3000` (the emulator's host loopback) |
   | Physical device on the same LAN | `http://<your-machine-ip>:3000` |

4. **Copy the dart-define template and edit:**

   ```bash
   cd mobile
   cp dart_define/example.json dart_define/dev.json
   # Edit dart_define/dev.json — set ORKESTRA_API_URL / ORKESTRA_WS_URL
   ```

   `dart_define/*.json` (except `example.json`) is gitignored.

5. **Run:**

   ```bash
   flutter pub get
   flutter run \
     -t lib/main_development.dart \
     --dart-define-from-file=dart_define/dev.json
   ```

   The home screen displays the active `Environment`, `API URL`, and `Debug` flag — confirms the override is taking effect.

For staging / production, repeat with `dart_define/staging.json` / `dart_define/production.json` and the matching `-t lib/main_staging.dart` / `-t lib/main_production.dart` entry point.

## Environment entry points

| Entry point | Default API URL | Override |
| --- | --- | --- |
| `lib/main_development.dart` | `http://localhost:3000` | `--dart-define=ORKESTRA_API_URL=http://10.0.2.2:3000` |
| `lib/main_staging.dart` | `https://staging-api.orkestra.cc` | `--dart-define=ORKESTRA_API_URL=https://staging-api.your-org.com` |
| `lib/main_production.dart` | `https://api.orkestra.cc` | `--dart-define=ORKESTRA_API_URL=https://api.your-org.com` |

Each entry point picks a different `EnvironmentConfig` from [`lib/config/environment.dart`](lib/config/environment.dart). The const constructors use `String.fromEnvironment` so `--dart-define` overrides at build time.

## Available `--dart-define` keys

| Key | Default | Purpose |
| --- | --- | --- |
| `ORKESTRA_API_URL` | per-env (see above) | Backend HTTP base URL |
| `ORKESTRA_WS_URL` | per-env (mirror of `ORKESTRA_API_URL` with `ws:`/`wss:`) | Backend WebSocket base URL |

OAuth client IDs / Stripe keys are not yet wired into the mobile app — the SDK dependencies aren't in `pubspec.yaml` yet. As those features land, add the matching keys here.

## Common commands

```bash
flutter pub get             # fetch dependencies
flutter analyze             # lint + type-check
flutter test                # widget tests
flutter run -t lib/main_development.dart --dart-define-from-file=dart_define/dev.json

# Release builds
flutter build apk     -t lib/main_production.dart --release \
  --dart-define-from-file=dart_define/production.json
flutter build ios     -t lib/main_production.dart --release \
  --dart-define-from-file=dart_define/production.json
flutter build appbundle -t lib/main_production.dart --release \
  --dart-define-from-file=dart_define/production.json
```

`flutter clean && flutter pub get` if you hit weird build-cache issues after switching environments.

## Deploying to your store

This README intentionally does **not** cover store-deploy procedure — Play Store + App Store onboarding is its own discipline and changes too often for a README to track. Start here:

- [Flutter: Build and release an Android app](https://docs.flutter.dev/deployment/android)
- [Flutter: Build and release an iOS app](https://docs.flutter.dev/deployment/ios)
- [Codemagic](https://codemagic.io/) / [Bitrise](https://www.bitrise.io/) / GitHub Actions for CI-driven release pipelines

Before your first store submission you'll need at minimum:

- A real Android signing keystore (NOT the debug keystore).
- An iOS scaffold (`flutter create --platforms=ios .`) plus an Apple Developer account.
- App icons, splash screens, store listings — all out of scope here.

## See also

- [`mobile/CLAUDE.md`](CLAUDE.md) — AI-assistant module guide
- [I just forked Orkestra](https://docs.orkestra.cc/getting-started/forking) — backend bootstrap on a fork
- [Mobile app — operator docs](https://docs.orkestra.cc/operating/mobile-app) — Tier-1 deployment view
- [Flutter documentation](https://docs.flutter.dev/)
