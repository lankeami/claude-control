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

const controllerEnv = "# Server\nPORT=8080\n\n# Managed session config\nCLAUDE_BIN=claude\n"

func TestMigrateLegacyEnvCopiesControllerEnv(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, controllerEnv)

	migrated, err := migrateLegacyEnv(legacyEnv, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if !migrated {
		t.Fatal("expected env migration to happen")
	}
	if got := readFile(t, filepath.Join(instDir, ".env")); got != controllerEnv {
		t.Errorf(".env = %q, want %q", got, controllerEnv)
	}
	if got := readFile(t, legacyEnv); got != controllerEnv {
		t.Errorf("legacy .env modified: %q", got)
	}
}

func TestMigrateLegacyEnvSkipsForeignEnv(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, "DATABASE_URL=postgres://foo\nSECRET=bar\n")

	migrated, err := migrateLegacyEnv(legacyEnv, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if migrated {
		t.Fatal("must not migrate a non-controller .env")
	}
	if _, err := os.Stat(filepath.Join(instDir, ".env")); !os.IsNotExist(err) {
		t.Error("instance .env should not have been created")
	}
}

func TestMigrateLegacyEnvNoopWhenInstanceEnvExists(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	if err := os.MkdirAll(instDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, controllerEnv)
	writeFile(t, filepath.Join(instDir, ".env"), "PORT=9090\n")

	migrated, err := migrateLegacyEnv(legacyEnv, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if migrated {
		t.Fatal("must not overwrite existing instance .env")
	}
	if got := readFile(t, filepath.Join(instDir, ".env")); got != "PORT=9090\n" {
		t.Errorf("instance .env overwritten: %q", got)
	}
}

func TestMigrateLegacyEnvCopiesShortcuts(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, controllerEnv)
	writeFile(t, filepath.Join(base, "shortcuts.json"), `[{"key":"👍","value":"yes"}]`)

	migrated, err := migrateLegacyEnv(legacyEnv, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration to happen")
	}
	if got := readFile(t, filepath.Join(instDir, "shortcuts.json")); got != `[{"key":"👍","value":"yes"}]` {
		t.Errorf("shortcuts.json = %q", got)
	}
}

func TestMigrateLegacyEnvCopiesShortcutsWhenInstanceEnvExists(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	if err := os.MkdirAll(instDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, controllerEnv)
	writeFile(t, filepath.Join(base, "shortcuts.json"), `[{"key":"👍","value":"yes"}]`)
	writeFile(t, filepath.Join(instDir, ".env"), "PORT=9090\n")

	migrated, err := migrateLegacyEnv(legacyEnv, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if !migrated {
		t.Fatal("expected shortcuts migration to happen")
	}
	if got := readFile(t, filepath.Join(instDir, ".env")); got != "PORT=9090\n" {
		t.Errorf("instance .env overwritten: %q", got)
	}
	if got := readFile(t, filepath.Join(instDir, "shortcuts.json")); got != `[{"key":"👍","value":"yes"}]` {
		t.Errorf("shortcuts.json = %q", got)
	}
}

func TestMigrateLegacyEnvDoesNotOverwriteInstanceShortcuts(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	if err := os.MkdirAll(instDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, controllerEnv)
	writeFile(t, filepath.Join(base, "shortcuts.json"), `[{"key":"old"}]`)
	writeFile(t, filepath.Join(instDir, "shortcuts.json"), `[{"key":"new"}]`)

	if _, err := migrateLegacyEnv(legacyEnv, instDir); err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if got := readFile(t, filepath.Join(instDir, "shortcuts.json")); got != `[{"key":"new"}]` {
		t.Errorf("instance shortcuts.json overwritten: %q", got)
	}
}

func TestMigrateLegacyEnvNoShortcutsWhenForeignEnv(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")
	legacyEnv := filepath.Join(base, ".env")

	writeFile(t, legacyEnv, "DATABASE_URL=postgres://foo\n")
	writeFile(t, filepath.Join(base, "shortcuts.json"), `[{"key":"old"}]`)

	migrated, err := migrateLegacyEnv(legacyEnv, instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if migrated {
		t.Fatal("must not migrate anything next to a foreign .env")
	}
	if _, err := os.Stat(filepath.Join(instDir, "shortcuts.json")); !os.IsNotExist(err) {
		t.Error("shortcuts.json should not have been created")
	}
}

func TestMigrateLegacyEnvNoopWhenLegacyMissing(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "default")

	migrated, err := migrateLegacyEnv(filepath.Join(base, ".env"), instDir)
	if err != nil {
		t.Fatalf("migrateLegacyEnv: %v", err)
	}
	if migrated {
		t.Fatal("expected no migration when legacy .env missing")
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
