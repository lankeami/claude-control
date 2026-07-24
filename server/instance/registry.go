package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Instance represents a single Claude Controller instance configuration.
type Instance struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Theme   string `json:"theme"`
	Account string `json:"account"`
}

// Registry manages instance configurations stored in ~/.claude-controller/instances.json
type Registry struct {
	path      string
	instances map[string]*Instance
	mu        sync.RWMutex
}

// New creates a new registry, loading from disk if it exists.
func New() (*Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".claude-controller")
	path := filepath.Join(dir, "instances.json")

	r := &Registry{
		path:      path,
		instances: make(map[string]*Instance),
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Ensure default instance exists
	if _, ok := r.instances["default"]; !ok {
		r.instances["default"] = &Instance{
			Name:    "default",
			Port:    8080,
			Theme:   "default",
			Account: "",
		}
		if err := r.save(); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Get retrieves an instance by name.
func (r *Registry) Get(name string) (*Instance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	inst, ok := r.instances[name]
	if !ok {
		return nil, fmt.Errorf("instance %q not found", name)
	}
	return inst, nil
}

// Create adds a new instance to the registry.
func (r *Registry) Create(name string, inst *Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.instances[name]; ok {
		return fmt.Errorf("instance %q already exists", name)
	}

	inst.Name = name
	r.instances[name] = inst
	return r.save()
}

// Update modifies an existing instance.
func (r *Registry) Update(name string, inst *Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.instances[name]; !ok {
		return fmt.Errorf("instance %q not found", name)
	}

	inst.Name = name
	r.instances[name] = inst
	return r.save()
}

// Delete removes an instance from the registry.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if name == "default" {
		return fmt.Errorf("cannot delete default instance")
	}

	if _, ok := r.instances[name]; !ok {
		return fmt.Errorf("instance %q not found", name)
	}

	delete(r.instances, name)
	return r.save()
}

// List returns all instances.
func (r *Registry) List() map[string]*Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*Instance)
	for k, v := range r.instances {
		result[k] = v
	}
	return result
}

func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &r.instances)
}

func (r *Registry) save() error {
	data, err := json.MarshalIndent(r.instances, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, data, 0644)
}

// ConfigDir returns the configuration directory for an instance.
func ConfigDir(instanceName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude-controller", instanceName), nil
}

// DBPath returns the database path for an instance.
func DBPath(instanceName string) (string, error) {
	dir, err := ConfigDir(instanceName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "claude.db"), nil
}

// EnvPath returns the .env file path for an instance.
func EnvPath(instanceName string) (string, error) {
	dir, err := ConfigDir(instanceName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".env"), nil
}

// SettingsPath returns the settings.json file path for an instance.
func SettingsPath(instanceName string) (string, error) {
	dir, err := ConfigDir(instanceName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}
