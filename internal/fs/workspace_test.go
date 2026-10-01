package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourname/fileship/internal/model"
)

type interruptedReader struct{ first bool }

func (r *interruptedReader) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		return copy(p, "partial"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestInterruptedUploadPreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpload(root, "report.txt", &interruptedReader{}); err == nil {
		t.Fatal("expected upload failure")
	}
	content, err := os.ReadFile(filepath.Join(root, "report.txt"))
	if err != nil || string(content) != "original" {
		t.Fatalf("original lost: %q, %v", content, err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("temporary uploads left behind: %v", entries)
	}
	if err := SaveUpload(root, "new/deep/file.txt", strings.NewReader("complete")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRootCannotBeDeletedOrRenamed(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"", ".", "folder/..", "../outside"} {
		if !errors.Is(Delete(root, path), ErrForbidden) {
			t.Errorf("delete %q must be rejected", path)
		}
		if !errors.Is(Rename(root, path, "moved"), ErrForbidden) {
			t.Errorf("rename %q must be rejected", path)
		}
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("root removed", err)
	}
}

func TestRenamePreservesDestination(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("A"), 0644)
	os.WriteFile(filepath.Join(root, "b"), []byte("B"), 0644)
	if !errors.Is(Rename(root, "a", "b"), os.ErrExist) {
		t.Fatal("rename must reject existing destination")
	}
	content, _ := os.ReadFile(filepath.Join(root, "b"))
	if string(content) != "B" {
		t.Fatal("destination overwritten")
	}
}

func TestDescendingSortIsStrictForEqualValues(t *testing.T) {
	a := model.FileInfo{Name: "same", Size: 42}
	for _, key := range []string{"name", "size", "mod_time"} {
		if less(a, a, key, false) {
			t.Errorf("equal %s values compare less in descending order", key)
		}
	}
	if !less(model.FileInfo{IsDir: true}, a, "size", false) {
		t.Fatal("folders must stay first")
	}
}

func TestTransferDoesNotOverwriteExistingTarget(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	os.WriteFile(src, []byte("source"), 0644)
	os.WriteFile(dst, []byte("destination"), 0644)
	if !errors.Is(Transfer(src, dst), os.ErrExist) {
		t.Fatal("target collision must be rejected")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source removed")
	}
}

func TestTransferAcrossVolumes(t *testing.T) {
	// Linux CI and local WSL use tmpfs to exercise a real EXDEV, not a mock.
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("tmpfs unavailable")
	}
	root := t.TempDir()
	dstRoot, err := os.MkdirTemp("/dev/shm", "fileship-test-")
	if err != nil {
		t.Skip(err)
	}
	defer os.RemoveAll(dstRoot)
	src := filepath.Join(root, "folder")
	dst := filepath.Join(dstRoot, "folder")
	os.MkdirAll(filepath.Join(src, "nested"), 0755)
	os.WriteFile(filepath.Join(src, "nested", "a.txt"), []byte("recover me"), 0644)
	if err := Transfer(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
	content, err := os.ReadFile(filepath.Join(dst, "nested", "a.txt"))
	if err != nil || string(content) != "recover me" {
		t.Fatal("transfer lost data", err)
	}
	if err := Transfer(dst, src); err != nil {
		t.Fatal("restore across volumes:", err)
	}
	// A symlink must fail without copying data from outside the tree.
	os.Symlink("/etc/passwd", filepath.Join(src, "link"))
	if err := Transfer(src, dst); err == nil {
		t.Fatal("expected symlink rejection")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("failed transfer removed source")
	}
}
