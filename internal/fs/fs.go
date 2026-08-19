package fs

import (
	"archive/zip"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourname/fileship/internal/model"
)

var ErrForbidden = errors.New("path outside root")

// Resolve gibt den absoluten, sicheren Pfad zurück oder ErrForbidden
func Resolve(root, rel string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", ErrForbidden
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", ErrForbidden
	}
	abs := filepath.Clean(filepath.Join(rootAbs, rel))
	if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) && abs != rootAbs {
		return "", ErrForbidden
	}
	for current := abs; current != rootAbs; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", ErrForbidden
		}
	}
	return abs, nil
}

func List(root, rel string) ([]model.FileInfo, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	files := make([]model.FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		fi := model.FileInfo{
			Name:    e.Name(),
			Path:    filepath.Join(rel, e.Name()),
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			ModTime: info.ModTime(),
		}
		if !e.IsDir() {
			fi.MimeType = mimeType(e.Name())
		}
		files = append(files, fi)
	}
	return files, nil
}

func Mkdir(root, rel string) error {
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0755)
}

func Delete(root, rel string) error {
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

func Rename(root, oldRel, newRel string) error {
	oldAbs, err := Resolve(root, oldRel)
	if err != nil {
		return err
	}
	newAbs, err := Resolve(root, newRel)
	if err != nil {
		return err
	}
	return os.Rename(oldAbs, newAbs)
}

func SaveUpload(root, rel string, r io.Reader) error {
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	f, err := os.Create(abs)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func ZipDir(root, rel string, w io.Writer) error {
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	defer zw.Close()
	return filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(abs, path)
		fw, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})
}

func ZipMulti(root string, rels []string, w io.Writer) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, rel := range rels {
		abs, err := Resolve(root, rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if info.IsDir() {
			filepath.Walk(abs, func(path string, fi os.FileInfo, err error) error {
				if err != nil || fi.IsDir() {
					return err
				}
				entry, _ := filepath.Rel(root, path)
				fw, err := zw.Create(entry)
				if err != nil {
					return err
				}
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(fw, f)
				return err
			})
		} else {
			entry, _ := filepath.Rel(root, abs)
			fw, err := zw.Create(entry)
			if err != nil {
				continue
			}
			f, err := os.Open(abs)
			if err != nil {
				continue
			}
			io.Copy(fw, f)
			f.Close()
		}
	}
	return nil
}

func Move(root, srcRel, dstRel string) error {
	return Rename(root, srcRel, dstRel)
}

func mimeType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// TypeAllowed prüft ob ein Dateiname einem der erlaubten MIME-Typen entspricht.
// allowedTypes ist kommagetrennt, z.B. "image/,application/pdf"
func TypeAllowed(filename, allowedTypes string) bool {
	if allowedTypes == "" {
		return true
	}
	mt := mimeType(filename)
	for _, allowed := range strings.Split(allowedTypes, ",") {
		allowed = strings.TrimSpace(allowed)
		if strings.HasSuffix(allowed, "/") {
			if strings.HasPrefix(mt, allowed) {
				return true
			}
		} else if mt == allowed {
			return true
		}
	}
	return false
}

func DetectMime(root, rel string) (string, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	detected := http.DetectContentType(buf[:n])
	if detected == "application/octet-stream" {
		return mimeType(rel), nil
	}
	return detected, nil
}
