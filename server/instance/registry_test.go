package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryCreate(t *testing.T) {
	tmpDir := t.TempDir()
	home := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", home)

	r, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	inst := &Instance{
		Port:    8081,
		Theme:   "ocean",
		Account: "personal@example.com",
	}
	if err := r.Create("personal", inst); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	retrieved, err := r.Get("personal")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.Name != "personal" || retrieved.Port != 8081 || retrieved.Theme != "ocean" {
		t.Errorf("Instance mismatch: %+v", retrieved)
	}
}

func TestRegistryList(t *testing.T) {
	tmpDir := t.TempDir()
	home := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", home)

	r, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	r.Create("personal", &Instance{Port: 8081, Theme: "ocean"})
	r.Create("work", &Instance{Port: 8082, Theme: "forest"})

	instances := r.List()
	if len(instances) != 3 { // default + personal + work
		t.Errorf("Expected 3 instances, got %d", len(instances))
	}
}

func TestRegistryPathHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	home := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", home)

	dbPath, err := DBPath("personal")
	if err != nil {
		t.Fatalf("DBPath() failed: %v", err)
	}

	expected := filepath.Join(tmpDir, ".claude-controller", "personal", "claude.db")
	if dbPath != expected {
		t.Errorf("DBPath mismatch: expected %s, got %s", expected, dbPath)
	}

	envPath, err := EnvPath("personal")
	if err != nil {
		t.Fatalf("EnvPath() failed: %v", err)
	}

	expected = filepath.Join(tmpDir, ".claude-controller", "personal", ".env")
	if envPath != expected {
		t.Errorf("EnvPath mismatch: expected %s, got %s", expected, envPath)
	}
}

func TestRegistryPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	home := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", home)

	// Create and add instance
	r1, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	r1.Create("personal", &Instance{Port: 8081, Theme: "ocean"})

	// Load again and verify persistence
	r2, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	retrieved, err := r2.Get("personal")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.Port != 8081 || retrieved.Theme != "ocean" {
		t.Errorf("Instance not persisted correctly: %+v", retrieved)
	}
}
