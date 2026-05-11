import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orkestra_mobile/config/app_config.dart';
import 'package:orkestra_mobile/config/environment.dart';
import 'package:orkestra_mobile/main.dart';

void main() {
  setUpAll(() {
    AppConfig.initialize(EnvironmentConfig.development);
  });

  testWidgets('OrkestraApp boots and renders the home page', (tester) async {
    await tester.pumpWidget(const OrkestraApp());

    expect(find.byType(MaterialApp), findsOneWidget);
    expect(find.byType(Scaffold), findsOneWidget);
    expect(find.text('Environment: development'), findsOneWidget);
  });
}
