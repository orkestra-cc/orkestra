package utils

// The boot banner enumerates which security features are relaxed. Once
// the dev-token endpoint moved behind IsProductionLike, staging stopped
// exposing it — but the banner still announced it as enabled. A security
// banner that overstates what is open is worse than no banner: it sends
// an operator looking for a backdoor that isn't there and misrepresents
// the environment's real posture.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func bannerFor(t *testing.T, environment string) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	PrintDevelopmentWarning(environment)

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestDevelopmentWarning_StagingReportsDevTokenDisabled(t *testing.T) {
	out := bannerFor(t, "staging")

	if !strings.Contains(out, "Dev token endpoints are DISABLED") {
		t.Errorf("staging banner must state that dev tokens are disabled, got:\n%s", out)
	}
	if strings.Contains(out, "Dev token endpoints are enabled") {
		t.Error("staging banner still claims dev tokens are enabled")
	}
}

func TestDevelopmentWarning_DevelopmentReportsDevTokenEnabled(t *testing.T) {
	out := bannerFor(t, "development")

	if !strings.Contains(out, "Dev token endpoints are enabled") {
		t.Errorf("development banner must state that dev tokens are enabled, got:\n%s", out)
	}
}

func TestDevelopmentWarning_SubstitutedLineKeepsItsWidth(t *testing.T) {
	// The banner is a fixed-width box drawn with literal padding, so a
	// swapped-in line of a different length visibly breaks the border.
	// (The box already has some pre-existing 1-rune raggedness elsewhere,
	// which is why this compares the two variants of the SAME line rather
	// than auditing every row.)
	devLine := devTokenBannerLine(bannerFor(t, "development"))
	stagingLine := devTokenBannerLine(bannerFor(t, "staging"))

	if devLine == "" || stagingLine == "" {
		t.Fatalf("could not locate the dev-token line (dev=%q staging=%q)", devLine, stagingLine)
	}
	if len([]rune(devLine)) != len([]rune(stagingLine)) {
		t.Errorf("staging line is %d runes, development line is %d — the box border will break:\n  %s\n  %s",
			len([]rune(stagingLine)), len([]rune(devLine)), stagingLine, devLine)
	}
}

func devTokenBannerLine(banner string) string {
	for _, line := range strings.Split(banner, "\n") {
		if strings.Contains(line, "Dev token endpoints") {
			return line
		}
	}
	return ""
}
