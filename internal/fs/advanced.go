package fs

import (
	"archive/zip"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

// Unzip entpackt ein ZIP-Archiv in das Zielverzeichnis
func Unzip(root, zipRel, destRel string) error {
	zipAbs, err := Resolve(root, zipRel)
	if err != nil {
		return err
	}
	destAbs, err := Resolve(root, destRel)
	if err != nil {
		return err
	}

	r, err := zip.OpenReader(zipAbs)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Sicherheit: Path Traversal verhindern
		target := filepath.Join(destAbs, filepath.Clean("/"+f.Name))
		if !strings.HasPrefix(target, filepath.Clean(destAbs)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		out, err := os.Create(target)
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// GenerateThumbnail erstellt ein 256x256 JPEG Thumbnail
func GenerateThumbnail(root, rel, thumbDir string) (string, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}

	img, err := imaging.Open(abs, imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}

	thumb := imaging.Thumbnail(img, 256, 256, imaging.Lanczos)

	thumbName := strings.ReplaceAll(rel, string(os.PathSeparator), "_") + ".jpg"
	thumbPath := filepath.Join(thumbDir, thumbName)

	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return "", err
	}

	out, err := os.Create(thumbPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if err := jpeg.Encode(out, thumb, &jpeg.Options{Quality: 80}); err != nil {
		return "", err
	}

	return thumbName, nil
}

// ThumbnailExists prüft ob ein Thumbnail bereits existiert
func ThumbnailExists(thumbDir, rel string) (string, bool) {
	thumbName := strings.ReplaceAll(rel, string(os.PathSeparator), "_") + ".jpg"
	thumbPath := filepath.Join(thumbDir, thumbName)
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbName, true
	}
	return "", false
}

// IsImage prüft ob eine Datei ein Bild ist
func IsImage(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff":
		return true
	}
	return false
}

// ReadTextFile liest eine Textdatei (max 1MB)
func ReadTextFile(root, rel string) (string, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.Size() > 1<<20 {
		return "", fmt.Errorf("file too large for editor (max 1MB)")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteTextFile schreibt Inhalt in eine Textdatei
func WriteTextFile(root, rel, content string) error {
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0644)
}
