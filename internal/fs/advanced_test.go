package fs

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourname/fileship/internal/model"
)

type zipTestEntry struct {
	name, content string
	mode          os.FileMode
}

func writeTestZip(t *testing.T, root string, entries ...zipTestEntry) string {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "archive.zip"), data.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return "archive.zip"
}

func TestUnzipPreservesNestedFoldersAndEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	archive := writeTestZip(t, root,
		zipTestEntry{name: "docs/nested/a.txt", content: "contents"},
		zipTestEntry{name: "empty/", mode: os.ModeDir | 0755},
	)
	if err := Unzip(root, archive, "output"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "output/docs/nested/a.txt"))
	if err != nil || string(content) != "contents" {
		t.Fatalf("nested content: %q, %v", content, err)
	}
	if info, err := os.Stat(filepath.Join(root, "output/empty")); err != nil || !info.IsDir() {
		t.Fatal("empty directory missing", err)
	}
}

func TestUnzipRejectsUnsafeArchiveBeforeWritingAnyEntry(t *testing.T) {
	for _, entry := range []zipTestEntry{
		{name: "../outside.txt", content: "escape"},
		{name: "/absolute.txt", content: "escape"},
		{name: `folder\outside.txt`, content: "ambiguous separator"},
		{name: "folder/../../outside.txt", content: "escape"},
		{name: "link", content: "/etc/passwd", mode: os.ModeSymlink | 0777},
		{name: "good.txt", content: "duplicate"},
		{name: "good.txt/child.txt", content: "file as parent"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			root := t.TempDir()
			archive := writeTestZip(t, root, zipTestEntry{name: "good.txt", content: "good"}, entry)
			if err := Unzip(root, archive, "output"); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "output")); !os.IsNotExist(err) {
				t.Fatal("preflight failure created output")
			}
		})
	}
}

func TestUnzipRejectsExistingFilesWithoutTruncatingThem(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "output"), 0755)
	os.WriteFile(filepath.Join(root, "output/existing.txt"), []byte("keep me"), 0644)
	archive := writeTestZip(t, root,
		zipTestEntry{name: "new.txt", content: "new"},
		zipTestEntry{name: "existing.txt", content: "overwrite"},
	)
	if err := Unzip(root, archive, "output"); err == nil {
		t.Fatal("existing destination accepted")
	}
	content, err := os.ReadFile(filepath.Join(root, "output/existing.txt"))
	if err != nil || string(content) != "keep me" {
		t.Fatalf("existing data lost: %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "output/new.txt")); !os.IsNotExist(err) {
		t.Fatal("preflight failure created an earlier entry")
	}
}

func TestUnzipRejectsSymlinkInDestination(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.Mkdir(filepath.Join(root, "output"), 0755)
	if err := os.Symlink(outside, filepath.Join(root, "output/link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	archive := writeTestZip(t, root, zipTestEntry{name: "link/escaped.txt", content: "private"})
	if err := Unzip(root, archive, "output"); err == nil {
		t.Fatal("followed destination symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatal("wrote outside workspace")
	}
}

func TestUnzipRemovesCorruptEntry(t *testing.T) {
	root := t.TempDir()
	archive := writeTestZip(t, root, zipTestEntry{name: "corrupt.txt", content: "unique checksum sentinel"})
	data, err := os.ReadFile(filepath.Join(root, archive))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("unique checksum sentinel"), []byte("broken checksum sentinel"), 1)
	if err := os.WriteFile(filepath.Join(root, archive), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Unzip(root, archive, "output"); err == nil {
		t.Fatal("corrupt ZIP accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "output/corrupt.txt")); !os.IsNotExist(err) {
		t.Fatal("corrupt partial file left behind")
	}
}

func TestUnzipRejectsOversizedArchiveFromMetadata(t *testing.T) {
	root := t.TempDir()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	if _, err := writer.CreateRaw(&zip.FileHeader{Name: "huge.txt", Method: zip.Store, UncompressedSize64: maxUnzipSize + 1}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "archive.zip"), data.Bytes(), 0644)
	if err := Unzip(root, "archive.zip", "output"); err == nil {
		t.Fatal("oversized archive accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "output")); !os.IsNotExist(err) {
		t.Fatal("oversized archive created output")
	}
}

func TestThumbnailCacheSeparatesWorkspacesPathsAndVersions(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	stamp := time.Unix(1700000000, 0)
	for _, root := range []string{rootA, rootB} {
		os.Mkdir(filepath.Join(root, "folder"), 0755)
		for _, name := range []string{"folder/image.png", "folder_image.png"} {
			path := filepath.Join(root, name)
			os.WriteFile(path, []byte("same"), 0644)
			os.Chtimes(path, stamp, stamp)
		}
	}
	keys := make(map[string]bool)
	for _, source := range []struct{ root, path string }{{rootA, "folder/image.png"}, {rootB, "folder/image.png"}, {rootA, "folder_image.png"}} {
		name, err := thumbnailName(source.root, source.path)
		if err != nil || filepath.Base(name) != name || keys[name] {
			t.Fatalf("unsafe or colliding cache name %q: %v", name, err)
		}
		keys[name] = true
	}
	oldName, _ := thumbnailName(rootA, "folder/image.png")
	os.Chtimes(filepath.Join(rootA, "folder/image.png"), stamp.Add(time.Second), stamp.Add(time.Second))
	newName, _ := thumbnailName(rootA, "folder/image.png")
	if oldName == newName {
		t.Fatal("edited source retained old cache key")
	}
	cache := t.TempDir()
	os.WriteFile(filepath.Join(cache, newName), []byte("cached"), 0644)
	if _, exists := ThumbnailExists(rootA, cache, "folder/image.png"); !exists {
		t.Fatal("current thumbnail not found")
	}
	os.Remove(filepath.Join(rootA, "folder/image.png"))
	if _, exists := ThumbnailExists(rootA, cache, "folder/image.png"); exists {
		t.Fatal("deleted source still exposes cached preview")
	}
}

func TestDirSizeCountsAllWorkspaceFolderNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"tmp", "dev", "run", "proc", "sys", "snap"} {
		os.Mkdir(filepath.Join(root, name), 0755)
		os.WriteFile(filepath.Join(root, name, "file"), []byte("data"), 0644)
	}
	size, err := DirSize(root)
	if err != nil || size != 24 {
		t.Fatalf("workspace contents omitted from usage: %d, %v", size, err)
	}
	if _, err := DirSize(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing workspace reported zero usage without an error")
	}
}

func TestTextEditorRejectsOversizedWriteWithoutChangingFile(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("original"), 0644)
	if err := WriteTextFile(root, "notes.txt", strings.Repeat("x", (1<<20)+1)); err == nil {
		t.Fatal("oversized editor write accepted")
	}
	content, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil || string(content) != "original" {
		t.Fatalf("rejected write changed existing data: %q, %v", content, err)
	}
}

func TestListingHandlesPaginationIntegerOverflow(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0644)
	maxInt := int(^uint(0) >> 1)
	for _, opts := range []model.ListOptions{
		{Page: maxInt, PerPage: maxInt},
		{Page: maxInt, PerPage: 2},
		{Page: 1, PerPage: maxInt},
	} {
		result, err := ListPaged(root, opts)
		if err != nil || result.Total != 1 {
			t.Fatalf("pagination failed: %+v, %v", result, err)
		}
		if opts.Page == 1 && len(result.Files) != 1 || opts.Page != 1 && len(result.Files) != 0 {
			t.Fatalf("wrong page contents: %+v", result)
		}
	}
}
