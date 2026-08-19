package fs

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourname/fileship/internal/model"
)

func ListPaged(root string, opts model.ListOptions) (model.ListResult, error) {
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
			Path:    filepath.Join(opts.Path, e.Name()),
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			ModTime: info.ModTime(),
		}
		if !e.IsDir() {
			fi.MimeType = mimeType(e.Name())
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
	start := (page - 1) * perPage
	if start >= total {
		files = []model.FileInfo{}
	} else {
		end := start + perPage
		if end > total {
			end = total
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
	var result bool
	switch by {
	case "size":
		result = a.Size < b.Size
	case "mod_time":
		result = a.ModTime.Before(b.ModTime)
	default:
		result = strings.ToLower(a.Name) < strings.ToLower(b.Name)
	}
	if !asc {
		return !result
	}
	return result
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
				Path:    rel,
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
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func Copy(root, srcRel, dstRel string) error {
	srcAbs, err := Resolve(root, srcRel)
	if err != nil {
		return err
	}
	dstAbs, err := Resolve(root, dstRel)
	if err != nil {
		return err
	}

	info, err := os.Stat(srcAbs)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return copyDir(srcAbs, dstAbs)
	}
	return copyFile(srcAbs, dstAbs)
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
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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
