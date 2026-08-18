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
		t.Fatal("resolve test file path")
	}

	servicesDir := filepath.Dir(testFile)
	for _, dir := range []string{servicesDir, filepath.Join(servicesDir, "..", "repository")} {
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
					t.Errorf("%s contains forbidden debug logging pattern %q", filepath.Base(path), pattern.String())
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk production auth directory %s: %v", dir, err)
		}
	}
}

func TestAppleOAuthServiceConstructionDoesNotLogPrivateKey(t *testing.T) {
	const privateKey = "DO-NOT-LOG-APPLE-PRIVATE-KEY"

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout capture pipe: %v", err)
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
		t.Fatal("NewAppleOAuthService returned nil error for invalid private key")
	}

	if err := writePipe.Close(); err != nil {
		t.Fatalf("close stdout capture pipe: %v", err)
	}
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("read stdout capture: %v", err)
	}
	for _, forbidden := range []string{privateKey, "private_key"} {
		if strings.Contains(string(output), forbidden) {
			t.Errorf("Apple OAuth construction logged sensitive value %q: %s", forbidden, output)
		}
	}
}
