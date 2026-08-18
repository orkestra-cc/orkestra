package services_test

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

var forbiddenAuthLogPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[(?:APPLE|AUTH|REPO)_DEBUG\]`),
	regexp.MustCompile(`fmt\.(?:Print|Printf|Println)\s*\(`),
}

func TestProductionAuthCodeContainsNoDirectDebugPrinting(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test path")
	}

	servicesDir := filepath.Dir(testFile)
	for _, dir := range []string{servicesDir, filepath.Join(servicesDir, "..", "repository"), filepath.Join(servicesDir, "..", "handlers")} {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, pattern := range forbiddenAuthLogPatterns {
				if pattern.Match(content) {
					t.Error("production auth source contains forbidden debug logging")
				}
			}
			if strings.Contains(path, "auth_handler.go") {
				for _, forbidden := range []string{`slog.String("sid"`} {
					if strings.Contains(string(content), forbidden) {
						t.Errorf("ordinary auth handler log contains forbidden structured field %q", forbidden)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal("unable to scan production auth source")
		}
	}
}

func TestAppleOAuthServiceConstructionDoesNotLogPrivateKey(t *testing.T) {
	const privateKey = "DO-NOT-LOG-APPLE-PRIVATE-KEY"

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal("unable to capture stdout")
	}
	originalStdout := os.Stdout
	os.Stdout = writePipe
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = readPipe.Close()
		_ = writePipe.Close()
	})

	_, constructorErr := services.NewAppleOAuthService(&services.OAuthProviderConfig{
		AdditionalConfig: map[string]string{"private_key": privateKey},
	}, nil)
	if constructorErr == nil {
		t.Fatal("Apple OAuth construction unexpectedly succeeded")
	}
	if constructorErr.Error() != "failed to load apple private key: failed to decode PEM block containing private key" {
		t.Error("Apple OAuth construction returned an unexpected private-key parse error")
	}

	if err := writePipe.Close(); err != nil {
		t.Fatal("unable to close stdout capture")
	}
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatal("unable to read stdout capture")
	}
	for _, forbidden := range []string{privateKey, "private_key"} {
		if strings.Contains(string(output), forbidden) {
			t.Error("Apple OAuth construction logged a sensitive value")
		}
	}
}
