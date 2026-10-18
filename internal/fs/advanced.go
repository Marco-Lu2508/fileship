package fs

import (
	"context"
	"crypto/sha256"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

// ffmpegAvailable prüft einmalig ob ffmpeg vorhanden ist
var ffmpegPath, _ = exec.LookPath("ffmpeg")

// IsVideo prüft ob eine Datei ein Video ist
func IsVideo(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".m4v", ".webm", ".mov", ".mkv", ".avi", ".wmv", ".flv", ".ogv", ".3gp", ".mts", ".m2ts":
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

	thumbName, err := thumbnailName(root, rel)
	if err != nil {
		return "", err
	}
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

// StreamVideoMP4 transcodes a video on the fly into a fragmented H.264/AAC MP4
// stream so browsers can play MKV, AVI, WMV, FLV, MTS, 3GP, and legacy codecs.
func StreamVideoMP4(ctx context.Context, root, rel string, w io.Writer) error {
	if ffmpegPath == "" {
		return fmt.Errorf("ffmpeg not available")
	}
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-i", abs,
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	)
	cmd.Stdout = w
	cmd.Stderr = nil
	return cmd.Run()
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

	thumbName, err := thumbnailName(root, rel)
	if err != nil {
		return "", err
	}
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
func ThumbnailExists(root, thumbDir, rel string) (string, bool) {
	thumbName, err := thumbnailName(root, rel)
	if err != nil {
		return "", false
	}
	thumbPath := filepath.Join(thumbDir, thumbName)
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbName, true
	}
	return "", false
}

// Include the resolved source and its version: different workspaces, nested
// paths and edited files must never share a cached private preview.
func thumbnailName(root, rel string) (string, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("thumbnail source is not a regular file")
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", abs, info.Size(), info.ModTime().UnixNano())
	return fmt.Sprintf("%x.jpg", sha256.Sum256([]byte(key))), nil
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
	if len(content) > 1<<20 {
		return fmt.Errorf("file too large for editor (max 1MB)")
	}
	return SaveUpload(root, rel, strings.NewReader(content))
}
