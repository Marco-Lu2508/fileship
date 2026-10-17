package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourname/fileship/internal/model"
)

func ListPaged(root string, opts model.ListOptions) (model.ListResult, error) {
	return ListPagedFiltered(root, opts, nil)
}

// Filter before sorting and pagination so totals and page boundaries describe
// only the files the caller is allowed to see.
func ListPagedFiltered(root string, opts model.ListOptions, allow func(model.FileInfo) bool) (model.ListResult, error) {
	abs, err := Resolve(root, opts.Path)
	if err != nil {
		return model.ListResult{}, err
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return model.ListResult{}, err
	}

	var files []model.FileInfo
	search := strings.ToLower(opts.Search)

	for _, e := range entries {
		if search != "" && !strings.Contains(strings.ToLower(e.Name()), search) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fi := model.FileInfo{
			Name:    e.Name(),
			Path:    filepath.ToSlash(filepath.Join(opts.Path, e.Name())),
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			ModTime: info.ModTime(),
		}
		if !e.IsDir() {
			fi.MimeType = mimeType(e.Name())
		}
		if allow != nil && !allow(fi) {
			continue
		}
		files = append(files, fi)
	}

	// Sort
	sortFiles(files, opts.SortBy, opts.SortAsc)

	total := len(files)

	// Paginate
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 100
	}
	page := opts.Page
	if page <= 0 {
		page = 1
	}
	// Compare page counts before multiplying untrusted query parameters.
	if total == 0 || page > (total-1)/perPage+1 {
		files = []model.FileInfo{}
	} else {
		start := (page - 1) * perPage
		end := total
		if perPage < total-start {
			end = start + perPage
		}
		files = files[start:end]
	}

	return model.ListResult{Files: files, Total: total, Page: page, PerPage: perPage}, nil
}

func sortFiles(files []model.FileInfo, by string, asc bool) {
	sort.SliceStable(files, func(i, j int) bool {
		return less(files[i], files[j], by, asc)
	})
}

func less(a, b model.FileInfo, by string, asc bool) bool {
	// Ordner immer zuerst
	if a.IsDir != b.IsDir {
		return a.IsDir
	}
	var comparison int
	switch by {
	case "size":
		if a.Size < b.Size {
			comparison = -1
		} else if a.Size > b.Size {
			comparison = 1
		}
	case "mod_time":
		if a.ModTime.Before(b.ModTime) {
			comparison = -1
		} else if a.ModTime.After(b.ModTime) {
			comparison = 1
		}
	default:
		comparison = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
	if !asc {
		return comparison > 0
	}
	return comparison < 0
}

func Search(root, query string) ([]model.FileInfo, error) {
	abs := filepath.Clean(root)
	query = strings.ToLower(query)
	var results []model.FileInfo

	err := filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(info.Name()), query) {
			rel, _ := filepath.Rel(abs, path)
			fi := model.FileInfo{
				Name:    info.Name(),
				Path:    filepath.ToSlash(rel),
				Size:    info.Size(),
				IsDir:   info.IsDir(),
				ModTime: info.ModTime(),
			}
			if !info.IsDir() {
				fi.MimeType = mimeType(info.Name())
			}
			results = append(results, fi)
		}
		return nil
	})
	return results, err
}

func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// User folders named tmp, dev or run are ordinary workspace content.
		// Symlinks and device files do not contribute file contents to usage.
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func Copy(root, srcRel, dstRel string) error {
	if _, err := CleanRelativePath(srcRel); err != nil {
		return err
	}
	if _, err := CleanRelativePath(dstRel); err != nil {
		return err
	}
	srcAbs, err := Resolve(root, srcRel)
	if err != nil {
		return err
	}
	dstAbs, err := Resolve(root, dstRel)
	if err != nil {
		return err
	}
	if srcAbs == dstAbs || strings.HasPrefix(dstAbs, srcAbs+string(os.PathSeparator)) {
		return fmt.Errorf("destination is inside source")
	}
	if _, err := os.Lstat(dstAbs); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	// Preflight the entire tree before creating a destination. Never follow a
	// symlink or a special file when copying a selected folder.
	if err := filepath.Walk(srcAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("cannot copy non-regular file: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}

	info, err := os.Stat(srcAbs)
	if err != nil {
		return err
	}

	if info.IsDir() {
		if err := os.Mkdir(dstAbs, info.Mode().Perm()); err != nil {
			return err
		}
		err = filepath.Walk(srcAbs, func(path string, entry os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == srcAbs {
				return nil
			}
			rel, err := filepath.Rel(srcAbs, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dstAbs, rel)
			if entry.IsDir() {
				return os.Mkdir(target, entry.Mode().Perm())
			}
			return copyExclusive(path, target)
		})
		if err != nil {
			os.RemoveAll(dstAbs)
		}
		return err
	}
	return copyExclusive(srcAbs, dstAbs)
}

func copyExclusive(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot copy non-regular file")
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(dst)
		}
	}()
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	return err
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}
