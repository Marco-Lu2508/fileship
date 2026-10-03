package fs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyRejectsCollisionsAndDescendantDestinations(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "docs"), 0755)
	os.WriteFile(filepath.Join(root, "docs", "a.txt"), []byte("original"), 0644)
	for _, dst := range []string{"docs", "docs/child", "."} {
		if err := Copy(root, "docs", dst); err == nil {
			t.Fatalf("accepted %s", dst)
		}
	}
	os.WriteFile(filepath.Join(root, "existing.txt"), []byte("keep"), 0644)
	if err := Copy(root, "docs/a.txt", "existing.txt"); err == nil {
		t.Fatal("overwrote collision")
	}
	content, _ := os.ReadFile(filepath.Join(root, "existing.txt"))
	if string(content) != "keep" {
		t.Fatal("destination was changed")
	}
	if err := Copy(root, "docs", "duplicate"); err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(filepath.Join(root, "duplicate", "a.txt"))
	if string(content) != "original" {
		t.Fatal("incorrect copy")
	}
}
func TestCopyRejectsNestedSymlinksBeforeCreatingDestination(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "docs"), 0755)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "docs", "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := Copy(root, "docs", "copy"); err == nil {
		t.Fatal("followed symlink")
	}
	if _, err := os.Stat(filepath.Join(root, "copy")); !os.IsNotExist(err) {
		t.Fatal("partial copy left behind")
	}
}
func TestArchiveFormatsRoundTripAndDeduplicateSelections(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "docs"), 0755)
	os.WriteFile(filepath.Join(root, "docs", "a.txt"), []byte("contents"), 0644)
	entries, err := PrepareArchive(root, []string{"docs", "docs/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("duplicates: %d", len(entries))
	}
	for _, format := range []string{"zip", "tar", "targz"} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			if err := WriteArchive(entries, format, &data); err != nil {
				t.Fatal(err)
			}
			found := false
			if format == "zip" {
				reader, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range reader.File {
					if file.Name == "docs/a.txt" {
						r, _ := file.Open()
						content, _ := io.ReadAll(r)
						r.Close()
						found = string(content) == "contents"
					}
				}
			} else {
				var input io.Reader = bytes.NewReader(data.Bytes())
				if format == "targz" {
					reader, err := gzip.NewReader(input)
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					input = reader
				}
				reader := tar.NewReader(input)
				for {
					header, err := reader.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if header.Name == "docs/a.txt" {
						content, _ := io.ReadAll(reader)
						found = string(content) == "contents"
					}
				}
			}
			if !found {
				t.Fatal("file missing or corrupted")
			}
		})
	}
}
