package fs

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxUnzipSize = 2 << 30 // 2 GiB across the entire archive.

type UnzipEntry struct {
	Path  string
	IsDir bool
	Size  int64
	file  *zip.File
}

// UnzipPlan keeps the inspected archive open so callers can enforce permissions,
// file types and quota for every entry before any file is created.
type UnzipPlan struct {
	Entries []UnzipEntry
	root    string
	reader  *zip.ReadCloser
}

func (p *UnzipPlan) Close() error { return p.reader.Close() }

func PrepareUnzip(root, zipRel, destRel string) (plan *UnzipPlan, err error) {
	zipAbs, err := Resolve(root, zipRel)
	if err != nil {
		return nil, err
	}
	if _, err := Resolve(root, destRel); err != nil {
		return nil, err
	}
	reader, err := zip.OpenReader(zipAbs)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			reader.Close()
		}
	}()
	if len(reader.File) > 100000 {
		return nil, fmt.Errorf("too many archive entries")
	}
	plan = &UnzipPlan{root: root, reader: reader}
	seen := make(map[string]bool)
	var total int64
	for _, file := range reader.File {
		name := strings.TrimSuffix(file.Name, "/")
		if strings.Contains(name, "\\") || !filepath.IsLocal(name) {
			return nil, fmt.Errorf("illegal path in zip: %s", file.Name)
		}
		clean, cleanErr := CleanRelativePath(name)
		if cleanErr != nil || clean != name {
			return nil, fmt.Errorf("illegal path in zip: %s", file.Name)
		}
		mode := file.Mode()
		if !mode.IsDir() && !mode.IsRegular() {
			return nil, fmt.Errorf("cannot extract non-regular file: %s", file.Name)
		}
		rel := filepath.ToSlash(filepath.Join(destRel, clean))
		key := rel
		if os.PathSeparator == '\\' {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate path in zip: %s", file.Name)
		}
		seen[key] = mode.IsDir()
		abs, resolveErr := Resolve(root, rel)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if info, statErr := os.Lstat(abs); statErr == nil {
			if !mode.IsDir() || !info.IsDir() {
				return nil, fmt.Errorf("destination exists: %s", rel)
			}
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		if file.UncompressedSize64 > uint64(maxUnzipSize-total) {
			return nil, fmt.Errorf("zip extraction limit exceeded (max 2GB)")
		}
		size := int64(file.UncompressedSize64)
		total += size
		plan.Entries = append(plan.Entries, UnzipEntry{rel, mode.IsDir(), size, file})
	}
	// An archive cannot use a file as a parent directory. Check the whole
	// selection before extraction, independent of ZIP entry ordering.
	for _, entry := range plan.Entries {
		for parent := filepath.Dir(entry.Path); parent != "."; parent = filepath.Dir(parent) {
			key := filepath.ToSlash(parent)
			if os.PathSeparator == '\\' {
				key = strings.ToLower(key)
			}
			if isDir, exists := seen[key]; exists && !isDir {
				return nil, fmt.Errorf("file used as directory in zip: %s", parent)
			}
			abs, resolveErr := Resolve(root, parent)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
				return nil, fmt.Errorf("destination is not a directory: %s", parent)
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return nil, statErr
			}
		}
	}
	return plan, nil
}

func (p *UnzipPlan) Extract() error {
	for _, entry := range p.Entries {
		abs, err := Resolve(p.root, entry.Path)
		if err != nil {
			return err
		}
		if entry.IsDir {
			if err := os.MkdirAll(abs, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return err
		}
		if err := extractZipFile(entry, abs); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(entry UnzipEntry, target string) (err error) {
	source, err := entry.file.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(target)
		}
	}()
	// Reading to EOF verifies the ZIP checksum. The extra byte detects size
	// overruns instead of accepting silently truncated output.
	written, err := io.Copy(output, io.LimitReader(source, entry.Size+1))
	if err == nil && written != entry.Size {
		err = fmt.Errorf("invalid uncompressed size: %s", entry.Path)
	}
	return err
}

func Unzip(root, zipRel, destRel string) error {
	plan, err := PrepareUnzip(root, zipRel, destRel)
	if err != nil {
		return err
	}
	defer plan.Close()
	return plan.Extract()
}
