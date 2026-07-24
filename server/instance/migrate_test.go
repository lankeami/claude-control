package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMigrateLegacyCopiesDBAndKey(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")

	writeFile(t, filepath.Join(base, "data.db"), "legacy-db")
	writeFile(t, filepath.Join(base, "data.db-wal"), "legacy-wal")
	writeFile(t, filepath.Join(base, "data.db-shm"), "legacy-shm")
	writeFile(t, filepath.Join(base, "api.key"), "legacy-key")

	migrated, err := migrateLegacyDirs(base, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyDirs: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration to happen")
	}

	if got := readFile(t, filepath.Join(instDir, "claude.db")); got != "legacy-db" {
		t.Errorf("claude.db = %q, want %q", got, "legacy-db")
	}
	if got := readFile(t, filepath.Join(instDir, "claude.db-wal")); got != "legacy-wal" {
		t.Errorf("claude.db-wal = %q, want %q", got, "legacy-wal")
	}
	if got := readFile(t, filepath.Join(instDir, "claude.db-shm")); got != "legacy-shm" {
		t.Errorf("claude.db-shm = %q, want %q", got, "legacy-shm")
	}
	if got := readFile(t, filepath.Join(instDir, "api.key")); got != "legacy-key" {
		t.Errorf("api.key = %q, want %q", got, "legacy-key")
	}

	// Legacy files must remain untouched.
	if got := readFile(t, filepath.Join(base, "data.db")); got != "legacy-db" {
		t.Errorf("legacy data.db modified: %q", got)
	}
}

func TestMigrateLegacyNoopWhenInstanceDBExists(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	if err := os.MkdirAll(instDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(base, "data.db"), "legacy-db")
	writeFile(t, filepath.Join(instDir, "claude.db"), "existing-db")

	migrated, err := migrateLegacyDirs(base, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyDirs: %v", err)
	}
	if migrated {
		t.Fatal("expected no migration when instance DB exists")
	}
	if got := readFile(t, filepath.Join(instDir, "claude.db")); got != "existing-db" {
		t.Errorf("claude.db overwritten: %q", got)
	}
}

func TestMigrateLegacyNoopWhenNoLegacyDB(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")

	migrated, err := migrateLegacyDirs(base, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyDirs: %v", err)
	}
	if migrated {
		t.Fatal("expected no migration when legacy DB missing")
	}
	if _, err := os.Stat(filepath.Join(instDir, "claude.db")); !os.IsNotExist(err) {
		t.Error("claude.db should not have been created")
	}
}

func TestMigrateLegacyDoesNotOverwriteExistingKey(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	if err := os.MkdirAll(instDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(base, "data.db"), "legacy-db")
	writeFile(t, filepath.Join(base, "api.key"), "legacy-key")
	writeFile(t, filepath.Join(instDir, "api.key"), "instance-key")

	migrated, err := migrateLegacyDirs(base, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyDirs: %v", err)
	}
	if !migrated {
		t.Fatal("expected DB migration to happen")
	}
	if got := readFile(t, filepath.Join(instDir, "api.key")); got != "instance-key" {
		t.Errorf("api.key overwritten: %q", got)
	}
}

func TestMigrateLegacySkipsMissingSidecarsAndKey(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")

	writeFile(t, filepath.Join(base, "data.db"), "legacy-db")

	migrated, err := migrateLegacyDirs(base, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyDirs: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration to happen")
	}
	for _, f := range []string{"claude.db-wal", "claude.db-shm", "api.key"} {
		if _, err := os.Stat(filepath.Join(instDir, f)); !os.IsNotExist(err) {
			t.Errorf("%s should not exist", f)
		}
	}
}

func TestMigrateLegacyOnlyDefaultInstance(t *testing.T) {
	migrated, err := MigrateLegacy("work")
	if err != nil {
		t.Fatalf("MigrateLegacy(non-default): %v", err)
	}
	if migrated {
		t.Fatal("non-default instance must never migrate")
	}
}
