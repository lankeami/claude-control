package instance

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MigrateLegacy copies the pre-multi-instance database and API key from
// ~/.claude-controller/ into the default instance directory. It runs only for
// the default instance, only when the instance has no database yet, and never
// modifies the legacy files.
func MigrateLegacy(instanceName string) (bool, error) {
	if instanceName != "default" {
		return false, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	legacyDir := filepath.Join(home, ".claude-controller")

	instDir, err := ConfigDir(instanceName)
	if err != nil {
		return false, err
	}

	dbMigrated, err := migrateLegacyDirs(legacyDir, instDir)
	if err != nil {
		return dbMigrated, err
	}

	// Pre-multi-instance, settings were saved to .env in the server's
	// working directory.
	envMigrated, err := migrateLegacyEnv(".env", instDir)
	return dbMigrated || envMigrated, err
}

// controllerEnvMarker is a section header formatEnvFile always writes, used to
// distinguish a controller settings file from an unrelated project's .env.
const controllerEnvMarker = "# Managed session config"

func migrateLegacyEnv(legacyEnv, instDir string) (bool, error) {
	data, err := os.ReadFile(legacyEnv)
	if err != nil {
		return false, nil
	}
	if !strings.Contains(string(data), controllerEnvMarker) {
		return false, nil
	}

	migrated := false
	// shortcuts.json is written by the settings API next to the .env file.
	files := map[string]string{
		legacyEnv: ".env",
		filepath.Join(filepath.Dir(legacyEnv), "shortcuts.json"): "shortcuts.json",
	}
	for src, name := range files {
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(instDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(instDir, 0755); err != nil {
			return migrated, err
		}
		if err := copyFile(src, dst); err != nil {
			return migrated, err
		}
		migrated = true
	}
	return migrated, nil
}

func migrateLegacyDirs(legacyDir, instDir string) (bool, error) {
	legacyDB := filepath.Join(legacyDir, "data.db")
	if _, err := os.Stat(legacyDB); err != nil {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(instDir, "claude.db")); err == nil {
		return false, nil
	}

	if err := os.MkdirAll(instDir, 0755); err != nil {
		return false, err
	}

	if err := copyFile(legacyDB, filepath.Join(instDir, "claude.db")); err != nil {
		return false, err
	}
	// WAL/SHM sidecars carry writes not yet checkpointed into the main DB.
	for _, suffix := range []string{"-wal", "-shm"} {
		src := legacyDB + suffix
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(instDir, "claude.db"+suffix)); err != nil {
			return false, err
		}
	}

	legacyKey := filepath.Join(legacyDir, "api.key")
	instKey := filepath.Join(instDir, "api.key")
	if _, err := os.Stat(legacyKey); err == nil {
		if _, err := os.Stat(instKey); os.IsNotExist(err) {
			if err := copyFile(legacyKey, instKey); err != nil {
				return false, err
			}
		}
	}

	return true, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
