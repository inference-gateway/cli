package config

import (
	"reflect"
	"testing"
)

func TestMergeSandboxDirectories(t *testing.T) {
	cfg := &Config{}
	cfg.Tools.Sandbox.Directories = []string{".", "/tmp"}

	cfg.MergeSandboxDirectories([]string{"/tmp", "/data", "", "/data", "/extra"})

	got := cfg.Tools.Sandbox.Directories
	want := []string{".", "/tmp", "/data", "/extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged sandbox dirs = %v, want %v", got, want)
	}
}

func TestMergeSandboxDirectoriesEmptyExtra(t *testing.T) {
	cfg := &Config{}
	cfg.Tools.Sandbox.Directories = []string{"."}
	cfg.MergeSandboxDirectories(nil)
	if !reflect.DeepEqual(cfg.Tools.Sandbox.Directories, []string{"."}) {
		t.Fatalf("unexpected dirs after empty merge: %v", cfg.Tools.Sandbox.Directories)
	}
}

// TestDefaultConfigSandboxHasNoAbsoluteHomePath locks in that DefaultConfig no
// longer bakes an absolute ~/.infer/skills path into the serialized sandbox
// directories - skills reads are handled by the isWithinSkillsDir carve-out at
// runtime, so the directories list stays machine-independent.
func TestDefaultConfigSandboxHasNoAbsoluteHomePath(t *testing.T) {
	dirs := DefaultConfig().Tools.Sandbox.Directories
	want := []string{".", "/tmp", ConfigDirName + "/tmp"}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("default sandbox dirs = %v, want %v", dirs, want)
	}
}
