package utils

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// SetupLogger creates and configures the application logger based on environment variables.
// LOG_LEVEL controls verbosity (debug, info, warn, error). Default: info.
// ENV controls format: JSON for production/staging, text for development.
func SetupLogger() *slog.Logger {
	level := slog.LevelInfo
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		switch strings.ToLower(logLevel) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	opts := slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	env := os.Getenv("ENV")

	if env == "production" || env == "staging" {
		handler = slog.NewJSONHandler(os.Stdout, &opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, &opts)
	}

	logger := slog.New(handler)

	return logger.With(
		slog.String("service", "orkestra-backend"),
		slog.String("version", "1.0.0"),
		slog.String("environment", env),
	)
}

// PrintDevelopmentWarning prints a prominent warning when running in non-production mode.
func PrintDevelopmentWarning(environment string) {
	isStaging := environment == "staging"

	var hstsLine string
	if isStaging {
		hstsLine = "║   • HSTS header is ENABLED (production-like security)                        ║"
	} else {
		hstsLine = "║   • HSTS header is disabled                                                   ║"
	}

	warning := `
╔═══════════════════════════════════════════════════════════════════════════════╗
║                                                                               ║
║   ██████╗ ███████╗██╗   ██╗    ███╗   ███╗ ██████╗ ██████╗ ███████╗          ║
║   ██╔══██╗██╔════╝██║   ██║    ████╗ ████║██╔═══██╗██╔══██╗██╔════╝          ║
║   ██║  ██║█████╗  ██║   ██║    ██╔████╔██║██║   ██║██║  ██║█████╗            ║
║   ██║  ██║██╔══╝  ╚██╗ ██╔╝    ██║╚██╔╝██║██║   ██║██║  ██║██╔══╝            ║
║   ██████╔╝███████╗ ╚████╔╝     ██║ ╚═╝ ██║╚██████╔╝██████╔╝███████╗          ║
║   ╚═════╝ ╚══════╝  ╚═══╝      ╚═╝     ╚═╝ ╚═════╝ ╚═════╝ ╚══════╝          ║
║                                                                               ║
║   RUNNING IN DEVELOPMENT MODE - NOT FOR PRODUCTION USE                        ║
║                                                                               ║
║   Environment: %-12s                                                       ║
║                                                                               ║
║   The following security features are RELAXED:                                ║
║   • Dev token endpoints are enabled (/dev/token)                              ║
║   • Verbose error messages are shown                                          ║
║   • Localhost OAuth redirects are allowed                                     ║
%s
║                                                                               ║
║   DO NOT deploy to production with these settings!                            ║
║   Set APP_ENV=production for production deployments.                          ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝
`
	fmt.Printf(warning, environment, hstsLine)
}
