package storage

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"unicode"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

func validateName(name string) error {
	invalid := api.JavaBlank(name) || api.JavaLength(name) > 255 || strings.ContainsAny(name, "/\\:")
	for _, r := range name {
		if r <= 31 || r >= 127 && r <= 159 || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
			invalid = true
		}
	}
	if invalid {
		return api.Failure(400, "INVALID_STUDY_FILENAME", "El nombre del archivo no es válido.")
	}
	return nil
}
func unsupported() error {
	return api.Failure(415, "INVALID_STUDY_FILE", "El archivo debe ser un PDF sin cifrar o una imagen válida (JPG, PNG, WebP, GIF, BMP o TIFF).")
}
func imageTooLarge() error {
	return api.Failure(400, "STUDY_IMAGE_TOO_LARGE", "La imagen supera la resolución máxima permitida.")
}
func extension(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}
func inspect(ctx context.Context, data []byte, name string) (string, error) {
	ext := extension(name)
	if ext == "pdf" {
		document, err := pdfapi.ReadContext(ctx, bytes.NewReader(data), model.NewDefaultConfiguration())
		if err != nil || document.Encrypt != nil {
			return "", unsupported()
		}
		if err := document.EnsurePageCount(); err != nil || document.PageCount < 1 {
			return "", unsupported()
		}
		for page := 1; page <= document.PageCount; page++ {
			_, _, attrs, err := document.PageDict(ctx, page, false)
			if err != nil || attrs == nil || attrs.MediaBox == nil || attrs.MediaBox.Width() <= 0 || attrs.MediaBox.Height() <= 0 {
				return "", unsupported()
			}
		}
		return "application/pdf", nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", unsupported()
	}
	allowed := map[string][]string{"jpeg": {"jpg", "jpeg"}, "png": {"png"}, "webp": {"webp"}, "gif": {"gif"}, "bmp": {"bmp"}, "tiff": {"tif", "tiff"}}
	valid := false
	for _, candidate := range allowed[format] {
		if candidate == ext {
			valid = true
		}
	}
	if !valid {
		return "", unsupported()
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return "", imageTooLarge()
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", unsupported()
	}
	return "image/" + format, nil
}
