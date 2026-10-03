package fs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ArchiveEntry struct {
	Path, Name string
	Info       os.FileInfo
}

func PrepareArchive(root string, paths []string) ([]ArchiveEntry, error) {
	rootAbs, err := Resolve(root, "")
	if err != nil {
		return nil, err
	}
	entries := []ArchiveEntry{}
	seen := map[string]bool{}
	for _, path := range paths {
		clean, err := CleanRelativePath(path)
		if err != nil {
			return nil, err
		}
		abs, err := Resolve(root, clean)
		if err != nil {
			return nil, err
		}
		err = filepath.Walk(abs, func(current string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("cannot archive non-regular file")
			}
			name, err := filepath.Rel(rootAbs, current)
			if err != nil {
				return err
			}
			name = filepath.ToSlash(name)
			if !seen[name] {
				if len(entries) >= 100000 {
					return fmt.Errorf("too many archive entries")
				}
				entries = append(entries, ArchiveEntry{current, name, info})
				seen[name] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func WriteArchive(entries []ArchiveEntry, format string, output io.Writer) error {
	if format == "zip" {
		writer := zip.NewWriter(output)
		for _, entry := range entries {
			header, err := zip.FileInfoHeader(entry.Info)
			if err != nil {
				writer.Close()
				return err
			}
			header.Name = entry.Name
			if entry.Info.IsDir() {
				header.Name = strings.TrimSuffix(header.Name, "/") + "/"
			} else {
				header.Method = zip.Deflate
			}
			target, err := writer.CreateHeader(header)
			if err != nil {
				writer.Close()
				return err
			}
			if !entry.Info.IsDir() {
				if err = copyArchiveEntry(entry.Path, target); err != nil {
					writer.Close()
					return err
				}
			}
		}
		return writer.Close()
	}
	if format != "tar" && format != "targz" {
		return fmt.Errorf("unsupported archive format")
	}
	var compressed *gzip.Writer
	if format == "targz" {
		compressed = gzip.NewWriter(output)
		output = compressed
	}
	writer := tar.NewWriter(output)
	for _, entry := range entries {
		header, err := tar.FileInfoHeader(entry.Info, "")
		if err != nil {
			writer.Close()
			if compressed != nil {
				compressed.Close()
			}
			return err
		}
		header.Name = entry.Name
		if err = writer.WriteHeader(header); err == nil && !entry.Info.IsDir() {
			err = copyArchiveEntry(entry.Path, writer)
		}
		if err != nil {
			writer.Close()
			if compressed != nil {
				compressed.Close()
			}
			return err
		}
	}
	err := writer.Close()
	if compressed != nil {
		closeErr := compressed.Close()
		if err == nil {
			err = closeErr
		}
	}
	return err
}
func copyArchiveEntry(path string, output io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(output, file)
	return err
}
