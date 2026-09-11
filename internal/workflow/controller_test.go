package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitStageRestoresPreviousFilesOnFailure(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(filepath.Join(stage, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(stage, "a.txt"), "new")
	write(filepath.Join(stage, "nested", "b.txt"), "new nested")
	write(filepath.Join(output, "a.txt"), "old")
	// This regular file prevents creation of output/nested/b.txt after a.txt
	// has already been replaced, forcing the rollback path.
	write(filepath.Join(output, "nested"), "directory collision")

	if err := commitStage(stage, output); err == nil {
		t.Fatal("expected staged commit to fail")
	}
	data, err := os.ReadFile(filepath.Join(output, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("a.txt was not restored: %q", data)
	}
}
