// Package imageutil содержит декодирование, ресайз и кодирование изображений.
package imageutil

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // Регистрация декодера GIF.
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Регистрация декодера WebP.

	"github.com/HugoSmits86/nativewebp"
)

// MIME-типы изображений, с которыми работает сервис.
const (
	MimeJPEG = "image/jpeg"
	MimePNG  = "image/png"
	MimeWebP = "image/webp"
)

// JPEGQuality — качество кодирования JPEG для миниатюр и конвертации.
const JPEGQuality = 90

// ThumbnailMIME выбирает формат миниатюры по формату оригинала: PNG остаётся PNG,
// WebP — WebP, иначе JPEG.
func ThumbnailMIME(sourceMIME string) string {
	switch sourceMIME {
	case MimePNG:
		return MimePNG
	case MimeWebP:
		return MimeWebP
	default:
		return MimeJPEG
	}
}

// DetectMIME определяет MIME-тип по сигнатуре файла.
// http.DetectContentType не знает WebP, поэтому проверяем RIFF-контейнер вручную.
func DetectMIME(head []byte) string {
	if isWebP(head) {
		return MimeWebP
	}

	mime := http.DetectContentType(head)
	if idx := strings.IndexByte(mime, ';'); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}

	return mime
}

func isWebP(head []byte) bool {
	return len(head) >= 12 && bytes.Equal(head[0:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))
}

// Decode декодирует изображение из потока.
func Decode(r io.Reader) (image.Image, string, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}

	return img, format, nil
}

// DecodeConfig читает только размеры изображения, не декодируя пиксели целиком.
func DecodeConfig(r io.Reader) (int, int, error) {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return 0, 0, fmt.Errorf("decode image config: %w", err)
	}

	return cfg.Width, cfg.Height, nil
}

// Thumbnail масштабирует изображение под размер width x height по принципу cover:
// сохраняет пропорции, обрезая лишнее по центру.
func Thumbnail(src image.Image, width, height int) image.Image {
	if width <= 0 || height <= 0 {
		return src
	}

	srcBounds := src.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	if srcW == 0 || srcH == 0 {
		return src
	}

	// Выбираем область исходника с соотношением сторон целевого размера.
	targetRatio := float64(width) / float64(height)
	cropW, cropH := float64(srcW), float64(srcH)
	if cropW/cropH > targetRatio {
		cropW = cropH * targetRatio
	} else {
		cropH = cropW / targetRatio
	}

	offsetX := srcBounds.Min.X + int((float64(srcW)-cropW)/2)
	offsetY := srcBounds.Min.Y + int((float64(srcH)-cropH)/2)
	crop := image.Rect(offsetX, offsetY, offsetX+int(cropW), offsetY+int(cropH))

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)

	return dst
}

// Encode кодирует изображение в указанный MIME-тип.
// WebP пишется в режиме VP8L (без потерь): кодировщика с потерями без cgo нет,
// поэтому webp-файл фотографии заметно тяжелее JPEG.
func Encode(w io.Writer, img image.Image, mime string) error {
	switch mime {
	case MimeJPEG:
		return jpeg.Encode(w, img, &jpeg.Options{Quality: JPEGQuality})
	case MimePNG:
		return png.Encode(w, img)
	case MimeWebP:
		return nativewebp.Encode(w, img, nil)
	default:
		return fmt.Errorf("encode: unsupported mime type %q", mime)
	}
}

// CanEncode сообщает, умеет ли сервис кодировать в указанный MIME-тип.
func CanEncode(mime string) bool {
	return mime == MimeJPEG || mime == MimePNG || mime == MimeWebP
}

// NormalizeFormat приводит query-параметр format к MIME-типу.
func NormalizeFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
		return "", true
	case "jpeg", "jpg":
		return MimeJPEG, true
	case "png":
		return MimePNG, true
	case "webp":
		return MimeWebP, true
	default:
		return "", false
	}
}

// ExtByMIME возвращает расширение файла для MIME-типа.
func ExtByMIME(mime string) string {
	switch mime {
	case MimeJPEG:
		return ".jpg"
	case MimePNG:
		return ".png"
	case MimeWebP:
		return ".webp"
	default:
		return ".bin"
	}
}

// SanitizeFileName убирает путь и управляющие символы из имени файла клиента.
func SanitizeFileName(name string) string {
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}

		return r
	}, name)

	if name == "" || name == "." || name == "/" {
		return "upload"
	}

	const maxNameLen = 255
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}

	return name
}
