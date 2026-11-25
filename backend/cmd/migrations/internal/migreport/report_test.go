package migreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRefusesExistingFileAndWritesRestrictive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.csv")
	w, err := Open(path, []string{"tenantId", "value"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write([]string{"t1", "=cmd()"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "'=cmd()") || !strings.HasPrefix(string(body), "tenantId,value\n") {
		t.Fatalf("body = %q", body)
	}
	if _, err := Open(path, nil); err == nil {
		t.Fatal("second Open on the same path must fail")
	}
}
