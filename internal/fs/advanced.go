package fs

import (
	"archive/zip"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

const maxUnzipSize = 2 << 30 // 2GB Limit

// ffmpegAvailable prüft einmalig ob ffmpeg vorhanden ist
var ffmpegPath, _ = exec.LookPath("ffmpeg")

// IsVideo prüft ob eine Datei ein Video ist
func IsVideo(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".m4v", ".webm", ".mov", ".mkv", ".avi", ".wmv", ".flv", ".ogv":
		return true
	}
	return false
}

// GenerateVideoThumbnail erzeugt ein Thumbnail aus einem Video via ffmpeg
// Gibt einen Fehler zurück wenn ffmpeg nicht vorhanden ist
func GenerateVideoThumbnail(root, rel, thumbDir string) (string, error) {
	if ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg not available")
	}
	abs, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}

	thumbName := strings.ReplaceAll(rel, string(os.PathSeparator), "_") + ".jpg"
	thumbPath := filepath.Join(thumbDir, thumbName)

	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return "", err
	}

	// ffmpeg: Frame bei 5s extrahieren, auf 256x256 skalieren, 1 Frame
	cmd := exec.Command(ffmpegPath,
		"-ss", "00:00:05",
		"-i", abs,
		"-vframes", "1",
		"-vf", "thumbnail,scale=256:256:force_original_aspect_ratio=increase,crop=256:256",
		"-q:v", "3",
		"-y", thumbPath,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// Fallback: Frame bei 0s
		cmd2 := exec.Command(ffmpegPath,
			"-i", abs,
			"-vframes", "1",
			"-vf", "scale=256:256:force_original_aspect_ratio=increase,crop=256:256",
			"-q:v", "3",
			"-y", thumbPath,
		)
		if err2 := cmd2.Run(); err2 != nil {
			return "", fmt.Errorf("ffmpeg failed: %w", err2)
		}
	}
	return thumbName, nil
}

// FFmpegAvailable gibt zurück ob ffmpeg gefunden wurde
func FFmpegAvailable() bool { return ffmpegPath != "" }

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

	var totalSize int64
	for _, f := range r.File {
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

		// ZIP Bomb Schutz: maximal maxUnzipSize pro Datei und insgesamt
		n, err := io.Copy(out, io.LimitReader(rc, maxUnzipSize-totalSize))
		totalSize += n
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		if totalSize >= maxUnzipSize {
			return fmt.Errorf("zip extraction limit exceeded (max 2GB)")
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
