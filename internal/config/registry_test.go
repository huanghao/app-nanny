// internal/config/registry_test.go
package config_test

import (
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/config"
)

func TestRegistry_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))

	if err := reg.Add("parquet-explorer", "/workspace/parquet-explorer"); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	path, ok := reg.Get("parquet-explorer")
	if !ok {
		t.Fatal("Get returned not-found after Add")
	}
	if path != "/workspace/parquet-explorer" {
		t.Errorf("path = %q, want %q", path, "/workspace/parquet-explorer")
	}
}

func TestRegistry_Remove(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))

	_ = reg.Add("parquet-explorer", "/workspace/parquet-explorer")
	if err := reg.Remove("parquet-explorer"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if _, ok := reg.Get("parquet-explorer"); ok {
		t.Error("Get returned found after Remove")
	}
}

func TestRegistry_RemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))
	if err := reg.Remove("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent project, got nil")
	}
}

func TestRegistry_Persist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	reg1 := config.NewRegistry(path)
	_ = reg1.Add("my-app", "/workspace/my-app")

	// Load again from disk
	reg2 := config.NewRegistry(path)
	if _, ok := reg2.Get("my-app"); !ok {
		t.Error("project not persisted to disk")
	}
}

func TestRegistry_List(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))
	_ = reg.Add("a", "/a")
	_ = reg.Add("b", "/b")

	entries := reg.List()
	if len(entries) != 2 {
		t.Errorf("List len = %d, want 2", len(entries))
	}
}
