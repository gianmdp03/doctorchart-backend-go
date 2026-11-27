package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"golang.org/x/image/webp"
)

// The study endpoint owns this selection; Angular never selects a processing profile.
const (
	studyProfile          = "doctorchart-study"
	logoProfile           = "avatar"
	maxConvertedLogoBytes = 200 * 1024
)

type ConvertedLogo struct {
	WebP   []byte
	PNG    []byte
	Width  int
	Height int
	SHA256 string
}

// Bound converter responses independently from the original upload limit.
const maxConverterResponseBytes = 25 << 20

type ConverterConfig struct {
	URL, APIKey              string
	AllowStudyTransformation bool
}

type studyConverter struct {
	endpoint, key       string
	allowTransformation bool
	client              *http.Client
}

// ValidateConverterConfig checks deployment settings without contacting any service.
func ValidateConverterConfig(c ConverterConfig) error {
	if c.URL == "" && c.APIKey == "" && !c.AllowStudyTransformation {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(c.URL, "#") || strings.TrimSpace(c.URL) != c.URL {
		return errors.New("Configuración inválida: IMAGE_CONVERTER_URL debe ser una URL HTTP(S) sin credenciales, parámetros ni fragmentos.")
	}
	if len(c.APIKey) < 32 || len(c.APIKey) > 4096 {
		return converterKeyError()
	}
	for i := range len(c.APIKey) {
		if c.APIKey[i] < 33 || c.APIKey[i] > 126 {
			return converterKeyError()
		}
	}
	return nil
}

func converterKeyError() error {
	return errors.New("Configuración inválida: IMAGE_CONVERTER_API_KEY requiere entre 32 y 4096 caracteres ASCII imprimibles sin espacios; configurala en .env o el entorno.")
}

func newStudyConverter(c ConverterConfig) (*studyConverter, error) {
	if err := ValidateConverterConfig(c); err != nil {
		return nil, err
	}
	if c.URL == "" {
		return nil, nil
	}
	return &studyConverter{
		endpoint: strings.TrimRight(c.URL, "/") + "/v1/convert", key: c.APIKey, allowTransformation: c.AllowStudyTransformation,
		client: &http.Client{Timeout: 160 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func converterUnavailable() error {
	return api.Failure(503, "IMAGE_CONVERTER_UNAVAILABLE", "El servicio de imágenes no está disponible. Reintentá más tarde.")
}
func converterInvalidResponse() error {
	return api.Failure(502, "IMAGE_CONVERTER_INVALID_RESPONSE", "El servicio de imágenes devolvió una respuesta inválida.")
}

// convertStudy never resizes or encodes pixels. Unsupported attachments retain their
// established storage path. Compatible rasters always go through the shared API.
func (s *Store) convertStudy(ctx context.Context, data []byte, name, contentType string) ([]byte, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", "", err
	}
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return data, name, contentType, nil
	}
	if s.converter == nil {
		return nil, "", "", converterUnavailable()
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Do not forward the medical filename or any user/Gateway credentials.
	part, err := writer.CreateFormFile("file", "original")
	if err != nil {
		return nil, "", "", converterUnavailable()
	}
	if _, err = part.Write(data); err != nil {
		return nil, "", "", converterUnavailable()
	}
	if err = writer.WriteField("profile", studyProfile); err != nil {
		return nil, "", "", converterUnavailable()
	}
	if err = writer.Close(); err != nil {
		return nil, "", "", converterUnavailable()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.converter.endpoint, &body)
	if err != nil {
		return nil, "", "", converterUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+s.converter.key)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := s.converter.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", "", ctx.Err()
		}
		return nil, "", "", converterUnavailable()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var result struct{ Error struct{ Code string } }
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result)
		// Explicit preservation policy for animation/high precision/ICC; never
		// fall back on credential, authorization, malformed-file or limit errors.
		if decodeErr == nil && response.StatusCode == 415 && (result.Error.Code == "special_image" || result.Error.Code == "animated_image") {
			return data, name, contentType, nil
		}
		switch response.StatusCode {
		case 400, 415:
			return nil, "", "", api.Failure(400, "STUDY_CONVERSION_REJECTED", "La imagen no admite el procesamiento configurado para estudios.")
		case 413:
			return nil, "", "", api.Failure(413, "STUDY_CONVERTER_LIMIT", "La imagen supera los límites de seguridad del servicio de imágenes.")
		default:
			return nil, "", "", converterUnavailable()
		}
	}
	output, err := io.ReadAll(io.LimitReader(response.Body, maxConverterResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", "", ctx.Err()
		}
		return nil, "", "", converterInvalidResponse()
	}
	if len(output) == 0 || len(output) > maxConverterResponseBytes {
		return nil, "", "", converterInvalidResponse()
	}
	mimeType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	exts := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}
	ext, ok := exts[mimeType]
	if err != nil || !ok || response.Header.Get("X-Image-Profile") != studyProfile {
		return nil, "", "", converterInvalidResponse()
	}
	w, errW := strconv.Atoi(response.Header.Get("X-Image-Width"))
	h, errH := strconv.Atoi(response.Header.Get("X-Image-Height"))
	preserved, errPreserved := strconv.ParseBool(response.Header.Get("X-Image-Preserved"))
	if errW != nil || errH != nil || errPreserved != nil || w < 1 || h < 1 {
		return nil, "", "", converterInvalidResponse()
	}
	if !s.converter.allowTransformation && !preserved {
		return nil, "", "", api.Failure(502, "STUDY_TRANSFORMATION_NOT_ALLOWED", "La configuración del servicio de imágenes no coincide con la política de conservación de estudios.")
	}
	outputName := strings.TrimSuffix(name, filepath.Ext(name)) + ext
	// Reuse existing file validation; check bytes, not just response headers.
	release, err := s.acquireValidation(ctx)
	if err != nil {
		return nil, "", "", err
	}
	detected, err := inspect(ctx, output, outputName)
	release()
	if err != nil || detected != mimeType {
		return nil, "", "", converterInvalidResponse()
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(output))
	if err != nil || config.Width != w || config.Height != h {
		return nil, "", "", converterInvalidResponse()
	}
	if err := ctx.Err(); err != nil {
		return nil, "", "", err
	}
	if preserved {
		if !bytes.Equal(data, output) || mimeType != contentType {
			return nil, "", "", converterInvalidResponse()
		}
		return data, name, contentType, nil
	}
	// Preserve the existing storage-saving policy without duplicating codec rules.
	if len(output) >= len(data) {
		return data, name, contentType, nil
	}
	return output, outputName, mimeType, nil
}

// ConvertClinicLogo sends the uploaded clinic logo through the shared image converter
// using the authorized "avatar" profile (512x512 WebP, metadata stripped) and produces
// a sanitized PNG copy for native PDF embedding.
func (s *Store) ConvertClinicLogo(ctx context.Context, data []byte) (ConvertedLogo, error) {
	var result ConvertedLogo
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(data) == 0 {
		return result, api.Failure(400, "INVALID_LOGO_IMAGE", "Seleccioná una imagen para el logo del consultorio.")
	}
	if len(data) > MaxBytes {
		return result, api.Failure(413, "LOGO_FILE_TOO_LARGE", "El logo supera el tamaño máximo permitido de 10 MB.")
	}
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return result, api.Failure(415, "UNSUPPORTED_LOGO_TYPE", "El logo debe estar en formato JPG, PNG o WebP.")
	}
	if s.converter == nil {
		return result, converterUnavailable()
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "logo")
	if err != nil {
		return result, converterUnavailable()
	}
	if _, err = part.Write(data); err != nil {
		return result, converterUnavailable()
	}
	for k, v := range map[string]string{
		"profile": logoProfile,
		"width":   "512",
		"height":  "512",
	} {
		if err = writer.WriteField(k, v); err != nil {
			return result, converterUnavailable()
		}
	}
	if err = writer.Close(); err != nil {
		return result, converterUnavailable()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.converter.endpoint, &body)
	if err != nil {
		return result, converterUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+s.converter.key)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := s.converter.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, converterUnavailable()
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case 400, 415, 422:
			return result, api.Failure(400, "INVALID_LOGO_IMAGE", "La imagen del logo no es válida o está dañada.")
		case 413:
			return result, api.Failure(413, "LOGO_FILE_TOO_LARGE", "El logo supera los límites permitidos por el optimizador de imágenes.")
		default:
			return result, converterUnavailable()
		}
	}

	webpBytes, err := io.ReadAll(io.LimitReader(response.Body, maxConvertedLogoBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, converterInvalidResponse()
	}
	if len(webpBytes) == 0 || len(webpBytes) > maxConvertedLogoBytes {
		return result, converterInvalidResponse()
	}
	mimeType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mimeType != "image/webp" {
		return result, converterInvalidResponse()
	}

	img, err := webp.Decode(bytes.NewReader(webpBytes))
	if err != nil {
		return result, converterInvalidResponse()
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 16 || w > 512 || h < 16 || h > 512 {
		return result, converterInvalidResponse()
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return result, converterInvalidResponse()
	}
	if pngBuf.Len() == 0 || pngBuf.Len() > 1048576 {
		return result, converterInvalidResponse()
	}
	sum := sha256.Sum256(webpBytes)
	return ConvertedLogo{
		WebP:   webpBytes,
		PNG:    pngBuf.Bytes(),
		Width:  w,
		Height: h,
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}
